package main

// 已装工具页：一张对齐的清单（列宽固定，路径吃掉剩余宽度）+ 可升级提示 + 行内动作。
//
// 交互纪律不变：只有"卸载"会毁东西，走确认框；其余动作直接执行并把结果弹成 toast。

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// moreSVG 是行尾的「更多」图标：三个点自己画，比字符 "⋯" 的对齐可控
// （字符的垂直位置由字体的下线决定，不同系统上会看起来偏上或偏下）。
//
//go:embed assets/more.svg
var moreSVG []byte

var iconMore = ui.MustParseSVG(moreSVG)

// 六列的相对宽度与最小宽度：用权重而不是像素。
//
// 0.12 那版写死 64/92/148/68/48：窗口拉宽时右边永远空一截，切英文后中文列又被挤。
// 自绘没有 @container（规格书 §10），响应式只能靠“权重 + 最小宽”两个数。
var toolCols = [6]struct {
	grow, min float32
}{
	{1.4, 56},  // 名称
	{1.4, 66},  // 版本
	{2.0, 112}, // 状态（最多挂三个徐标）
	{3.4, 0},   // 路径（吃剩下的宽度，不设下限，反正单行截断）
	{1.1, 54},  // 大小
	{0.7, 34},  // 操作
}

// col 给第 i 列套宽度规则：表头与每一行共用，改一个数字两边一起动。
func col(el ui.Element, i int) ui.Element {
	c := toolCols[i]
	el = el.Grow(c.grow)
	if c.min > 0 {
		el = el.MinWidth(c.min)
	}
	return el
}

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
			// 一行一块：浅色下 Surface 与画布只差 1%，必须靠发丝边才看得出“这是可操作的一块”
			// （只给背景的话，浅色下它和画布都是白、深色下和画布同值，两边都看不见）。
			ui.Row(c).FillWidth().Gap(8).Padding(spaceSM).Background(c.Theme().Surface).
				Radius(radiusRow).Border(1, c.Theme().Border).AlignItems(ui.Center).Children(func() {
				ui.Badge(c, g.t("可升级", "update"))
				ui.Textf(c, "%s %s → %s", o.Name, o.Installed, o.Latest).Grow(1)
				ui.Button(c, g.t("升级", "Upgrade")).FontSize(fontButton).OnClick(func() {
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
	ui.Row(c).FillWidth().Gap(10).Padding(0, spaceSM).Children(func() {
		for i, h := range []string{
			g.t("名称", "Name"), g.t("版本", "Version"), g.t("状态", "State"),
			g.t("路径", "Path"), g.t("大小", "Size"), g.t("操作", "Actions"),
		} {
			header := col(ui.Text(c, h).FontSize(fontCaption).TextColor(c.Theme().TextMuted), i)
			if i == 4 {
				header.TextAlign(ui.End) // 数字右对齐：一排大小才能上下扫着看
			}
		}
	})

	list := g.table("installed")
	list.Key = func(i int) any { return tools[i].Name + "@" + tools[i].Version }
	ui.List(c, list, len(tools), func(i int) { // 列表撑满剩余高度，别在下面留一截空白
		t := tools[i]
		ui.Row(c).FillWidth().Gap(10).Padding(rowPadY, spaceSM).AlignItems(ui.Center).Children(func() {
			col(ui.Text(c, t.Name).Bold(), 0)
			col(ui.Text(c, t.Version), 1)
			col(ui.Row(c).Gap(4), 2).Children(func() {
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
			col(ui.Text(c, t.Path).TextColor(c.Theme().TextMuted).SingleLine(), 3)
			size := ""
			if t.SizeBytes > 0 {
				size = humanBytes(t.SizeBytes)
			}
			col(ui.Text(c, size).TextColor(c.Theme().TextMuted).TextAlign(ui.End), 4)
			// 行内动作收进一个菜单：三个按钮平铺会把"路径"挤没，也不好看
			// 幽灵按钮：常驻不画底色（不然深色下像个浮在行上的小块），悬停/聚焦时才有面
			col(ui.Button(c, "").Label(g.t("更多", "More")).Height(22), 5).
				Background(ui.Transparent).BorderWidth(0).Menu(func(m *ui.Menu) {
				if m.Item(g.t("设为默认版本", "Make active")).Disabled(t.Active).Chosen() {
					g.useVersion(t.Name, t.Version)
				}
				if m.Item(g.t("复制安装路径", "Copy install path")).Chosen() {
					g.copyText(t.Path)
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
		ui.Button(c, g.t("复制", "Copy")).FontSize(fontButton).OnClick(func() {
			g.copyText("pk install node@lts")
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
