package main

// 跨页共用的小件：区块标题、键值行、文本小工具。
// 只放"两页以上都要用"的东西，单页用的留在各自文件里。

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// installEvent 是 `pk install --json` 的一行（别名，免得每页都写包名）。
type installEvent = pkcli.Event

// head 画一页的标题行：标题 + 忙指示 + 可选的刷新按钮。
func (g *gui) head(c *ui.Context, zh, en, busyKey string, reload func()) {
	ui.Row(c).Fill().Gap(8).Children(func() {
		ui.Text(c, g.t(zh, en)).FontSize(16).Bold()
		if busyKey != "" && g.busy[busyKey] {
			ui.Spinner(c)
		}
		ui.Row(c).Grow(1).Children(func() {})
		if reload != nil && ui.Button(c, g.t("刷新", "Refresh")).Clicked() {
			reload()
		}
	})
}

// kv 是一行"标签：值"（值可复制时给一个复制按钮）。
func (g *gui) kv(c *ui.Context, label, value string, copyable bool) {
	ui.Row(c).Fill().Gap(8).Children(func() {
		ui.Text(c, label).TextColor(c.Theme().TextMuted).Width(120)
		ui.Text(c, value).SingleLine().Grow(1)
		if copyable && value != "" && ui.Button(c, g.t("复制", "Copy")).Clicked() {
			g.copyText(value)
		}
	})
}

// kvTable 把一批键值画成两列表格（环境变量这种长列表用它）。
func (g *gui) kvTable(c *ui.Context, key string, rows [][2]string) {
	list := g.table(key)
	list.Key = func(i int) any { return rows[i][0] }
	cols := []ui.TableColumn{
		{Title: g.t("键", "Key"), Width: 240},
		{Title: g.t("值", "Value")},
	}
	ui.Table(c, list, cols, len(rows), func(row, col int) {
		ui.Text(c, rows[row][col]).SingleLine()
	})
}

// codeBlock 是等宽的一行命令/路径（长文本截断，复制走 kv 的按钮）。
func codeBlock(c *ui.Context, s string) {
	ui.Text(c, s).Font("monospace").SingleLine().TextColor(c.Theme().TextMuted)
}

// note 是灰字说明。
func (g *gui) note(c *ui.Context, zh, en string) {
	ui.Text(c, g.t(zh, en)).TextColor(c.Theme().TextMuted)
}

// itoa 是 strconv.Itoa 的短名字（表格里行号用得多）。
func itoa(n int) string { return strconv.Itoa(n) }

func trimSpace(s string) string { return strings.TrimSpace(s) }

// containsFold 是大小写无关的子串判断（版本过滤用）。
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
