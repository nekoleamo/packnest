package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/jsonout"

	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// 失败分支与边角（完善路线 §E8c）：
// 磁盘写失败、台账缺项、provider 未知这些分支"平时一辈子不跑"，但正是它们决定
// 出错时是给一句人话还是 panic / 静默成功。用"把路径变成目录"这种零依赖手法注入。

// TestFinalizeSaveFailures：三份状态文件任一写失败都要把错误抛出来（不能静默丢状态）。
func TestFinalizeSaveFailures(t *testing.T) {
	t.Run("台账写不进", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		dir := inst.opt.Paths.Ledger()
		if err := os.MkdirAll(dir, 0o755); err != nil { // 把台账路径做成目录 → 写失败
			t.Fatal(err)
		}
		if err := inst.Finalize(); err == nil {
			t.Fatal("台账写不进时必须报错")
		}
	})
	t.Run("校验表写不进", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		if err := os.MkdirAll(inst.opt.Paths.Known(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := inst.Finalize(); err == nil || !strings.Contains(err.Error(), "") {
			if err == nil {
				t.Fatal("known.json 写不进时必须报错")
			}
		}
	})
	t.Run("配置写不进", func(t *testing.T) {
		inst := newInstaller(t, nil, nil)
		if err := os.MkdirAll(inst.opt.Paths.Config(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := inst.Finalize(); err == nil {
			t.Fatal("config.toml 写不进时必须报错")
		}
	})
}

// TestActivateErrors：没装的版本 / 未知工具 → 可执行的错误文案，而不是空指针。
func TestActivateErrors(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	err := inst.Activate("node", "22.98.0")
	if err == nil || !strings.Contains(err.Error(), "还没装") {
		t.Fatalf("未安装应提示先装: %v", err)
	}
	// 台账里塞一条记录，但 provider 查不到这个工具
	inst.ledger.Upsert(store.Install{Tool: "ghost", Version: "1.0.0", Path: t.TempDir()})
	if err := inst.Activate("ghost", "1.0.0"); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("未知 provider 应报错并带工具名: %v", err)
	}
}

// TestLockFromInstalls：台账记录 → 锁条目（字段一个不许丢，顺序即台账顺序）。
func TestLockFromInstalls(t *testing.T) {
	l := LockFromInstalls([]store.Install{
		{Tool: "node", Version: "22.98.0", SHA256: "aa", Source: "npmmirror"},
		{Tool: "npm", Version: "11.20.0", CompanionOf: "node"},
	})
	if len(l.Tool) != 2 {
		t.Fatalf("应有 2 条: %+v", l.Tool)
	}
	if l.Tool[0].Name != "node" || l.Tool[0].SHA256 != "aa" || l.Tool[0].Source != "npmmirror" {
		t.Fatalf("第一条第字段丢了: %+v", l.Tool[0])
	}
	if l.Tool[1].CompanionOf != "node" {
		t.Fatalf("伴生关系丢了: %+v", l.Tool[1])
	}
}

// TestInstallShimsFailure：bin 目录不可写时安装 shim 必须报错（否则装完调不动）。
func TestInstallShimsFailure(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	// 工具目录里放一个真的 node（FindBin 找得到），但 bin 路径被做成文件 → 写 shim 必失败
	toolDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolDir, "node"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(inst.opt.Paths.Bin()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inst.opt.Paths.Bin(), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := inst.installShims(mkProvider("node", nil, "node"), toolDir); err == nil {
		t.Fatal("bin 不可用时必须报错")
	}
	// 找不到二进制时是"跳过"而不是报错（Windows zip 里的 .cmd 兼容性靠这条）
	if err := inst.installShims(mkProvider("node", nil, "node"), t.TempDir()); err != nil {
		t.Fatalf("找不到二进制应跳过: %v", err)
	}
}

// TestRemoveDirWithRetry：删不存在的目录不报错；正常目录删得掉。
func TestRemoveDirWithRetry(t *testing.T) {
	if err := removeDirWithRetry(filepath.Join(t.TempDir(), "nope"), 2); err != nil {
		t.Fatalf("不存在的目录应视为已删: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeDirWithRetry(dir, 2); err != nil {
		t.Fatalf("正常目录应删掉: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("目录还在")
	}
}

// TestUpgradeUnknownTool：没装的工具不升级，但要给出"Skipped 理由"（不能假装升过）。
func TestUpgradeUnknownTool(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	items, err := inst.Upgrade(context.Background(), []string{"ghost"}, false)
	if err != nil {
		t.Fatalf("单个工具没装不该让整条命令失败: %v", err)
	}
	if len(items) != 1 || items[0].Tool != "ghost" || !strings.Contains(items[0].Skipped, "还没装") {
		t.Fatalf("应给出没装的理由: %+v", items)
	}
}

// TestUpgradeSkipsWhenCurrent：已是最新时不重复下载（幂等），并给出 skipped 而不是 error。
func TestUpgradeSkipsWhenCurrent(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(context.Background(), "node", "22.98.0"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	items, err := inst.Upgrade(context.Background(), []string{"node"}, false)
	if err != nil {
		t.Fatalf("已是最新不该报错: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("应至少给出 node 的一条结论（已是最新）")
	}
	for _, it := range items {
		if it.Changed || it.Err != "" {
			t.Fatalf("已是最新不该改也不该报错: %+v", it)
		}
		if it.Skipped == "" {
			t.Fatalf("应说明为什么没升: %+v", it)
		}
	}
}

// TestCommitReplaceFailure：改名失败（磁盘满 / 权限 / 目录被占）时必须报错，
// 并把临时目录清掉，不留半成品（方案 §原子落盘）。
func TestCommitReplaceFailure(t *testing.T) {
	inst := newInstaller(t, nil, func(o *Options) {
		o.ArchiveReplace = func(dst, src string) error { return errors.New("rename 失败") }
	})
	tmp := filepath.Join(inst.opt.Paths.Root, "tools", "node", "22.98.0.tmp.abcd")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	prep := &prepared{
		p:      mkProvider("node", nil, "node"),
		plan:   false,
		res:    Result{Tool: "node", Version: "22.98.0", Path: inst.opt.Paths.ToolVersion("node", "22.98.0")},
		tmpDir: tmp,
	}
	if _, err := inst.commit(context.Background(), prep); err == nil {
		t.Fatal("改名失败必须报错")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("失败后临时目录应删掉: %v", err)
	}
	if len(inst.ledger.Installs) != 0 {
		t.Fatalf("失败不该写台账: %+v", inst.ledger.Installs)
	}
}

// TestCommitPlanAndSkip：--dry-run 的 plan 与"已存在"的 skip 都不该落盘（只补事件）。
func TestCommitPlanAndSkip(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	plan := &prepared{
		p:      mkProvider("node", nil, "node"),
		plan:   true,
		res:    Result{Tool: "node", Version: "22.98.0"},
		events: []jsonout.Event{{Ev: "plan", Tool: "node"}},
	}
	if _, err := inst.commit(context.Background(), plan); err != nil {
		t.Fatalf("plan 不该失败: %v", err)
	}
	skip := &prepared{p: mkProvider("node", nil, "node"), skip: true, res: Result{Tool: "node", Version: "22.98.0"}}
	if _, err := inst.commit(context.Background(), skip); err != nil {
		t.Fatalf("skip 不该失败: %v", err)
	}
	if len(inst.ledger.Installs) != 0 {
		t.Fatalf("plan/skip 都不该写台账: %+v", inst.ledger.Installs)
	}
}

// TestInstallAtomicCommitFailure（E5 的边界）：commit 阶段失败时，
// 已落盘的不回滚，剩下的转 abort —— 文档里承诺的就是这个，别让测试承诺更多。
func TestInstallAtomicCommitFailure(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	var events []jsonout.Event
	calls := 0
	inst := newInstaller(t, fm, func(o *Options) {
		o.Atomic = true
		o.Events = func(ev jsonout.Event) { events = append(events, ev) }
		o.ArchiveReplace = func(dst, src string) error {
			calls++
			if calls == 2 { // 第二个目标在 commit 阶段失败
				return errors.New("rename 失败")
			}
			return archive.Replace(dst, src)
		}
	})
	out := inst.InstallMany(context.Background(), []Target{{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if out[0].Err != nil {
		t.Fatalf("第一个目标应成功落盘: %v", out[0].Err)
	}
	if out[1].Err == nil {
		t.Fatal("第二个目标应失败")
	}
	aborts := 0
	for _, ev := range events {
		if ev.Ev == "abort" {
			aborts++
		}
	}
	// 第二个目标自己是失败（不是被放弃），所以 abort 可能为 0；关键是"不回滚第一个"
	if n := len(inst.Ledger().Installs); n == 0 {
		t.Fatal("已 commit 的目标不该被回滚（文档明确说 commit 失败不回滚）")
	}
	_ = aborts
}
