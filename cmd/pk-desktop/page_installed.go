package main

// 已装工具页：一张表 + 可升级提示 + 行内动作（升级 / 设为默认 / 复制路径 / 卸载）。
//
// 只有"卸载"会毁东西，所以只有它走确认框；其余动作直接执行并把结果弹成 toast。

import (
	"context"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

func (g *gui) pageInstalled(c *ui.Context) {
	g.head(c, "已装工具", "Installed tools", "tools", func() { g.reloadTools() })

	if !g.status.OK {
		ui.Text(c, g.status.Reason).TextColor(c.Theme().Warning)
		ui.Text(c, g.t("装好 CLI 后点上面的「刷新」。", "Install the CLI, then press Refresh.")).TextColor(c.Theme().TextMuted)
		return
	}

	// 可升级提示（1.3 的 outdated[]：只读索引缓存，不联网）
	if len(g.tools.Outdated) > 0 {
		ui.Column(c).Fill().Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
			ui.Textf(c, g.t("有 %d 项可升级", "%d updates available"), len(g.tools.Outdated)).Bold()
			for _, o := range g.tools.Outdated {
				o := o
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.Textf(c, "%s %s → %s", o.Name, o.Installed, o.Latest)
					ui.Button(c, g.t("升级", "Upgrade")).OnClick(func() {
						g.enqueue(o.Name + "@" + o.Latest)
						g.page = "queue"
						g.runQueue()
					})
				})
			}
		})
	}

	tools := g.tools.Tools
	if len(tools) == 0 {
		ui.Text(c, g.t("还没有装任何工具。", "No tools installed yet.")).TextColor(c.Theme().TextMuted)
		g.installHint(c)
		return
	}
	ui.Textf(c, g.t("%d 条记录", "%d records"), len(tools)).TextColor(c.Theme().TextMuted)

	list := g.table("installed")
	list.Key = func(i int) any { return tools[i].Name + "@" + tools[i].Version }
	ui.List(c, list, len(tools), func(i int) {
		t := tools[i]
		ui.Row(c).FillWidth().Gap(8).Padding(6).Children(func() {
			ui.Text(c, t.Name).Bold().Width(90)
			ui.Text(c, t.Version).Width(130)
			if t.Active {
				ui.Badge(c, g.t("默认", "active"))
			}
			if t.Verified != "" && t.Verified != "ok" {
				ui.Badge(c, t.Verified) // weak / 未校验
			}
			if t.NPM != nil {
				ui.Badge(c, "npm "+t.NPM.Version)
			}
			ui.Text(c, t.Path).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			if t.SizeBytes > 0 {
				ui.Text(c, humanBytes(t.SizeBytes)).TextColor(c.Theme().TextMuted).Width(80)
			}
			if !t.Active {
				ui.Button(c, g.t("设为默认", "Use")).OnClick(func() { g.useVersion(t.Name, t.Version) })
			}
			if ui.Button(c, g.t("复制路径", "Copy path")).Clicked() {
				mygo.Clipboard.WriteText(t.Path)
				g.flashNow(g.t("已复制", "Copied"))
			}
			ui.Button(c, g.t("卸载", "Uninstall")).OnClick(func() { g.askUninstall(t.Name) }) // 改机器的动作放到确认之后
		})
	}).Height(320)
}

// installHint 在空列表时给一条能立刻照做的命令。
func (g *gui) installHint(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("装一个试试：", "Try: ")).TextColor(c.Theme().TextMuted)
		ui.Text(c, "pk install node@lts").Font("monospace").SingleLine()
		if ui.Button(c, g.t("复制", "Copy")).Clicked() {
			mygo.Clipboard.WriteText("pk install node@lts")
			g.flashNow(g.t("已复制", "Copied"))
		}
	})
}

func (g *gui) reloadTools() {
	g.load("tools", func() (commit, error) {
		tools, err := g.svc.Tools(context.Background())
		if err != nil {
			return nil, err
		}
		return func() { g.tools = tools }, nil
	})
}

func (g *gui) useVersion(tool, version string) {
	g.load("use", func() (commit, error) {
		if err := g.svc.Use(context.Background(), tool, version); err != nil {
			return nil, err
		}
		tools, err := g.svc.Tools(context.Background())
		if err != nil {
			return nil, err
		}
		return func() { g.tools = tools }, nil
	})
}

// askUninstall 是先确认再执行的那条路：卸载会删掉该工具的**全部**版本与入口。
func (g *gui) askUninstall(tool string) {
	g.ask(g.t("卸载 "+tool, "Uninstall "+tool),
		g.t("会删掉 "+tool+" 的全部已装版本与 pk 建的入口，不可撤销。默认版本关系也会重建。",
			"Removes every installed version of "+tool+" and the shims pk made. This cannot be undone."),
		func() {
			g.load("uninstall", func() (commit, error) {
				if err := g.svc.Uninstall(context.Background(), tool); err != nil {
					return nil, err
				}
				tools, err := g.svc.Tools(context.Background())
				if err != nil {
					return nil, err
				}
				msg := tool + g.t(" 已卸载", " uninstalled")
				return func() { g.tools = tools; g.flashNow(msg) }, nil
			})
		})
}

// toolNames 给"装点什么"的下拉用（可装工具 + 已装工具，避免用户手打错）。
func (g *gui) toolNames() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range g.available {
		if !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t.Name)
		}
	}
	for _, t := range g.tools.Tools {
		if !seen[t.Name] {
			seen[t.Name] = true
			out = append(out, t.Name)
		}
	}
	return out
}

// shortTool 把 pkcli.Tool 压成一行文字（日志/提示里用）。
func shortTool(t pkcli.Tool) string { return fmt.Sprintf("%s@%s", t.Name, t.Version) }
