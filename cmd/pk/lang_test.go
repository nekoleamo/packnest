package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode"
)

// runCLI 跑一次命令并合并 stdout/stderr（语言测试关心"用户看到什么"）。
func runCLI(t *testing.T, env map[string]string, args ...string) (string, int) {
	t.Helper()
	t.Setenv("PK_ROOT", t.TempDir())
	t.Setenv("PK_LANG", "")
	t.Setenv("NO_COLOR", "1")
	for k, v := range env {
		t.Setenv(k, v)
	}
	langEN = false // 全局开关：每个用例从默认开始
	t.Cleanup(func() { langEN = false })
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return out.String() + errOut.String(), code
}

func hasHan(t *testing.T, s string) bool {
	t.Helper()
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// TestAcceptance_Lang_DefaultChinese：不设任何变量时默认中文（不能静默变英文）。
func TestAcceptance_Lang_DefaultChinese(t *testing.T) {
	out, code := runCLI(t, nil, "uninstall")
	if code != 2 {
		t.Fatalf("用法错误应退出 2，得到 %d", code)
	}
	if !hasHan(t, out) {
		t.Fatalf("默认应为中文输出:\n%s", out)
	}
}

// TestAcceptance_Lang_HelpEnglish：--lang=en 时 help 数据走 help/en。
func TestAcceptance_Lang_HelpEnglish(t *testing.T) {
	out, code := runCLI(t, nil, "--lang=en", "help", "install")
	if code != 0 {
		t.Fatalf("help 应成功: %d\n%s", code, out)
	}
	if hasHan(t, out) {
		t.Fatalf("英文 help 里还有中文:\n%s", out)
	}
	if !strings.Contains(out, "Install tools") {
		t.Fatalf("help/en/install.txt 没被用上:\n%s", out)
	}
}

// TestAcceptance_Lang_CommandsEnglish：CLI 层命令输出无汉字（internal 的 mirror 描述除外，见方案 §二）。
func TestAcceptance_Lang_CommandsEnglish(t *testing.T) {
	cases := [][]string{
		{"version"}, {"list"}, {"doctor"}, {"env", "--shell", "bash"}, {"help"},
		{"gc", "--dry-run"}, {"upgrade", "--dry-run"}, {"which", "node"}, {"mirror"}, {"mirror", "use"},
		{"uninstall"}, {"use"}, {"help", "list"}, {"help", "doctor"},
	}
	for _, args := range cases {
		out, _ := runCLI(t, nil, append([]string{"--lang=en"}, args...)...)
		if hasHan(t, out) {
			t.Fatalf("pk --lang=en %s 仍有中文:\n%s", strings.Join(args, " "), out)
		}
	}
}

// TestAcceptance_Lang_ErrorsEnglish：CLI 层的用法错误与建议是英文，退出码不变。
func TestAcceptance_Lang_ErrorsEnglish(t *testing.T) {
	out, code := runCLI(t, nil, "--lang=en", "uninstall")
	if code != 2 {
		t.Fatalf("退出码应为 2，得到 %d", code)
	}
	if hasHan(t, out) {
		t.Fatalf("用法错误应是英文:\n%s", out)
	}
	if !strings.Contains(out, "usage:") {
		t.Fatalf("缺少英文用法提示:\n%s", out)
	}
}

// TestLangFlagPriority：命令行 > 环境变量。
func TestLangFlagPriority(t *testing.T) {
	out, _ := runCLI(t, map[string]string{"PK_LANG": "en"}, "--lang=zh", "uninstall")
	if !hasHan(t, out) {
		t.Fatalf("--lang=zh 应压过 PK_LANG=en:\n%s", out)
	}
	out, _ = runCLI(t, map[string]string{"PK_LANG": "en"}, "uninstall")
	if hasHan(t, out) {
		t.Fatalf("PK_LANG=en 应生效:\n%s", out)
	}
}

// TestLangInvalidValue：非法值按用法错误（退出码 2），不静默回退。
func TestLangInvalidValue(t *testing.T) {
	out, code := runCLI(t, nil, "--lang=fr", "version")
	if code != 2 {
		t.Fatalf("非法语言应退出 2，得到 %d:\n%s", code, out)
	}
	if !strings.Contains(out, "en") || !strings.Contains(out, "zh") {
		t.Fatalf("提示应说明可选值:\n%s", out)
	}
}

// TestLangJSONUnaffected：--json 结构不因语言改变。
func TestLangJSONUnaffected(t *testing.T) {
	out, code := runCLI(t, nil, "--json", "--lang=en", "version")
	if code != 0 {
		t.Fatalf("version --json 应成功: %d\n%s", code, out)
	}
	if !strings.Contains(out, `"schema": 1`) || hasHan(t, out) {
		t.Fatalf("--json 输出不该受语言影响:\n%s", out)
	}
}
