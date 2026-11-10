package env

import (
	"os"
	"path/filepath"
	"testing"
)

// Windows profile 路径：两套 PowerShell 互不读对方 profile，只写一条就会出现
// "写了托管块、新开 shell 还是找不到 node"（用户实测 Windows PowerShell 5.1 上踩到）。
func TestWindowsProfilePaths(t *testing.T) {
	home := t.TempDir()
	envOf := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	profile := func(base, folder string) string {
		return filepath.Join(base, folder, "Microsoft.PowerShell_profile.ps1")
	}
	docs := filepath.Join(home, "Documents")

	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "PS7 只写 PowerShell 目录",
			env:  map[string]string{"PSModulePath": "C:/Program Files/PowerShell/7/Modules"},
			want: []string{profile(docs, "PowerShell")},
		},
		{
			name: "PS5.1 只写 WindowsPowerShell 目录",
			env:  map[string]string{"PSModulePath": `C:\Users\me\Documents\WindowsPowerShell\Modules`},
			want: []string{profile(docs, "WindowsPowerShell")},
		},
		{
			name: "判定不出（如从 cmd.exe 跑）两个都写，5.1 排在前面",
			env:  map[string]string{},
			want: []string{profile(docs, "WindowsPowerShell"), profile(docs, "PowerShell")},
		},
		{
			name: "POWERSHELL_DISTRIBUTION_CHANNEL 也认 pwsh",
			env:  map[string]string{"POWERSHELL_DISTRIBUTION_CHANNEL": "MSI:store"},
			want: []string{profile(docs, "PowerShell")},
		},
	}
	for _, c := range cases {
		got := WindowsProfilePaths(home, envOf(c.env))
		if len(got) != len(c.want) {
			t.Fatalf("%s: 路径数 %d，期望 %d（%v）", c.name, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: 第 %d 条 = %q，期望 %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// Documents 被 OneDrive 重定向时，要写进 OneDrive 那一份（Windows 的默认布局）。
func TestWindowsProfilePathsOneDrive(t *testing.T) {
	home := t.TempDir()
	oneDrive := filepath.Join(home, "OneDrive")
	ps7Dir := filepath.Join(oneDrive, "Documents", "PowerShell")
	if err := os.MkdirAll(ps7Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := WindowsProfilePaths(home, func(k string) string {
		switch k {
		case "OneDrive":
			return oneDrive
		case "PSModulePath":
			return "C:/Program Files/PowerShell/7/Modules"
		}
		return ""
	})
	want := filepath.Join(ps7Dir, "Microsoft.PowerShell_profile.ps1")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("应写 OneDrive 那份 profile：got %v want %s", got, want)
	}
}
