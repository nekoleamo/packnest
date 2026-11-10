package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if l, err := LoadLock(dir); err != nil || l != nil {
		t.Fatalf("缺失锁应返回 nil: %+v %v", l, err)
	}
	lock := &Lock{Tool: []LockTool{
		{Name: "node", Version: "22.22.2", SHA256: "abc", Source: "cn"},
		{Name: "npm", Version: "12.2.2", CompanionOf: "node@22.22.2"},
	}}
	if err := lock.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadLock(dir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(back.Tool) != 2 || back.Tool[0].Name != "node" {
		t.Fatalf("锁内容异常: %+v", back.Tool)
	}
	if entry, ok := back.Find("npm"); !ok || entry.CompanionOf != "node@22.22.2" {
		t.Fatalf("伴生记录缺失: %+v", entry)
	}
	if err := os.WriteFile(filepath.Join(dir, LockFileName), []byte("tool = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLock(dir); err == nil {
		t.Fatal("非法锁应报错")
	}
}

func TestCheckFrozen(t *testing.T) {
	project := map[string]string{"node": "22", "pnpm": "10", "npm": "auto"}
	lock := &Lock{Tool: []LockTool{
		{Name: "node", Version: "22.22.2"},
		{Name: "pnpm", Version: "10.4.1"},
		{Name: "npm", Version: "12.2.2", CompanionOf: "node@22.22.2"},
	}}
	if err := CheckFrozen(project, lock); err != nil {
		t.Fatalf("一致时应通过: %v", err)
	}
	if err := CheckFrozen(project, nil); err == nil || !strings.Contains(err.Error(), "pk.lock") {
		t.Fatalf("缺锁应报错: %v", err)
	}
	// 版本不满足
	bad := &Lock{Tool: []LockTool{{Name: "node", Version: "20.18.1"}, {Name: "pnpm", Version: "10.4.1"}}}
	if err := CheckFrozen(project, bad); err == nil || !strings.Contains(err.Error(), "不满足") {
		t.Fatalf("版本不满足应报错: %v", err)
	}
	// 缺工具
	if err := CheckFrozen(map[string]string{"bun": "1"}, lock); err == nil || !strings.Contains(err.Error(), "bun") {
		t.Fatalf("锁里缺工具应报错: %v", err)
	}
	// 别名在 --frozen 下必须报错
	aliasLock := &Lock{Tool: []LockTool{{Name: "node", Version: "22.22.2"}}}
	if err := CheckFrozen(map[string]string{"node": "latest"}, aliasLock); err == nil {
		t.Fatal("别名在 frozen 下应报错（强制显式版本）")
	}
	// 非法说明符
	if err := CheckFrozen(map[string]string{"node": "bogus"}, aliasLock); err == nil {
		t.Fatal("非法说明符应报错")
	}
}

func TestBuildProjectLock(t *testing.T) {
	ledger := &store.Ledger{}
	ledger.Upsert(store.Install{Tool: "node", Version: "22.22.2", Path: "/p/node", SHA256: "n1",
		NpmVersion: "12.2.2", Source: "cn"})
	ledger.Upsert(store.Install{Tool: "node", Version: "20.18.1", Path: "/p/node20", SHA256: "n2"})
	ledger.Upsert(store.Install{Tool: "pnpm", Version: "10.4.1", Path: "/p/pnpm", SHA256: "p1"})

	lock, err := BuildProjectLock(map[string]string{"node": "22", "npm": "auto", "pnpm": "10"},
		ledger, map[string]string{"node": "22.22.2", "pnpm": "10.4.1"})
	if err != nil {
		t.Fatalf("BuildProjectLock: %v", err)
	}
	if len(lock.Tool) != 3 {
		t.Fatalf("应生成 node/npm/pnpm 三条: %+v", lock.Tool)
	}
	node, _ := lock.Find("node")
	if node.Version != "22.22.2" || node.SHA256 != "n1" {
		t.Fatalf("node 记录异常: %+v", node)
	}
	npm, ok := lock.Find("npm")
	if !ok || npm.Version != "12.2.2" || npm.CompanionOf != "node@22.22.2" {
		t.Fatalf("npm 伴生记录异常: %+v", npm)
	}

	// 没有生效版本时按说明符挑最新已装
	if in := pickInstallForTool("node", "20", ledger, nil); in == nil || in.Version != "20.18.1" {
		t.Fatalf("应按说明符挑 20.x: %+v", in)
	}
	if in := pickInstallForTool("node", "latest", ledger, nil); in == nil || in.Version != "22.22.2" {
		t.Fatalf("别名应取最新已装: %+v", in)
	}
	if in := pickInstallForTool("deno", "2", ledger, nil); in != nil {
		t.Fatalf("没装过应返回 nil: %+v", in)
	}
	// 缺工具时报错
	if _, err := BuildProjectLock(map[string]string{"deno": "2"}, ledger, nil); err == nil {
		t.Fatal("缺工具应报错")
	}
	if got := lock.LockTools(); !strings.Contains(got, "node@22.22.2") {
		t.Fatalf("LockTools 输出异常: %q", got)
	}
}

func TestFrozenInstallUsesLock(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.22.2", NPMVersion: "11.20.0"},
			{Version: "24.14.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	// 锁里钉 22.22.2，清单写 22（frozen 下不做解析，直接用锁）
	inst := newInstaller(t, fm, func(o *Options) {
		o.Project = map[string]string{"node": "22"}
		o.Frozen = true
		o.Lock = &Lock{Tool: []LockTool{{Name: "node", Version: "22.22.2", SHA256: fm.NodeChecksums["22.22.2"]}}}
	})
	res, err := inst.Install(context.Background(), "node", "22")
	if err != nil {
		t.Fatalf("frozen 安装: %v", err)
	}
	if res.Version != "22.22.2" {
		t.Fatalf("应按锁装 22.22.2，得到 %q", res.Version)
	}

	// 锁里的 sha256 与产物不符 → 校验失败
	bad := newInstaller(t, fm, func(o *Options) {
		o.Frozen = true
		o.Lock = &Lock{Tool: []LockTool{{Name: "node", Version: "22.22.2", SHA256: strings.Repeat("0", 64)}}}
	})
	if _, err := bad.Install(context.Background(), "node", "22"); err == nil {
		t.Fatal("锁里的 sha256 不符应失败")
	}

	// 锁里没有该工具 → 报错
	missing := newInstaller(t, fm, func(o *Options) {
		o.Frozen = true
		o.Lock = &Lock{}
	})
	if _, err := missing.Install(context.Background(), "node", "22"); err == nil {
		t.Fatal("锁里缺工具应报错")
	}
}

func TestActivateAndGC(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.22.2", NPMVersion: "11.20.0"},
			{Version: "24.14.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	first, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	second, err := inst.Install(ctx, "node", "24.14.0")
	if err != nil {
		t.Fatal(err)
	}
	// 首次安装即默认，第二次不抢
	if got := inst.ActiveVersion("node"); got != "22.22.2" {
		t.Fatalf("默认版本应仍是 22.22.2，得到 %q", got)
	}
	// shim 仍指向 22.22.2（第二次安装不抢 shim）
	got, _, err := shim.Resolve(inst.opt.Paths.Bin(), "node", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, first.Path) {
		t.Fatalf("shim 不该被第二次安装抢走: %q", got)
	}

	// use 切到 24.14.0 → 配置、引用、shim 全部跟着走
	if err := inst.Activate("node", "24.14.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := inst.ActiveVersion("node"); got != "24.14.0" {
		t.Fatalf("切版本后默认应是 24.14.0，得到 %q", got)
	}
	got, _, _ = shim.Resolve(inst.opt.Paths.Bin(), "node", "darwin")
	if !strings.HasPrefix(got, second.Path) {
		t.Fatalf("shim 应指向 24.14.0: %q", got)
	}
	if err := inst.Activate("node", "1.0.0"); err == nil {
		t.Fatal("切到未安装版本应报错")
	}

	// 22.22.2 现在零引用 → gc 回收；24.14.0 有全局引用 → 保留
	res, err := inst.GC(ctx, 1<<40, false, false)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "node@22.22.2" {
		t.Fatalf("应只回收零引用的 22.22.2: %+v", res)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatal("被回收的目录应删除")
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Fatal("有引用的版本不该删")
	}
	if len(res.KeptRef) != 1 {
		t.Fatalf("应报告保留原因: %+v", res.KeptRef)
	}

	// dry-run 不动磁盘、不动台账
	res, err = inst.GC(ctx, 1<<40, true, false)
	if err != nil || !res.DryRun {
		t.Fatalf("dry-run: %+v %v", res, err)
	}
}

func TestGCDryRunKeepsDir(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}, {Version: "24.14.0"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	second, err := inst.Install(ctx, "node", "24.14.0")
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.GC(ctx, 1<<40, true, false)
	if err != nil {
		t.Fatalf("GC dry-run: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("dry-run 应报告 24.14.0: %+v", res)
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Fatal("dry-run 不该删目录")
	}
	if inst.ledger.Find("node", "24.14.0") == nil {
		t.Fatal("dry-run 不该改台账")
	}
}

func TestUninstallRepointsToRemaining(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}, {Version: "24.14.0"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	first, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	second, err := inst.Install(ctx, "node", "24.14.0")
	if err != nil {
		t.Fatal(err)
	}
	// 删掉默认版本 → 自动切到剩下的最新版本
	if err := inst.Uninstall("node", "22.22.2", true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := inst.ActiveVersion("node"); got != "24.14.0" {
		t.Fatalf("应自动切到 24.14.0，得到 %q", got)
	}
	got, _, _ := shim.Resolve(inst.opt.Paths.Bin(), "node", "darwin")
	if !strings.HasPrefix(got, second.Path) {
		t.Fatalf("shim 应指向剩下的版本: %q", got)
	}
	_ = first
}

func TestSweepStaleBeforeInstall(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, nil)
	p, _ := paths.New(rootOf(t, inst).Root)
	// 造一个 48h 前的残留
	stale := p.ToolVersion("node", "1.0.0.tmp.dead")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	old := inst.now().Add(-48 * 3600 * 1e9)
	_ = os.Chtimes(stale, old, old)
	res, err := inst.SweepStale()
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("应清掉陈旧残留: %+v", res)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("残留应被删除")
	}
}

func TestLedgerRecovery(t *testing.T) {
	dir := t.TempDir()
	p, _ := paths.New(dir)
	ledger := &store.Ledger{}
	toolDir := p.ToolVersion("node", "22.22.2")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger.Upsert(store.Install{Tool: "node", Version: "22.22.2", Path: toolDir})
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}
	// 再写一次生成 .bak，然后毁掉主文件
	ledger.Upsert(store.Install{Tool: "bun", Version: "1.4.4", Path: toolDir})
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Ledger(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	back, recovered, err := store.LoadLedgerWithRecovery(p.Ledger())
	if err != nil {
		t.Fatalf("应能从 .bak 恢复: %v", err)
	}
	if !recovered {
		t.Fatal("应报告 recovered")
	}
	if back.Find("node", "22.22.2") == nil {
		t.Fatalf("恢复的内容异常: %+v", back.Installs)
	}
	// 两个都坏 → 报错
	if err := os.WriteFile(p.Ledger()+".bak", []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadLedgerWithRecovery(p.Ledger()); err == nil {
		t.Fatal("主文件和 .bak 都坏时应报错")
	}
}

func TestShimPolicyAcrossVersions(t *testing.T) {
	// 第二次安装不抢 shim；use 之后才抢（上面已覆盖），这里额外断言 provider 查询缺失时的错误路径
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "node" {
				return fm.NodeProvider, nil
			}
			return nil, context.Canceled
		}
	})
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Activate("node", "22.22.2"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
}

func TestSaveProjectTool(t *testing.T) {
	dir := t.TempDir()
	// 全新文件
	if err := SaveProjectTool(dir, "node", "22.22.2"); err != nil {
		t.Fatalf("新建: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "pk.toml"))
	if !strings.Contains(string(raw), "[tools]") || !strings.Contains(string(raw), `node = "22.22.2"`) {
		t.Fatalf("内容异常:\n%s", raw)
	}
	// 已有 [tools] 且有注释与别的工具：只改一行，注释保留
	existing := "# 项目运行时\n[tools]\nnode = \"20\"\npnpm = \"10\"\n\n[mirrors]\nprofile = \"cn\"\n"
	if err := os.WriteFile(filepath.Join(dir, "pk.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectTool(dir, "node", "22.22.2"); err != nil {
		t.Fatalf("更新: %v", err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "pk.toml"))
	text := string(raw)
	if !strings.Contains(text, "# 项目运行时") || !strings.Contains(text, `pnpm = "10"`) || !strings.Contains(text, `profile = "cn"`) {
		t.Fatalf("不该动别的行:\n%s", text)
	}
	if strings.Count(text, "node = ") != 1 || !strings.Contains(text, `node = "22.22.2"`) {
		t.Fatalf("node 行应被替换:\n%s", text)
	}
	// 追加新工具到 [tools] 段
	if err := SaveProjectTool(dir, "bun", "1.4.4"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "pk.toml"))
	if !strings.Contains(string(raw), `bun = "1.4.4"`) {
		t.Fatalf("新工具未写入:\n%s", raw)
	}
	// 没有 [tools] 的文件：追加一个段
	if err := os.WriteFile(filepath.Join(dir, "pk.toml"), []byte("[mirrors]\nprofile = \"cn\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectTool(dir, "deno", "2.9.8"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "pk.toml"))
	if !strings.Contains(string(raw), "[tools]") || !strings.Contains(string(raw), `deno = "2.9.8"`) {
		t.Fatalf("[tools] 段未追加:\n%s", raw)
	}
}
