package main

// 更新页：GUI 与 CLI 各自的当前/最新版本，两个独立的升级按钮。
//
// GUI 自更新会替换自己这个二进制（替换完要重开）；CLI 的升级完全交给 `pk upgrade --self`
// —— 渠道（brew/scoop）规则只有 CLI 一份实现。

import (
	"context"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageUpdate(c *ui.Context) {
	g.head(c, "更新", "Update", "update", nil)

	ui.Row(c).Fill().Gap(8).Children(func() {
		if g.busy["update"] {
			ui.Spinner(c)
		}
		if ui.Button(c, g.t("检查更新", "Check for updates")).Clicked() {
			g.checkUpdate()
		}
	})

	shown := g.upd.GUI.Current != "" || g.upd.CLI.Current != ""
	if !shown {
		g.note(c, "点「检查更新」会问 CLI：它探测各源延迟、挑最快的，GUI 跟着用同一个源。",
			"Checking asks the CLI: it probes the sources, picks the fastest, and the GUI downloads from the same one.")
		return
	}
	if g.upd.Source != "" {
		ui.Textf(c, g.t("本次使用源：%s", "Source: %s"), g.upd.Source).TextColor(c.Theme().TextMuted)
	}

	// GUI 自己
	ui.Column(c).Fill().Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
		ui.Text(c, g.t("界面（GUI）", "App (GUI)")).Bold()
		ui.Textf(c, "当前 %s · 最新 %s", g.upd.GUI.Current, orDash(g.upd.GUI.Latest))
		ui.Row(c).Fill().Gap(8).Children(func() {
			switch {
			case g.upd.GUI.Channel == "scoop" || g.upd.GUI.Channel == "brew":
				ui.Textf(c, g.t("这个 GUI 是 %s 装的：%s", "Installed by %s: %s"), g.upd.GUI.Channel, g.upd.GUI.ChannelHint).
					TextColor(c.Theme().TextMuted)
			case g.upd.GUI.UpdateAvailable:
				if ui.PrimaryButton(c, g.t("升级界面", "Upgrade app")).Clicked() {
					g.askGUUpdate()
				}
			case g.upd.GUI.Restart:
				ui.Text(c, g.t("已替换，重开这个窗口生效。", "Replaced: reopen the window to use it.")).TextColor(c.Theme().Success)
			default:
				ui.Text(c, g.t("已是最新", "Up to date")).TextColor(c.Theme().TextMuted)
			}
		})
	})

	// CLI
	ui.Column(c).Fill().Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
		ui.Text(c, g.t("命令行（pk）", "CLI (pk)")).Bold()
		ui.Textf(c, "当前 %s · 最新 %s", g.upd.CLI.Current, orDash(g.upd.CLI.Latest))
		ui.Row(c).Fill().Gap(8).Children(func() {
			switch {
			case g.upd.CLI.Channel == "scoop" || g.upd.CLI.Channel == "brew":
				ui.Textf(c, g.t("CLI 是 %s 装的：%s", "CLI installed by %s: %s"), g.upd.CLI.Channel, g.upd.CLI.ChannelHint).
					TextColor(c.Theme().TextMuted)
			case g.upd.CLI.UpdateAvailable:
				if ui.PrimaryButton(c, g.t("升级 CLI", "Upgrade CLI")).Clicked() {
					g.applyUpdate("cli")
				}
			default:
				ui.Text(c, g.t("已是最新", "Up to date")).TextColor(c.Theme().Success)
			}
		})
	})

	if len(g.upd.Order) > 0 {
		ui.Text(c, g.t("候选源（按探测延迟）", "Sources (by latency)")).Bold()
		rows := make([][2]string, 0, len(g.upd.Order))
		for _, s := range g.upd.Order {
			rows = append(rows, [2]string{s.Name, s.Base})
		}
		g.kvTable(c, "sources", rows)
	}
}

func (g *gui) checkUpdate() {
	g.load("update", func() (commit, error) {
		res, err := g.svc.UpdateCheck(context.Background())
		if err != nil {
			return nil, err
		}
		return func() { g.upd = res }, nil
	})
}

func (g *gui) applyUpdate(target string) {
	g.load("apply", func() (commit, error) {
		res, err := g.svc.UpdateApply(context.Background(), target)
		if err != nil {
			return nil, err
		}
		return func() {
			if res.Error != "" {
				g.flashErr = true
				g.flash = res.Error
				return
			}
			g.flashNow(g.t("升级完成", "Upgraded"))
			if target == "cli" {
				g.reloadTools()
			}
		}, nil
	})
}

// askGUUpdate 先确认再替换自身：替换中的二进制被占用时必须让用户知道要重开。
func (g *gui) askGUUpdate() {
	g.ask(g.t("升级界面？", "Upgrade the app?"),
		g.t("会下载新版、校验后替换当前二进制；替换完成请重开这个窗口。",
			"The new build is downloaded, verified and replaces this binary: reopen the window afterwards."),
		func() { g.applyUpdate("gui") })
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
