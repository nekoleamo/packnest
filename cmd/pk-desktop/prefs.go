package main

// 界面自己的偏好文件（主题/语言/上次的页）：只写 App 配置目录，不碰 pk 的任何文件。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prefs 是落盘的三项偏好。
type prefs struct {
	Theme int    `json:"theme"`
	Lang  string `json:"lang"`
	Page  string `json:"page"`
}

func prefsPath() string { return filepath.Join(appDir(), "gui.json") }

func (g *gui) prefsLoad() {
	b, err := os.ReadFile(prefsPath())
	if err != nil {
		return
	}
	var p prefs
	if json.Unmarshal(b, &p) != nil {
		return
	}
	g.themeMode = p.Theme
	themeModeSeen = p.Theme
	if p.Lang == "en" || p.Lang == "zh" {
		g.lang = p.Lang
	}
	for _, pg := range pages {
		if pg.id == p.Page {
			g.page = p.Page
		}
	}
	g.langIdx = 0
	if g.lang == "en" {
		g.langIdx = 1
	}
	pageSeen = g.page
}

func (g *gui) prefsSave() {
	if err := os.MkdirAll(appDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(prefs{Theme: g.themeMode, Lang: g.lang, Page: g.page})
	if err != nil {
		return
	}
	_ = os.WriteFile(prefsPath(), b, 0o644)
}
