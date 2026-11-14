// Package present 是 CLI 的呈现层：表格、进度条、问题码行、文案翻译。
//
// 为什么要单独一层（完善路线 §E6）：`cmd/pk` 之前同时干三件事 —— 解析参数、编排、把结果
// 画成终端文本。于是"改文案要动命令实现""加一行进度要碰 install 的调用点""做英文覆盖
// 要满仓库找 tr("。把呈现收在这里之后：
//   - `cmd/pk` 只剩参数与退出码（编排在 internal/install，早就在了）；
//   - E4（多行 TTY 进度）、E5（--atomic 的事件文案）、E1（英文覆盖）都只改这一个包；
//   - 人读输出有快照兜底（cmd/pk/testdata/human），搬家必须逐字节一致。
//
// 这一层不许反向依赖 `cmd/pk`：只 import `internal/*`，且只用它的数据结构（不做业务判断）。
package present

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/packnest/packnest/internal/jsonout"
)

// ── 语言与文案 ──────────────────────────────────────────────────────────

// langEN 为 true 时所有 T(zh, en) 取英文；默认中文（不跟随 LANG，避免 CI 日志静默变英文）。
var langEN bool

// T 是唯一的翻译入口：中文在前、英文在后。不做 key 表 —— 预算与心智成本都不划算，
// 收益只是"文案集中"，代价是代码里全是 key。E1 若要全量英文化，改的就是这一个函数。
func T(zh, en string) string {
	if langEN {
		return en
	}
	return zh
}

// SetLang 解析 --lang / PK_LANG（命令行在 cmd/pk 的 parseGlobals 里优先）。
func SetLang(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "zh", "cn", "zh-cn", "zh_cn", "chinese":
		langEN = false
	case "en", "en-us", "en_us", "english":
		langEN = true
	default:
		return fmt.Errorf("--lang 只接受 en|zh（得到 %q）; --lang accepts en|zh", v)
	}
	return nil
}

// LangIsEN 告诉调用方当前是不是英文（嵌入式帮助文本是按语言分开存的，需要查表）。
func LangIsEN() bool { return langEN }

// InitLangFromEnv 在解析命令行之前读 PK_LANG。
func InitLangFromEnv() error {
	if v, ok := os.LookupEnv("PK_LANG"); ok && v != "" {
		return SetLang(v)
	}
	return nil
}

// ── 尺寸 ────────────────────────────────────────────────────────────────

// HumanBytes 把字节数变成 1.2 MB 这种人读形式（列表与进度条共用同一套单位）。
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

// ── 进度 ────────────────────────────────────────────────────────────────

// ProgressGroup 是"每工具一行"的下载进度（E4）。
//
// 三种渲染：
//   - TTY 且支持 ANSI：整块上移重绘（`pk install node pnpm bun` 时三行同时走）；
//   - TTY 但检测不到 ANSI（老 conhost）：退化成逐行，每 25% 一行；
//   - 非 TTY（管道 / CI 日志）：同样逐行，且绝不打 ANSI 控制符。
//
// 并发：并行安装时每个工具的 fetch.Client 是独立副本，但渲染到同一个 io.Writer，
// 所以内部加锁；同一工具的回调本身已是串行的（fetch.parallel 自己串行化）。
type ProgressGroup struct {
	out   io.Writer
	isTTY bool
	ansi  bool
	mu    sync.Mutex
	lines []*progressLine
	index map[string]int
	drawn int // 已画出的行数（ANSI 重绘要按它上移）
}

// progressLine 是一个工具的进度状态。
type progressLine struct {
	label     string
	start     time.Time
	done      int64
	total     int64
	lastAt    int64 // 非 TTY 里程碑去重（上次打印时的 done）
	wrote     bool
	lastFlush time.Time
}

// NewGroup 为一组工具建进度（labels 的顺序就是显示顺序）。
func NewGroup(out io.Writer, labels []string) *ProgressGroup {
	isTTY := false
	if f, ok := out.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			isTTY = st.Mode()&os.ModeCharDevice != 0
		}
	}
	return newGroupWithMode(out, labels, isTTY, isTTY && ansiSupported())
}

// newGroupWithMode 是测试缝：TTY/ANSI 由调用方指定，免得测试要么依赖真终端、要么测不到。
func newGroupWithMode(out io.Writer, labels []string, isTTY, ansi bool) *ProgressGroup {
	g := &ProgressGroup{out: out, isTTY: isTTY, ansi: ansi, index: map[string]int{}}
	for _, l := range labels {
		g.index[l] = len(g.lines)
		g.lines = append(g.lines, &progressLine{label: l, start: time.Now()})
	}
	return g
}

// ansiSupported 决定能不能用光标上移重绘。
//
// Windows 的老 conhost 默认不吃 ANSI（除非开了 VT 处理）：宁可退化成逐行，
// 也不要在用户终端里刷出 `←[2K` 这种乱码。
func ansiSupported() bool {
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		return false
	}
	if runtime.GOOS != "windows" {
		return true
	}
	return os.Getenv("WT_SESSION") != "" || os.Getenv("ConEmuANSI") == "ON" ||
		os.Getenv("TERM_PROGRAM") != ""
}

// For 返回某个工具的进度回调（闭包 tool：worker 的 Client 副本各自挂一个）。
// g 为 nil（--quiet / --json）时返回 nil，调用方直接判断即可。
func (g *ProgressGroup) For(tool string) func(done, total int64) {
	if g == nil {
		return nil
	}
	return func(done, total int64) { g.update(tool, done, total) }
}

// update 记一次进度；是否需要真的画由节流规则决定。
func (g *ProgressGroup) update(tool string, done, total int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i, ok := g.index[tool]
	if !ok { // 没登记的工具（例如伴生包的 npm 下载）：补一行，别丢进度
		g.index[tool] = len(g.lines)
		g.lines = append(g.lines, &progressLine{label: tool, start: time.Now()})
		i = g.index[tool]
	}
	l := g.lines[i]
	l.done, l.total = done, total
	now := time.Now()
	if g.ansi {
		// ANSI 重绘整块：80ms 内只画一次，最后一行（done==total）必画
		if now.Sub(l.lastFlush) < 80*time.Millisecond && done != total {
			return
		}
		l.lastFlush = now
		g.drawANSI()
		return
	}
	// 非 ANSI / 非 TTY：每 25% 一行（未知总长时每 1MB 才打一次）
	if total > 0 {
		step := total / 4
		if step > 0 && done-l.lastAt < step && done != total {
			return
		}
	} else if l.wrote || done < 1<<20 {
		return
	}
	if done == l.lastAt && !l.wrote {
		return
	}
	l.lastAt, l.wrote = done, true
	fmt.Fprintln(g.out, progressText(l))
}

// drawANSI 上移重画整块（块内行数 = 已登记的工具数）。
func (g *ProgressGroup) drawANSI() {
	if g.drawn > 0 {
		fmt.Fprintf(g.out, "\x1b[%dA", g.drawn)
	}
	for _, l := range g.lines {
		fmt.Fprintf(g.out, "\r\x1b[2K%s\n", progressText(l))
	}
	g.drawn = len(g.lines)
}

// Done 收尾：ANSI 模式下把光标放回块尾（否则后续日志会覆盖最后一行）。
func (g *ProgressGroup) Done() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ansi && g.drawn > 0 {
		fmt.Fprint(g.out, "\n")
		g.drawn = 0
	}
}

// progressText 拼一行进度文案（单/多工具共用，改格式只改这里）。
func progressText(l *progressLine) string {
	elapsed := time.Since(l.start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(l.done) / elapsed / (1 << 20)
	}
	if l.total > 0 {
		pct := float64(l.done) / float64(l.total) * 100
		return fmt.Sprintf("%s %s/%s %3.0f%% (%.1f MB/s)",
			l.label, HumanBytes(l.done), HumanBytes(l.total), pct, speed)
	}
	return fmt.Sprintf("%s %s (%.1f MB/s)", l.label, HumanBytes(l.done), speed)
}

// ── 列表 ────────────────────────────────────────────────────────────────

// InstalledTable 打印"已装"表：一行一个工具，`*` 标当前生效版本，后面追加 npm 信息。
func InstalledTable(w io.Writer, entries []jsonout.ToolEntry) {
	fmt.Fprintln(w, T("已装:", "installed:"))
	for _, e := range entries {
		mark := " "
		if e.Active {
			mark = "*"
		}
		line := fmt.Sprintf("  %s %-6s %-10s %8s", mark, e.Name, e.Version, HumanBytes(e.SizeBytes))
		if e.NPM != nil && e.NPM.Version != "" {
			line += fmt.Sprintf(" (npm %s", e.NPM.Version)
			if e.NPM.Replaced != "" && e.NPM.Replaced != e.NPM.Version {
				line += fmt.Sprintf(T(" ← 自带 %s", " <- bundled %s"), e.NPM.Replaced)
			}
			line += ")"
		}
		if e.Verified == "unverified" {
			line += T(" [未校验]", " [unverified]")
		}
		if e.Source == "external" {
			line += " *"
		}
		fmt.Fprintln(w, line)
	}
}

// Upgrades 打印"可升级"提示（只读索引缓存，不联网）。
func Upgrades(w io.Writer, hints []jsonout.OutdatedEntry) {
	fmt.Fprintln(w, T("可升级（索引缓存；详情 `pk list --available`）:", "upgradable (index cache; see `pk list --available`):"))
	for _, h := range hints {
		fmt.Fprintf(w, "  %-6s %s → %s   %s\n", h.Name, h.Installed, h.Latest, h.Advice)
	}
}

// ── doctor ──────────────────────────────────────────────────────────────

// Issues 打印体检结果：每行 `[级别] 问题码 说明`（问题码宽度固定，方便肉眼扫）。
func Issues(w io.Writer, issues []jsonout.Issue) {
	if len(issues) == 0 {
		fmt.Fprintln(w, T("体检通过：没有发现问题。", "check passed: no problems found."))
		return
	}
	for _, is := range issues {
		fmt.Fprintf(w, "  [%-5s] %-22s %s\n", is.Severity, is.Code, is.Message)
	}
}

// Fixed 打印 `doctor --fix` 的结果。
func Fixed(w io.Writer, fixed []string) {
	if len(fixed) == 0 {
		fmt.Fprintln(w, T("没有可自动修的问题（其余项需要人工处理）", "nothing can be auto-fixed (the rest needs manual work)"))
		return
	}
	for _, line := range fixed {
		fmt.Fprintf(w, T("  已修 %s\n", "  fixed %s\n"), line)
	}
	fmt.Fprintln(w, T("重跑 `pk doctor` 复核；新开窗口后 PATH 生效", "rerun `pk doctor` to verify; PATH takes effect in a new window"))
}
