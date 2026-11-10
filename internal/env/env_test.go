package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
)

func kvs(list []KV) map[string]string {
	out := make(map[string]string, len(list))
	for _, kv := range list {
		out[kv.Key] = kv.Value
	}
	return out
}

func TestRegistryVars(t *testing.T) {
	cn := kvs(RegistryVars("cn"))
	if cn["npm_config_registry"] != "https://registry.npmmirror.com" {
		t.Fatalf("cn registry = %q", cn["npm_config_registry"])
	}
	if cn["BUN_CONFIG_REGISTRY"] != cn["npm_config_registry"] {
		t.Fatal("bun 与 npm registry 应一致")
	}
	if cn["GOTOOLCHAIN"] != "local" {
		t.Fatal("必须写 GOTOOLCHAIN=local")
	}
	if !strings.Contains(cn["GOPROXY"], "goproxy.cn") {
		t.Fatalf("cn GOPROXY = %q", cn["GOPROXY"])
	}

	auto := kvs(RegistryVars("auto"))
	if auto["npm_config_registry"] != "https://registry.npmmirror.com" {
		t.Fatalf("auto 应回落到 cn 的 registry: %q", auto["npm_config_registry"])
	}

	dflt := kvs(RegistryVars("default"))
	if dflt["npm_config_registry"] != "https://registry.npmjs.org" {
		t.Fatalf("default registry = %q", dflt["npm_config_registry"])
	}
	if kvs(RegistryVars("nope"))["npm_config_registry"] == "" {
		t.Fatal("未知档位也要给出可用值")
	}
}

func TestSharedVars(t *testing.T) {
	p, err := paths.New("/root")
	if err != nil {
		t.Fatal(err)
	}
	vars := kvs(SharedVars(p, "/home/u/.npm"))
	if vars["npm_config_prefix"] != "/root/global" {
		t.Fatalf("prefix = %q", vars["npm_config_prefix"])
	}
	if vars["npm_config_cache"] != "/home/u/.npm" {
		t.Fatalf("cache 应沿用 ~/.npm: %q", vars["npm_config_cache"])
	}
	if vars["npm_config_globalconfig"] != "/root/global/etc/npmrc" {
		t.Fatalf("globalconfig 必须引出（方案 §8.4）: %q", vars["npm_config_globalconfig"])
	}
	if vars["PNPM_HOME"] != "/root/global/pnpm" {
		t.Fatalf("PNPM_HOME = %q", vars["PNPM_HOME"])
	}
	if vars["BUN_INSTALL"] != "/root/global/bun" {
		t.Fatalf("BUN_INSTALL = %q", vars["BUN_INSTALL"])
	}
	// deno 已在 0.8.0 移除：共享前缀里不该再出现 DENO_* 变量
	if _, ok := vars["DENO_INSTALL_ROOT"]; ok {
		t.Fatalf("不该再有 DENO_INSTALL_ROOT: %+v", vars)
	}
	if _, ok := vars["DENO_DIR"]; ok {
		t.Fatalf("不该再有 DENO_DIR: %+v", vars)
	}
}

func TestExportScriptPOSIX(t *testing.T) {
	p, _ := paths.New("/root dir")
	script := ExportScript([]KV{{"npm_config_registry", "https://registry.npmmirror.com"}},
		p.PathSegments(), ShellPOSIX)
	for _, want := range []string{
		"export PATH=",
		"/root dir/bin",
		":$PATH",
		"export npm_config_registry=https://registry.npmmirror.com",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("片段缺少 %q:\n%s", want, script)
		}
	}
	// PATH 四段顺序固定：bin 在最前。
	first := strings.Index(script, "export PATH=")
	if first < 0 || !strings.Contains(script[first:first+60], "/root dir/bin") {
		t.Errorf("bin 必须在 PATH 最前:\n%s", script)
	}
}

func TestExportScriptPowerShell(t *testing.T) {
	script := ExportScript([]KV{{"GOTOOLCHAIN", "local"}}, []string{`C:\pn\bin`}, ShellPowerShell)
	if !strings.Contains(script, "$env:PATH") || !strings.Contains(script, "C:\\pn\\bin") {
		t.Fatalf("PowerShell 片段异常:\n%s", script)
	}
	if !strings.Contains(script, "$env:GOTOOLCHAIN = 'local'") {
		t.Fatalf("变量未导出:\n%s", script)
	}
}

func TestExportScriptEscapesQuotes(t *testing.T) {
	script := ExportScript([]KV{{"X", "a'b"}}, nil, ShellPOSIX)
	if !strings.Contains(script, `'\''`) {
		t.Fatalf("单引号应转义:\n%s", script)
	}
	script = ExportScript([]KV{{"X", "a'b"}}, nil, ShellPowerShell)
	if !strings.Contains(script, "a''b") {
		t.Fatalf("PowerShell 单引号应双写:\n%s", script)
	}
}

func TestParseShell(t *testing.T) {
	for _, name := range []string{"", "bash", "ZSH", " sh "} {
		if s, err := ParseShell(name); err != nil || s != ShellPOSIX {
			t.Errorf("ParseShell(%q) = %q/%v", name, s, err)
		}
	}
	for _, name := range []string{"powershell", "pwsh", "PS"} {
		if s, err := ParseShell(name); err != nil || s != ShellPowerShell {
			t.Errorf("ParseShell(%q) = %q/%v", name, s, err)
		}
	}
	if _, err := ParseShell("fish"); err == nil {
		t.Error("fish 未支持，应报错并提示")
	}
}

func TestToolchainVars(t *testing.T) {
	vars := ToolchainVars(map[string]string{"GOTOOLCHAIN": "local", "A": "1"})
	if len(vars) != 2 || vars[0].Key != "A" || vars[1].Key != "GOTOOLCHAIN" {
		t.Fatalf("应排序输出: %+v", vars)
	}
	if PathHint([]string{"/a", "/b"}) == "" {
		t.Fatal("PathHint 不该为空")
	}
}

func TestWriteManagedBlockIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	original := "# 用户自己的配置\nexport FOO=1" // 故意不带结尾换行
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	block := "export PATH=/pk/bin:$PATH\nexport GOTOOLCHAIN=local\n"
	for i := 0; i < 3; i++ {
		if err := WriteManagedBlock(path, block); err != nil {
			t.Fatalf("第 %d 次写: %v", i+1, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, ManagedBegin) != 1 || strings.Count(text, ManagedEnd) != 1 {
		t.Fatalf("托管块应该只有一份:\n%s", text)
	}
	// 反复写不能垫空行（块外内容一个字节不改，块内也只写一份）。
	want := original + "\n" + ManagedBegin + "\n" + strings.TrimRight(block, "\n") + "\n" + ManagedEnd + "\n"
	if text != want {
		t.Fatalf("反复写不幂等：\n--- got ---\n%q\n--- want ---\n%q", text, want)
	}
}

func TestWriteManagedBlockReplacesMiddleBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	pre := "before\n"
	post := "after\n"
	seed := pre + ManagedBegin + "\nold line\n" + ManagedEnd + "\n" + post
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(path, "new line\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := pre + ManagedBegin + "\nnew line\n" + ManagedEnd + "\n" + post
	if string(data) != want {
		t.Fatalf("got %q want %q", data, want)
	}
}

func TestRemoveManagedBlockLeavesNoBlankLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	original := "before\nafter\n"
	seed := "before\n" + ManagedBegin + "\nX=1\n" + ManagedEnd + "\n" + "after\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveManagedBlock(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatalf("删块后应完全还原: got %q want %q", data, original)
	}
	if got := ReadManagedBlock(path); got != "" {
		t.Fatalf("删完不该还能读到块: %q", got)
	}
	// 没有块时再删一次不应报错，也不改文件。
	if err := RemoveManagedBlock(path); err != nil {
		t.Fatal(err)
	}
}
