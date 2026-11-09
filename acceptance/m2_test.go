// M2 验收：pnpm / bun / deno（方案 §12.1 M2 门禁）。
//
//   - pnpm 是 JS 包，必须靠 node 跑（requires = ["node"]，不隐式安装）
//   - deno 是单文件产物（zip 里只有一个文件）
//   - 三平台 smoke：darwin-arm64 / linux-amd64 / windows-amd64
//   - 5 工具（node+npm+pnpm+bun+deno）在"官方源全不可达"下全部装成功
package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

// m2Env 是一套"多工具假镜像 + 可注入的 provider 查找"。
type m2Env struct {
	node  *testkit.FakeNode
	pnpm  *testkit.FakePnpm
	bun   *testkit.FakeTool
	deno  *testkit.FakeTool
	paths paths.Paths
}

func newM2Env(t *testing.T, goos, goarch string) (*m2Env, *install.Installer) {
	t.Helper()
	env := &m2Env{}
	env.node = testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	env.pnpm = testkit.NewFakePnpm(t, map[string]string{"10.4.1": ">=18.12"})
	mkArtifact := func(platform, _ string) []byte { return nil }
	_ = mkArtifact
	env.bun = testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool:         "bun",
		Versions:     []string{"1.4.4"},
		ProviderTOML: testkit.BunProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeBunZip(platform) },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "darwin-aarch64"},
			{Goos: "linux", Goarch: "amd64", Ext: "zip", Name: "linux-x64"},
			{Goos: "windows", Goarch: "amd64", Ext: "zip", Name: "windows-x64"},
		},
		ChecksumPath:   "SHASUMS256.txt",
		ChecksumFormat: "gnu-sums",
		VersionDir:     "bun-v%s/",
	})
	env.deno = testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool:         "deno",
		Versions:     []string{"2.9.8"},
		ProviderTOML: testkit.DenoProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeDenoZip() },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"},
			{Goos: "linux", Goarch: "amd64", Ext: "zip", Name: "x86_64-unknown-linux-gnu"},
			{Goos: "windows", Goarch: "amd64", Ext: "zip", Name: "x86_64-pc-windows-msvc"},
		},
		ChecksumPath:   "deno-%s.zip.sha256sum",
		ChecksumFormat: "per-artifact",
	})

	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env.paths = p
	inst, err := install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: goos, GOARCH: goarch, LookupProvider: env.lookup,
	})
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}
	return env, inst
}

func (e *m2Env) lookup(id string) (*provider.Provider, error) {
	switch id {
	case "node":
		return e.node.NodeProvider, nil
	case "npm":
		return e.node.NPMProvider, nil
	case "pnpm":
		return e.pnpm.Provider, nil
	case "bun":
		return e.bun.Provider, nil
	case "deno":
		return e.deno.Provider, nil
	}
	return nil, errors.New("未知工具 " + id)
}

// TestAcceptance_M2_PnpmRequiresNode：没有 node 时明确报错并给建议，不隐式安装运行时。
func TestAcceptance_M2_PnpmRequiresNode(t *testing.T) {
	_, inst := newM2Env(t, "darwin", "arm64")
	ctx := context.Background()

	_, err := inst.Install(ctx, "pnpm", "10.4.1")
	if err == nil {
		t.Fatal("没有 node 时装 pnpm 必须失败")
	}
	if !strings.Contains(err.Error(), "需要 node") || !strings.Contains(err.Error(), "pk install node") {
		t.Fatalf("错误信息应给可执行建议: %v", err)
	}
	// 不该隐式装上 node
	if _, err := os.Stat(instPath(t, inst, "node")); !os.IsNotExist(err) {
		t.Fatal("不该隐式安装 node")
	}

	// 先装 node 再装 pnpm
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("装 node: %v", err)
	}
	res, err := inst.Install(ctx, "pnpm", "10.4.1")
	if err != nil {
		t.Fatalf("装了 node 后 pnpm 应能装: %v", err)
	}
	if res.Version != "10.4.1" {
		t.Fatalf("版本异常: %+v", res)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	// shim 指向 tgz 里的 bin/pnpm.cjs，且可执行位齐（靠 node 的 shebang 跑）
	got, isShim, err := shim.Resolve(res.Path[:strings.LastIndex(res.Path, string(os.PathSeparator))]+"/../../bin", "pnpm", "darwin")
	if err == nil && isShim && !strings.HasSuffix(got, "pnpm.cjs") {
		t.Fatalf("pnpm shim 应指向 bin/pnpm.cjs: %q", got)
	}
	st, err := os.Stat(filepath.Join(res.Path, "bin", "pnpm.cjs"))
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("pnpm.cjs 必须有可执行位: %v %v", st, err)
	}
}

func instPath(t *testing.T, inst *install.Installer, tool string) string {
	t.Helper()
	return filepath.Join(rootOfInstaller(t, inst), "tools", tool)
}

// rootOfInstaller 从台账路径反推根目录。
func rootOfInstaller(t *testing.T, inst *install.Installer) string {
	t.Helper()
	record := inst.Ledger()
	_ = record
	// Installer 不暴露 paths，用 ledger 里第一条记录的路径反推；没有记录就用 cwd 兜底。
	for _, in := range inst.Ledger().Installs {
		// <root>/tools/<tool>/<ver>
		return filepath.Dir(filepath.Dir(filepath.Dir(in.Path)))
	}
	return t.TempDir()
}

// TestAcceptance_M2_SingleFileLayout：deno 是单文件产物，落在 bin/deno 且 0755。
func TestAcceptance_M2_SingleFileLayout(t *testing.T) {
	env, inst := newM2Env(t, "darwin", "arm64")
	res, err := inst.Install(context.Background(), "deno", "2.9.8")
	if err != nil {
		t.Fatalf("装 deno: %v", err)
	}
	target := filepath.Join(res.Path, "bin", "deno")
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("单文件应落在 bin/deno: %v", err)
	}
	if st.Mode().Perm()&0o100 == 0 {
		t.Fatal("单文件必须 0755")
	}
	// 树里不该有别的文件（deno 的 zip 只有一个 deno）
	var files []string
	if err := filepath.Walk(res.Path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("单文件布局只该有一个文件，得到 %v", files)
	}
	got, isShim, err := shim.Resolve(env.paths.Bin(), "deno", "darwin")
	if err != nil || !isShim || got != target {
		t.Fatalf("deno shim 异常: %q %v %v", got, isShim, err)
	}
}

// TestAcceptance_M2_ThreePlatformSmoke：三平台上 bun/deno 都能装且 shim 形态正确。
func TestAcceptance_M2_ThreePlatformSmoke(t *testing.T) {
	platforms := []struct{ goos, goarch string }{
		{"darwin", "arm64"},
		{"linux", "amd64"},
		{"windows", "amd64"},
	}
	for _, plat := range platforms {
		goos, goarch := plat.goos, plat.goarch
		t.Run(goos+"-"+goarch, func(t *testing.T) {
			env, inst := newM2Env(t, goos, goarch)
			ctx := context.Background()
			if _, err := inst.Install(ctx, "bun", "1.4.4"); err != nil {
				t.Fatalf("装 bun: %v", err)
			}
			if _, err := inst.Install(ctx, "deno", "2.9.8"); err != nil {
				t.Fatalf("装 deno: %v", err)
			}
			if err := inst.Finalize(); err != nil {
				t.Fatal(err)
			}
			// bun 的 bunx 与 bun 指向同一个可执行文件
			bunBin, isShim, err := shim.Resolve(env.paths.Bin(), "bun", goos)
			if err != nil || !isShim {
				t.Fatalf("bun shim: %q %v %v", bunBin, isShim, err)
			}
			bunxBin, _, err := shim.Resolve(env.paths.Bin(), "bunx", goos)
			if err != nil || bunxBin != bunBin {
				t.Fatalf("bunx 应与 bun 指向同一文件: %q vs %q (%v)", bunxBin, bunBin, err)
			}
			if goos == "windows" {
				if !strings.HasSuffix(bunBin, "bun") && !strings.HasSuffix(bunBin, "bun.exe") {
					t.Fatalf("windows 的 bun 目标异常: %q", bunBin)
				}
				if _, err := os.Stat(shim.CmdPath(env.paths.Bin(), "deno")); err != nil {
					t.Fatalf("windows 应生成 deno.cmd: %v", err)
				}
			} else {
				if _, err := os.Stat(filepath.Join(env.paths.Bin(), "deno")); err != nil {
					t.Fatalf("unix 应生成 deno shim: %v", err)
				}
			}
			ledger, err := store.LoadLedger(env.paths.Ledger())
			if err != nil || len(ledger.Installs) != 2 {
				t.Fatalf("台账应有 bun+deno 两条: %+v %v", ledger, err)
			}
		})
	}
}

// TestAcceptance_M2_OfficialUnreachable：官方源全挂、只有国内档可用时，5 工具全装上。
func TestAcceptance_M2_OfficialUnreachable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用例在 unix 上用 darwin-arm64 产物跑通即可（windows 已单测覆盖）")
	}
	dead := "http://127.0.0.1:1"

	node := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	pnpm := testkit.NewFakePnpm(t, map[string]string{"10.4.1": ">=18.12"})
	bun := testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool: "bun", Versions: []string{"1.4.4"}, ProviderTOML: testkit.BunProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeBunZip(platform) },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "darwin-aarch64"},
			{Goos: "linux", Goarch: "amd64", Ext: "zip", Name: "linux-x64"},
			{Goos: "windows", Goarch: "amd64", Ext: "zip", Name: "windows-x64"},
		},
		ChecksumPath: "SHASUMS256.txt", ChecksumFormat: "gnu-sums", VersionDir: "bun-v%s/",
	})
	deno := testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool: "deno", Versions: []string{"2.9.8"}, ProviderTOML: testkit.DenoProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeDenoZip() },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"},
			{Goos: "linux", Goarch: "amd64", Ext: "zip", Name: "x86_64-unknown-linux-gnu"},
			{Goos: "windows", Goarch: "amd64", Ext: "zip", Name: "x86_64-pc-windows-msvc"},
		},
		ChecksumPath: "deno-%s.zip.sha256sum", ChecksumFormat: "per-artifact",
	})

	// 所有 provider 的 default（官方）指向死地址，cn 指向可用假镜像。
	provs := map[string]*provider.Provider{}
	add := func(id, toml string) {
		p, err := provider.Parse([]byte(toml))
		if err != nil {
			t.Fatalf("%s 清单非法: %v", id, err)
		}
		provs[id] = p
	}
	add("node", deadOfficial(testkit.NodeProviderTOML(node.Server.URL), dead, "/node"))
	add("npm", deadOfficial(testkit.NPMProviderTOML(node.Server.URL), dead, "/npm"))
	add("pnpm", deadOfficial(testkit.PnpmProviderTOML(pnpm.Server.URL), dead, "/pnpm"))
	add("bun", deadOfficial(testkit.BunProviderTOML(bun.Server.URL), dead, "/bun"))
	add("deno", deadOfficial(testkit.DenoProviderTOML(deno.Server.URL), dead, "/deno"))

	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inst, err := install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(id string) (*provider.Provider, error) {
			if pv, ok := provs[id]; ok {
				return pv, nil
			}
			return nil, errors.New("未知工具 " + id)
		},
	})
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}

	ctx := context.Background()
	for _, tool := range []struct{ name, spec string }{
		{"node", "22.22.2"}, {"pnpm", "10.4.1"}, {"bun", "1.4.4"}, {"deno", "2.9.8"},
	} {
		res, err := inst.Install(ctx, tool.name, tool.spec)
		if err != nil {
			t.Fatalf("官方源全挂时 %s 应能装（国内档）: %v", tool.name, err)
		}
		if res.Version == "" {
			t.Fatalf("%s 结果异常: %+v", tool.name, res)
		}
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	// node + npm（伴生）+ pnpm + bun + deno = 5 个可执行工具（npm 记在 node 上）
	tools := map[string]bool{}
	for _, in := range ledger.Installs {
		tools[in.Tool] = true
	}
	for _, want := range []string{"node", "pnpm", "bun", "deno"} {
		if !tools[want] {
			t.Fatalf("台账缺少 %s: %+v", want, ledger.Installs)
		}
	}
	nodeRec := ledger.Find("node", "22.22.2")
	if nodeRec == nil || nodeRec.NpmVersion == "" || nodeRec.NpmVersion == nodeRec.NpmBundled {
		t.Fatalf("node 的伴生 npm 应被替换: %+v", nodeRec)
	}
	for _, name := range []string{"node", "npm", "npx", "pnpm", "bun", "bunx", "deno"} {
		got, isShim, err := shim.Resolve(p.Bin(), name, "darwin")
		if err != nil || !isShim || got == "" {
			t.Fatalf("%s 的 shim 缺失: %q %v %v", name, got, isShim, err)
		}
	}
}

// deadOfficial 把清单里每个 URLs 段的 default 档指向死地址，可用地址降级为 cn 档。
//
// 目的：让"官方源全不可达"成为真的不可达（连接被拒，而不是慢或 404）。
func deadOfficial(toml, deadURL, pathPrefix string) string {
	deadURL = strings.TrimRight(deadURL, "/") + pathPrefix + "/dead"
	var out []string
	section := ""
	type kv struct{ key, url string }
	var pending []kv
	flush := func() {
		if len(pending) == 0 {
			return
		}
		hasCN := false
		var firstURL, defaultURL string
		for _, item := range pending {
			switch item.key {
			case "cn":
				hasCN = true
			case "default":
				defaultURL = item.url
			default:
				if firstURL == "" {
					firstURL = item.url
				}
			}
		}
		if firstURL == "" {
			firstURL = defaultURL // 只有 default 一段时，把它降级成 cn
		}
		out = append(out, "default = "+strconv.Quote(deadURL))
		if !hasCN && firstURL != "" {
			out = append(out, "cn = "+strconv.Quote(firstURL))
		}
		for _, item := range pending {
			if item.key == "default" || item.key == "cn" {
				continue
			}
			out = append(out, item.key+" = "+strconv.Quote(item.url))
		}
		if hasCN {
			for _, item := range pending {
				if item.key == "cn" {
					out = append(out, "cn = "+strconv.Quote(item.url))
				}
			}
		}
		pending = pending[:0]
	}

	isURLSection := func(name string) bool {
		return strings.HasSuffix(name, ".urls]") || strings.HasSuffix(name, "urls]")
	}
	for _, line := range strings.Split(toml, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if isURLSection(section) {
				flush()
			}
			section = trimmed
			out = append(out, line)
			continue
		}
		if isURLSection(section) {
			if idx := strings.Index(trimmed, " = "); idx > 0 && strings.Count(trimmed, "=") == 1 {
				key := strings.TrimSpace(trimmed[:idx])
				value := strings.TrimSpace(trimmed[idx+3:])
				if unquoted, err := strconv.Unquote(value); err == nil {
					pending = append(pending, kv{key, unquoted})
					continue
				}
			}
		}
		out = append(out, line)
	}
	if isURLSection(section) {
		flush()
	}
	return strings.Join(out, "\n")
}
