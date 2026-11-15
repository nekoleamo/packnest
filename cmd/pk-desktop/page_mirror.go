package main

// 镜像页：档位选择 + 切换 + 各源探测表。
//
// 探测会真的发 HTTP HEAD（慢），所以它是单独一个按钮、单独一个 busy key。

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageMirror(c *ui.Context) {
	g.head(c, "镜像档", "Mirrors", "mirrors", func() { g.loadMirrors("") })

	if g.mirror.Current == "" && !g.busy["mirrors"] {
		ui.Text(c, g.t("还没读到镜像档。", "No mirror list yet.")).TextColor(c.Theme().TextMuted)
		return
	}

	// 当前档位
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("当前档位", "Current")).TextColor(c.Theme().TextMuted).Width(90)
		ui.Badge(c, g.mirror.Current)
	})

	// 档位列表：一行一个，带 registry，点「切到」即切
	list := g.table("mirrors")
	mirrors := g.mirror.Mirrors
	list.Key = func(i int) any { return mirrors[i].Name }
	ui.List(c, list, len(mirrors), func(i int) {
		m := mirrors[i]
		ui.Row(c).FillWidth().Gap(8).Padding(6).Children(func() {
			if m.Current {
				ui.Badge(c, g.t("使用中", "in use"))
			}
			ui.Text(c, m.Display).Bold().Width(160)
			ui.Text(c, m.Name).Width(90)
			ui.Text(c, m.Registry).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			if !m.Current && ui.Button(c, g.t("切到", "Use")).Clicked() {
				g.loadMirrors(m.Name)
			}
		})
	}).Height(200)

	ui.Divider(c)

	// 探测：host + 状态 + 延迟
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("源探测", "Probe sources")).Bold()
		if g.busy["probe"] {
			ui.Spinner(c)
		}
		ui.Row(c).Grow(1).Children(func() {})
		if ui.Button(c, g.t("探测各源", "Probe now")).Clicked() {
			g.runProbe()
		}
	})
	if len(g.probe) == 0 {
		g.note(c, "探测会真的连一次各源（HEAD，不下载），慢一点但最准。",
			"Probing makes one HEAD request per source: slower, but the truth.")
		return
	}
	rows := make([][4]string, 0, len(g.probe))
	for _, p := range g.probe {
		status := fmt.Sprint(p.Status)
		if !p.OK {
			status = g.t("失败", "failed")
			if p.Err != "" {
				status += ": " + p.Err
			}
		}
		rows = append(rows, [4]string{hostOf(p.URL), status, fmt.Sprintf("%d ms", p.LatencyMS), p.URL})
	}
	tl := g.table("probe")
	tl.Key = func(i int) any { return rows[i][3] }
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		for _, h := range []string{g.t("主机", "Host"), g.t("状态", "Status"), g.t("延迟", "Latency"), g.t("地址", "URL")} {
			ui.Text(c, h).TextColor(c.Theme().TextMuted).Width(200)
		}
	})
	ui.List(c, tl, len(rows), func(i int) {
		ui.Row(c).FillWidth().Gap(8).Padding(4).Children(func() {
			ui.Text(c, rows[i][0]).Width(200).SingleLine()
			ui.Text(c, rows[i][1]).Width(200).SingleLine()
			ui.Text(c, rows[i][2]).Width(80).SingleLine()
			ui.Text(c, rows[i][3]).SingleLine().Grow(1)
		})
	}).Height(220)
}

func (g *gui) loadMirrors(use string) {
	g.load("mirrors", func() (commit, error) {
		m, err := g.svc.Mirrors(context.Background(), use)
		if err != nil {
			return nil, err
		}
		return func() { g.mirror = m }, nil
	})
}

func (g *gui) runProbe() {
	tool := g.probeTool
	g.load("probe", func() (commit, error) {
		res, err := g.svc.Probe(context.Background(), tool)
		if err != nil {
			return nil, err
		}
		return func() { g.probe = res.Results }, nil
	})
}

// hostOf 从 URL 里取主机名（表格第一列只给这个，省地方）。
func hostOf(u string) string {
	s := u
	for _, p := range []string{"https://", "http://"} {
		if len(s) >= len(p) && s[:len(p)] == p {
			s = s[len(p):]
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i]
		}
	}
	return s
}
