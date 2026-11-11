// EXT 验收：外部 provider 清单（方案 A）。
//
//   - ExternalProviderInstall：合成工具写进 $PK_ROOT/providers → install → shim → source=external → uninstall 干净
//   - BadManifestDoesNotBreakCLI：坏清单（http/绝对路径/超大）只产生 issue，内嵌工具照常可用
//   - HTTPNeedsOptIn：http 源默认拒绝；PK_ALLOW_HTTP=1 放行后校验强度最高 weak，台账带 transport=http
package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// extEnv 造一个"外部清单 + 假镜像"的现场：清单由 testkit 的合成工具生成，落到 root/providers。
type extEnv struct {
	root  string
	tool  *testkit.FakeTool
	table string // 外部清单 TOML（指向假镜像）
	inst  *install.Installer
}

func newExtEnv(t *testing.T, allowHTTP bool) *extEnv {
	t.Helper()
	env := &extEnv{root: t.TempDir()}
	env.tool = testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool:         "singlefile",
		Versions:     []string{"1.0.0"},
		ProviderTOML: testkit.SingleFileProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeSingleFileZip() },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"},
			{Goos: "linux", Goarch: "amd64", Ext: "zip", Name: "x86_64-unknown-linux-gnu"},
			{Goos: "windows", Goarch: "amd64", Ext: "zip", Name: "x86_64-pc-windows-msvc"},
		},
		ChecksumPath:   "singlefile-%s.zip.sha256sum",
		ChecksumFormat: "per-artifact",
	})
	// httptest 是 http://，所以外部清单必须先放行（这也正好是 EXT_HTTPNeedsOptIn 的场景）。
	env.table = testkit.SingleFileProviderTOML(env.tool.Server.URL)
	if allowHTTP {
		t.Setenv(provider.AllowHTTPEnv, "1")
	}
	writeProvider(t, env.root, "singlefile.toml", env.table)
	provider.SetRoot(env.root)
	t.Cleanup(func() { provider.SetRoot("") })

	p, err := paths.New(env.root)
	if err != nil {
		t.Fatal(err)
	}
	env.inst, err = install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", LookupProvider: provider.Get, Insecure: false,
	})
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}
	return env
}

func writeProvider(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "providers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAcceptance_EXT_ExternalProviderInstall：外部清单能走完 install → shim → uninstall。
func TestAcceptance_EXT_ExternalProviderInstall(t *testing.T) {
	env := newExtEnv(t, true)
	ctx := context.Background()

	p, err := provider.Get("singlefile")
	if err != nil {
		t.Fatalf("外部清单应被加载: %v", err)
	}
	if p.Source != provider.SourceExternal {
		t.Fatalf("source 应为 external，实际 %q", p.Source)
	}

	res, err := env.inst.Install(ctx, "singlefile", "1.0.0")
	if err != nil {
		t.Fatalf("装外部工具: %v", err)
	}
	if res.Verified != "weak" {
		t.Fatalf("http 源（产物与校验和都是 http）最高只能 weak，实际 %q", res.Verified)
	}
	if path, _, err := shim.Resolve(filepath.Join(env.root, "bin"), "singlefile", "darwin"); err != nil || path == "" {
		t.Fatalf("shim 应建好: path=%q err=%v", path, err)
	}
	// 台账里留了传输方式的痕（只增字段）
	in := env.inst.Ledger().Find("singlefile", "1.0.0")
	if in == nil || in.Transport != "http" {
		t.Fatalf("台账应记 transport=http: %+v", in)
	}

	if _, err := env.inst.UninstallTool("singlefile", false); err != nil {
		t.Fatalf("卸载: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.root, "tools", "singlefile")); !os.IsNotExist(err) {
		t.Fatalf("卸载应删掉工具目录: %v", err)
	}
}

// TestAcceptance_EXT_BadManifestDoesNotBreakCLI：坏清单只产生 issue，不挡住内嵌工具。
func TestAcceptance_EXT_BadManifestDoesNotBreakCLI(t *testing.T) {
	root := t.TempDir()
	// 1) http 未放行（默认）
	writeProvider(t, root, "h.ttoml", "x") // 非 .toml 后缀 → 直接忽略
	writeProvider(t, root, "http.toml", strings.Replace(testkit.SingleFileProviderTOML("https://example.invalid"), "https://", "http://", 1))
	// 2) 绝对路径 single
	writeProvider(t, root, "abs.toml", strings.Replace(testkit.SingleFileProviderTOML("https://example.invalid"),
		`single = "bin/singlefile"`, `single = "/etc/passwd"`, 1))
	// 3) 超大文件（不是清单）
	writeProvider(t, root, "huge.toml", strings.Repeat("x", provider.MaxManifestBytes+1))
	t.Setenv(provider.AllowHTTPEnv, "")
	provider.SetRoot(root)
	t.Cleanup(func() { provider.SetRoot("") })

	all, err := provider.All()
	if err != nil {
		t.Fatalf("外部清单有问题也不能让 All() 失败: %v", err)
	}
	if _, err := provider.Get("node"); err != nil {
		t.Fatalf("内嵌工具必须照常可用: %v", err)
	}
	for _, p := range all {
		if p.Source == provider.SourceExternal {
			t.Fatalf("坏清单不该被加载: %s", p.ID)
		}
	}
	codes := map[string]int{}
	for _, is := range provider.ExternalIssues() {
		codes[is.Code]++
		if is.File == "" || is.Message == "" {
			t.Fatalf("issue 必须带文件名与原因: %+v", is)
		}
	}
	if codes[provider.IssueExternalInvalid] != 3 {
		t.Fatalf("应有 3 条 PROVIDER_EXTERNAL_INVALID，实际 %+v", codes)
	}
}

// TestAcceptance_EXT_HTTPNeedsOptIn：http 源默认拒绝，放行后标记降级传输。
func TestAcceptance_EXT_HTTPNeedsOptIn(t *testing.T) {
	env := newExtEnv(t, false) // 不设 PK_ALLOW_HTTP

	if _, err := provider.Get("singlefile"); err == nil {
		t.Fatal("http 源未放行时必须拒绝加载")
	}
	issues := provider.ExternalIssues()
	if len(issues) != 1 || !strings.Contains(issues[0].Message, provider.AllowHTTPEnv) {
		t.Fatalf("报错里要直接给出放行办法: %+v", issues)
	}

	// 放行后重新加载：能装，但强度降到 weak。
	t.Setenv(provider.AllowHTTPEnv, "1")
	provider.SetRoot(env.root)
	p, err := provider.Get("singlefile")
	if err != nil {
		t.Fatalf("放行后应能加载: %v", err)
	}
	if !p.InsecureTransport {
		t.Fatal("http 清单必须标记 InsecureTransport")
	}
	res, err := env.inst.Install(context.Background(), "singlefile", "1.0.0")
	if err != nil {
		t.Fatalf("放行后应能安装: %v", err)
	}
	if res.Verified == "strong" {
		t.Fatal("http 源不得判为 strong")
	}
	if in := env.inst.Ledger().Find("singlefile", "1.0.0"); in == nil || in.Transport != "http" {
		t.Fatalf("台账应记 transport=http: %+v", in)
	}
}
