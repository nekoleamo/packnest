package present

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/jsonout"
)

// TestHumanBytes 锁单位换算：列表与进度条都靠它，改了会立刻体现在人读快照里。
func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:            "0 B",
		512:          "512 B",
		1536:         "1.5 KB",
		47 * 1 << 20: "47.0 MB",
		5 << 30:      "5.0 GB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q，期望 %q", in, got, want)
		}
	}
}

// TestGroupNonTTY 非 TTY：每工具按 25% 里程碑逐行，绝不写 ANSI 控制符与 \r。
func TestGroupNonTTY(t *testing.T) {
	var buf bytes.Buffer
	g := newGroupWithMode(&buf, []string{"node", "pnpm"}, false, false)
	for _, label := range []string{"node", "pnpm"} {
		cb := g.For(label)
		cb(0, 100)
		cb(25, 100)
		cb(100, 100)
	}
	g.Done()
	out := buf.String()
	if strings.ContainsAny(out, "\r\x1b") {
		t.Fatalf("非 TTY 不该写 \\r 或 ANSI: %q", out)
	}
	if n := strings.Count(out, "\n"); n != 4 { // 每工具 25% + 100% 两行
		t.Fatalf("应打印 4 行，得到 %d: %q", n, out)
	}
	if !strings.Contains(out, "node 25 B/100 B  25%") || !strings.Contains(out, "pnpm 100 B/100 B 100%") {
		t.Fatalf("进度行内容不对: %q", out)
	}
}

// TestGroupANSI TTY + ANSI：整块上移重绘，行数等于工具数，Done 收尾补换行。
func TestGroupANSI(t *testing.T) {
	var buf bytes.Buffer
	g := newGroupWithMode(&buf, []string{"node", "pnpm"}, true, true)
	g.For("node")(100, 100)
	g.For("pnpm")(100, 100)
	g.Done()
	out := buf.String()
	if !strings.Contains(out, "\x1b[2A") {
		t.Fatalf("第二次重绘应上移两行: %q", out)
	}
	if strings.Count(out, "\x1b[2K") != 4 { // 两次重绘 × 两行
		t.Fatalf("清行次数不对: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("Done 应补一个换行: %q", out)
	}
}

// TestGroupNil --quiet / --json 时 group 为 nil：For 返回 nil，调用方据此不挂回调。
func TestGroupNil(t *testing.T) {
	var g *ProgressGroup
	if cb := g.For("node"); cb != nil {
		t.Fatal("nil group 的 For 应返回 nil")
	}
	g.Done() // 不该 panic
}

// TestGroupUnknownTool 没登记的工具（伴生包下载）也补一行，不丢进度。
func TestGroupUnknownTool(t *testing.T) {
	var buf bytes.Buffer
	g := newGroupWithMode(&buf, []string{"node"}, false, false)
	g.For("npm")(1<<20, 0)
	g.Done()
	if !strings.Contains(buf.String(), "npm 1.0 MB") {
		t.Fatalf("未登记工具应补一行: %q", buf.String())
	}
}

// TestAnsiSupported 只要求"不 panic 且 TERM=dumb 一定关"（其余取决于机器）。
func TestAnsiSupported(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if ansiSupported() {
		t.Fatal("TERM=dumb 必须关掉 ANSI")
	}
}

// TestLangRoundTrip 语言是全局开关：SetLang 解析要宽松（zh/cn/zh-cn 都是中文）。
func TestLangRoundTrip(t *testing.T) {
	t.Cleanup(func() { _ = SetLang("zh") })
	for _, v := range []string{"", "zh", "cn", "zh-CN", "chinese"} {
		if err := SetLang(v); err != nil || LangIsEN() {
			t.Fatalf("SetLang(%q) 应为中文: err=%v en=%v", v, err, LangIsEN())
		}
	}
	for _, v := range []string{"en", "en-US", "English"} {
		if err := SetLang(v); err != nil || !LangIsEN() {
			t.Fatalf("SetLang(%q) 应为英文: err=%v en=%v", v, err, LangIsEN())
		}
	}
	if err := SetLang("fr"); err == nil {
		t.Fatal("未知语言应报错")
	}
}

// TestNewGroupRealDevice 真字符设备（os.DevNull）上跑一遍：能建组、能画、Done 幂等。
// 这台机器上 /dev/null 不是字符设备时跳过（TTY/ANSI 逻辑已由上面的模式注入用例覆盖）。
func TestNewGroupRealDevice(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip("没有可写的字符设备")
	}
	defer f.Close()
	g := NewGroup(f, []string{"node"})
	if !g.isTTY {
		t.Skip("os.DevNull 在这台机器上不是字符设备")
	}
	g.For("node")(0, 0)
	g.For("node")(1<<20, 0)
	g.For("node")(100, 100)
	g.Done()
	g.Done()
}

// TestIssues 两种分支：没问题 / 逐行渲染（级别定宽，问题码定宽）。
func TestIssues(t *testing.T) {
	var buf bytes.Buffer
	Issues(&buf, nil)
	if !strings.Contains(buf.String(), "体检通过") {
		t.Fatalf("空问题集应打印通过: %q", buf.String())
	}
	buf.Reset()
	Issues(&buf, []jsonout.Issue{
		{Code: "PATH_ORDER", Severity: jsonout.SeverityWarn, Message: "不在 PATH 最前"},
		{Code: "LEDGER_BROKEN", Severity: jsonout.SeverityErr, Message: "台账坏了"},
	})
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 行，得到 %d: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "  [warn ] PATH_ORDER") {
		t.Fatalf("第一行格式不对: %q", lines[0])
	}
}

// TestFixed 两种分支：没得修 / 修了 N 条 + 复核提示。
func TestFixed(t *testing.T) {
	var buf bytes.Buffer
	Fixed(&buf, nil)
	if !strings.Contains(buf.String(), "没有可自动修") {
		t.Fatalf("空列表提示不对: %q", buf.String())
	}
	buf.Reset()
	Fixed(&buf, []string{"PATH 托管块", "pk 入口"})
	out := buf.String()
	if !strings.Contains(out, "已修 PATH 托管块") || !strings.Contains(out, "重跑 `pk doctor`") {
		t.Fatalf("修复输出不对: %q", out)
	}
}

// TestInstalledTable 覆盖表格的四种后缀：npm（自带/替换）、未校验、外部清单。
func TestInstalledTable(t *testing.T) {
	var buf bytes.Buffer
	InstalledTable(&buf, []jsonout.ToolEntry{
		{Name: "node", Version: "22.22.2", Active: true, SizeBytes: 47 << 20,
			NPM: &jsonout.ToolNPM{Version: "12.2.2", Replaced: "11.20.0"}},
		{Name: "pnpm", Version: "10.4.1", Verified: "unverified", Source: "external"},
	})
	out := buf.String()
	for _, want := range []string{
		"已装:",
		"* node   22.22.2     47.0 MB (npm 12.2.2 ← 自带 11.20.0)",
		"  pnpm   10.4.1          0 B [未校验] *",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
}

// TestUpgrades 可升级表：工具名定宽 + 版本箭头 + 建议。
func TestUpgrades(t *testing.T) {
	var buf bytes.Buffer
	Upgrades(&buf, []jsonout.OutdatedEntry{{Name: "node", Installed: "22.22.2", Latest: "24.15.0", Advice: "pk upgrade node"}})
	out := buf.String()
	if !strings.Contains(out, "可升级") || !strings.Contains(out, "22.22.2 → 24.15.0   pk upgrade node") {
		t.Fatalf("可升级输出不对: %q", out)
	}
}

// TestGroupThreeToolsDistinct（E4 的验收点）：三工具并行时三行各走各的百分比，
// 谁也不许覆盖谁的字节数。
func TestGroupThreeToolsDistinct(t *testing.T) {
	var buf bytes.Buffer
	g := newGroupWithMode(&buf, []string{"node", "pnpm", "bun"}, false, false)
	g.For("node")(50, 100)
	g.For("pnpm")(25, 100)
	g.For("bun")(100, 100)
	g.Done()
	out := buf.String()
	for _, want := range []string{
		"node 50 B/100 B  50%",
		"pnpm 25 B/100 B  25%",
		"bun 100 B/100 B 100%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q:\n%s", want, out)
		}
	}
}
