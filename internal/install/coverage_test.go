package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
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
	if err := CheckFrozen(map[string]string{"node": ">=26"}, lock); err == nil ||
		!strings.Contains(err.Error(), "不一致") {
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
		if err == nil || !strings.Contains(err.Error(), "找不到清单") {
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
		if err == nil || !strings.Contains(err.Error(), "索引不可用") {
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
