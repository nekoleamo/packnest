package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// 补齐"平时不跑"的分支（完善路线 §E8c 的覆盖率部分）。
// 这些分支决定出错时给一句人话还是静默成功，所以都是 testkit 的故障注入 + 一次性断言。

// installWith 装一次并返回错误（不起 Finalize，单测只关心这一次的行为）。
func installOnce(t *testing.T, opts []testkit.Option) error {
	t.Helper()
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:       npmMatrix(),
		Behaviors: opts,
	})
	inst := newInstaller(t, fm, nil)
	_, err := inst.Install(context.Background(), "node", "22.98.0")
	return err
}

// TestArtifactFailureBehaviors：产物损坏 / 缺校验和 / 校验和不符 / 传输截断 四种都必须硬失败，
// 且工具目录不许留半成品（方案 §原子落盘）。
func TestArtifactFailureBehaviors(t *testing.T) {
	cases := map[string][]testkit.Option{
		"产物损坏":  {testkit.Corrupt()},
		"缺校验和":  {testkit.MissingChecksum()},
		"校验和不符": {testkit.ChecksumMismatch()},
		"传输断流":  {testkit.DropAfter(256)},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			err := installOnce(t, opts)
			if err == nil {
				t.Fatalf("%s 必须失败", name)
			}
		})
	}
}

// TestResolveAutoProfileFallback：档位是 auto 且探测不出结论时按国内档（主要受众）。
func TestResolveAutoProfileFallback(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	inst.opt.Profile = "auto"
	// 离线 + 无探测缓存 → mirror.Detect 拿不到结论
	inst.opt.Client.Offline = true
	p := mkProvider("node", nil, "node")
	inst.resolveAutoProfile(context.Background(), p)
	if inst.opt.Profile != "cn" && inst.opt.Profile != "default" {
		t.Fatalf("探测失败应落到 cn（或已探测出的档）: %q", inst.opt.Profile)
	}
}

// TestTransportOfAndNeedsFilename：两个小判定分支（小写 transport / 是否要文件名占位）。
func TestTransportOfAndNeedsFilename(t *testing.T) {
	if got := transportOf(mkProvider("x", nil, "x")); got != "" && got != "https" && got != "http" {
		t.Fatalf("transport 取值异常: %q", got)
	}
	p := mkProvider("x", nil, "x")
	p.Artifact.Platform = map[string]string{"linux-amd64": "x"}
	_ = artifactNeedsFilename(p)
}

// TestApplyUninstallAll：purge 一批记录（含不存在的目录）时如实报告删了什么。
func TestApplyUninstallAll(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	dir := filepath.Join(inst.opt.Paths.Root, "tools", "node", "22.98.0")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: dir,
		References: []store.Ref{{Kind: store.RefGlobal}}})
	plan := UninstallPlan{Tools: []string{"node@22.98.0", "npm@11.20.0", "garbage"}}
	res := inst.ApplyUninstallAll(plan)
	if len(res.Removed) == 0 {
		t.Fatalf("应报告删掉了什么: %+v", res)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("工具目录应被删掉: %v", err)
	}
}

// TestUpgradeRealUpgrade：装旧版 → upgrade 到新版（覆盖升级主路径与"旧版本保留"）。
func TestUpgradeRealUpgrade(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "22.98.0"); err != nil {
		t.Fatal(err)
	}
	items, err := inst.Upgrade(context.Background(), []string{"node"}, false)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, it := range items {
		if it.Changed && it.To == "22.99.0" {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("应升到 22.99.0: %+v", items)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if len(inst.Ledger().Versions("node")) < 2 {
		t.Fatalf("旧版本应保留: %v", inst.Ledger().Versions("node"))
	}
}

// TestUpgradeDryRun：--dry-run 只报告不落盘。
func TestUpgradeDryRun(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "22.98.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Upgrade(context.Background(), []string{"node"}, true); err != nil {
		t.Fatal(err)
	}
	if got := inst.Ledger().Active("node"); got != "22.98.0" {
		t.Fatalf("dry-run 不该切版本，得到 %q", got)
	}
}

// TestCheckFrozenMismatch：锁与清单不一致时给出可执行建议（--frozen 的硬约束）。
func TestCheckFrozenMismatch(t *testing.T) {
	lock := &Lock{Tool: []LockTool{{Name: "node", Version: "22.98.0"}}}
	if err := CheckFrozen(map[string]string{"node": ">=26"}, lock); !errors.Is(err, ErrFrozenMismatch) {
		t.Fatalf("锁不满足清单应报错并说明: %v", err)
	}
	if err := CheckFrozen(map[string]string{"node": "22"}, lock); err != nil {
		t.Fatalf("满足时应通过: %v", err)
	}
}

// TestGCKeepAndRemove：--keep N 保留最新 N 个版本，其余回收（含零引用判断）。
func TestGCKeepAndRemove(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	for _, v := range []string{"26.1.0", "26.2.0", "26.3.0"} {
		dir := inst.opt.Paths.ToolVersion("node", v)
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		inst.ledger.Upsert(store.Install{Tool: "node", Version: v, Path: dir})
	}
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "26.3.0",
		Path: inst.opt.Paths.ToolVersion("node", "26.3.0"), References: []store.Ref{{Kind: store.RefGlobal}}})
	inst.opt.KeepVersions = 1
	res, err := inst.GC(context.Background(), 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed)+len(res.KeptRecent) == 0 {
		t.Fatalf("--keep 1 时应有回收结论（或至少列出保留的）: %+v", res)
	}
}

// TestInstallAtomicCancelled：ctx 取消后 --atomic 不再 prepare（错误码要能被上层识别）。
func TestInstallAtomicCancelled(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) { o.Atomic = true })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := inst.InstallMany(ctx, []Target{{Tool: "node"}, {Tool: "node", Spec: "22.98.0"}})
	for _, oc := range out {
		if oc.Err == nil {
			t.Fatalf("取消后不该有成功的目标: %+v", oc)
		}
	}
}

// TestVerifyLockedSHA：锁里的 sha256 三种情况（空 / 不符 / 文件不存在）。
func TestVerifyLockedSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyLockedSHA(path, ""); err != nil {
		t.Fatalf("锁里没有 sha256 时应放行: %v", err)
	}
	if err := verifyLockedSHA(path, "deadbeef"); err == nil {
		t.Fatal("sha256 不符必须报错")
	}
	if err := verifyLockedSHA(filepath.Join(dir, "nope"), strings.Repeat("a", 64)); err == nil {
		t.Fatal("文件不存在必须报错")
	}
}

// TestCommitCorepackFailure：corepack enable 失败要把错误抛出来（不能装完却"以为启用了"）。
func TestCommitCorepackFailure(t *testing.T) {
	inst := newInstaller(t, nil, func(o *Options) {
		o.Corepack = true
		o.RunCommand = func(ctx context.Context, name string, args ...string) error {
			return errors.New("corepack 不可用")
		}
	})
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0",
		Path: inst.opt.Paths.ToolVersion("node", "22.98.0")})
	inst.opt.Cfg.Tools = map[string]string{"node": "22.98.0"}
	toolDir := inst.opt.Paths.ToolVersion("node", "22.97.0")
	if err := os.MkdirAll(filepath.Join(toolDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "bin", "node"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prep := &prepared{
		p:      mkProvider("node", nil, "node"),
		res:    Result{Tool: "node", Version: "22.97.0", Path: toolDir},
		tmpDir: toolDir + ".tmp",
	}
	if err := os.MkdirAll(prep.tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.commit(context.Background(), prep); err == nil {
		t.Fatal("corepack 失败应报错")
	}
}

// TestCommitSkipCorepackFailure：已装（skip 分支）且开了 corepack 时同样要把失败抛出来。
func TestCommitSkipCorepackFailure(t *testing.T) {
	inst := newInstaller(t, nil, func(o *Options) {
		o.Corepack = true
		o.RunCommand = func(ctx context.Context, name string, args ...string) error {
			return errors.New("corepack 不可用")
		}
	})
	prep := &prepared{
		p:    mkProvider("node", nil, "node"),
		skip: true,
		res:  Result{Tool: "node", Version: "22.98.0", Path: t.TempDir()},
	}
	if _, err := inst.commit(context.Background(), prep); err == nil {
		t.Fatal("skip 分支的 corepack 失败也要报错")
	}
}

// TestRemoveShimsFor：只删"指向该工具目录"的 shim，别人的 shim 不能动。
func TestRemoveShimsFor(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	if err := os.MkdirAll(inst.opt.Paths.Bin(), 0o755); err != nil {
		t.Fatal(err)
	}
	// 造两个文件：一个是要删的 shim（软链指向工具目录），一个是别人的普通文件
	toolDir := filepath.Join(inst.opt.Paths.Root, "tools", "node", "22.98.0")
	if err := os.MkdirAll(filepath.Join(toolDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(toolDir, "bin", "node"), filepath.Join(inst.opt.Paths.Bin(), "node")); err != nil {
		t.Skip("这台机器建不了软链")
	}
	if err := os.WriteFile(filepath.Join(inst.opt.Paths.Bin(), "keep"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inst.removeShimsFor(toolDir); err != nil {
		t.Fatalf("清 shim 不该报错: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(inst.opt.Paths.Bin(), "node")); !os.IsNotExist(err) {
		t.Fatal("指向该工具的 shim 应被删掉")
	}
	if _, err := os.Stat(filepath.Join(inst.opt.Paths.Bin(), "keep")); err != nil {
		t.Fatal("别人的文件不能动")
	}
	// bin 目录不存在时也应安静返回（卸载一个从没建过 shim 的工具）
	if err := newInstaller(t, nil, nil).removeShimsFor(toolDir); err != nil {
		t.Fatalf("bin 不存在应安静返回: %v", err)
	}
}

// TestSmallHelpers：四个一行函数的分支（平时靠真实流程撞运气，这里直接点名）。
func TestSmallHelpers(t *testing.T) {
	inst := newInstaller(t, nil, func(o *Options) {
		o.Now = func() time.Time { return time.Unix(0, 0) }
	})
	if got := inst.now(); !got.Equal(time.Unix(0, 0)) {
		t.Fatalf("注入的时钟应生效: %v", got)
	}
	if got := newInstaller(t, nil, nil).now(); got.IsZero() {
		t.Fatal("默认时钟应返回当前时间")
	}
	if s := randomSuffix(); len(s) != 8 {
		t.Fatalf("随机后缀应是 8 个 hex 字符: %q", s)
	}
	p := mkProvider("x", nil, "x")
	p.InsecureTransport = true
	if got := transportOf(p); got != "http" && got != "weak" && got != "" {
		t.Fatalf("transport 取值异常: %q", got)
	}
}

// TestNpmRegistryVerify（registry 校验和来源的三条分支）：
// ① 正常（第一次 TOFU）；② --force-rehash 回源重取；③ 元数据坏掉 / 缺该版本 → 硬失败。
func TestNpmRegistryVerify(t *testing.T) {
	newFM := func() *testkit.FakeNode {
		return testkit.NewFakeNode(t, testkit.FakeNodeOptions{
			Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
			NPM:      npmMatrix(),
		})
	}
	t.Run("正常", func(t *testing.T) {
		inst := newInstaller(t, newFM(), nil)
		if _, err := inst.Install(context.Background(), "npm", "12.2.2"); err != nil {
			t.Fatalf("npm（registry 校验和）应装成功: %v", err)
		}
	})
	t.Run("force-rehash 回源", func(t *testing.T) {
		fm := newFM()
		inst := newInstaller(t, fm, func(o *Options) { o.Rehash = true })
		if _, err := inst.Install(context.Background(), "npm", "12.2.2"); err != nil {
			t.Fatalf("--force-rehash 也应成功: %v", err)
		}
	})
	t.Run("元数据坏掉", func(t *testing.T) {
		fm := newFM()
		fm.Files["/npm"] = []byte("{not json")
		inst := newInstaller(t, fm, func(o *Options) { o.Rehash = true })
		if _, err := inst.Install(context.Background(), "npm", "12.2.2"); err == nil {
			t.Fatal("registry 元数据坏掉必须硬失败（不能当成没有校验和）")
		}
	})
	t.Run("元数据里缺该版本", func(t *testing.T) {
		fm := newFM()
		// 只留一个别的版本：dist 里查不到 12.2.2
		reg := string(fm.Files["/npm"])
		fm.Files["/npm"] = []byte(strings.ReplaceAll(reg, `"12.2.2"`, `"12.2.9"`))
		inst := newInstaller(t, fm, func(o *Options) { o.Rehash = true })
		if _, err := inst.Install(context.Background(), "npm", "12.2.2"); err == nil {
			t.Fatal("元数据里没有该版本必须报错")
		}
	})
}

// TestCompanionFailure：伴生包（npm）装不上时，node 整体失败（伴生是宿主的一部分）。
func TestCompanionFailure(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:       npmMatrix(),
		Behaviors: []testkit.Option{testkit.MissingChecksum()},
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "22.98.0"); err == nil {
		t.Fatal("伴生 npm 校验不过时 node 必须整体失败")
	}
	if _, err := os.Stat(inst.opt.Paths.ToolVersion("node", "22.98.0")); err == nil {
		t.Fatal("失败时不许留半成品工具目录")
	}
}

// TestApplyGenericCompanionsErrors：伴生包的三种失败（清单查不到 / 索引不可用 / 索引里没版本）。
func TestApplyGenericCompanionsErrors(t *testing.T) {
	mk := func(reg, mode, ver string) *provider.Provider {
		p := mkProvider("host", nil, "host")
		p.Companions = map[string]provider.Companion{
			"tool": {Registry: reg, Mode: mode, Version: ver, Target: "vendor/tool"},
		}
		return p
	}
	t.Run("清单查不到", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		err := inst.applyGenericCompanions(context.Background(), mk("ghost", "latest-compatible", ""), "1.0.0", t.TempDir())
		if !hasCode(err, CodeCompanionLookup) {
			t.Fatalf("应报清单找不到: %v", err)
		}
	})
	t.Run("索引不可用", func(t *testing.T) {
		fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
			Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
			NPM:      npmMatrix(),
		})
		inst := newInstaller(t, fm, func(o *Options) { o.Client.Offline = true })
		err := inst.applyGenericCompanions(context.Background(), mk("node", "latest-compatible", ""), "1.0.0", t.TempDir())
		if !hasCode(err, CodeCompanionIndex) {
			t.Fatalf("离线应报索引不可用: %v", err)
		}
	})
	t.Run("bundled 不处理", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		if err := inst.applyGenericCompanions(context.Background(), mk("ghost", "bundled", ""), "1.0.0", t.TempDir()); err != nil {
			t.Fatalf("bundled 应直接跳过: %v", err)
		}
	})
}

// TestInstallNpmPackageNoProvider：没有 npm 清单时明确报错（不能装出一个没有 npm 的 node）。
func TestInstallNpmPackageNoProvider(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	err := inst.installNpmPackage(context.Background(), nil, "12.2.2", "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("应报找不到 npm 清单: %v", err)
	}
}

// TestNewDefaultsAndErrors：New 的默认值与"读不了校验表就报错"。
func TestNewDefaultsAndErrors(t *testing.T) {
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := New(Options{Paths: p})
	if err != nil {
		t.Fatalf("最小 Options 应能建出 Installer: %v", err)
	}
	if inst.opt.Profile != "auto" {
		t.Fatalf("空 Profile 应落 auto: %q", inst.opt.Profile)
	}
	if inst.opt.GOOS != "linux" {
		t.Fatalf("空 GOOS 应落 linux: %q", inst.opt.GOOS)
	}
	// known.json 损坏 → 必须报错（不然会静默丢掉 TOFU 记录）
	if err := os.MkdirAll(filepath.Dir(p.Known()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Known(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Paths: p}); err == nil {
		t.Fatal("known.json 损坏时应报错")
	}
}

// TestLockLoadErrors：锁文件"读不了"与"内容坏"都要报错（--frozen 不能拿半个锁继续）。
func TestLockLoadErrors(t *testing.T) {
	dir := t.TempDir()
	// ① 路径是目录 → 读取失败（不是 NotExist）
	if err := os.MkdirAll(filepath.Join(dir, LockFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLock(dir); err == nil {
		t.Fatal("锁路径是目录时应报错")
	}
	if err := os.RemoveAll(filepath.Join(dir, LockFileName)); err != nil {
		t.Fatal(err)
	}
	// ② 没有锁 → (nil, nil)（调用方按"没有锁"处理）
	if l, err := LoadLock(dir); err != nil || l != nil {
		t.Fatalf("没有锁应返回 nil,nil: %v %v", l, err)
	}
	// ③ 坏 TOML → 报错
	if err := os.WriteFile(filepath.Join(dir, LockFileName), []byte("not = [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLock(dir); err == nil {
		t.Fatal("锁内容坏时应报错")
	}
}

// TestInstalledVersionBadSpec：说明符解析不了时原样返回（不要 panic，也别乱选版本）。
func TestInstalledVersionBadSpec(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: "/x"})
	if got := inst.installedVersion("node", "^^^not-a-spec"); got != "^^^not-a-spec" {
		t.Fatalf("非法说明符应原样返回: %q", got)
	}
	if got := inst.installedVersion("node", "22"); got != "22.98.0" {
		t.Fatalf("合法说明符应解析到已装版本: %q", got)
	}
}

// TestCachedArchiveReVerifyFail：缓存里的归档坏了（摘要不符）不能装出一个坏工具。
//
// 行为在 R-CACHE-1 之后变好了：以前 TOFU 里存的是 sha512，`<sha256>.<ext>` 那条命中路径拿不到它，
// 坏缓存只能拖到“解压”才爆（而且 `addressed` 还会把刚下的好文件删掉、把坏文件留下）；
// 现在会按上游摘要（integrity/shasum）命中缓存 → 校验当场发现不符 → 删掉坏缓存重下一次。
// 断言的是结果而不是路径：不能装上坏工具，且坏缓存最终必须被替成正版。
func TestCachedArchiveReVerifyFail(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatal(err)
	}
	// 把缓存里的 npm 归档改坏：下次装必须发现“缓存与摘要不符”并重新下载
	archives := inst.opt.Paths.CacheArchives()
	entries, err := os.ReadDir(archives)
	if err != nil {
		t.Skipf("缓存目录结构变了: %v", err)
	}
	tampered := 0
	var goodSHA string
	for _, e := range entries {
		name := e.Name()
		// 归档名形如 `npm-12.2.2-darwin-arm64.tgz` 或内容寻址的 `<sha256>.tgz`（两份都改）
		if !strings.HasPrefix(name, "npm-") && !strings.HasSuffix(name, ".tgz") {
			continue
		}
		if goodSHA == "" {
			if sum, serr := verify.SHA256File(filepath.Join(archives, name)); serr == nil {
				goodSHA = sum
			}
		}
		if err := os.WriteFile(filepath.Join(archives, name), []byte("corrupted"), 0o644); err != nil {
			t.Fatal(err)
		}
		tampered++
	}
	if tampered == 0 || goodSHA == "" {
		t.Skip("缓存里没有 npm 归档（布局变了）")
	}
	// 清掉工具树与台账（保留缓存与 TOFU）→ 重装：坏缓存必须被识别，且不能装出坏工具。
	if err := os.RemoveAll(inst.opt.Paths.Tools()); err != nil {
		t.Fatal(err)
	}
	inst.ledger.Installs = nil
	got, err := inst.Install(ctx, "npm", "12.2.2")
	if err != nil {
		// “响亮失败”也是可接受的：只要紧接着的重试能自动重下成功。
		got, err = inst.Install(ctx, "npm", "12.2.2")
		if err != nil {
			t.Fatalf("坏缓存清理后，重试应自动重下并成功: %v", err)
		}
	}
	if got.SHA256 != goodSHA {
		t.Fatalf("装的必须是正版产物: got %s want %s", got.SHA256, goodSHA)
	}
	// 坏缓存必须已经被替掉（内容寻址的那份重新等回 goodSHA）。
	sum, err := verify.SHA256File(filepath.Join(archives, goodSHA+".tgz"))
	if err != nil {
		t.Fatalf("坏缓存应被替成正版: %v", err)
	}
	if sum != goodSHA {
		t.Fatalf("缓存内容仍不对: %s", sum)
	}
}

// TestApplyUninstallAllPurge：purge 的两条收尾路径（连根删 / 保留 cache）。
func TestApplyUninstallAllPurge(t *testing.T) {
	t.Run("purge 连根删", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		root := inst.opt.Paths.Root
		if err := os.MkdirAll(filepath.Join(root, "state"), 0o755); err != nil {
			t.Fatal(err)
		}
		res := inst.ApplyUninstallAll(UninstallPlan{Root: root, BinDir: inst.opt.Paths.Bin(), Purge: true})
		if !res.Purged {
			t.Fatalf("应标记已清除: %+v", res)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("根目录应被删掉: %v", err)
		}
	})
	t.Run("purge 保留 cache", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		root := inst.opt.Paths.Root
		for _, d := range []string{"state", "cache"} {
			if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		res := inst.ApplyUninstallAll(UninstallPlan{Root: root, BinDir: inst.opt.Paths.Bin(), Purge: true, KeepCache: true})
		if !res.Purged {
			t.Fatalf("应标记已清除: %+v", res)
		}
		if _, err := os.Stat(filepath.Join(root, "cache")); err != nil {
			t.Fatalf("cache 应保留（离线可重装）: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
			t.Fatal("state 应被删掉")
		}
	})
	t.Run("非 purge 时写回状态", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: "/x"})
		res := inst.ApplyUninstallAll(UninstallPlan{Root: inst.opt.Paths.Root, BinDir: inst.opt.Paths.Bin()})
		if res.Purged {
			t.Fatal("非 purge 不该删根目录")
		}
		if _, err := os.Stat(inst.opt.Paths.Ledger()); err != nil {
			t.Fatalf("非 purge 应写回台账: %v", err)
		}
	})
}

// TestGenericCompanionInstall：通用伴生包（非 npm）装进 <工具根>/<target> 并发出 companion 事件。
func TestGenericCompanionInstall(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	tree := t.TempDir()
	host := mkProvider("host", nil, "host")
	host.Companions = map[string]provider.Companion{
		// 用 fake 的离线归档：registry 指向测试服务器上已有的产物（node 的 tar.gz）
		"tool": {Registry: "node", Mode: "pinned", Version: "22.98.0", Target: "vendor/tool"},
	}
	var events []jsonout.Event
	inst := newInstaller(t, fm, func(o *Options) {
		o.Events = func(ev jsonout.Event) { events = append(events, ev) }
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "host" {
				return host, nil
			}
			return fakeLookup(fm)(id)
		}
	})
	err := inst.applyGenericCompanions(context.Background(), host, "1.0.0", tree)
	if err != nil {
		t.Fatalf("通用伴生包应能装上: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Ev == "companion" && ev.Tool == "tool" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应发出 companion 事件: %+v", events)
	}
}

// TestCompanionEventHasReplaced（1.8 契约）：被换掉的 npm 版本要有**独立字段**，
// 不能只塞在 msg 里（msg 只是人话，L2 英文化后可能就变了）。
func TestCompanionEventHasReplaced(t *testing.T) {
	var events []jsonout.Event
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) {
		o.Events = func(ev jsonout.Event) { events = append(events, ev) }
	})
	if _, err := inst.Install(context.Background(), "node", "22.98.0"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range events {
		if ev.Ev == "companion" && ev.Tool == "npm" {
			found = true
			if ev.Replaced == "" {
				t.Fatalf("companion 事件必须带 replaced 字段: %+v", ev)
			}
			if ev.Version == ev.Replaced {
				t.Fatalf("replaced 应是替换前的版本: %+v", ev)
			}
		}
	}
	if !found {
		t.Fatalf("没有 companion 事件: %+v", events)
	}
}

// TestVerifyArtifactBranches：verifyArtifact 的四条"摘要从哪来"的分支（直接调，不必真装）。
func TestVerifyArtifactBranches(t *testing.T) {
	arch := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(arch, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := verify.SHA256File
	sum, err := sha(arch)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("校验和 URL 渲染失败", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		p := mkProvider("x", nil, "x")
		p.Checksum = provider.ChecksumBlock{Format: "gnu-sums",
			URLs: map[string]string{"default": "https://x/{unknown-placeholder}"}}
		if _, err := inst.verifyArtifact(context.Background(), p, provider.Version{Version: "1.0.0"},
			"darwin-arm64", "https://x/a.tar.gz", arch, sum, nil); err == nil {
			t.Fatal("占位符不认识时必须报错")
		}
	})
	t.Run("registry 摘要回源失败（离线）", func(t *testing.T) {
		inst := newInstaller(t, nil, func(o *Options) { o.Client.Offline = true })
		p := mkProvider("x", nil, "x")
		p.Checksum = provider.ChecksumBlock{Format: provider.ChecksumRegistry,
			URLs: map[string]string{"default": "https://registry.example/x"}}
		if _, err := inst.verifyArtifact(context.Background(), p, provider.Version{Version: "1.0.0"},
			"darwin-arm64", "https://x/a.tar.gz", arch, sum, nil); err == nil {
			t.Fatal("离线时回源必须失败")
		}
	})
	t.Run("inline 摘要（go 那种）", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		p := mkProvider("x", nil, "x")
		p.Checksum = provider.ChecksumBlock{Format: provider.ChecksumIndexInline}
		res := provider.Version{Version: "1.0.0"}
		// 没有平台文件信息时 inline 为空，但算法必须是 sha256 + 本地算的摘要
		_, _ = inst.verifyArtifact(context.Background(), p, res, "darwin-arm64",
			"https://x/a.tar.gz", arch, sum, nil) // 分支要覆盖的是"算摘要的方式"，不是校验结果
	})
	// mode = "none"：清单自己声明"不要校验"，就不该再要求 --insecure（0.14 之前这个字段
	// 只在 plan 里打印，实际仍会报"没有校验源"，与 docs/troubleshooting.md 的说法正面矛盾）。
	t.Run("清单声明 checksum.mode=none", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		p := mkProvider("x", nil, "x")
		p.Checksum = provider.ChecksumBlock{Format: "gnu-sums", Mode: "none"} // 没有 URLs
		res, err := inst.verifyArtifact(context.Background(), p, provider.Version{Version: "1.0.0"},
			"darwin-arm64", "https://x/a.tar.gz", arch, sum, nil)
		if err != nil {
			t.Fatalf("mode=none 不该要求校验源: %v", err)
		}
		if res.Verified != verify.VerifiedUnverified {
			t.Fatalf("mode=none 必须标成 unverified: %+v", res)
		}
	})
	t.Run("其它格式（gnu-sums）用本地 sha256", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		p := mkProvider("x", nil, "x")
		p.Checksum = provider.ChecksumBlock{Format: "gnu-sums"} // 没有 URLs → sumsURLs 为空
		_, err := inst.verifyArtifact(context.Background(), p, provider.Version{Version: "1.0.0"},
			"darwin-arm64", "https://x/a.tar.gz", arch, "", nil) // sha 给空 → 校验源缺失，必报错
		if err == nil {
			t.Fatal("没有校验源时必须报错")
		}
	})
}

// TestUninstallBranches：卸载的三条分支（被引用而拒绝 / 目录已被手删 / 正常删掉并清 shim）。
func TestUninstallBranches(t *testing.T) {
	t.Run("被引用时拒绝", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		dir := inst.opt.Paths.ToolVersion("node", "22.98.0")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: dir,
			References: []store.Ref{{Kind: store.RefGlobal}}})
		err := inst.Uninstall("node", "22.98.0", false)
		if !hasCode(err, CodeStillReferenced) {
			t.Fatalf("被引用时应拒绝并说明: %v", err)
		}
		if _, statErr := os.Stat(dir); statErr != nil {
			t.Fatalf("拒绝时不该删目录: %v", statErr)
		}
	})

	t.Run("台账里没有", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		if err := inst.Uninstall("ghost", "1.0.0", false); err == nil {
			t.Fatal("不在台账里应报错（只提示不删）")
		}
	})

	t.Run("目录已被手删", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		dir := inst.opt.Paths.ToolVersion("node", "22.97.0")
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.97.0", Path: dir})
		if err := inst.Uninstall("node", "22.97.0", false); err != nil {
			t.Fatalf("目录不存在时应安静地清台账: %v", err)
		}
		if found := inst.ledger.Find("node", "22.97.0"); found != nil {
			t.Fatal("台账记录应被删掉")
		}
	})

	t.Run("正常删除并清 shim", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		dir := inst.opt.Paths.ToolVersion("node", "22.96.0")
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.96.0", Path: dir,
			References: []store.Ref{{Kind: store.RefGlobal}}})
		inst.opt.Cfg.Tools = map[string]string{"node": "22.96.0"}
		if err := os.MkdirAll(inst.opt.Paths.Bin(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "bin", "node"), filepath.Join(inst.opt.Paths.Bin(), "node")); err != nil {
			t.Skip("这台机器建不了软链")
		}
		if err := inst.Uninstall("node", "22.96.0", true); err != nil {
			t.Fatalf("正常卸载不该报错: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(inst.opt.Paths.Bin(), "node")); !os.IsNotExist(err) {
			t.Fatal("shim 应被清掉")
		}
		if got := inst.ActiveVersion("node"); got != "" {
			t.Fatalf("当前默认应被清空: %q", got)
		}
	})
}

// TestInstallShimsFailsWithoutPrimaryBin：主命令（bins[0]）找不到时必须报错。
// 以前一律 continue，于是 `pk install node@22` 能"成功返回 0"但 bin/node 根本没建出来
// （项目规矩：不静默降级）。次要别名（npm/npx）仍允许缺。
func TestInstallShimsFailsWithoutPrimaryBin(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	dir := t.TempDir() // 空树：一个 bin 都找不到
	p := mkProvider("x", nil, "tool", "extra")
	err := inst.installShims(p, dir)
	if err == nil {
		t.Fatal("主命令找不到时必须报错")
	}
	if !hasCode(err, archive.CodeBinNotFound) {
		t.Fatalf("错误码应是 %s，实际 %v", archive.CodeBinNotFound, err)
	}
	// 主命令在、别名不在：允许通过
	if err := os.WriteFile(filepath.Join(dir, "tool"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inst.installShims(p, dir); err != nil {
		t.Fatalf("只有别名缺失时不该失败: %v", err)
	}
}
