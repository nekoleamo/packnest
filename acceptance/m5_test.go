// M5 验收：CLI-DONE 门禁里能用自动化覆盖的那几条（方案 §12.2）。
//
//   - 第 1 条：6 工具在"官方源全不可达"下装成功（这里做全 7 个）
//   - 第 2 条：node↔npm 伴生矩阵 7 个版本全对（含预发布排除与补丁号分界）
//   - 第 8 条：`--json` 契约由 internal/jsonout 的 golden 锁住（见 docs/json-schema.md）
//   - 第 9 条：doctor 对 4 类坏环境的建议（PATH/镜像/ABI/台账）——见 cmd/pk 与 internal/env 用例
package acceptance

import (
	"context"
	"errors"
	"fmt"
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

// TestAcceptance_M5_AutoDetect：档位 auto 要按网络环境选源（国外走官方、国内走镜像）。
func TestAcceptance_M5_AutoDetect(t *testing.T) {
	alive := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	dead := testkit.Mirror(t, map[string][]byte{}, testkit.Status(503))

	nodeP := func(officialURL, cnURL string) *provider.Provider {
		toml := fmt.Sprintf(`id = "node"
display = "Node.js"
kind = "binary"
[index]
kind = "json"
[index.urls]
default = %q
cn = %q
[artifact]
[artifact.urls]
default = "%[3]s/node/v{version}/node-v{version}-{platform}.{ext}"
cn = "%[4]s/node/v{version}/node-v{version}-{platform}.{ext}"
[artifact.ext]
windows = "zip"
darwin = "tar.gz"
linux = "tar.gz"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-x64"
windows-amd64 = "win-x64"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[3]s/node/v{version}/SHASUMS256.txt"
cn = "%[4]s/node/v{version}/SHASUMS256.txt"
[layout]
strip = 1
bins = ["node", "npm", "npx"]
[companions.npm]
mode = "latest-compatible"
registry = "npm"
target = "lib/node_modules/npm"
`, officialURL+"/node/index.json", cnURL+"/node/index.json", officialURL, cnURL)
		p, err := provider.Parse([]byte(toml))
		if err != nil {
			t.Fatalf("清单非法: %v", err)
		}
		return p
	}

	run := func(officialURL, cnURL string) (*install.Result, error) {
		p, err := paths.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		inst, err := install.New(install.Options{
			Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "auto",
			GOOS: "darwin", GOARCH: "arm64",
			LookupProvider: func(id string) (*provider.Provider, error) {
				switch id {
				case "node":
					return nodeP(officialURL, cnURL), nil
				case "npm":
					return alive.NPMProvider, nil
				}
				return nil, errors.New("未知工具 " + id)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := inst.Install(context.Background(), "node", "22.22.2")
		return &res, err
	}

	// 国外：官方源可用、国内档挂 → auto 应判 default 并装成功
	res, err := run(alive.Server.URL, dead.URL)
	if err != nil {
		t.Fatalf("官方源可达时应装成功（auto→default）: %v", err)
	}
	if res.Version != "22.22.2" {
		t.Fatalf("版本异常: %+v", res)
	}
	// 国内：官方源挂、镜像可用 → auto 应判 cn 并装成功
	res, err = run(dead.URL, alive.Server.URL)
	if err != nil {
		t.Fatalf("官方源不可达时应回落到镜像（auto→cn）: %v", err)
	}
	if res.Version != "22.22.2" {
		t.Fatalf("版本异常: %+v", res)
	}
	// 两边都挂 → 明确报错，不静默降级
	if _, err := run(dead.URL, dead.URL); err == nil {
		t.Fatal("两边都不可达应报错")
	}
}

// TestAcceptance_M5_CompanionMatrix7：方案 §7 的 7 行矩阵逐行验证。
func TestAcceptance_M5_CompanionMatrix7(t *testing.T) {
	nodes := []testkit.NodeSpec{
		{Version: "18.20.4", NPMVersion: "10.9.9"},
		{Version: "20.18.1", NPMVersion: "10.9.9"},
		{Version: "22.21.1", NPMVersion: "11.20.0"},
		{Version: "22.22.2", NPMVersion: "11.20.0"},
		{Version: "24.14.0", NPMVersion: "11.20.0"},
		{Version: "24.15.0", NPMVersion: "11.20.0"},
		{Version: "26.13.0", NPMVersion: "11.20.0"},
	}
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{Versions: nodes, NPM: npmMatrixAcceptance()})
	inst, _ := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})

	want := map[string]string{
		"18.20.4": "10.9.10", // engines ^18.17.0 || >=20.5.0
		"20.18.1": "11.21.2", // engines ^20.17.0 || >=22.9.0
		"22.21.1": "11.21.2", // ⭐ 补丁号分界
		"22.22.2": "12.2.2",  // ⭐ 22.22.2 才进 12.x
		"24.14.0": "11.21.2", // ⭐ 24.14.0 还在 11.x
		"24.15.0": "12.2.2",
		"26.13.0": "12.2.2",
	}
	ctx := context.Background()
	for _, node := range nodes {
		res, err := inst.Install(ctx, "node", node.Version)
		if err != nil {
			t.Fatalf("装 node %s: %v", node.Version, err)
		}
		if res.NPMVersion != want[node.Version] {
			t.Errorf("node %s → npm %s，期望 %s（自带 %s）", node.Version, res.NPMVersion, want[node.Version], res.NPMReplaced)
		}
		if strings.Contains(res.NPMVersion, "pre") {
			t.Errorf("node %s 落到了预发布 npm %s（方案 §7 坑 1）", node.Version, res.NPMVersion)
		}
	}
}

// TestAcceptance_M5_AllSevenOfficialUnreachable：官方源全挂，7 个工具都能从国内档装。
func TestAcceptance_M5_AllSevenOfficialUnreachable(t *testing.T) {
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
	goTool := testkit.NewFakeGo(t, []string{"1.27.3"})
	pbs := testkit.NewFakePBS(t, testkit.PBSOptions{Tags: []string{"20261026"}, Versions: []string{"3.13.18"}})

	// 每个 provider 都改成：default（官方）指向死地址，cn 指向可用假镜像。
	provs := map[string]*provider.Provider{}
	add := func(id, toml, prefix string) {
		p, err := provider.Parse([]byte(deadOfficial(toml, dead, prefix)))
		if err != nil {
			t.Fatalf("%s 清单非法: %v", id, err)
		}
		provs[id] = p
	}
	add("node", testkit.NodeProviderTOML(node.Server.URL), "/node")
	add("npm", testkit.NPMProviderTOML(node.Server.URL), "/npm")
	add("pnpm", testkit.PnpmProviderTOML(pnpm.Server.URL), "/pnpm")
	add("bun", testkit.BunProviderTOML(bun.Server.URL), "/bun")
	add("deno", testkit.DenoProviderTOML(deno.Server.URL), "/deno")
	add("go", testkit.GoProviderTOML(goTool.Server.URL), "/dl")
	add("python", testkit.PBSProviderTOML(pbs.Server.URL), "/pbs")

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
	targets := []struct{ tool, spec string }{
		{"node", "22.22.2"}, {"pnpm", "10.4.1"}, {"bun", "1.4.4"},
		{"deno", "2.9.8"}, {"go", "1.27.3"}, {"python", "3.13"},
	}
	for _, target := range targets {
		if _, err := inst.Install(ctx, target.tool, target.spec); err != nil {
			t.Fatalf("官方源全挂时 %s 应能从国内档装: %v", target.tool, err)
		}
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	// 7 个工具全部可解析（node 记录里还带伴生 npm）
	for _, name := range []string{"node", "npm", "npx", "pnpm", "bun", "bunx", "deno", "go", "gofmt", "python3", "pip3"} {
		got, isShim, err := shim.Resolve(p.Bin(), name, "darwin")
		if err != nil || !isShim || got == "" {
			t.Errorf("%s 的 shim 缺失: %q %v %v", name, got, isShim, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.ToolVersion("go", "1.27.3"), "bin", "go")); err != nil {
		t.Fatalf("go 目录异常: %v", err)
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Installs) != 6 {
		t.Fatalf("台账应有 6 条（npm 记在 node 上）: %+v", ledger.Installs)
	}
	_ = fmt.Sprint
}
