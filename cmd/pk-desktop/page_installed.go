package main

// 已装工具页：一张对齐的清单（列宽固定，路径吃掉剩余宽度）+ 可升级提示 + 行内动作。
//
// 交互纪律不变：只有"卸载"会毁东西，走确认框；其余动作直接执行并把结果弹成 toast。

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// moreSVG 是行尾的「更多」图标：三个点自己画，比字符 "⋯" 的对齐可控
// （字符的垂直位置由字体的下线决定，不同系统上会看起来偏上或偏下）。
//
//go:embed assets/more.svg
var moreSVG []byte

var iconMore = ui.MustParseSVG(moreSVG)

// 列的宽度集中在这里：表头与每一行共用，改一个数字两边一起动。
const (
	colName  float32 = 64
	colVer   float32 = 92
	colBadge float32 = 148
	colSize  float32 = 68
	colOps   float32 = 48
)

func (g *gui) pageInstalled(c *ui.Context) {
	g.head(c, "已装工具", "Installed tools", "tools", nil) // 全局刷新在顶栏，这页不再重复

	if !g.status.OK {
		ui.Text(c, g.status.Reason).TextColor(c.Theme().Warning)
		g.note(c, "装好 CLI 后点顶栏的「刷新」。", "Install the CLI, then press Refresh above.")
		return
	}

	// 可升级提示：一行一句，不占整块
	if len(g.tools.Outdated) > 0 {
		for _, o := range g.tools.Outdated {
			o := o
			ui.Row(c).FillWidth().Gap(8).Padding(6).Background(c.Theme().Surface).Radius(6).AlignItems(ui.Center).Children(func() {
				ui.Badge(c, g.t("可升级", "update"))
				ui.Textf(c, "%s %s → %s", o.Name, o.Installed, o.Latest).Grow(1)
				ui.Button(c, g.t("升级", "Upgrade")).FontSize(12).OnClick(func() {
					g.enqueue(o.Name + "@" + o.Latest)
					g.page = "queue"
					g.runQueue()
				})
			})
		}
	}

	tools := g.tools.Tools
	if len(tools) == 0 {
		ui.Text(c, g.t("还没有装任何工具。", "No tools installed yet.")).TextColor(c.Theme().TextMuted)
		g.installHint(c)
		return
	}

	// 一行摘要：把"这一屏能一眼知道的事"挤在标题下面，别让下面是空白
	var total int64
	for _, t := range tools {
		total += t.SizeBytes
	}
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, g.t("%d 个工具", "%d tools"), len(tools)).Bold()
		ui.Text(c, "·").TextColor(c.Theme().TextMuted)
		ui.Textf(c, g.t("占用 %s", "%s on disk"), humanBytes(total)).TextColor(c.Theme().TextMuted)
		if n := len(g.tools.Outdated); n > 0 {
			ui.Text(c, "·").TextColor(c.Theme().TextMuted)
			ui.Textf(c, g.t("%d 项可升级", "%d updates"), n).TextColor(c.Theme().Warning)
		}
	})

	// 列头：让对齐自己解释自己（不然一排数字很难读）
	ui.Row(c).FillWidth().Gap(10).Padding(0, 6).Children(func() {
		for _, h := range [][2]any{
			{g.t("名称", "Name"), colName},
			{g.t("版本", "Version"), colVer},
			{g.t("状态", "State"), colBadge},
			{g.t("路径", "Path"), float32(0)},
			{g.t("大小", "Size"), colSize},
			{g.t("操作", "Actions"), colOps},
		} {
			txt := ui.Text(c, h[0].(string)).FontSize(11).TextColor(c.Theme().TextMuted)
			if w, ok := h[1].(float32); ok && w > 0 {
				txt.Width(w)
			} else {
				txt.Grow(1)
			}
		}
	})

	list := g.table("installed")
	list.Key = func(i int) any { return tools[i].Name + "@" + tools[i].Version }
	ui.List(c, list, len(tools), func(i int) { // 列表撑满剩余高度，别在下面留一截空白
		t := tools[i]
		ui.Row(c).FillWidth().Gap(10).Padding(4, 6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, t.Name).Bold().Width(colName)
			ui.Text(c, t.Version).Width(colVer)
			ui.Row(c).Width(colBadge).Gap(4).Children(func() {
				if t.Active {
					ui.Badge(c, g.t("默认", "active"))
				}
				// verified 的取值：ok 之外的都是"值得看一眼"的（weak/跳过校验）
				if t.Verified != "" && t.Verified != "ok" {
					ui.Badge(c, t.Verified)
				}
				if t.NPM != nil {
					ui.Badge(c, "npm "+t.NPM.Version)
				}
			})
			ui.Text(c, t.Path).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			size := ""
			if t.SizeBytes > 0 {
				size = humanBytes(t.SizeBytes)
			}
			ui.Text(c, size).TextColor(c.Theme().TextMuted).Width(colSize)
			// 行内动作收进一个菜单：三个按钮平铺会把"路径"挤没，也不好看
			// 幽灵按钮：常驻不画底色（不然深色下像个浮在行上的小块），悬停/聚焦时才有面
			ui.Button(c, "").Label(g.t("更多", "More")).Width(colOps).Height(22).
				Background(ui.Transparent).BorderWidth(0).Menu(func(m *ui.Menu) {
				if m.Item(g.t("设为默认版本", "Make active")).Disabled(t.Active).Chosen() {
					g.useVersion(t.Name, t.Version)
				}
				if m.Item(g.t("复制安装路径", "Copy install path")).Chosen() {
					mygo.Clipboard.WriteText(t.Path)
					g.flashNow(g.t("已复制路径", "Path copied"))
				}
				m.Separator()
				if m.Item(g.t("卸载", "Uninstall")).Chosen() {
					g.askUninstall(t.Name)
				}
			}).Children(func() { ui.Icon(c, iconMore).FontSize(14) })
		})
	}).Grow(1)
}

// installHint 在空列表时给一条能立刻照做的命令。
func (g *gui) installHint(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, g.t("装一个试试：", "Try: ")).TextColor(c.Theme().TextMuted)
		ui.Text(c, "pk install node@lts").Font("monospace").SingleLine()
		ui.Button(c, g.t("复制", "Copy")).FontSize(12).OnClick(func() {
			mygo.Clipboard.WriteText("pk install node@lts")
			g.flashNow(g.t("已复制", "Copied"))
		})
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
