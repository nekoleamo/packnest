package main

// CDS 便携令牌（来源 ~/Documents/Plan/23-mygo-UI两条路线/mygo-ui-purego规格书.md）。
//
// 只收"这个界面真的会用到"的那些：35 阶灰阶收了 10 个落点、十条曲线留两条、
// 弹簧与行为物理（§8.3 图钉/死区/波形）在控制台型界面里没有落点，不收。
// 规格书 §9 的 API 对照表与 mygo v0.10 有三处偏差（Shadow/InsetShadow/ClipInset 是
// Element 方法而非 ui.X()，Markdown 在 ui/markdown 子包，v0.10 无 DiffViewer 导出），
// 本文件只写 v0.10 真实存在的 API。

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// 尺度：字号/圆角/间距/时长/断点的唯一出处。改观感只改这里。
const (
	fontTitle   float32 = 15 // 页标题、应用名
	fontBody    float32 = 13 // 正文（= Theme.FontSize）
	fontButton  float32 = 12 // 按钮字：比正文小一档才不像链接
	fontCaption float32 = 11 // 列头、辅助说明

	radiusCard float32 = 8 // 卡片、区块
	radiusRow  float32 = 6 // 行内小块（可升级行、徽标容器）

	// §7 间距阶梯裁成四档。行内 Gap 仍写字面量：微间距读数字比读名字快，
	// 这里的四档只用于"块级"的内边距，保证各页留白是同一个数。
	spaceXS float32 = 4
	spaceSM float32 = 6
	spaceMD float32 = 8
	spaceLG float32 = 12

	pagePadX float32 = 14 // 页面左右留白（标题行、卡片、列表共用）
	rowPadY  float32 = 4  // 表格行上下留白

	// §8.1 时长：只留三档。动效在操作型界面上超过这一档就是拖沓。
	durFast = 60 * time.Millisecond  // 按压、focus
	durSnap = 120 * time.Millisecond // 切页（几何咬合）
	durBase = 200 * time.Millisecond // 浮层、卡片入场

	// §10「响应式靠阈值分支」的落点：窄于它就收掉侧栏、改用顶栏下拉。
	narrowWidth float32 = 620
	// 侧栏在"中等宽度"下先收窄一档，再窄才整个收起。
	slimWidth float32 = 780

	// 侧栏两档宽度（150 是 0.12 排版那一轮定的，收窄档只压缩留白不动字号）。
	sidebarFullWidth float32 = 150
	sidebarSlimWidth float32 = 120
)

// §8.2 曲线：留 swift（高频微交互）与 out（标准出场）。入场用 ease-out 系。
var (
	easeSwift = ui.CubicBezier(0.16, 1, 0.3, 1)
	easeOut   = ui.CubicBezier(0.165, 0.84, 0.44, 1)
)

// pageEnter 是切页的入场：自下方 6 DIP 消融进来（§8.2 enter 的相位，曲线取 easeOut）。
//
// 只在"父级上一帧就在"时生效，所以它不会连带把页内每个元素都动一遍；
// 退出不设 Exit：切页时旧页立刻让位，否则两页会叠着抢高度。
// Position 只开位置一项：尺寸要是也动，从一个长页切到短页会看到内容被拉长 120ms。
var pageEnter = ui.ElementTransition{
	Duration: durSnap, Ease: easeOut, Position: true,
	Enter: &ui.Motion{Y: 6},
}

// navResize 让侧栏宽度在 150↔120 之间平滑收放（§8.2 的“几何咬合”），不动内容布局。
var navResize = ui.ElementTransition{Size: true, Duration: durBase, Ease: easeOut}

// palette 是"我们的配色"的字段表（§2 灰阶 + §3 四层表面 + §5 发丝边的落点）。
//
// 只列会覆盖的字段，其余继承 mygo 默认主题（字体族、控件几何、焦点环宽度等）。
// 命名不是 CSS 令牌名而是 mygo 的 Theme 字段名：改一个字段两处一起动。
type palette struct {
	bg, surface, surfaceHover, surfacePressed, border string
	text, textMuted, selection, inactive              string
	accent, accentHover, accentPressed, accentText    string
	danger, warning, success                          string
	inverse, inverseText, scrollbar                   string
}

// lightPalette 是 Claude 暖灰阶的浅色档：侧栏/窗口 = gray-20，卡片 = gray-0（白）。
//
// 取色不是照抄上游 #d97757：那个陶土橙配白字只有 2.96:1，过不了本项目的
// TestAcceptance_GUI8（≥4.5）。浅色档把陶土橙压深到 #b0512f（4.99:1），
// 深色档保持原值改用深字（5.88:1）——两档都真到了 4.5，且还是同一个色相。
var lightPalette = palette{
	bg: "#f9f9f7", surface: "#ffffff", surfaceHover: "#f6f6f4", surfacePressed: "#ecebe5",
	border: "#e1e0d9",
	text:   "#20201f", textMuted: "#6e6c66", selection: "#f2ddd2", inactive: "#8a8780",
	accent: "#b0512f", accentHover: "#a04526", accentPressed: "#8e3c20", accentText: "#fffaf7",
	danger: "#b3261e", warning: "#8a5a00", success: "#1a7f4b",
	inverse: "#20201f", inverseText: "#fcfcfb", scrollbar: "#d6d4cd",
}

// darkPalette 是暗色档：窗口 gray-900、卡片 gray-850、按压档 gray-800，
// 三级微对比（1.1%–2%）就是"温润"的来源，不能用"看起来差不多"的灰。
var darkPalette = palette{
	bg: "#0b0b0b", surface: "#151514", surfaceHover: "#1c1c1a", surfacePressed: "#232321",
	border: "#2a2a28",
	text:   "#f0efec", textMuted: "#9a978f", selection: "#3d2f28", inactive: "#6a675f",
	accent: "#d97757", accentHover: "#e08363", accentPressed: "#c86848", accentText: "#1a1310",
	danger: "#f08a78", warning: "#e3aa4a", success: "#6fcf97",
	inverse: "#f0efec", inverseText: "#151514", scrollbar: "#3a3a37",
}
