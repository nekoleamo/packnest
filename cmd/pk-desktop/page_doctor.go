package main

// 体检页：跑 `pk doctor`，把 issues 列出来；`--fix` 只修"能安全自动修"的那几类
// （PATH 托管块与 pk/packnest 入口），修完给"怎么让改动生效"的建议。

import (
	"context"

	"github.com/egoist/mygo/ui"
)

func (g *gui) pageDoctor(c *ui.Context) {
	g.head(c, "体检", "Doctor", "doctor", func() { g.runDoctor(false) })

	ui.Row(c).Fill().Gap(8).Children(func() {
		ui.Checkbox(c, &g.dGlobals, g.t("检查全局包", "Check global packages"))
		ui.Checkbox(c, &g.dProbe, g.t("实测原生模块", "Probe native modules"))
		if ui.PrimaryButton(c, g.t("开始体检", "Run doctor")).Clicked() {
			g.runDoctor(false)
		}
		ui.Button(c, g.t("修复入口与 PATH", "Fix shims and PATH")).OnClick(func() { g.askFix() })
	})

	if len(g.doctor.Issues) == 0 {
		if g.busy["doctor"] {
			ui.Spinner(c)
		} else {
			ui.Text(c, g.t("没问题。", "No issues.")).TextColor(c.Theme().Success)
		}
	} else {
		ui.Textf(c, g.t("%d 个问题", "%d issues"), len(g.doctor.Issues)).Bold()
		rows := make([][3]string, 0, len(g.doctor.Issues))
		for _, is := range g.doctor.Issues {
			rows = append(rows, [3]string{is.Severity, is.Code, is.Message})
		}
		cols := []ui.TableColumn{
			{Title: g.t("级别", "Severity"), Width: 80},
			{Title: g.t("代码", "Code"), Width: 220},
			{Title: g.t("说明", "Message")},
		}
		tl := g.table("issues")
		tl.Key = func(i int) any { return rows[i][1] + rows[i][2] }
		ui.Table(c, tl, cols, len(rows), func(row, col int) {
			ui.Text(c, rows[row][col]).SingleLine()
		})
	}

	if len(g.doctor.Fixed) > 0 {
		ui.Text(c, g.t("这次修好的：", "Fixed this run:")).Bold()
		for _, f := range g.doctor.Fixed {
			ui.Text(c, "✓ "+f).TextColor(c.Theme().Success).SingleLine()
		}
	}

	if r := g.doctor.Reload; r != nil && r.Needed {
		ui.Column(c).Fill().Gap(6).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
			ui.Text(c, g.t("让改动生效", "Make it take effect")).Bold()
			if r.Msg != "" {
				ui.Text(c, r.Msg).TextColor(c.Theme().TextMuted)
			}
			if r.Command != "" {
				codeBlock(c, r.Command)
				if ui.Button(c, g.t("复制命令", "Copy command")).Clicked() {
					g.copyText(r.Command)
				}
			}
		})
	}
}

func (g *gui) runDoctor(fix bool) {
	globals, probe := g.dGlobals, g.dProbe
	g.load("doctor", func() (commit, error) {
		res, err := g.svc.Doctor(context.Background(), globals, probe, fix)
		if err != nil {
			return nil, err
		}
		return func() { g.doctor = res }, nil
	})
}

// askFix 先确认再改：--fix 会写 shell rc 的托管块与 pk 入口，属于"改用户环境"。
func (g *gui) askFix() {
	g.ask(g.t("修复入口与 PATH？", "Fix shims and PATH?"),
		g.t("只会改写 pk 自己管理的块（带 # managed by pk 标记）与 pk/packnest 入口，别的行不动；其它问题只报不改。",
			"Only the block pk manages (marked # managed by pk) and the pk/packnest shims are rewritten; nothing else is touched."),
		func() { g.runDoctor(true) })
}
