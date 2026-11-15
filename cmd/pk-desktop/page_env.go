package main

// 环境页：which（命令 → 真实二进制）、PATH 与环境变量、重载提示、写 .npmrc。
//
// 唯一会改用户文件的是「写 .npmrc」，所以它走确认框；其余全是只读。

import (
	"context"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageEnv(c *ui.Context) {
	g.head(c, "环境", "Environment", "env", nil)

	// which
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, "pk which").Font("monospace")
		ui.TextInput(c, &g.whichCmd).Placeholder("node").Width(200).OnSubmit(func() { g.runWhich() })
		if ui.Button(c, g.t("查一遍", "Look up")).Clicked() {
			g.runWhich()
		}
		if g.which.Path != "" {
			ui.Text(c, g.which.Path).SingleLine().Grow(1)
			if g.which.Shim {
				ui.Badge(c, g.t("pk 入口", "pk shim"))
			}
			if ui.Button(c, g.t("复制", "Copy")).Clicked() {
				g.copyText(g.which.Path)
			}
		}
	})

	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if ui.Switch(c, &g.envProj).Changed() {
			g.loadEnv(g.envProj)
		}
		ui.Text(c, g.t("项目视角（按 pk.lock 前置）", "Project view (per pk.lock)")).TextColor(c.Theme().TextMuted)
	})
	ui.Divider(c)

	// PATH
	if len(g.env.Paths) > 0 {
		ui.Textf(c, g.t("PATH（%d 段，按生效顺序）", "PATH (%d entries, in effect order)"), len(g.env.Paths)).Bold()
		rows := make([][2]string, 0, len(g.env.Paths))
		for i, p := range g.env.Paths {
			rows = append(rows, [2]string{itoa(i + 1), p})
		}
		g.kvTable(c, "envpath", rows)
	}

	// 环境变量
	if len(g.env.Env) > 0 {
		ui.Textf(c, g.t("环境变量（%d）", "Variables (%d)"), len(g.env.Env)).Bold()
		rows := make([][2]string, 0, len(g.env.Env))
		for _, e := range g.env.Env {
			rows = append(rows, [2]string{e.Key, e.Value})
		}
		g.kvTable(c, "envvars", rows)
	}

	// 重载提示（doctor --fix 之后要开新 shell 或 source 一次）
	if r := g.doctor.Reload; r != nil && r.Needed {
		ui.Column(c).FillWidth().Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
			ui.Text(c, g.t("改动要重载环境才生效", "Reload your environment")).Bold()
			if r.Msg != "" {
				ui.Text(c, r.Msg).TextColor(c.Theme().TextMuted)
			}
			if r.Command != "" {
				codeBlock(c, r.Command)
				if ui.Button(c, g.t("复制命令", "Copy command")).Clicked() {
					g.copyText(r.Command)
				}
			}
			for _, f := range r.Files {
				codeBlock(c, f)
			}
		})
	}

	ui.Divider(c)

	// 写 .npmrc（会改用户已有配置文件：只增一行 registry，写前 CLI 自己备份）
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("把当前档位的 registry 写进 ~/.npmrc", "Write the current registry into ~/.npmrc")).Grow(1)
		ui.Button(c, g.t("写入 .npmrc", "Write .npmrc")).OnClick(func() { g.askNpmrc() })
	})
	g.note(c, "只增不改：已有 registry 行会被替换，其它行与注释保持原样，写前备份成 .bak。",
		"Additive only: an existing registry line is replaced, everything else stays, and it is backed up first.")
}

func (g *gui) loadEnv(project bool) {
	g.load("env", func() (commit, error) {
		doc, err := g.svc.Env(context.Background(), project)
		if err != nil {
			return nil, err
		}
		return func() { g.env = doc }, nil
	})
}

func (g *gui) runWhich() {
	if trimSpace(g.whichCmd) == "" {
		g.whichCmd = "node"
	}
	cmd := trimSpace(g.whichCmd)
	g.load("which", func() (commit, error) {
		res, err := g.svc.Which(context.Background(), cmd)
		if err != nil {
			return nil, err
		}
		return func() { g.which = res }, nil
	})
}

// askNpmrc 是先确认再改用户文件的那条路。
func (g *gui) askNpmrc() {
	g.ask(g.t("写入 ~/.npmrc？", "Write ~/.npmrc?"),
		g.t("会把当前镜像档的 registry 写进 ~/.npmrc（已有 registry 行被替换，其余不动，写前备份）。",
			"The current mirror's registry goes into ~/.npmrc: an existing registry line is replaced, the rest stays, and a backup is made first."),
		func() {
			g.load("npmrc", func() (commit, error) {
				res, err := g.svc.MirrorWrite(context.Background(), "")
				if err != nil {
					return nil, err
				}
				return func() {
					verb := g.t("已写入", "written")
					if !res.Replaced {
						verb = g.t("已新增", "added")
					}
					g.flashNow(verb + " " + res.File)
				}, nil
			})
		})
}
