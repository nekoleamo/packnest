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

// TestProgressNonTTY 非 TTY（bytes.Buffer 不是 *os.File）时按里程碑逐行，且不写 \r。
func TestProgressNonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, "download")
	if p.isTTY {
		t.Fatal("bytes.Buffer 不应被当成 TTY")
	}
	p.Update(0, 100)   // 0%：step 还没到，不打印
	p.Update(25, 100)  // 25%：打印一行
	p.Update(100, 100) // 100%：打印一行
	p.Done()
	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Fatalf("非 TTY 不该写 \\r: %q", out)
	}
	if n := strings.Count(out, "\n"); n != 2 {
		t.Fatalf("非 TTY 应打印 2 行，得到 %d 行: %q", n, out)
	}
	if !strings.Contains(out, "download") || !strings.Contains(out, "100%") {
		t.Fatalf("进度行内容不对: %q", out)
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

// TestProgressTTY 用 /dev/null（字符设备）走一遍 TTY 分支：应写 \r、不清屏幕换行。
func TestProgressTTY(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip("没有可写的字符设备")
	}
	defer f.Close()
	p := NewProgress(f, "download")
	if !p.isTTY {
		t.Skip("os.DevNull 在这台机器上不是字符设备")
	}
	p.Update(0, 0)     // total=0 且 done<1MB：非 TTY 分支才判断，TTY 直接画
	p.Update(1<<20, 0) // 未知总长
	p.Update(100, 100)
	p.Done()
	p.Done() // 幂等：wrote 已复位
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
