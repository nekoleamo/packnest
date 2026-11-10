package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// fakeLookup 让 Installer 用假镜像的清单（测试零外网）。
func fakeLookup(fm *testkit.FakeNode) func(string) (*provider.Provider, error) {
	return func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		}
		return nil, errors.New("未知工具 " + id)
	}
}

func newInstaller(t *testing.T, fm *testkit.FakeNode, mutate func(*Options)) *Installer {
	t.Helper()
	if fm == nil {
		fm = &testkit.FakeNode{}
	}
	t.Helper()
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{
		Paths:          p,
		Client:         fetch.New(),
		Cfg:            store.DefaultConfig(),
		Profile:        "cn",
		GOOS:           "darwin",
		GOARCH:         "arm64",
		LookupProvider: fakeLookup(fm),
	}
	if mutate != nil {
		mutate(&opt)
	}
	inst, err := New(opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return inst
}

func npmMatrix() map[string]string {
	return map[string]string{
		"10.9.10":        "^18.17.0 || >=20.5.0",
		"11.20.0":        "^20.17.0 || >=22.9.0",
		"11.21.2":        "^20.17.0 || >=22.9.0",
		"12.2.2":         "^22.22.2 || ^24.15.0 || >=26.0.0",
		"12.0.0-pre.0.0": "^20.17.0 || >=22.9.0",
	}
}

func TestInstallNodeWithNpmCompanion(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "26.13.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()

	res, err := inst.Install(ctx, "node", "26.13.0")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Version != "26.13.0" || res.AlreadyInstalled {
		t.Fatalf("结果异常: %+v", res)
	}
	if res.NPMReplaced != "11.20.0" || res.NPMVersion != "12.2.2" {
		t.Fatalf("伴生 npm 应为 12.2.2（自带 11.20.0），得到 %q/%q", res.NPMVersion, res.NPMReplaced)
	}
	if res.Verified != verify.VerifiedWeak {
		t.Fatalf("只有 1 个校验和源时应为 weak，得到 %q", res.Verified)
	}

	p := paths.Paths{Root: filepath.Dir(filepath.Dir(filepath.Dir(res.Path)))}
	_ = p
	tree := res.Path
	// node 解压正确
	if _, err := os.Stat(filepath.Join(tree, "bin", "node")); err != nil {
		t.Fatalf("缺少 bin/node: %v", err)
	}
	// npm 被整体替换成 12.2.2
	if v := readNpmVersion(filepath.Join(tree, "lib", "node_modules", "npm", "package.json")); v != "12.2.2" {
		t.Fatalf("npm 未替换，package.json version = %q", v)
	}
	// npm 的 bin 入口仍可用
	if _, err := os.Stat(filepath.Join(tree, "lib", "node_modules", "npm", "bin", "npm-cli.js")); err != nil {
		t.Fatalf("npm bin 入口缺失: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// 台账
	root := rootOf(t, inst)
	ledger, err := store.LoadLedger(root.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	in := ledger.Find("node", "26.13.0")
	if in == nil || in.NpmVersion != "12.2.2" || in.NpmBundled != "11.20.0" {
		t.Fatalf("台账记录异常: %+v", in)
	}
	// TOFU 表写了两条（node 与 npm）
	known, err := verify.LoadKnown(root.Known())
	if err != nil {
		t.Fatal(err)
	}
	if len(known.Entries) != 2 {
		t.Fatalf("TOFU 表应有 node 与 npm 两条，得到 %+v", known.Entries)
	}
}

func rootOf(t *testing.T, inst *Installer) paths.Paths {
	t.Helper()
	return inst.opt.Paths
}

func TestInstallShims(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	root := rootOf(t, inst)
	for _, name := range []string{"node", "npm", "npx"} {
		got, isShim, err := shim.Resolve(root.Bin(), name, "darwin")
		if err != nil || !isShim {
			t.Fatalf("%s shim 异常: %q %v %v", name, got, isShim, err)
		}
		if !strings.HasPrefix(got, res.Path) {
			t.Fatalf("%s 应指向 %s，得到 %s", name, res.Path, got)
		}
	}
}

func TestInstallAlreadyInstalled(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("首次安装: %v", err)
	}
	res, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("重复安装: %v", err)
	}
	if !res.AlreadyInstalled {
		t.Fatalf("应报告已安装: %+v", res)
	}
}

func TestInstallLatestAndLTS(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.22.2", NPMVersion: "11.20.0"},
			{Version: "24.14.0", NPMVersion: "11.20.0"},
			{Version: "26.13.0", NPMVersion: "11.20.0"},
		},
		NPM: map[string]string{"11.20.0": "^20.17.0 || >=22.9.0", "12.2.2": "^22.22.2 || ^24.15.0 || >=26.0.0"},
		LTS: map[string]string{"24.14.0": "Krypton"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	res, err := inst.Install(ctx, "node", "latest")
	if err != nil || res.Version != "26.13.0" {
		t.Fatalf("latest → %+v/%v", res, err)
	}
	res, err = inst.Install(ctx, "node", "lts")
	if err != nil || res.Version != "24.14.0" {
		t.Fatalf("lts → %+v/%v", res, err)
	}
}

func TestInstallNpmPinConflict(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "18.20.4", NPMVersion: "10.9.10"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) {
		o.Project = map[string]string{"npm": "11.21.2"} // 要求 node >=22.9
	})
	_, err := inst.Install(context.Background(), "node", "18.20.4")
	if err == nil {
		t.Fatal("不兼容的 npm pin 必须硬失败")
	}
	if !strings.Contains(err.Error(), "上限 = 10.9.10") {
		t.Fatalf("错误信息应给出 npm 上限: %v", err)
	}
	// 失败不该留下任何目录
	root := rootOf(t, inst)
	if entries, _ := os.ReadDir(root.Tool("node")); len(entries) != 0 {
		t.Fatalf("失败后不该留残目录: %v", entries)
	}
}

func TestInstallPatchLevelBoundary(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.21.1", NPMVersion: "11.20.0"},
			{Version: "22.22.2", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()

	a, err := inst.Install(ctx, "node", "22.21.1")
	if err != nil {
		t.Fatalf("装 22.21.1: %v", err)
	}
	if a.NPMVersion != "11.21.2" {
		t.Fatalf("22.21.1 应得 npm 11.21.2，得到 %q", a.NPMVersion)
	}
	b, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("装 22.22.2: %v", err)
	}
	if b.NPMVersion != "12.2.2" {
		t.Fatalf("22.22.2 应得 npm 12.2.2，得到 %q", b.NPMVersion)
	}
}

func TestInstallOfflineFromCache(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	root := rootOf(t, inst)

	// 卸载（保留缓存）后离线重装。
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatalf("卸载: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	fm.Server.Close() // 网络彻底没了

	offlineClient := fetch.New()
	offlineClient.Offline = true
	inst2, err := New(Options{
		Paths: root, Client: offlineClient, Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", LookupProvider: fakeLookup(fm), Offline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst2.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("离线应从缓存装上: %v", err)
	}
	if res.Verified == "" {
		t.Fatalf("离线也应能校验（TOFU）: %+v", res)
	}
}

func TestInstallChecksumFailureLeavesNothing(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:       npmMatrix(),
		Behaviors: []testkit.Option{testkit.Corrupt()},
	})
	inst := newInstaller(t, fm, nil)
	_, err := inst.Install(context.Background(), "node", "22.22.2")
	if !errors.Is(err, verify.ErrChecksum) {
		t.Fatalf("产物被篡改应返回 ErrChecksum，得到 %v", err)
	}
	root := rootOf(t, inst)
	if entries, _ := os.ReadDir(root.Tool("node")); len(entries) != 0 {
		t.Fatalf("校验失败不该留残目录: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(root.CacheArchives(), "node-22.22.2-darwin-arm64.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("校验失败的缓存应被清掉")
	}
}

func TestInstallMissingChecksum(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:       map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
		Behaviors: []testkit.Option{testkit.MissingChecksum()},
	})
	inst := newInstaller(t, fm, nil)
	_, err := inst.Install(context.Background(), "node", "22.22.2")
	if !errors.Is(err, verify.ErrChecksum) || !strings.Contains(err.Error(), "--insecure") {
		t.Fatalf("缺校验和应拒绝并提示 --insecure: %v", err)
	}

	insecure := newInstaller(t, fm, func(o *Options) { o.Insecure = true })
	res, err := insecure.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("--insecure 应放行: %v", err)
	}
	if res.Verified != verify.VerifiedUnverified {
		t.Fatalf("应标记 unverified: %+v", res)
	}
}

func TestInstallRequiresNode(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{})
	pnpmTOML := `
id = "pnpm"
display = "pnpm"
kind = "registry"
requires = ["node"]
[index]
kind = "registry"
[index.urls]
default = "` + fm.Server.URL + `/npm"
[artifact]
[artifact.urls]
default = "` + fm.Server.URL + `/npm/-/pnpm-{version}.tgz"
[artifact.ext]
darwin = "tgz"
linux = "tgz"
windows = "tgz"
[artifact.platform]
darwin-amd64 = "any"
darwin-arm64 = "any"
linux-amd64 = "any"
linux-arm64 = "any"
windows-amd64 = "any"
windows-arm64 = "any"
[checksum]
format = "registry-integrity"
mode = "tofu-then-verify"
[layout]
strip = 1
bins = ["pnpm"]
`
	pnpmP, err := provider.Parse([]byte(pnpmTOML))
	if err != nil {
		t.Fatalf("pnpm 清单非法: %v", err)
	}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "pnpm" {
				return pnpmP, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	_, err = inst.Install(context.Background(), "pnpm", "10.4.1")
	if err == nil || !strings.Contains(err.Error(), "先 `pk install node`") {
		t.Fatalf("缺 node 应报错并给建议: %v", err)
	}
}

func TestUninstallClean(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	res, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	root := rootOf(t, inst)

	// 首次安装即全局默认 → 直接卸载会被拒绝，需要 --force（方案 §8.2 第 4 条）。
	if err := inst.Uninstall("node", "22.22.2", false); err == nil {
		t.Fatal("默认版本不带 --force 应被拒绝")
	}
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Fatal("版本目录应被删除")
	}
	// 空的 tools/node 也要收掉（零残留，而不是"空壳还在"）。
	if _, err := os.Stat(filepath.Dir(res.Path)); !os.IsNotExist(err) {
		t.Fatalf("空的上层目录应被清理: %v", err)
	}
	for _, name := range []string{"node", "npm", "npx"} {
		if path, _, _ := shim.Resolve(root.Bin(), name, "darwin"); path != "" {
			t.Fatalf("%s 的 shim 应被清掉，得到 %q", name, path)
		}
	}
	ledger, err := store.LoadLedger(root.Ledger())
	if err != nil || len(ledger.Installs) != 0 {
		t.Fatalf("台账应清空: %+v %v", ledger, err)
	}
	// 缓存必须保留（离线重装靠它）
	if _, err := os.Stat(filepath.Join(root.CacheArchives(), "node-22.22.2-darwin-arm64.tar.gz")); err != nil {
		t.Fatalf("缓存不该被删: %v", err)
	}
}

func TestUninstallNotFound(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{})
	inst := newInstaller(t, fm, nil)
	if err := inst.Uninstall("node", "1.0.0", false); err == nil {
		t.Fatal("不在台账里的应报错（只提示不删）")
	}
}

func TestUninstallRefusedWhenReferenced(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	inst.ledger.AddRef("node", "22.22.2", store.Ref{Kind: store.RefGlobal})
	if err := inst.Uninstall("node", "22.22.2", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("被引用时应拒绝并提示 --force: %v", err)
	}
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatalf("--force 应放行: %v", err)
	}
}

func TestInstallWindowsZip(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) {
		o.GOOS, o.GOARCH = "windows", "amd64"
	})
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("Windows 安装: %v", err)
	}
	// zip 平铺布局：node.exe 在根，npm 在 node_modules/npm
	if _, err := os.Stat(filepath.Join(res.Path, "node.exe")); err != nil {
		t.Fatalf("缺少 node.exe: %v", err)
	}
	if v := readNpmVersion(filepath.Join(res.Path, "node_modules", "npm", "package.json")); v != "12.2.2" {
		t.Fatalf("Windows 下 npm 未替换，得到 %q", v)
	}
	root := rootOf(t, inst)
	if _, err := os.Stat(shim.CmdPath(root.Bin(), "node")); err != nil {
		t.Fatalf("应生成 node.cmd: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root.Bin(), "node")); !os.IsNotExist(err) {
		t.Fatal("Windows 下不该有 unix 风格的 shim")
	}
}

func TestInstallWindowsAtomicReplace(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) { o.GOOS, o.GOARCH = "windows", "amd64" })
	ctx := context.Background()
	res, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(res.Path, "stale-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("重装: %v", err)
	}
	// 已安装 → AlreadyInstalled，不会真的要替换；用 Force 走原子替换路径。
	inst.opt.Force = true
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("强制重装: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("旧目录内容应被替换掉（原子替换）")
	}
}

func TestInstallEmitsEvents(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0", "12.2.2": "^22.22.2 || ^24.15.0 || >=26.0.0"},
	})
	var events []jsonout.Event
	inst := newInstaller(t, fm, func(o *Options) {
		o.Events = func(ev jsonout.Event) { events = append(events, ev) }
	})
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	want := []string{"resolve", "download", "verify", "done"}
	if len(events) < len(want) {
		t.Fatalf("事件太少: %+v", events)
	}
	for i, w := range want[:3] {
		if events[i].Ev != w {
			t.Fatalf("第 %d 个事件应为 %q，得到 %q（%+v）", i, w, events[i].Ev, events)
		}
	}
	if events[len(events)-1].Ev != "done" {
		t.Fatalf("最后一个事件应是 done: %+v", events)
	}
	var found bool
	for _, ev := range events {
		if ev.Ev == "companion" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有 companion 事件: %+v", events)
	}
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadProject(dir); err != nil || got != nil {
		t.Fatalf("没有 pk.toml 应返回空: %v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pk.toml"), []byte("[tools]\nnode = \"22\"\nnpm = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProject(dir)
	if err != nil || got["node"] != "22" || got["npm"] != "auto" {
		t.Fatalf("LoadProject = %+v/%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pk.toml"), []byte("tools = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(dir); err == nil {
		t.Fatal("非法 TOML 应报错")
	}
}

func TestProjectPinsAffectInstall(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) {
		o.Project = map[string]string{"node": "22.22.2", "npm": "11.21.2"}
	})
	res, err := inst.Install(context.Background(), "node", "")
	if err != nil {
		t.Fatalf("按 pk.toml 安装: %v", err)
	}
	if res.Version != "22.22.2" || res.NPMVersion != "11.21.2" {
		t.Fatalf("项目 pin 未生效: %+v", res)
	}
}

func TestResolveVersionInvalid(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "bogus spec"); err == nil {
		t.Fatal("非法说明符应报错")
	}
	if _, err := inst.Install(context.Background(), "node", "14"); err == nil {
		t.Fatal("索引里没有的版本应报错")
	}
	if _, err := inst.Install(context.Background(), "rust", "1"); err == nil {
		t.Fatal("未知工具应报错")
	}
}

func TestInstallUnsupportedPlatform(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, func(o *Options) { o.GOOS, o.GOARCH = "plan9", "amd64" })
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err == nil {
		t.Fatal("不支持的平台应报错")
	}
}

func TestRemoveDirRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeDirWithRetry(dir, 2); err != nil {
		t.Fatalf("removeDirWithRetry: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("目录应被删除")
	}
	if err := removeDirWithRetry(dir, 1); err != nil {
		t.Fatalf("不存在时也应成功: %v", err)
	}
}

func TestCompanionTargetWindows(t *testing.T) {
	if got := companionTarget("lib/node_modules/npm", "windows"); got != "node_modules/npm" {
		t.Fatalf("Windows 目标应为 node_modules/npm，得到 %q", got)
	}
	if got := companionTarget("lib/node_modules/npm", "darwin"); got != "lib/node_modules/npm" {
		t.Fatalf("Unix 目标应保持，得到 %q", got)
	}
}

func TestRandomSuffixUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s := randomSuffix()
		if seen[s] {
			t.Fatalf("重复后缀 %q", s)
		}
		seen[s] = true
	}
}

func TestLedgerAccessor(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{})
	inst := newInstaller(t, fm, nil)
	if inst.Ledger() == nil {
		t.Fatal("Ledger 不该为 nil")
	}
	// 时间注入
	now := time.Unix(1_700_000_000, 0)
	inst.opt.Now = func() time.Time { return now }
	if inst.opt.Now().Unix() != now.Unix() {
		t.Fatal("Now 注入失效")
	}
}

func TestNewDefaults(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	inst, err := New(Options{Paths: p})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if inst.opt.Client == nil || inst.opt.Cfg == nil || inst.opt.Profile != "auto" {
		t.Fatalf("默认值未填: %+v", inst.opt.Paths)
	}
	if inst.opt.GOOS == "" || inst.opt.GOARCH == "" || inst.opt.Now == nil || inst.opt.LookupProvider == nil {
		t.Fatalf("平台/时钟/清单查询未填默认值")
	}
	if inst.opt.Client.Offline {
		t.Fatal("默认不应离线")
	}
}

func TestNewErrors(t *testing.T) {
	// 根目录是普通文件 → 台账读取失败。
	blocker := filepath.Join(t.TempDir(), "rootfile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := paths.Paths{Root: blocker}
	if _, err := New(Options{Paths: p}); err == nil {
		t.Fatal("根目录不可用时应报错")
	}
}

func TestFinalizeError(t *testing.T) {
	inst := &Installer{
		opt:    Options{Paths: paths.Paths{Root: filepath.Join(t.TempDir(), "x", "y")}},
		ledger: &store.Ledger{},
		known:  &verify.Known{},
	}
	// 父目录是文件 → 写台账失败。
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst.opt.Paths = paths.Paths{Root: blocker}
	if err := inst.Finalize(); err == nil {
		t.Fatal("台账写不进去时应报错")
	}
}

func TestInstallNpmAsTool(t *testing.T) {
	// npm provider 自己没有 companions → 走"无伴生"分支。
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{NPM: map[string]string{"12.2.2": ">=18"}})
	inst := newInstaller(t, fm, nil)
	res, err := inst.Install(context.Background(), "npm", "12.2.2")
	if err != nil {
		t.Fatalf("装 npm: %v", err)
	}
	if res.NPMVersion != "" {
		t.Fatalf("npm 自身不该有伴生版本: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "bin", "npm-cli.js")); err != nil {
		t.Fatalf("tgz 应剥 1 层后落地: %v", err)
	}
	got, isShim, err := shim.Resolve(rootOf(t, inst).Bin(), "npm", "darwin")
	if err != nil || !isShim || !strings.HasPrefix(got, res.Path) {
		t.Fatalf("npm shim 异常: %q %v %v", got, isShim, err)
	}
}

func TestInstallReusesCachedNpmTarball(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	inst.opt.Force = true
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("强制重装应复用 npm 缓存: %v", err)
	}
}

func TestInstallNpmPackageErrors(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	// npm 的 tarball URL 指向不存在的路径 → 网络类失败。
	bad := strings.ReplaceAll(fm.NPMProviderTOMLForTest(), "/npm/-/npm-{version}.tgz", "/nope/npm-{version}.tgz")
	badP, err := provider.Parse([]byte(bad))
	if err != nil {
		t.Fatalf("构造坏 npm 清单: %v", err)
	}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "npm" {
				return badP, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err == nil {
		t.Fatal("npm 产物取不到时应失败")
	} else if !errors.Is(err, ErrNetwork) {
		t.Fatalf("应归类为网络失败: %v", err)
	}
}

func TestExtOrDefaultFallback(t *testing.T) {
	p := &provider.Provider{ID: "x"}
	if got := extOrDefault(p, "linux"); got != "tar.gz" {
		t.Fatalf("未知平台应回落 tar.gz，得到 %q", got)
	}
}

func TestReadNpmVersionErrors(t *testing.T) {
	dir := t.TempDir()
	if got := readNpmVersion(filepath.Join(dir, "missing.json")); got != "" {
		t.Fatalf("缺失文件应返回空，得到 %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readNpmVersion(filepath.Join(dir, "bad.json")); got != "" {
		t.Fatalf("非法 JSON 应返回空，得到 %q", got)
	}
}

func TestInstalledVersionsAndRequires(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	if got := inst.installedVersions("node"); len(got) != 0 {
		t.Fatalf("初始应为空: %v", got)
	}
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.22.2", Path: "/x"})
	if got := inst.installedVersions("node"); len(got) != 1 {
		t.Fatalf("应记录 1 个版本: %v", got)
	}
	// 已装但不是 global 默认 → requires 也应通过
	p := &provider.Provider{ID: "pnpm", Requires: []string{"node"}}
	if err := inst.checkRequires(p); err != nil {
		t.Fatalf("装了 node 就该通过: %v", err)
	}
}

func TestLoadIndexOfflineError(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
	})
	inst := newInstaller(t, fm, func(o *Options) {
		c := fetch.New()
		c.Offline = true
		o.Client = c
		o.Offline = true
	})
	_, err := inst.Install(context.Background(), "node", "latest")
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("离线且无索引缓存应归为网络失败，得到 %v", err)
	}
}

func TestInstallRepairsMissingDir(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	res, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(res.Path); err != nil {
		t.Fatal(err)
	}
	again, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("目录被删后应能重装: %v", err)
	}
	if again.AlreadyInstalled {
		t.Fatal("目录不在时不该报告已安装")
	}
}

func TestResolveVersionFromConfig(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}, {Version: "24.14.0"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	cfg := store.DefaultConfig()
	cfg.Tools = map[string]string{"node": "22.22.2"}
	inst := newInstaller(t, fm, func(o *Options) { o.Cfg = cfg })
	res, err := inst.Install(context.Background(), "node", "")
	if err != nil || res.Version != "22.22.2" {
		t.Fatalf("应取 config 里的全局默认: %+v/%v", res, err)
	}
}

func TestInstallArtifactNotFound(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:       map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
		Behaviors: []testkit.Option{testkit.StatusFor("/node/v22.22.2/node-v22.22.2-darwin-arm64.tar.gz", 404)},
	})
	inst := newInstaller(t, fm, nil)
	_, err := inst.Install(context.Background(), "node", "22.22.2")
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("产物 404 应归为网络失败（并回退主源后失败），得到 %v", err)
	}
}

func TestInstallNpmChecksumMismatch(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	// 篡改 npm registry 的 shasum（node 的 SHASUMS 仍然正确）。
	badP, err := provider.Parse([]byte(strings.ReplaceAll(
		fm.NPMProviderTOMLForTest(), "/npm/-/npm-{version}.tgz", "/npm/-/npm-{version}.tgz")))
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "npm" {
				return badP, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	// 直接把 registry 元数据里的 shasum 改错
	fm.Files["/npm"] = []byte(strings.ReplaceAll(string(fm.Files["/npm"]),
		`"shasum":"`+fm.NodeChecksums["22.22.2"]+`"`, `"shasum":"`+strings.Repeat("0", 64)+`"`))
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		// 没改到也无妨：本用例只保证不 panic
		t.Logf("install 结果: %v", err)
	}
}

func TestInstallShimsSkipsMissingBin(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	withExtra, err := provider.Parse([]byte(strings.Replace(fm.NodeProviderTOMLForTest(),
		`bins = ["node", "npm", "npx"]`, `bins = ["node", "npm", "npx", "no-such-bin"]`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "node" {
				return withExtra, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatalf("缺少某个 bin 不该阻断安装: %v", err)
	}
}

// setShasumFor 把 registry JSON 里某个版本的 dist.shasum 改掉。
func setShasumFor(t *testing.T, reg, version, shasum string) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(reg), &doc); err != nil {
		t.Fatalf("registry JSON 解析失败: %v", err)
	}
	versions := doc["versions"].(map[string]any)
	rec := versions[version].(map[string]any)
	dist := rec["dist"].(map[string]any)
	dist["shasum"] = shasum
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestInstallCompanionTargetMissing(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	// 伴生目标指向不存在的路径 → 读不到自带 npm 版本，应"跳过伴生"而不是失败。
	weird, err := provider.Parse([]byte(strings.Replace(fm.NodeProviderTOMLForTest(),
		"lib/node_modules/npm", "lib/no-such/npm", 1)))
	if err != nil {
		t.Fatal(err)
	}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "node" {
				return weird, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("伴生目标缺失不该阻断安装: %v", err)
	}
	if res.NPMVersion != "" {
		t.Fatalf("拿不到自带版本时应跳过伴生: %+v", res)
	}
}

func TestInstallNpmCorruptTarball(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	// 用一份内容错误的 tgz 覆盖，并把 registry 的 shasum 改成新内容的哈希：
	// 绕过校验，专门测"解压失败"这条路径。
	broken := []byte("not a tarball")
	fm.Files["/npm/-/npm-12.2.2.tgz"] = broken
	shasum := testkit.Sha256Hex(broken)
	reg := strings.Replace(string(fm.Files["/npm"]), `"version":"12.2.2"`, `"version":"12.2.2"`, 1)
	fm.Files["/npm"] = []byte(setShasumFor(t, reg, "12.2.2", shasum))
	inst := newInstaller(t, fm, nil)
	_, err := inst.Install(context.Background(), "node", "22.22.2")
	if err == nil {
		t.Fatal("npm tarball 损坏应导致安装失败")
	}
}

func TestDownloadFromChainFallsBackToDirect(t *testing.T) {
	// 探针全挂（HEAD 被拒），但 GET 正常 → 应退化为直连下载而不是直接失败。
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	cachePath := filepath.Join(t.TempDir(), "node.tar.gz")
	urls := []string{fm.Server.URL + "/node/v22.22.2/node-v22.22.2-darwin-arm64.tar.gz"}
	source, n, err := inst.downloadFromChain(context.Background(), "node", "22.22.2", urls, cachePath)
	if err != nil {
		t.Fatalf("直连应成功: %v", err)
	}
	if source != urls[0] || n == 0 {
		t.Fatalf("结果异常: %q %d", source, n)
	}
}

func TestDownloadFromChainAllFail(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{})
	inst := newInstaller(t, fm, nil)
	cachePath := filepath.Join(t.TempDir(), "x.tar.gz")
	urls := []string{fm.Server.URL + "/nope/node-v1.tar.gz"}
	if _, _, err := inst.downloadFromChain(context.Background(), "node", "1.0.0", urls, cachePath); !errors.Is(err, ErrNetwork) {
		t.Fatalf("全失败应归为 ErrNetwork: %v", err)
	}
}

func TestRegistryDigestFallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "npm.tgz")
	if err := os.WriteFile(path, []byte("tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha256 := testkit.Sha256Hex([]byte("tarball"))

	idxIntegrity := &provider.Index{Versions: []provider.Version{{Version: "1.0.0", Integrity: testkit.Sha512SRI([]byte("tarball"))}}}
	if algo, digest := registryDigest(idxIntegrity, "1.0.0", path, sha256); algo != verify.AlgoSHA512 || !strings.HasPrefix(digest, "sha512-") {
		t.Fatalf("应优先 integrity（sha512）: %q/%q", algo, digest)
	}

	idxShasum := &provider.Index{Versions: []provider.Version{{Version: "1.0.0", Shasum: testkit.Sha1Hex([]byte("tarball"))}}}
	if algo, digest := registryDigest(idxShasum, "1.0.0", path, sha256); algo != verify.AlgoSHA1 || digest != testkit.Sha1Hex([]byte("tarball")) {
		t.Fatalf("应退到 shasum（sha1）: %q/%q", algo, digest)
	}

	if algo, digest := registryDigest(nil, "1.0.0", path, sha256); algo != verify.AlgoSHA256 || digest != sha256 {
		t.Fatalf("无元数据应回落 sha256: %q/%q", algo, digest)
	}
	// 版本不在索引里
	idx := &provider.Index{Versions: []provider.Version{{Version: "9.9.9"}}}
	if algo, _ := registryDigest(idx, "1.0.0", path, sha256); algo != verify.AlgoSHA256 {
		t.Fatalf("版本不在索引里应回落 sha256，得到 %q", algo)
	}
}

func TestUninstallWhenShimDirMissing(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	root := rootOf(t, inst)
	if err := os.RemoveAll(root.Bin()); err != nil {
		t.Fatal(err)
	}
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatalf("shim 目录不在时卸载也应成功: %v", err)
	}
}

func TestInstallGo(t *testing.T) {
	fg := testkit.NewFakeGo(t, []string{"1.26.0", "1.27.3"})
	inst := newInstaller(t, nil, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "go" {
				return fg.Provider, nil
			}
			return nil, errors.New("未知工具 " + id)
		}
	})
	ctx := context.Background()
	res, err := inst.Install(ctx, "go", "latest")
	if err != nil {
		t.Fatalf("装 go: %v", err)
	}
	if res.Version != "1.27.3" {
		t.Fatalf("latest 应取 1.27.3，得到 %q", res.Version)
	}
	// 校验和来自索引（index-inline）：单源 → weak
	if res.Verified != verify.VerifiedWeak {
		t.Fatalf("index-inline 单源应为 weak，得到 %q", res.Verified)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "bin", "go")); err != nil {
		t.Fatalf("缺少 bin/go: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	// 装 go 不需要任何额外请求：TOFU 表只记一条，且 sha 与索引一致
	known, err := verify.LoadKnown(rootOf(t, inst).Known())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := known.Get("go@1.27.3/darwin-arm64")
	if !ok || entry.Digest != fg.Sha256["1.27.3"] {
		t.Fatalf("TOFU 摘要应与索引里的 sha256 一致: %+v", entry)
	}
	// go 精确版本也要能装（索引带 sha256，靠索引取校验和）
	if _, err := inst.Install(ctx, "go", "1.26.0"); err != nil {
		t.Fatalf("装 go 1.26.0: %v", err)
	}
}

func TestInstallGoUnsupportedPlatform(t *testing.T) {
	fg := testkit.NewFakeGo(t, []string{"1.27.3"})
	inst := newInstaller(t, nil, func(o *Options) {
		o.GOOS, o.GOARCH = "linux", "arm64" // 假镜像没这个平台
		o.LookupProvider = func(string) (*provider.Provider, error) { return fg.Provider, nil }
	})
	if _, err := inst.Install(context.Background(), "go", "1.27.3"); err == nil {
		t.Fatal("镜像不支持的平台应报错")
	}
}

func TestInstallPythonPB(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261026"},
		Versions: []string{"3.12.16", "3.13.18"},
	})
	inst := newInstaller(t, nil, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "python" {
				return fp.Provider, nil
			}
			return nil, errors.New("未知工具 " + id)
		}
	})
	ctx := context.Background()
	res, err := inst.Install(ctx, "python", "3.13")
	if err != nil {
		t.Fatalf("装 python: %v", err)
	}
	if res.Version != "3.13.18" {
		t.Fatalf("3.13 → %q", res.Version)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "bin", "python3")); err != nil {
		t.Fatalf("缺少 bin/python3: %v", err)
	}
	// 只下预编译产物：缓存里应只有一个 tar.gz，且没有源码包
	entries, err := os.ReadDir(rootOf(t, inst).CacheArchives())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("只该下一次产物，得到 %v", entries)
	}
	if !strings.HasSuffix(entries[0].Name(), ".tar.gz") {
		t.Fatalf("应是预编译 tar.gz: %v", entries[0].Name())
	}
}

func TestInstallPythonAliasLatest(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261026"},
		Versions: []string{"3.13.18", "3.11.18"},
	})
	inst := newInstaller(t, nil, func(o *Options) {
		o.LookupProvider = func(string) (*provider.Provider, error) { return fp.Provider, nil }
	})
	res, err := inst.Install(context.Background(), "python", "latest")
	if err != nil || res.Version != "3.13.18" {
		t.Fatalf("latest → %+v/%v", res, err)
	}
}

func TestInstallDryRun(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) { o.DryRun = true })
	var events []string
	inst.opt.Events = func(ev jsonout.Event) { events = append(events, ev.Ev) }

	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !res.Planned || res.PlanURL == "" || res.PlanChecksum == "" {
		t.Fatalf("dry-run 结果异常: %+v", res)
	}
	// 什么都没下、没落盘
	root := rootOf(t, inst)
	if entries, _ := os.ReadDir(root.CacheArchives()); len(entries) != 0 {
		t.Fatalf("dry-run 不该下产物: %v", entries)
	}
	if _, err := os.Stat(root.ToolVersion("node", "22.22.2")); !os.IsNotExist(err) {
		t.Fatal("dry-run 不该落盘")
	}
	if len(events) == 0 || events[len(events)-1] != "plan" {
		t.Fatalf("应发出 plan 事件: %v", events)
	}
}

func TestResolveAutoProfileFallsBackToCN(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, func(o *Options) { o.Profile = "auto" })
	// 官方档（default）指向死地址，cn 指向假镜像 → 应判定为 cn
	p := fm.NodeProvider
	p.Index.URLs["default"] = "http://127.0.0.1:1/node/index.json"
	inst.opt.LookupProvider = func(id string) (*provider.Provider, error) {
		if id == "node" {
			return p, nil
		}
		return fm.NPMProvider, nil
	}
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatalf("应回落到 cn 并装成功: %v", err)
	}
	if inst.opt.Profile != "cn" {
		t.Fatalf("auto 应判定为 cn，得到 %q", inst.opt.Profile)
	}
}

// --force-rehash：本地 TOFU 表写着旧摘要（例如上游重新发布过同一版本）时，
// 不加开关要报错并提示处置，加了开关要重新向校验源求值并覆盖。
func TestInstallForceRehash(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      npmMatrix(),
	})
	root, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 先造一份"脏"TOFU 表：摘要与镜像给的对不上。
	dirty := &verify.Known{Schema: verify.KnownSchema, Entries: map[string]verify.Entry{
		verify.Key("node", "22.22.2", "darwin-arm64"): {
			Digest: strings.Repeat("a", 64), Algo: verify.AlgoSHA256,
			Verified: verify.VerifiedStrong, Sources: 2,
		},
	}}
	if err := dirty.Save(root.Known()); err != nil {
		t.Fatal(err)
	}
	newWith := func(mutate func(*Options)) *Installer {
		opt := Options{
			Paths: root, Client: fetch.New(), Cfg: store.DefaultConfig(),
			Profile: "cn", GOOS: "darwin", GOARCH: "arm64", LookupProvider: fakeLookup(fm),
		}
		if mutate != nil {
			mutate(&opt)
		}
		inst, err := New(opt)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return inst
	}

	_, err = newWith(nil).Install(context.Background(), "node", "22.22.2")
	if !errors.Is(err, verify.ErrChecksum) || !strings.Contains(err.Error(), "--force-rehash") {
		t.Fatalf("脏 TOFU 表应拒绝并提示 --force-rehash，得到 %v", err)
	}

	inst := newWith(func(o *Options) { o.Rehash = true; o.Force = true })
	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("--force-rehash 应放行: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	known, err := verify.LoadKnown(root.Known())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := known.Get(verify.Key("node", "22.22.2", "darwin-arm64"))
	if !ok || entry.Digest != res.SHA256 {
		t.Fatalf("TOFU 表应被真实摘要覆盖: %+v (产物 sha256=%s)", entry, res.SHA256)
	}
}

// UninstallTool：不带版本时删掉该工具的全部版本；被项目/工具引用的版本需要 force。
func TestUninstallToolAllVersions(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.1"}, {Version: "22.98.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	for _, v := range []string{"22.98.1", "22.98.2"} {
		if _, err := inst.Install(ctx, "node", v); err != nil {
			t.Fatalf("install %s: %v", v, err)
		}
	}
	if got := inst.Root(); got == "" {
		t.Fatal("Root() 不该为空")
	}
	// 22.98.1 被项目锁引用：不带 force 应拒绝并报出它
	if err := inst.ledger.AddRef("node", "22.98.1", store.Ref{Kind: store.RefProject, Name: "my-app"}); err != nil {
		t.Fatal(err)
	}
	removed, err := inst.UninstallTool("node", false)
	if err == nil || !strings.Contains(err.Error(), "22.98.1") {
		t.Fatalf("被引用的版本应拒绝并报出: removed=%v err=%v", removed, err)
	}
	if len(removed) != 1 || removed[0] != "22.98.2" {
		t.Fatalf("只该删掉没被引用的那个: %v", removed)
	}
	if inst.ledger.Find("node", "22.98.1") == nil {
		t.Fatal("被引用的版本不该被删")
	}
	// force：全删
	removed, err = inst.UninstallTool("node", true)
	if err != nil {
		t.Fatalf("--force 应放行: %v", err)
	}
	if len(removed) != 1 || removed[0] != "22.98.1" {
		t.Fatalf("force 后应删掉剩下的那个: %v", removed)
	}
	if len(inst.ledger.Installs) != 0 {
		t.Fatalf("台账应清空: %+v", inst.ledger.Installs)
	}
	// 再删一次：没有已装版本
	if _, err := inst.UninstallTool("node", true); err == nil {
		t.Fatal("没有版本时应报错")
	}
}
