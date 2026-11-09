// M1 验收：node + 伴生 npm 的"真安装"路径（方案 §12.1 的 M1 门禁）。
//
// 全部用例零外网：node/npm 的索引、产物、校验和都来自 internal/testkit 的假镜像。
package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// npmMatrixAcceptance 是方案 §7 的实测矩阵（含预发布陷阱）。
func npmMatrixAcceptance() map[string]string {
	return map[string]string{
		"10.9.10":        "^18.17.0 || >=20.5.0",
		"11.20.0":        "^20.17.0 || >=22.9.0",
		"11.21.2":        "^20.17.0 || >=22.9.0",
		"12.2.2":         "^22.22.2 || ^24.15.0 || >=26.0.0",
		"12.0.0-pre.0.0": "^20.17.0 || >=22.9.0",
	}
}

// installerFor 用假镜像构造 install.Installer（测试零外网的关键）。
func installerFor(t *testing.T, fm *testkit.FakeNode, goos, goarch string, mutate func(*install.Options)) (*install.Installer, paths.Paths) {
	t.Helper()
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opt := install.Options{
		Paths:   p,
		Client:  fetch.New(),
		Cfg:     store.DefaultConfig(),
		Profile: "cn",
		GOOS:    goos,
		GOARCH:  goarch,
		LookupProvider: func(id string) (*provider.Provider, error) {
			switch id {
			case "node":
				return fm.NodeProvider, nil
			case "npm":
				return fm.NPMProvider, nil
			}
			return nil, errors.New("未知工具 " + id)
		},
	}
	if mutate != nil {
		mutate(&opt)
	}
	inst, err := install.New(opt)
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}
	return inst, p
}

// TestAcceptance_M1_NpmCompanion：node 自带 npm 会被替换成"最新兼容版"。
func TestAcceptance_M1_NpmCompanion(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "26.13.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, p := installerFor(t, fm, "darwin", "arm64", nil)
	res, err := inst.Install(context.Background(), "node", "26.13.0")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.NPMVersion != "12.2.2" || res.NPMReplaced != "11.20.0" {
		t.Fatalf("node 26.13.0 应替换成 npm 12.2.2，得到 %q（自带 %q）", res.NPMVersion, res.NPMReplaced)
	}
	// 伴生 npm 落在 node 的 prefix 内 → 天然跟着 node 切版本，不需要第二个 shim。
	pkg := filepath.Join(res.Path, "lib", "node_modules", "npm", "package.json")
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("npm 应落在 node prefix 内: %v", err)
	}
	got, isShim, err := shim.Resolve(p.Bin(), "npm", "darwin")
	if err != nil || !isShim || !strings.HasPrefix(got, res.Path) {
		t.Fatalf("npm shim 应指向 node 树内: %q %v %v", got, isShim, err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	in := ledger.Find("node", "26.13.0")
	if in == nil || in.NpmVersion != "12.2.2" || in.NpmBundled != "11.20.0" {
		t.Fatalf("台账应记 npm = 12.2.2 (replaced 11.20.0): %+v", in)
	}
}

// TestAcceptance_M1_NpmPinConflict：显式 pin 不满足 engines → 硬失败并给出上限。
func TestAcceptance_M1_NpmPinConflict(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "18.20.4", NPMVersion: "10.9.10"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, _ := installerFor(t, fm, "darwin", "arm64", func(o *install.Options) {
		o.Project = map[string]string{"npm": "11.21.2"}
	})
	_, err := inst.Install(context.Background(), "node", "18.20.4")
	if err == nil {
		t.Fatal("npm 11.21.2 要求 node >=22.9，与 node 18.20.4 冲突，必须失败")
	}
	if !strings.Contains(err.Error(), "上限 = 10.9.10") {
		t.Fatalf("错误应给出该 node 可用的 npm 上限: %v", err)
	}
}

// TestAcceptance_M1_PatchLevelBoundary：补丁号级分界（按大版本算必错）。
func TestAcceptance_M1_PatchLevelBoundary(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.21.1", NPMVersion: "11.20.0"},
			{Version: "22.22.2", NPMVersion: "11.20.0"},
			{Version: "24.14.0", NPMVersion: "11.20.0"},
			{Version: "24.15.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrixAcceptance(),
	})
	inst, _ := installerFor(t, fm, "darwin", "arm64", nil)
	ctx := context.Background()
	cases := map[string]string{
		"22.21.1": "11.21.2",
		"22.22.2": "12.2.2",
		"24.14.0": "11.21.2",
		"24.15.0": "12.2.2",
	}
	for node, wantNpm := range cases {
		res, err := inst.Install(ctx, "node", node)
		if err != nil {
			t.Fatalf("装 node %s: %v", node, err)
		}
		if res.NPMVersion != wantNpm {
			t.Errorf("node %s 应得 npm %s，得到 %s", node, wantNpm, res.NPMVersion)
		}
	}
}

// TestAcceptance_M1_OfflineFromCache：缓存命中时 install 是纯本地操作（0 网络）。
func TestAcceptance_M1_OfflineFromCache(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, p := installerFor(t, fm, "darwin", "arm64", nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}

	fm.Server.Close() // 断网
	offline := fetch.New()
	offline.Offline = true
	inst2, err := install.New(install.Options{
		Paths: p, Client: offline, Cfg: store.DefaultConfig(), Profile: "cn",
		Offline: true, GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(id string) (*provider.Provider, error) {
			if id == "npm" {
				return fm.NPMProvider, nil
			}
			return fm.NodeProvider, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst2.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("离线应从缓存装上: %v", err)
	}
	if res.Verified == "" {
		t.Fatalf("离线校验应走 TOFU: %+v", res)
	}
}

// TestAcceptance_M1_ShimLatency：resolver 必须在 15ms 内完成（方案 §9.5 第 6 项）。
func TestAcceptance_M1_ShimLatency(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst, p := installerFor(t, fm, "darwin", "arm64", nil)
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	const iterations = 200
	start := time.Now()
	for i := 0; i < iterations; i++ {
		if _, _, err := shim.Resolve(p.Bin(), "node", "darwin"); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(start) / iterations
	if limit := 15 * time.Millisecond; per > limit {
		t.Fatalf("shim 解析平均 %v，超过 %v 预算", per, limit)
	}
	t.Logf("shim 解析平均 %v（预算 15ms）", per)
}

// TestAcceptance_M1_UninstallClean：卸载后目录/shim/台账全清，缓存保留。
func TestAcceptance_M1_UninstallClean(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, p := installerFor(t, fm, "darwin", "arm64", nil)
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall("node", "22.22.2", false); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Fatal("版本目录应被删除")
	}
	if entries, _ := os.ReadDir(p.Tool("node")); len(entries) != 0 {
		t.Fatalf("tools/node 下不该留东西: %v", entries)
	}
	for _, name := range []string{"node", "npm", "npx"} {
		if path, _, _ := shim.Resolve(p.Bin(), name, "darwin"); path != "" {
			t.Fatalf("%s shim 应被清掉: %q", name, path)
		}
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil || len(ledger.Installs) != 0 {
		t.Fatalf("台账应清空: %+v %v", ledger, err)
	}
	if _, err := os.Stat(p.CacheArchives()); err != nil {
		t.Fatalf("缓存目录应保留: %v", err)
	}
}

// TestAcceptance_M1_WindowsAtomicReplace：已存在版本目录也能原子替换（Windows 三坑之一）。
func TestAcceptance_M1_WindowsAtomicReplace(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, _ := installerFor(t, fm, "windows", "amd64", nil)
	ctx := context.Background()
	res, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(res.Path, "old-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst2, _ := installerFor(t, fm, "windows", "amd64", func(o *install.Options) {
		o.Paths = paths.Paths{Root: filepath.Dir(filepath.Dir(filepath.Dir(res.Path)))}
		o.Force = true
	})
	if _, err := inst2.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("强制重装应成功（原子替换）: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("旧目录内容应被替换掉")
	}
	if _, err := os.Stat(res.Path + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old 应被清理")
	}
}

// TestAcceptance_M1_WindowsZipNoSymlink：Windows 一律取 zip（node tar 里 bin/npm 是符号链接）。
func TestAcceptance_M1_WindowsZipNoSymlink(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrixAcceptance(),
	})
	inst, p := installerFor(t, fm, "windows", "amd64", nil)

	ext, err := fm.NodeProvider.ExtFor("windows")
	if err != nil || ext != "zip" {
		t.Fatalf("Windows 必须取 zip 产物（避开 tar 里的符号链接），得到 %q/%v", ext, err)
	}
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	// 解出的树里不该有符号链接（zip 天然没有）。
	err = filepath.Walk(res.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Windows 产物里不该有符号链接: " + path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "node.exe")); err != nil {
		t.Fatalf("zip 平铺布局应有 node.exe: %v", err)
	}
	if _, err := os.Stat(shim.CmdPath(p.Bin(), "node")); err != nil {
		t.Fatalf("Windows 应生成 .cmd shim: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(p.Bin(), "node")); !os.IsNotExist(err) {
		t.Fatal("Windows 不该有 unix 风格 shim")
	}
}

// 编译期断言：M1 的校验强度标记不许改名（台账与 list 都依赖它）。
var _ = []string{verify.VerifiedStrong, verify.VerifiedWeak, verify.VerifiedUnverified}

// 保证未使用的 runtime 引入有意义（Windows 用例在 mac 上跑时仍需要 GOOS 常量语义）。
var _ = runtime.GOOS
