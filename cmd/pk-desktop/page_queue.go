package main

// 安装页：一个输入框写 spec（node@22）、一个队列（多个 spec 顺序装）、一张"可装版本"表。
//
// 事件流来自 `pk install --json`（NDJSON）：每行一个事件，进度条直接吃 progress 的字节数。

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageQueue(c *ui.Context) {
	g.head(c, "安装", "Install", "", func() { g.reloadAvailable() })

	// 一条 spec 一行：回车或点「加进队列」
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.TextInput(c, &g.queueInput).Placeholder(g.t("例如 node@22 / pnpm / go@1.24.0", "e.g. node@22 / pnpm / go@1.24.0")).Grow(1).OnSubmit(func() {
			g.enqueue(g.queueInput)
		})
		if ui.PrimaryButton(c, g.t("加进队列", "Add to queue")).Clicked() {
			g.enqueue(g.queueInput)
		}
		if len(g.toolNames()) > 0 && ui.Button(c, g.t("已装工具", "Installed tools")).Clicked() {
			g.page = "installed"
		}
	})

	// 队列
	if len(g.queue) > 0 {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Textf(c, g.t("队列（%d）", "Queue (%d)"), len(g.queue)).Bold()
			ui.Button(c, g.t("开始", "Start")).Disabled(g.queuePending() == 0 || g.busy["queue"]).OnClick(g.runQueue)
			ui.Button(c, g.t("清掉已完成", "Clear finished")).OnClick(g.clearDone)
		})
		for i, q := range g.queue {
			i, q := i, q
			ui.Row(c).FillWidth().Gap(8).Padding(6).Children(func() {
				ui.Text(c, q.Spec).Width(200)
				switch q.State {
				case "wait":
					ui.Badge(c, g.t("等待", "waiting"))
				case "run":
					ui.Progress(c, q.Percent).Width(160)
					ui.Textf(c, "%.0f%%", q.Percent*100).TextColor(c.Theme().TextMuted).Width(50)
				case "done":
					ui.Badge(c, g.t("完成", "done"))
				case "fail":
					ui.Badge(c, g.t("失败", "failed"))
				}
				if q.Msg != "" {
					ui.Text(c, q.Msg).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
				}
				if q.State == "wait" && ui.Button(c, g.t("移除", "Remove")).Clicked() {
					g.queue = append(g.queue[:i], g.queue[i+1:]...)
				}
			})
		}
		ui.Divider(c)
	}

	ui.Text(c, g.t("可装版本（索引缓存，24h 内不刷新）", "Available versions (cached index)")).Bold()
	if len(g.available) == 0 {
		if g.busy["available"] {
			ui.Spinner(c)
		} else {
			ui.Text(c, g.t("还没读索引：点右上角刷新。", "No index yet: press Refresh.")).TextColor(c.Theme().TextMuted)
		}
		return
	}
	list := g.table("available")
	list.Key = func(i int) any { return g.available[i].Name }
	ui.List(c, list, len(g.available), func(i int) {
		a := g.available[i]
		ui.Row(c).FillWidth().Gap(8).Padding(6).Children(func() {
			ui.Text(c, a.Name).Bold().Width(90)
			ui.Text(c, a.Latest).Width(120)
			if a.LTS != "" {
				ui.Badge(c, "lts "+a.LTS)
			}
			if a.Stale {
				ui.Badge(c, g.t("索引过期", "stale index"))
			}
			if a.Err != "" {
				ui.Text(c, a.Err).TextColor(c.Theme().Warning).SingleLine().Grow(1)
			} else {
				ui.Text(c, a.Source).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			}
			if ui.Button(c, g.t("看版本", "Versions")).Clicked() {
				g.page = "versions"
				g.loadVersions(a.Name, false)
			}
			ui.Button(c, g.t("装最新", "Install latest")).OnClick(func() {
				g.enqueue(a.Name + "@" + a.Latest)
				g.runQueue()
			})
		})
	}).Height(240)
}

// versionsPage 是"某个工具的全部版本"：从安装页点进来。
func (g *gui) pageVersions(c *ui.Context) {
	g.head(c, "可选版本", "Versions", "versions", func() { g.loadVersions(g.verTool, g.verAll) })
	if g.verTool == "" {
		ui.Text(c, g.t("先从「安装」页挑一个工具。", "Pick a tool on the Install page first.")).TextColor(c.Theme().TextMuted)
		return
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.verTool).Bold()
		ui.Textf(c, g.t("最新 %s", "latest %s"), g.versions.Latest).TextColor(c.Theme().TextMuted)
		ui.Textf(c, g.t("共 %d 个版本", "%d versions"), g.versions.Total).TextColor(c.Theme().TextMuted)
		ui.Row(c).Grow(1).Children(func() {})
		if ui.Switch(c, &g.verAll).Changed() {
			g.loadVersions(g.verTool, g.verAll)
		}
		ui.Text(c, g.t("显示全部", "Show all")).TextColor(c.Theme().TextMuted)
	})
	ui.TextInput(c, &g.verFilter).Placeholder(g.t("本地过滤版本号", "Filter versions")).Width(240)

	rows := g.filteredVersions()
	list := g.table("versions")
	list.Key = func(i int) any { return rows[i].Version }
	ui.List(c, list, len(rows), func(i int) {
		v := rows[i]
		ui.Row(c).FillWidth().Gap(8).Padding(6).Children(func() {
			ui.Text(c, v.Version).Width(140)
			if v.LTS != "" {
				ui.Badge(c, "lts")
			}
			if v.Installed {
				ui.Badge(c, g.t("已装", "installed"))
			}
			ui.Row(c).Grow(1).Children(func() {})
			if !v.Installed {
				ui.Button(c, g.t("安装", "Install")).OnClick(func() {
					g.enqueue(g.verTool + "@" + v.Version)
					g.page = "queue"
					g.runQueue()
				})
			}
		})
	}).Height(360)
}

// filteredVersions 是"全部版本 + 本地过滤"（不过滤时直接给索引里的顺序）。
func (g *gui) filteredVersions() []versionRow {
	out := make([]versionRow, 0, len(g.versions.Available))
	for _, v := range g.versions.Available {
		if g.verFilter != "" && !containsFold(v.Version, g.verFilter) {
			continue
		}
		out = append(out, versionRow{Version: v.Version, LTS: v.LTS, Installed: v.Installed})
	}
	return out
}

type versionRow struct {
	Version   string
	LTS       string
	Installed bool
}

// enqueue 把一个 spec 加进队列（空串与重复项都忽略）。
func (g *gui) enqueue(spec string) {
	spec = trimSpace(spec)
	if spec == "" {
		return
	}
	for _, q := range g.queue {
		if q.Spec == spec && q.State == "wait" {
			return
		}
	}
	g.queue = append(g.queue, &queueItem{Spec: spec, State: "wait"})
	g.queueInput = ""
	if g.page != "queue" {
		g.page = "queue"
	}
}

func (g *gui) clearDone() {
	keep := g.queue[:0]
	for _, q := range g.queue {
		if q.State == "wait" || q.State == "run" {
			keep = append(keep, q)
		}
	}
	g.queue = keep
}

// runQueue 顺序跑队列：一次只装一个（pk 自己有锁，但串行才能让进度看得懂）。
func (g *gui) runQueue() {
	if g.busy["queue"] {
		return
	}
	g.busy["queue"] = true
	go func() {
		for {
			var item *queueItem
			g.onMain(func() {
				for _, q := range g.queue {
					if q.State == "wait" {
						item = q
						q.State = "run"
						break
					}
				}
			})
			if item == nil {
				break
			}
			err := g.svc.Install(context.Background(), item.Spec, func(ev installEvent) {
				g.onMain(func() { applyEvent(item, ev) })
			})
			g.onMain(func() {
				if err != nil {
					item.State = "fail"
					item.Msg = friendly(err)
					g.flashErr = true
					g.flash = friendly(err)
				} else {
					item.State = "done"
					item.Percent = 1
				}
			})
		}
		g.onMain(func() {
			delete(g.busy, "queue")
			g.reloadTools()
		})
	}()
}

// applyEvent 把一行 NDJSON 事件画进队列行：progress 用字节数算百分比。
func applyEvent(item *queueItem, ev installEvent) {
	switch ev.Ev {
	case "progress":
		if ev.BytesTotal > 0 {
			item.Percent = float64(ev.BytesDone) / float64(ev.BytesTotal)
		}
		item.Msg = fmt.Sprintf("%s %s", ev.Tool, ev.Version)
		if ev.BytesTotal > 0 {
			item.Msg += fmt.Sprintf("  %s / %s", humanBytes(ev.BytesDone), humanBytes(ev.BytesTotal))
		}
	case "download", "verify", "resolve":
		item.Msg = ev.Ev + " " + ev.Tool + " " + ev.Version
	case "done":
		item.Tool, item.Version = ev.Tool, ev.Version
		item.Msg = fmt.Sprintf("%s %s 装好了", ev.Tool, ev.Version)
	case "error":
		item.State = "fail"
		item.Msg = ev.Msg
	}
}

func (g *gui) reloadAvailable() {
	g.load("available", func() (commit, error) {
		res, err := g.svc.Available(context.Background())
		if err != nil {
			return nil, err
		}
		return func() { g.available = res.Available }, nil
	})
}

func (g *gui) loadVersions(tool string, all bool) {
	g.verTool, g.verAll = tool, all
	g.load("versions", func() (commit, error) {
		v, err := g.svc.Versions(context.Background(), tool, all)
		if err != nil {
			return nil, err
		}
		return func() { g.versions = v }, nil
	})
}
