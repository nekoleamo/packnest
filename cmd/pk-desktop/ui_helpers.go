package main

// 跨页共用的小件：区块标题、键值行、文本小工具。
// 只放"两页以上都要用"的东西，单页用的留在各自文件里。

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// installEvent 是 `pk install --json` 的一行（别名，免得每页都写包名）。
type installEvent = pkcli.Event

// head 画一页的标题行：标题 + 忙指示 + 可选的刷新按钮。
func (g *gui) head(c *ui.Context, zh, en, busyKey string, reload func()) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, g.t(zh, en)).FontSize(15).Bold()
		if busyKey != "" && g.busy[busyKey] {
			ui.Spinner(c)
		}
		ui.Row(c).Grow(1).Children(func() {})
		if reload != nil {
			ui.Button(c, g.t("刷新", "Refresh")).FontSize(12).OnClick(reload)
		}
	})
}

// kv 是一行"标签：值"（值可复制时给一个复制按钮）。
func (g *gui) kv(c *ui.Context, label, value string, copyable bool) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, label).TextColor(c.Theme().TextMuted).Width(96)
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
	// 用 List + Row 而不是 Table：mygo 0.9.1 的 Table 在真实窗口（GPU）里只画表头，
	// ui.Tester 里却正常 —— 这类"只有窗口里错"的问题只能靠 List 绕开（2026-11-15 实测）。
	// 高度必须显式给：放在 Scroll 里没有高度约束时 List 会被压成一行。
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, g.t("键", "Key")).TextColor(c.Theme().TextMuted).Width(190)
		ui.Text(c, g.t("值", "Value")).TextColor(c.Theme().TextMuted)
	})
	ui.List(c, list, len(rows), func(i int) {
		ui.Row(c).FillWidth().Gap(8).Padding(4).Children(func() {
			ui.Text(c, rows[i][0]).Width(190).SingleLine()
			ui.Text(c, rows[i][1]).SingleLine().Grow(1)
		})
	}).Height(200)
}

// codeBlock 是等宽的一行命令/路径（长文本截断，复制走 kv 的按钮）。
func codeBlock(c *ui.Context, s string) {
	ui.Text(c, s).Font("monospace").SingleLine().TextColor(c.Theme().TextMuted)
}

// card 是"信息块"的统一外观：浅底 + 圆角 + 细边线。
//
// 深色下光靠 Surface 与 Background 的色差分不开卡片，边线才是分层的那根线；
// 顺带把 GAP/内边距收成一处，卡片之间不再忽大忽小。
func (g *gui) card(c *ui.Context, fn func()) {
	ui.Column(c).FillWidth().Gap(6).Padding(12).
		Background(c.Theme().Surface).Radius(8).
		Border(1, c.Theme().Border).Children(fn)
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

// humanBytes 把字节数写成人读的（GC 统计块用）。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// since 是"多久以前"，用于首屏的时间戳。
func since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t).Truncate(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return d.Truncate(time.Minute).String()
}

// platformHint 在设置页显示（排障时第一句要问的）。
func platformHint() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// queuePending 是"还没跑完的 spec 数"（侧边栏徽标）。
func (g *gui) queuePending() int {
	n := 0
	for _, q := range g.queue {
		if q.State == "wait" || q.State == "run" {
			n++
		}
	}
	return n
}
