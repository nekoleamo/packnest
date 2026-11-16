package main

// 主题：三态（跟随系统/浅/深）。视图每帧都会调用 applyTheme，所以调色板按
// (模式, 系统深浅) 缓存；对比度测试（TestAcceptance_GUI8_ThemeContrast）读的是同一个 themeFor。

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

// themeFor 是"我们的配色"：视图与对比度测试共用同一份，改颜色只改这里。
func themeFor(base *ui.Theme) *ui.Theme {
	t := *base
	// 全局字号与间距在这里定一次：控件、表格行高、内外边距都按它们缩放，
	// 这样"信息密度"是统一的一个数，而不是每页各写一套 Padding。
	t.FontSize = 13
	t.Spacing = 3
	if t.Dark {
		// 深色下自己定一套：默认的 TextMuted 在 #161616 上偏暗（路径/大小这类次要信息看不清），
		// 侧边栏的"非焦点选中态"也会糊成一团灰。
		t.Background = ui.Hex("#161616")
		t.Surface = ui.Hex("#232323")
		t.SurfaceHover = ui.Hex("#2a2a2a")
		t.SurfacePressed = ui.Hex("#333333")
		t.Border = ui.Hex("#3a3a3a")
		t.Text = ui.Hex("#e9e9e9")
		t.TextMuted = ui.Hex("#a8a8a8")
		t.Selection = ui.Hex("#2f5f46")
		t.InactiveAccent = ui.Hex("#5a5a5a")
		t.Accent = ui.Hex("#4ade80")
		t.AccentText = ui.Hex("#10240f")
	} else {
		t.Accent = ui.Hex("#0a7d4f")
		t.TextMuted = ui.Hex("#5c5c5c") // 浅色下默认的 #666 在灰底卡片上偏淡
	}
	return &t
}
