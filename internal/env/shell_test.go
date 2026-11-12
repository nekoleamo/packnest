package env

import (
	"strings"
	"testing"
)

// Windows 上没有 $SHELL：检测必须落到 powershell，否则 env --write 会把 POSIX 片段
// 写进 ~/.bashrc（Windows 上没人读），用户就是"写了托管块还是不生效"。
func TestDetectShellFor(t *testing.T) {
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"Linux bash", "linux", map[string]string{"SHELL": "/bin/bash"}, "bash"},
		{"macOS zsh", "darwin", map[string]string{"SHELL": "/bin/zsh"}, "zsh"},
		{"Windows 无 SHELL", "windows", map[string]string{}, "powershell"},
		{"Windows 但 SHELL 是 git-bash", "windows", map[string]string{"SHELL": "/usr/bin/bash"}, "bash"},
		{"SHELL 直接写 pwsh", "windows", map[string]string{"SHELL": "/usr/bin/pwsh"}, "powershell"},
		{"Linux 没 SHELL 时仍回落 bash", "linux", map[string]string{}, "bash"},
	}
	for _, c := range cases {
		got := DetectShellFor(c.goos, func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%s: detectShellFor = %q，期望 %q", c.name, got, c.want)
		}
	}
}

// `pk env --write` 的目标文件：Windows 上必须是 PowerShell profile（5.1/7 都可能），
// 绝不能写 ~/.bashrc —— 那正是用户"写了托管块、新开 shell 还是不识别 node"的根因。
func TestEnvWriteTargets(t *testing.T) {
	home := t.TempDir()
	noEnv := func(string) string { return "" }
	ps7 := func(k string) string {
		if k == "PSModulePath" {
			return "C:/Program Files/PowerShell/7/Modules"
		}
		return ""
	}

	if got := WriteTargets("", ShellPowerShell, "windows", home, ps7); len(got) != 1 ||
		!strings.Contains(got[0], "PowerShell") {
		t.Fatalf("Windows + pwsh 应写 PowerShell profile，得到 %v", got)
	}
	// 判定不出（从 cmd.exe 里跑）→ 5.1 与 7 两份都写
	got := WriteTargets("", ShellPowerShell, "windows", home, noEnv)
	if len(got) != 2 || !strings.Contains(got[0], "WindowsPowerShell") || !strings.Contains(got[1], "PowerShell") {
		t.Fatalf("判定不出时两份都要写，得到 %v", got)
	}
	// --rc 优先
	if got := WriteTargets("/tmp/x.rc", ShellPowerShell, "windows", home, ps7); len(got) != 1 || got[0] != "/tmp/x.rc" {
		t.Fatalf("--rc 应优先，得到 %v", got)
	}
	// Git Bash 用户（$SHELL 是 bash）：仍按 posix 写
	got = WriteTargets("", ShellPOSIX, "windows", home, func(k string) string {
		if k == "SHELL" {
			return "/usr/bin/bash"
		}
		return ""
	})
	if len(got) != 1 || !strings.HasSuffix(got[0], ".bashrc") {
		t.Fatalf("git-bash 应写 .bashrc，得到 %v", got)
	}
	// macOS 不变
	got = WriteTargets("", ShellPOSIX, "darwin", home, func(k string) string {
		if k == "SHELL" {
			return "/bin/zsh"
		}
		return ""
	})
	if len(got) != 1 || !strings.HasSuffix(got[0], ".zshrc") {
		t.Fatalf("macOS zsh 应写 .zshrc，得到 %v", got)
	}
}
