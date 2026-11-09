package env

import (
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
	if vars["DENO_INSTALL_ROOT"] != "/root/global/deno" || vars["DENO_DIR"] != "/root/cache/deno" {
		t.Fatalf("deno 变量异常: %+v", vars)
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
	// PATH 三段顺序固定：bin 在最前。
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
