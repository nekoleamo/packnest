package env

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
)

func writePkg(t *testing.T, dir, name, version, engines string, extra map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"` + name + `","version":"` + version + `"`
	if engines != "" {
		body += `,"engines":{"node":"` + engines + `"}`
	}
	for k, v := range extra {
		body += `,"` + k + `":` + v
	}
	body += `}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanGlobals(t *testing.T) {
	prefix := t.TempDir()
	writePkg(t, filepath.Join(prefix, "lib", "node_modules", "vite"), "vite", "7.0.0", ">=24", nil)
	writePkg(t, filepath.Join(prefix, "lib", "node_modules", "@scope", "cli"), "@scope/cli", "1.0.0", "", map[string]string{
		"bin": `{"scoped-cli":"bin/cli.js"}`,
	})
	// 原生模块（无 N-API 标记）
	nativeDir := filepath.Join(prefix, "lib", "node_modules", "sharp")
	writePkg(t, nativeDir, "sharp", "0.33.0", "", map[string]string{"gypfile": "true"})
	if err := os.WriteFile(filepath.Join(nativeDir, "binding.node"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// N-API 预编译（跨版本安全）
	napiDir := filepath.Join(prefix, "lib", "node_modules", "napi-native")
	writePkg(t, napiDir, "napi-native", "1.0.0", "", nil)
	if err := os.MkdirAll(filepath.Join(napiDir, "prebuilds", "darwin-arm64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(napiDir, "prebuilds", "darwin-arm64", "node.napi.node"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := ScanGlobals(prefix, "darwin")
	if err != nil {
		t.Fatalf("ScanGlobals: %v", err)
	}
	if len(pkgs) != 4 {
		t.Fatalf("应扫到 4 个包（含 scope），得到 %d: %+v", len(pkgs), pkgs)
	}
	byName := map[string]GlobalPackage{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}
	if byName["vite"].EnginesNode != ">=24" {
		t.Fatalf("engines 未读到: %+v", byName["vite"])
	}
	if !byName["sharp"].Native || byName["sharp"].NAPI {
		t.Fatalf("sharp 应判定为原生且非 N-API: %+v", byName["sharp"])
	}
	if !byName["napi-native"].NAPI {
		t.Fatalf("prebuilds/*.napi.node 应判定 N-API: %+v", byName["napi-native"])
	}
	if len(byName["@scope/cli"].Bins) != 1 || byName["@scope/cli"].Bins[0] != "scoped-cli" {
		t.Fatalf("bin 名未读到: %+v", byName["@scope/cli"])
	}

	// 目录不存在 → 空结果、不报错
	if pkgs, err := ScanGlobals(filepath.Join(prefix, "nope"), "darwin"); err != nil || len(pkgs) != 0 {
		t.Fatalf("缺失目录应返回空: %+v %v", pkgs, err)
	}
}

func TestCheckGlobals(t *testing.T) {
	pkgs := []GlobalPackage{
		{Name: "vite", EnginesNode: ">=24"},
		{Name: "sharp", Native: true},
		{Name: "napi-native", Native: true, NAPI: true},
	}
	problems := CheckGlobals(pkgs, "22.22.2")
	codes := map[string]bool{}
	for _, p := range problems {
		codes[p.Code] = true
	}
	if !codes["GLOBAL_ENGINES_UNMET"] || !codes["GLOBAL_NATIVE_ABI"] {
		t.Fatalf("应报 engines 与 ABI 两类问题: %+v", problems)
	}
	if len(problems) != 2 {
		t.Fatalf("N-API 包不该告警: %+v", problems)
	}
	// engines 满足时不报
	if problems := CheckGlobals([]GlobalPackage{{Name: "vite", EnginesNode: ">=20"}}, "22.22.2"); len(problems) != 0 {
		t.Fatalf("engines 满足时不该告警: %+v", problems)
	}
	// 没有生效 node 版本时跳过 engines 判断
	if problems := CheckGlobals([]GlobalPackage{{Name: "vite", EnginesNode: ">=24"}}, ""); len(problems) != 0 {
		t.Fatalf("没有 node 版本时不该判 engines: %+v", problems)
	}
}

func TestCheckShadowing(t *testing.T) {
	pkgs := []GlobalPackage{{Name: "typescript", Bins: []string{"tsc", "tsserver"}}}
	project := map[string]string{"tsc": "node_modules/.bin"}
	problems := CheckShadowing(pkgs, project)
	if len(problems) != 1 || problems[0].Code != "GLOBAL_SHADOWS_PROJECT" {
		t.Fatalf("应报命令遮蔽: %+v", problems)
	}
	if problems := CheckShadowing(pkgs, nil); len(problems) != 0 {
		t.Fatalf("没有项目时不该报: %+v", problems)
	}
}

func TestSharedVarsForPerVersion(t *testing.T) {
	p, _ := paths.New("/root")
	shared := kvs(SharedVarsFor("shared", p, "/home/u/.npm", "22.22.2"))
	if shared["npm_config_prefix"] != "/root/global" {
		t.Fatalf("shared 前缀应固定: %q", shared["npm_config_prefix"])
	}
	perVersion := kvs(SharedVarsFor("per-version", p, "/home/u/.npm", "22.22.2"))
	if perVersion["npm_config_prefix"] != "/root/global/node-22.22.2" {
		t.Fatalf("per-version 前缀应带版本: %q", perVersion["npm_config_prefix"])
	}
	if perVersion["npm_config_cache"] != "/home/u/.npm" {
		t.Fatal("缓存仍应共享")
	}
	// 没有版本时退回 shared
	fallback := kvs(SharedVarsFor("per-version", p, "/home/u/.npm", ""))
	if fallback["npm_config_prefix"] != "/root/global" {
		t.Fatalf("缺版本应退回 shared: %q", fallback["npm_config_prefix"])
	}
}

func TestRCFileFor(t *testing.T) {
	cases := []struct {
		shell                Shell
		goos, envShell, want string
	}{
		{ShellPOSIX, "darwin", "/bin/zsh", "/home/u/.zshrc"},
		{ShellPOSIX, "darwin", "/bin/bash", "/home/u/.bashrc"},
		{ShellPOSIX, "linux", "", "/home/u/.bashrc"},
		// Windows 单值版本给首选：判不出 PowerShell 版本时 5.1（系统自带）优先；
		// CLI 实际会写 WindowsProfilePaths 返回的全部路径（见 cmd 的 envWriteTargets 用例）。
		{ShellPowerShell, "windows", "", "/home/u/Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1"},
		{ShellPowerShell, "darwin", "/bin/zsh", "/home/u/.config/powershell/Microsoft.PowerShell_profile.ps1"},
	}
	for _, c := range cases {
		if got := RCFileFor(c.shell, c.goos, "/home/u", c.envShell); got != filepath.FromSlash(c.want) {
			t.Errorf("RCFileFor(%v,%s,%q) = %q，期望 %q", c.shell, c.goos, c.envShell, got, c.want)
		}
	}
}

// TestRCFileForIgnoresForeignEnv：非 Windows 上"预测 windows profile 路径"时不应读本机环境。
// CI（Linux）装着 pwsh，PSModulePath/POWERSHELL_DISTRIBUTION_CHANNEL 会让预测误判成 PS7 ——
// 2026-11-12 的门禁在 ubuntu 上就因这个红过一次。
func TestRCFileForIgnoresForeignEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("真 Windows 上读本机 PSModulePath 是预期行为")
	}
	t.Setenv("PSModulePath", `C:\Program Files\PowerShell\7\Modules`)
	t.Setenv("POWERSHELL_DISTRIBUTION_CHANNEL", "MSI:7.4.6")
	want := filepath.FromSlash("/home/u/Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1")
	if got := RCFileFor(ShellPowerShell, "windows", "/home/u", ""); got != want {
		t.Fatalf("RCFileFor 被本机环境带偏了: %q，期望 %q", got, want)
	}
}

func TestWriteManagedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte("export FOO=1\nalias ll='ls -l'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(path, "export PATH=/x:$PATH\n"); err != nil {
		t.Fatalf("WriteManagedBlock: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "export FOO=1") || !strings.Contains(text, ManagedBegin) || !strings.Contains(text, "/x") {
		t.Fatalf("托管块应追加且不动原有内容:\n%s", text)
	}
	if _, err := os.Stat(path + ".pk.bak"); err != nil {
		t.Fatalf("应留备份: %v", err)
	}

	// 幂等：再写一次仍只有一段块
	if err := WriteManagedBlock(path, "export PATH=/y:$PATH\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	if strings.Count(text, ManagedBegin) != 1 || strings.Contains(text, "/x") {
		t.Fatalf("应替换旧块:\n%s", text)
	}
	block := ReadManagedBlock(path)
	if !strings.Contains(block, "/y") {
		t.Fatalf("ReadManagedBlock = %q", block)
	}

	// 残缺块（只有开始标记）也要能修好
	if err := os.WriteFile(path, []byte("keep\n"+ManagedBegin+"\nno end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(path, "export A=1\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), ManagedEnd) || !strings.Contains(string(data), "keep") {
		t.Fatalf("残缺块应被整段重写:\n%s", data)
	}

	// 删除
	if err := RemoveManagedBlock(path); err != nil {
		t.Fatalf("RemoveManagedBlock: %v", err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), ManagedBegin) {
		t.Fatalf("托管块应被删掉:\n%s", data)
	}
	if !strings.Contains(string(data), "keep") {
		t.Fatal("块外内容不该动")
	}
	// 删除不存在的文件 / 没有块的文件的文件都应是 no-op
	if err := RemoveManagedBlock(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("缺文件应 no-op: %v", err)
	}
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(plain, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveManagedBlock(plain); err != nil {
		t.Fatalf("没有块应 no-op: %v", err)
	}
	if ReadManagedBlock(plain) != "" {
		t.Fatal("没有块时应返回空")
	}
}

// TestOnSyncDir：云同步目录上的 PK_ROOT 要能识别出来（锁与原子 rename 在同步盘都不可靠）。
func TestOnSyncDir(t *testing.T) {
	yes := []string{
		"/Users/me/OneDrive/packnest",
		"/Users/me/Library/Mobile Documents/com~apple~CloudDocs/.packnest",
		`C:\Users\me\Dropbox\packnest`,
		"/home/me/Google Drive/pk",
		"/Users/me/坚果云/.packnest",
	}
	no := []string{"/Users/me/.packnest", "/opt/pk", `D:\AI\packnest`, "/home/me/onedrive-backup-local"}
	for _, p := range yes {
		if !OnSyncDir(p) {
			t.Errorf("%q 应判为同步盘", p)
		}
	}
	for _, p := range no {
		if OnSyncDir(p) {
			t.Errorf("%q 不该判为同步盘", p)
		}
	}
}
