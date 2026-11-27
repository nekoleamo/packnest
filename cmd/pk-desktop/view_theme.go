package main

// 主题：三态（跟随系统/浅/深）。视图每帧都会调用 applyTheme，所以调色板按
// (模式, 系统深浅) 缓存；对比度测试（TestAcceptance_GUI8_ThemeContrast）读的是同一个 themeFor。
//
// 取色全部在 tokens.go 的 lightPalette/darkPalette 里，本文件只负责"把调色板落到 ui.Theme 上"。

import "github.com/egoist/mygo/ui"

// applyTheme 把主题三态落到 ui 上：默认跟随系统，手动选浅/深时换主题。
func (g *gui) applyTheme(c *ui.Context) {
	base := c.Theme()
	switch g.themeMode {
	case 1:
		base = ui.LightTheme()
	case 2:
		base = ui.DarkTheme()
	}
	c.SetTheme(cachedTheme(g.themeMode, base))
}

func cachedTheme(mode int, base *ui.Theme) *ui.Theme {
	key := [2]int{mode, 0}
	if base.Dark {
		key[1] = 1
	}
	if t, ok := themeCache[key]; ok {
		return t
	}
	t := themeFor(base)
	themeCache[key] = t
	return t
}

// themeFor 是"我们的配色"：视图与对比度测试共用同一份，改颜色只改 tokens.go。
//
// 2026-11-28（令牌移植）之前这里只覆盖了 9 个字段，Danger/Warning/Success/Focus/
// Inverse/Scrollbar/AccentHover/AccentPressed 全都还是 mygo 默认值 —— 在自定义的
// #161616 底上从没校过对比度。现在整张调色板一起给，缺一档就会在深色下露出默认蓝。
func themeFor(base *ui.Theme) *ui.Theme {
	t := *base
	p := lightPalette
	if t.Dark {
		p = darkPalette
	}

	// 全局字号与间距在这里定一次：控件、表格行高、内外边距都按它们缩放，
	// 这样"信息密度"是统一的一个数，而不是每页各写一套 Padding。
	t.FontSize = fontBody
	t.Spacing = 3
	t.Radius = radiusCard

	t.Background = ui.Hex(p.bg)
	t.Surface = ui.Hex(p.surface)
	t.SurfaceHover = ui.Hex(p.surfaceHover)
	t.SurfacePressed = ui.Hex(p.surfacePressed)
	t.Border = ui.Hex(p.border)
	t.Text = ui.Hex(p.text)
	t.TextMuted = ui.Hex(p.textMuted)
	t.Selection = ui.Hex(p.selection)

	// 非活动窗口里的选中态：太淡会和 Background 糊成一片（对比度 <3 就认不出选的是谁）。
	t.InactiveAccent = ui.Hex(p.inactive)

	t.Accent = ui.Hex(p.accent)
	t.AccentHover = ui.Hex(p.accentHover)
	t.AccentPressed = ui.Hex(p.accentPressed)
	t.AccentText = ui.Hex(p.accentText)
	// 焦点环跟强调色同色：键盘用户一眼能看出焦点在哪，不用再多一个颜色。
	t.Focus = ui.Hex(p.accent)

	t.Danger = ui.Hex(p.danger)
	t.Warning = ui.Hex(p.warning)
	t.Success = ui.Hex(p.success)

	// 反色面（toast/tooltip）：浅色主题下是深底浅字，深色主题下反过来。
	t.Inverse = ui.Hex(p.inverse)
	t.InverseText = ui.Hex(p.inverseText)

	t.Scrollbar = ui.Hex(p.scrollbar)
	return &t
}
