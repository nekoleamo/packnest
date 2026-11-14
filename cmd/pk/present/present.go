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
	"strings"
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

// Progress 是单行下载进度：TTY 用 \r 原地刷新，非 TTY 按里程碑逐行打印。
//
// E4（多行 TTY 进度）会在这里改成"每工具一行"的 ProgressGroup；调用方只依赖
// NewProgress/Update/Done 三个方法，所以改造不需要再动 cmd/pk 与 internal/install。
type Progress struct {
	out   io.Writer
	isTTY bool
	label string
	start time.Time
	last  time.Time
	// lastAt 是上次打印时的 done（非 TTY 的里程碑去重用）
	lastAt int64
	wrote  bool
	width  int
}

// NewProgress 判断输出是否是终端（字符设备）。
func NewProgress(out io.Writer, label string) *Progress {
	isTTY := false
	if f, ok := out.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			isTTY = st.Mode()&os.ModeCharDevice != 0
		}
	}
	return &Progress{out: out, isTTY: isTTY, label: label, start: time.Now(), width: 78}
}

// Update 由 fetch 的 Progress 回调驱动。
func (p *Progress) Update(done, total int64) {
	if p == nil {
		return
	}
	now := time.Now()
	if p.isTTY {
		if now.Sub(p.last) < 80*time.Millisecond && done != total {
			return
		}
	} else {
		// 非 TTY：每 25% 一行，避免日志爆炸
		if total > 0 {
			step := total / 4
			if step > 0 && done-p.lastAt < step && done != total {
				return
			}
		} else if p.wrote || done < 1<<20 {
			return
		}
	}
	p.last, p.lastAt, p.wrote = now, done, true
	p.render(done, total)
}

// render 打印一行/刷新一行。
func (p *Progress) render(done, total int64) {
	elapsed := time.Since(p.start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(done) / elapsed / (1 << 20)
	}
	if total > 0 {
		pct := float64(done) / float64(total) * 100
		p.line(fmt.Sprintf("%s %s/%s %3.0f%% (%.1f MB/s)",
			p.label, HumanBytes(done), HumanBytes(total), pct, speed))
		return
	}
	p.line(fmt.Sprintf("%s %s (%.1f MB/s)", p.label, HumanBytes(done), speed))
}

// line 输出一行：TTY 覆盖同一行，非 TTY 追加一行。
func (p *Progress) line(s string) {
	if p.isTTY {
		p.clear()
		fmt.Fprintf(p.out, "\r%s", s)
		return
	}
	fmt.Fprintln(p.out, s)
}

// Done 收尾（换行，避免后面的日志接在进度行后面）。
func (p *Progress) Done() {
	if p == nil || !p.wrote {
		return
	}
	if p.isTTY {
		p.clear()
	}
	p.wrote = false
}

// clear 用空格覆盖上一行（不打 ANSI 控制符，Windows 老终端也安全）。
func (p *Progress) clear() {
	fmt.Fprint(p.out, "\r"+strings.Repeat(" ", p.width)+"\r")
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
