package main

// 设置页：开机自启、两个版本号、路径、"这个界面是怎么拿到数据的"。

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (g *gui) pageSettings(c *ui.Context) {
	g.head(c, "设置", "Settings", "", nil)

	// 开机自启（只动用户级条目；不支持的平台直接说清楚，不画一个假的开关）
	g.card(c, func() {
		ui.Text(c, g.t("开机自启", "Launch at login")).Bold()
		if g.auto.Supported {
			enabled := g.auto.Enabled
			if ui.Switch(c, &enabled).Changed() {
				g.setAutostart(enabled)
			}
			ui.Text(c, g.auto.Path).Font("monospace").SingleLine().TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, orDash(g.auto.Note)).TextColor(c.Theme().TextMuted)
		}
	})

	// 版本
	g.card(c, func() {
		ui.Text(c, g.t("版本", "Versions")).Bold()
		cli := g.t("未安装", "not installed")
		if g.status.OK && g.status.CLI != nil {
			cli = g.status.CLI.Version
		}
		g.kv(c, "GUI", guiVersion, false)
		g.kv(c, "pk (CLI)", cli, false)
		g.kv(c, g.t("平台", "Platform"), platformHint(), false)
		if g.status.Path != "" {
			g.kv(c, g.t("CLI 位置", "CLI path"), g.status.Path, true)
		}
	})

	// 数据怎么来的（排障时第一句要问的）
	g.card(c, func() {
		ui.Text(c, g.t("这个界面怎么拿数据", "Where the data comes from")).Bold()
		g.note(c, "全部通过 `pk <命令> --json` 子进程：界面本身不读 PK_ROOT、不改 pk 的任何文件。",
			"Everything goes through `pk <cmd> --json` subprocesses: this app never reads PK_ROOT and never edits pk's files.")
		g.kv(c, g.t("偏好文件", "Preferences"), prefsPath(), true)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if ui.Button(c, g.t("打开偏好目录", "Open preferences folder")).Clicked() {
				_ = mygo.Shell.OpenPath(appDir())
			}
			if ui.Button(c, g.t("在浏览器里打开", "Open in browser")).Clicked() {
				_ = mygo.Shell.OpenExternal("http://127.0.0.1:7799/")
			}
		})
	})

	// 说明
	g.card(c, func() {
		ui.Text(c, g.t("说明", "About")).Bold()
		g.note(c, "packnest 是只解决国内网络环境的多语言运行时安装器；这个界面是它的可视化补充，必须先装 pk。",
			"packnest installs language runtimes with China-friendly mirrors; this app is its visual companion and needs the CLI.")
		ui.Link(c, g.t("官网与文档", "Site and docs"), "https://nekoleamo.github.io/packnest/")
	})
}

func (g *gui) setAutostart(enable bool) {
	g.load("autostart", func() (commit, error) {
		st, err := g.svc.SetAutostart(enable)
		if err != nil {
			return nil, err
		}
		return func() { g.auto = st }, nil
	})
}

// copyText 把一段文字放剪贴板并给一条提示（所有“复制”按钮共用）。
func (g *gui) copyText(s string) {
	if s == "" {
		return
	}
	mygo.Clipboard.WriteText(s)
	g.flashNow(g.t("已复制", "Copied"))
}
