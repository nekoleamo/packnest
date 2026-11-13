package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/testkit"
)

// mkProvider 造一个只带分层相关字段的清单（planLevels 只用 Requires / Layout.Bins）。
func mkProvider(id string, requires []string, bins ...string) *provider.Provider {
	return &provider.Provider{ID: id, Requires: requires, Layout: provider.LayoutBlock{Bins: bins}}
}

func levelsOf(t *testing.T, inst *Installer, tools ...string) [][]string {
	t.Helper()
	targets := make([]Target, len(tools))
	for k, tool := range tools {
		targets[k] = Target{Tool: tool}
	}
	out := make([][]string, 0, 4)
	for _, lvl := range inst.planLevels(targets) {
		names := make([]string, 0, len(lvl))
		for _, k := range lvl {
			names = append(names, targets[k].Tool)
		}
		out = append(out, names)
	}
	return out
}

// TestPlanLevels：分层规则（requires / 同工具 / shim 同名）都在这一层收口。
func TestPlanLevels(t *testing.T) {
	provs := map[string]*provider.Provider{
		"node": mkProvider("node", nil, "node", "npm", "npx"),
		"npm":  mkProvider("npm", nil, "npm"),
		"go":   mkProvider("go", nil, "go", "gofmt"),
		"pnpm": mkProvider("pnpm", []string{"node"}, "pnpm", "pnpx"),
		"bun":  mkProvider("bun", []string{"node"}, "bun", "bunx"),
	}
	inst := newInstaller(t, nil, nil)
	inst.opt.LookupProvider = func(id string) (*provider.Provider, error) {
		if p, ok := provs[id]; ok {
			return p, nil
		}
		return nil, errors.New("未知工具 " + id)
	}

	t.Run("无关工具同层", func(t *testing.T) {
		if got := levelsOf(t, inst, "node", "go"); len(got) != 1 || len(got[0]) != 2 {
			t.Fatalf("node/go 应同层并行: %v", got)
		}
	})
	t.Run("依赖下一层", func(t *testing.T) {
		got := levelsOf(t, inst, "pnpm", "node")
		if len(got) != 2 || got[0][0] != "node" || got[1][0] != "pnpm" {
			t.Fatalf("pnpm 必须在 node 之后: %v", got)
		}
	})
	t.Run("依赖写在前面也一样", func(t *testing.T) {
		got := levelsOf(t, inst, "node", "pnpm")
		if len(got) != 2 || got[1][0] != "pnpm" {
			t.Fatalf("顺序不该影响分层: %v", got)
		}
	})
	t.Run("同工具串行", func(t *testing.T) {
		got := levelsOf(t, inst, "go", "go", "go")
		if len(got) != 3 {
			t.Fatalf("同工具三个 spec 必须三层: %v", got)
		}
	})
	t.Run("shim 同名冲突错层", func(t *testing.T) {
		got := levelsOf(t, inst, "node", "npm")
		if len(got) != 2 {
			t.Fatalf("node 与 npm 的 shim 同名，必须错层: %v", got)
		}
	})
}

// TestPrepareCommitSplit：prepare 不碰台账/TOFU 主表/shim，commit 才落盘。
func TestPrepareCommitSplit(t *testing.T) {
	fm := testkitNode(t)
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()

	prep, err := inst.prepare(ctx, "node", "22.98.0", false)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prep.skip && prep.tmpDir == "" {
		t.Fatal("prepare 应产出 tmp 目录")
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("prepare 不该写台账，实际 %d 条", n)
	}
	if len(inst.known.Entries) != 0 {
		t.Fatalf("prepare 不该写 TOFU 主表，实际 %d 条", len(inst.known.Entries))
	}
	if _, err := os.Stat(prep.tmpDir); err != nil {
		t.Fatalf("tmp 应存在: %v", err)
	}

	res, err := inst.commit(ctx, prep)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.AlreadyInstalled {
		t.Fatal("首次安装不该标记 AlreadyInstalled")
	}
	if len(inst.Ledger().Installs) != 1 {
		t.Fatalf("commit 后台账应有一条: %+v", inst.Ledger().Installs)
	}
	if len(inst.known.Entries) == 0 {
		t.Fatal("commit 应把 TOFU 结果并回主表")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("目标目录应已落盘: %v", err)
	}
}

// TestPrepareFailureLeavesNothing：prepare 失败时 tmp 目录不残留、台账为空。
func TestPrepareFailureLeavesNothing(t *testing.T) {
	fm := testkitNode(t, testkit.Status(500))
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()

	if _, err := inst.prepare(ctx, "node", "22.98.0", false); err == nil {
		t.Fatal("镜像 500 时 prepare 应失败")
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("失败后台账应为空: %d", n)
	}
	if leftovers := tmpLeftovers(t, inst); len(leftovers) != 0 {
		t.Fatalf("失败后不该留 .tmp 目录: %v", leftovers)
	}
}

// TestInstallManyKeepsOrder：结果与输入同序（GUI/脚本按参数位置读取）。
func TestInstallManyKeepsOrder(t *testing.T) {
	fm := testkitNode(t)
	inst := newInstaller(t, fm, nil)
	out := inst.InstallMany(context.Background(), []Target{{Tool: "node", Spec: ""}})
	if len(out) != 1 || out[0].Tool != "node" || out[0].Err != nil {
		t.Fatalf("单目标该走串行且成功: %+v", out)
	}
}

// testkitNode 造一个只有一个 node 版本（带 npm）的假镜像；opts 是故障注入。
func testkitNode(t *testing.T, opts ...testkit.Option) *testkit.FakeNode {
	t.Helper()
	return testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:       npmMatrix(),
		Behaviors: opts,
	})
}

// tmpLeftovers 列出工具树里残留的 .tmp.*（失败或取消后必须为空）。
func tmpLeftovers(t *testing.T, inst *Installer) []string {
	t.Helper()
	var out []string
	root := filepath.Join(inst.opt.Paths.Root, "tools")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if strings.Contains(info.Name(), ".tmp.") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// TestInstallManyParallel：多目标走并行层（node/npm 的 shim 同名 → 错层串行），
// 未知工具只影响自己（失败隔离），结果与输入同序。
func TestInstallManyParallel(t *testing.T) {
	fm := testkitNode(t)
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	out := inst.InstallMany(ctx, []Target{{Tool: "npm"}, {Tool: "ghost"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Tool != "npm" || out[1].Tool != "ghost" {
		t.Fatalf("结果应与输入同序: %+v", out)
	}
	if out[1].Err == nil {
		t.Fatal("未知工具应失败")
	}
	if out[0].Err != nil {
		t.Fatalf("npm 应成功: %+v", out)
	}
	if n := len(inst.Ledger().Installs); n != 1 {
		t.Fatalf("台账应有 1 条，实际 %d", n)
	}
	if left := tmpLeftovers(t, inst); len(left) != 0 {
		t.Fatalf("不该残留 .tmp 目录: %v", left)
	}
}

// TestInstallManySameToolSerialized：同工具多 spec 必须串行（分层成不同层）。
func TestInstallManySameToolSerialized(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	if got := inst.planLevels([]Target{{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"}}); len(got) != 2 {
		t.Fatalf("同工具两个 spec 必须两层: %v", got)
	}
	out := inst.InstallMany(context.Background(), []Target{{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("node@%s 失败: %v", oc.Spec, oc.Err)
		}
	}
	if got := inst.ActiveVersion("node"); got != "22.98.0" {
		t.Fatalf("默认应留在第一个（22.98.0），得到 %q", got)
	}
}

// TestInstallManyCancel：取消后每个目标都带 context.Canceled，且不留半成品。
func TestInstallManyCancel(t *testing.T) {
	inst := newInstaller(t, testkitNode(t), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := inst.InstallMany(ctx, []Target{{Tool: "node"}, {Tool: "npm"}})
	for _, oc := range out {
		if oc.Err == nil {
			t.Fatalf("取消后 %s 不该成功", oc.Tool)
		}
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("取消后台账应为空: %d", n)
	}
	if left := tmpLeftovers(t, inst); len(left) != 0 {
		t.Fatalf("取消后不该残留 .tmp 目录: %v", left)
	}
}
