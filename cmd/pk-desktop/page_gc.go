package main

// 磁盘页：gc 的预演/真删 + 分组清单。
//
// 规矩：真删必须二次确认（默认按钮是"取消"），预演永远可以先看。

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageGC(c *ui.Context) {
	g.head(c, "磁盘占用与回收", "Storage", "gc", func() { g.runGC(false) })

	keep := float64(g.gcKeep)
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("每个工具保留最新", "Keep the newest")).TextColor(c.Theme().TextMuted)
		ui.NumberInput(c, &keep, 0, 20, 1).Width(90)
		if int(keep) != g.gcKeep {
			g.gcKeep = int(keep)
		}
		ui.Text(c, g.t("个版本（0 = 只留当前使用的）", "versions per tool (0 = active only)")).TextColor(c.Theme().TextMuted)
	})
	g.gcKeep = int(keep)

	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if g.busy["gc"] {
			ui.Spinner(c)
		}
		if ui.Button(c, g.t("预演（只看不删）", "Dry run")).Clicked() {
			g.runGC(false)
		}
		ui.Button(c, g.t("执行回收", "Reclaim")).OnClick(func() { g.askGC() })
	})

	res := g.gc
	if len(res.Removed) == 0 && len(res.CacheRemoved) == 0 && len(res.KeptRecent) == 0 {
		g.note(c, "先预演看一眼：会列出可删的版本、可清的缓存，以及因为仍被引用/最近用过的保留项。",
			"Start with a dry run: it lists what can go, what the cache holds, and what is kept because it is referenced or recent.")
		return
	}

	verb := g.t("将删", "would remove")
	if !res.DryRun {
		verb = g.t("已删", "removed")
	}
	ui.Column(c).Fill().Gap(4).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
		ui.Textf(c, "%s %d 项 · 释放 %s", verb, len(res.Removed), humanBytes(res.FreedBytes)).Bold()
		if res.Keep > 0 {
			ui.Textf(c, g.t("（--keep %d）", "(--keep %d)"), res.Keep).TextColor(c.Theme().TextMuted)
		}
	})

	g.gcGroup(c, g.t("版本", "Versions"), "gc-removed", res.Removed)
	g.gcGroup(c, g.t("缓存", "Cache"), "gc-cache", res.CacheRemoved)
	g.gcGroup(c, g.t("因被引用而保留", "Kept (referenced)"), "gc-keptref", res.KeptRef)
	g.gcGroup(c, g.t("因最近使用而保留", "Kept (recent)"), "gc-keptrecent", res.KeptRecent)
}

// gcGroup 是一组可折叠的清单（空的组不画）。
func (g *gui) gcGroup(c *ui.Context, title, key string, rows []string) {
	if len(rows) == 0 {
		return
	}
	open := g.groupOpen(key)
	ui.Collapsible(c, fmt.Sprintf("%s（%d）", title, len(rows)), open, func() {
		list := g.table(key)
		list.Key = func(i int) any { return rows[i] }
		ui.List(c, list, len(rows), func(i int) {
			ui.Text(c, rows[i]).SingleLine().TextColor(c.Theme().TextMuted)
		}).Height(140)
	})
}

// groupOpen 给每个折叠组一个跨帧存活的开关。
func (g *gui) groupOpen(key string) *bool {
	if g.open == nil {
		g.open = map[string]*bool{}
	}
	if g.open[key] == nil {
		v := true // 默认展开：清单一出来就该看见，而不是再点一次
		g.open[key] = &v
	}
	return g.open[key]
}

func (g *gui) runGC(run bool) {
	keep := g.gcKeep
	g.load("gc", func() (commit, error) {
		res, err := g.svc.GC(context.Background(), run, keep)
		if err != nil {
			return nil, err
		}
		return func() { g.gc = res }, nil
	})
}

// askGC：真删前的确认。文案要说清"删的是什么、删了会怎样"。
func (g *gui) askGC() {
	g.ask(g.t("执行回收？", "Reclaim disk space?"),
		g.t("会真的删掉不在保留范围内的旧版本与 `archives/`、`meta/` 缓存；装回去要重新下载。",
			"Old versions outside the keep range and the archives/meta caches are deleted for real; installing them again means downloading again."),
		func() { g.runGC(true) })
}
