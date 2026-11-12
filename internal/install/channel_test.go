package install

import (
	"strings"
	"testing"
)

// TestDetectInstallChannel 覆盖各渠道识别（纯函数，跨平台同一套规则）。
func TestDetectInstallChannel(t *testing.T) {
	cases := []struct {
		exe, goos, want string
	}{
		{"/opt/homebrew/Cellar/packnest/0.9.0/bin/pk", "darwin", "brew"},
		{`C:\Users\me\AppData\Roaming\npm\node_modules\packnest\pk.exe`, "windows", "npm"},
		{`C:\Users\me\scoop\apps\packnest\current\pk.exe`, "windows", "scoop"},
		{`C:\Users\me\AppData\Local\packnest\bin\pk.exe`, "windows", "script-windows"},
		{"/home/me/.local/bin/pk", "linux", "script-posix"},
		{"/home/me/.packnest/bin/pk", "linux", "script-posix"},
		{"/tmp/whatever/pk", "linux", "manual"},
	}
	for _, c := range cases {
		got := DetectInstallChannel(c.exe, c.goos)
		if got.Name != c.want {
			t.Errorf("%s (%s) → %q，期望 %q", c.exe, c.goos, got.Name, c.want)
		}
		if got.Hint == "" {
			t.Errorf("%s 应该给出可照抄的命令", c.exe)
		}
	}
	// manual 渠道要把可执行文件路径带进提示里，用户才知道删哪个
	if got := DetectInstallChannel("/tmp/whatever/pk", "linux"); !strings.Contains(got.Hint, "/tmp/whatever/pk") {
		t.Errorf("manual 提示应包含路径: %q", got.Hint)
	}
}

// TestSelfRemoveHint 按平台给出删 pk 自己的命令。
func TestSelfRemoveHint(t *testing.T) {
	if got := SelfRemoveHint("/usr/local/bin", "darwin"); !strings.Contains(got, "rm -f") {
		t.Fatalf("unix 提示不对: %q", got)
	}
	if got := SelfRemoveHint(`C:\Apps\pk`, "windows"); !strings.Contains(got, "Remove-Item") {
		t.Fatalf("windows 提示不对: %q", got)
	}
}
