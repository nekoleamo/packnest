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

	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if g.busy["update"] {
			ui.Spinner(c)
			ui.Text(c, g.t("正在检查…", "Checking…")).TextColor(c.Theme().TextMuted)
		}
		ui.Button(c, g.t("检查更新", "Check for updates")).
			Disabled(g.busy["update"] || g.busy["apply"]).
			OnClick(func() { g.checkUpdate() })
	})

	// 升级中：阶段 + 进度条（下载大文件时按字节走），别再"点了没反应"
	if g.busy["apply"] {
		ui.Column(c).FillWidth().Gap(4).Padding(12).
			Background(c.Theme().Surface).Radius(8).Border(1, c.Theme().Border).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, g.applyStage).Grow(1)
				if g.applyPct > 0 {
					ui.Textf(c, "%.0f%%", g.applyPct*100).TextColor(c.Theme().TextMuted)
				}
			})
			if g.applyPct > 0 {
				ui.Progress(c, g.applyPct)
			}
			g.note(c, "正在下载并校验，完成后会替换当前程序；请等这一条变成结果再关窗口。",
				"Downloading and verifying; it will replace the app. Wait for the result line before closing.")
		})
	}

	// 上一次升级的结果：成功后"要重开窗口才生效"这句话必须留在页面上，不能只弹个 toast
	if g.applyMsg != "" {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(10).
			Background(c.Theme().Surface).Radius(8).Border(1, c.Theme().Border).Children(func() {
			col := c.Theme().Success
			if g.applyErr {
				col = c.Theme().Warning
			}
			ui.Text(c, g.applyMsg).Grow(1).TextColor(col)
			if g.applyErr {
				ui.Button(c, g.t("复制错误", "Copy error")).FontSize(12).OnClick(func() { g.copyText(g.applyMsg) })
			}
			ui.Button(c, g.t("知道了", "Dismiss")).FontSize(12).OnClick(func() { g.applyMsg = "" })
		})
	}

	shown := g.upd.GUI.Current != "" || g.upd.CLI.Current != ""
	if !shown {
		if g.busy["update"] {
			ui.Spinner(c)
			return
		}
		g.note(c, "没读到版本信息：点「检查更新」重试。", "Could not read versions: press Check for updates to retry.")
		return
	}
	if g.upd.Source != "" {
		ui.Textf(c, g.t("本次使用源：%s", "Source: %s"), g.upd.Source).TextColor(c.Theme().TextMuted)
	}

	// GUI 自己
	g.card(c, func() {
		ui.Text(c, g.t("界面（GUI）", "App (GUI)")).Bold()
		ui.Textf(c, "当前 %s · 已发布 %s", g.upd.GUI.Current, orDash(g.upd.GUI.Latest))
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			switch {
			case g.upd.GUI.Channel == "scoop" || g.upd.GUI.Channel == "brew":
				ui.Textf(c, g.t("这个 GUI 是 %s 装的：%s", "Installed by %s: %s"), g.upd.GUI.Channel, g.upd.GUI.ChannelHint).
					TextColor(c.Theme().TextMuted)
			case g.upd.GUI.Restart:
				ui.Text(c, g.t("已替换：托盘 → 退出 → 重新打开。", "Replaced: tray → Quit → reopen.")).TextColor(c.Theme().Success)
			case g.upd.GUI.UpdateAvailable:
				ui.PrimaryButton(c, g.t("升级界面", "Upgrade app")).
					Disabled(g.busy["apply"]).
					OnClick(func() { g.askGUUpdate() })
			case g.upd.GUI.Restart:
				ui.Text(c, g.t("已替换，重开这个窗口生效。", "Replaced: reopen the window to use it.")).TextColor(c.Theme().Success)
			default:
				ui.Text(c, g.t("已是最新", "Up to date")).TextColor(c.Theme().TextMuted)
			}
		})
	})

	// CLI
	g.card(c, func() {
		ui.Text(c, g.t("命令行（pk）", "CLI (pk)")).Bold()
		ui.Textf(c, "当前 %s · 已发布 %s", g.upd.CLI.Current, orDash(g.upd.CLI.Latest))
		ui.Row(c).FillWidth().Gap(8).Children(func() {
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
	g.applyStage = g.t("准备下载…", "Preparing…")
	g.applyPct = 0
	g.applyMsg = ""
	g.applyErr = false

	// 进度钩子在后台 goroutine 里被调用：一律回主线程改状态
	g.svc.Progress = func(stage string, done, total int64) {
		g.onMain(func() {
			g.applyStage = stage
			if total > 0 {
				g.applyPct = float64(done) / float64(total)
			}
		})
	}

	g.load("apply", func() (commit, error) {
		g.applying.Store(true)
		defer g.applying.Store(false)
		res, err := g.svc.UpdateApply(context.Background(), target)
		if err != nil {
			// 失败也要留在页面上：升级按钮点了没反应 = 用户以为坏了
			return func() {
				g.applyPct = 0
				g.applyStage = ""
				g.applyErr = true
				g.applyMsg = friendly(err)
			}, nil
		}
		return func() {
			g.applyPct = 0
			g.applyStage = ""
			if res.Error != "" {
				g.applyErr = true
				g.applyMsg = res.Error
				return
			}
			if res.Noop {
				g.applyMsg = g.t("已经是最新版本，没有需要替换的。", "Already up to date: nothing to replace.")
				return
			}
			if target == "cli" {
				g.reloadTools()
				g.applyMsg = g.t("CLI 已升级完成。", "The CLI is up to date.")
				return
			}
			// 已经替换完成：必须把“可升级”降下来，否则页面继续画“升级界面”，
			// 再点一次会重新下载 + 再替换一次（正在运行的这个进程永远是旧版本号）。
			g.upd.GUI.UpdateAvailable = false
			g.upd.GUI.Restart = true
			g.applyMsg = g.t("界面已替换：托盘菜单 → 退出 → 重新打开就是新版本。",
				"The app was replaced: tray menu → Quit → reopen to run the new version.")
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
