package main

// 界面自己的偏好文件（主题/语言/上次的页）：只写 App 配置目录，不碰 pk 的任何文件。

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// prefsSchema 是 gui.json 的格式版本，属于 GUI 1.0 的发布契约（docs/gui.md「GUI 1.0 契约」）：
// 字段只增不改；读到比本程序新的版本时整份按「读不懂」处理 —— 不套用、不回写（降级后也不能
// 把新格式的偏好覆盖掉），也绝不删文件。
const prefsSchema = 1

// prefs 是落盘的三项偏好 + 格式版本。
type prefs struct {
	Schema int    `json:"schema"`
	Theme  int    `json:"theme"`
	Lang   string `json:"lang"`
	Page   string `json:"page"`
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
	// schema 缺省（1.0 之前的文件没有这个字段）视为当前版本；比本程序新 → 整份放弃，
	// 本会话按默认值跑且不再回写。未知的新字段（如 future 版本的 json 结构）在上面
	// 反序列化时自然被忽略，不会影响读写。
	if p.Schema > prefsSchema {
		g.prefsFrozen = true
		log.Printf("偏好文件由更新版本（schema %d > %d）写入，本次按默认值运行且不回写：%s",
			p.Schema, prefsSchema, prefsPath())
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
	if g.prefsFrozen {
		return
	}
	if err := os.MkdirAll(appDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(prefs{Schema: prefsSchema, Theme: g.themeMode, Lang: g.lang, Page: g.page})
	if err != nil {
		return
	}
	_ = os.WriteFile(prefsPath(), b, 0o644)
}
