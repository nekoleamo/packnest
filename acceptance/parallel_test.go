// 并行安装（#7）验收：跨工具并行、同工具/依赖/shim 冲突串行。
package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

func parSetup(t *testing.T, nodeVersions []testkit.NodeSpec, mutate ...func(*install.Options)) (*install.Installer, paths.Paths) {
	t.Helper()
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: nodeVersions,
		NPM:      npmMatrixAcceptance(),
	})
	fg := testkit.NewFakeGo(t, []string{"1.27.3"})
	fp := testkit.NewFakePnpm(t, map[string]string{"9.15.0": "https://registry.npmjs.org/pnpm/-/pnpm-9.15.0.tgz"})
	lookup := func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		case "go":
			return fg.Provider, nil
		case "pnpm":
			return fp.Provider, nil
		}
		return nil, errors.New("未知工具 " + id)
	}
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opt := install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", LookupProvider: lookup,
	}
	for _, m := range mutate {
		m(&opt)
	}
	inst, err := install.New(opt)
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}
	return inst, p
}

func parTmpLeftovers(t *testing.T, p paths.Paths) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(filepath.Join(p.Root, "tools"), func(path string, info os.FileInfo, err error) error {
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

// TestAcceptance_Par_InstallThreeTools：node+go+pnpm 同批，三条台账、四个 shim。
func TestAcceptance_Par_InstallThreeTools(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	out := inst.InstallMany(context.Background(), []install.Target{
		{Tool: "node"}, {Tool: "go"}, {Tool: "pnpm"},
	})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("%s 安装失败: %v", oc.Tool, oc.Err)
		}
	}
	if n := len(inst.Ledger().Installs); n != 3 {
		t.Fatalf("台账应有 3 条，实际 %d: %+v", n, inst.Ledger().Installs)
	}
	for _, name := range []string{"node", "npm", "pnpm", "go"} {
		if _, err := os.Stat(filepath.Join(p.Bin(), name)); err != nil {
			t.Fatalf("缺少 shim %s: %v", name, err)
		}
	}
	if left := parTmpLeftovers(t, p); len(left) != 0 {
		t.Fatalf("不该残留 .tmp 目录: %v", left)
	}
}

// TestAcceptance_Par_RequiresOrder：pnpm 写在 node 前面也得先装 node（分层保证）。
func TestAcceptance_Par_RequiresOrder(t *testing.T) {
	inst, _ := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	out := inst.InstallMany(context.Background(), []install.Target{{Tool: "pnpm"}, {Tool: "node"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("%s 安装失败（依赖分层没生效）: %v", oc.Tool, oc.Err)
		}
	}
	if inst.ActiveVersion("node") == "" {
		t.Fatal("node 应成为全局默认")
	}
}

// TestAcceptance_Par_SameToolSerialized：同工具两个版本按命令行顺序落盘，默认留在第一个。
func TestAcceptance_Par_SameToolSerialized(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{
		{Version: "22.98.0", NPMVersion: "11.20.0"},
		{Version: "22.99.0", NPMVersion: "11.20.0"},
	})
	out := inst.InstallMany(context.Background(), []install.Target{
		{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"},
	})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("node@%s 失败: %v", oc.Spec, oc.Err)
		}
	}
	if got := inst.ActiveVersion("node"); got != "22.98.0" {
		t.Fatalf("默认应留在命令行第一个（22.98.0），得到 %q", got)
	}
	for _, v := range []string{"22.98.0", "22.99.0"} {
		if _, err := os.Stat(p.ToolVersion("node", v)); err != nil {
			t.Fatalf("版本 %s 应已落盘: %v", v, err)
		}
	}
}

// TestAcceptance_Par_NPMConflict：node 与独立 npm 的 shim 同名，必须错层，最终 npm 指向独立包。
func TestAcceptance_Par_NPMConflict(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	out := inst.InstallMany(context.Background(), []install.Target{{Tool: "node"}, {Tool: "npm"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("%s 安装失败: %v", oc.Tool, oc.Err)
		}
	}
	target, err := os.Readlink(filepath.Join(p.Bin(), "npm"))
	if err != nil {
		t.Fatalf("npm shim 应是软链: %v", err)
	}
	if !strings.Contains(target, filepath.Join("tools", "npm")) {
		t.Fatalf("npm shim 应指向独立 npm（后 commit 覆盖），实际 %s", target)
	}
}

// TestAcceptance_Par_FailureIsolation：一路失败不影响其余，失败后无残留。
func TestAcceptance_Par_FailureIsolation(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	out := inst.InstallMany(context.Background(), []install.Target{
		{Tool: "node"}, {Tool: "ghost"}, {Tool: "go"},
	})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if out[1].Err == nil {
		t.Fatal("未知工具应报错")
	}
	if out[0].Err != nil || out[2].Err != nil {
		t.Fatalf("其余工具应成功: %+v", out)
	}
	if n := len(inst.Ledger().Installs); n != 2 {
		t.Fatalf("台账应有 2 条，实际 %d", n)
	}
	if left := parTmpLeftovers(t, p); len(left) != 0 {
		t.Fatalf("不该残留 .tmp 目录: %v", left)
	}
}

// TestAcceptance_Par_JSONEventOrder：每个工具的事件组内 resolve 先于 done，done 恰好 N 条。
func TestAcceptance_Par_JSONEventOrder(t *testing.T) {
	var events []jsonout.Event
	inst, _ := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}}, func(o *install.Options) {
		o.Events = func(ev jsonout.Event) { events = append(events, ev) }
	})
	out := inst.InstallMany(context.Background(), []install.Target{
		{Tool: "node"}, {Tool: "go"}, {Tool: "pnpm"},
	})
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("%s 失败: %v", oc.Tool, oc.Err)
		}
	}
	seen := map[string][]string{}
	for _, ev := range events {
		seen[ev.Tool] = append(seen[ev.Tool], ev.Ev)
	}
	if len(seen["node"]) == 0 {
		t.Fatalf("没有 node 事件: %+v", seen)
	}
	for tool, evs := range seen {
		if evs[0] != "resolve" {
			t.Fatalf("%s 的第一条事件应是 resolve，实际 %v", tool, evs)
		}
		if evs[len(evs)-1] != "done" {
			t.Fatalf("%s 的最后一条事件应是 done，实际 %v", tool, evs)
		}
	}
	dones := 0
	for _, ev := range events {
		if ev.Ev == "done" {
			dones++
		}
	}
	if dones != 3 {
		t.Fatalf("应有 3 条 done，实际 %d", dones)
	}
}

// TestAcceptance_Par_CtrlCNoPartial：中途取消 → 全部报错、无半成品目录。
func TestAcceptance_Par_CtrlCNoPartial(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := inst.InstallMany(ctx, []install.Target{{Tool: "node"}, {Tool: "go"}, {Tool: "pnpm"}})
	for _, oc := range out {
		if oc.Err == nil {
			t.Fatalf("取消后 %s 不该成功", oc.Tool)
		}
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("取消后台账应为空: %d", n)
	}
	if left := parTmpLeftovers(t, p); len(left) != 0 {
		t.Fatalf("取消后不该残留 .tmp 目录: %v", left)
	}
	for _, tool := range []string{"node", "go", "pnpm"} {
		entries, _ := os.ReadDir(filepath.Join(p.Root, "tools", tool))
		if len(entries) != 0 {
			t.Fatalf("%s 不该留下目录: %v", tool, entries)
		}
	}
}

// ── --atomic（E5）：全成功才落盘 ─────────────────────────────────────────
//
// 三个验收用例锁三件事：整批放弃 / 默认行为不变 / abort 事件契约。

// atomicSetup 造两工具的一批：node 正常、ghost 必然失败（未知工具）。
func atomicSetup(t *testing.T, atomic bool) (install.Installer, paths.Paths, *[]jsonout.Event, func()) {
	t.Helper()
	var events []jsonout.Event
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		func(o *install.Options) {
			o.Atomic = atomic
			o.Events = func(ev jsonout.Event) { events = append(events, ev) }
		})
	return *inst, p, &events, func() {}
}

// TestAcceptance_Par_AtomicAllOrNothing：一个目标失败 → 另一个也不落盘（台账空、无 shim、无 .tmp）。
func TestAcceptance_Par_AtomicAllOrNothing(t *testing.T) {
	inst, p := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		func(o *install.Options) { o.Atomic = true })
	out := inst.InstallMany(context.Background(), []install.Target{{Tool: "node"}, {Tool: "ghost"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("结果数不符: %+v", out)
	}
	if out[0].Err == nil {
		t.Fatal("ghost 失败时 node 必须一并放弃（--atomic）")
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("--atomic 下不该写台账，实际 %d 条: %+v", n, inst.Ledger().Installs)
	}
	if got := inst.ActiveVersion("node"); got != "" {
		t.Fatalf("--atomic 下不该切默认版本，得到 %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.ToolVersion("node", "22.98.0"))); err == nil {
		t.Fatal("工具目录不该存在（全成功才落盘）")
	}
	if _, err := os.Stat(filepath.Join(p.Bin(), "node")); err == nil {
		t.Fatal("不该装出 shim")
	}
	if left := parTmpLeftovers(t, p); len(left) != 0 {
		t.Fatalf("不该残留 .tmp 目录: %v", left)
	}
}

// TestAcceptance_Par_AtomicDefaultUnchanged：不带 --atomic 时"尽力而为"（成功的照旧落盘）。
func TestAcceptance_Par_AtomicDefaultUnchanged(t *testing.T) {
	inst, _ := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}})
	out := inst.InstallMany(context.Background(), []install.Target{{Tool: "node"}, {Tool: "ghost"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if out[0].Err != nil {
		t.Fatalf("默认行为下 node 应成功: %v", out[0].Err)
	}
	if out[1].Err == nil {
		t.Fatal("ghost 应失败")
	}
	if n := len(inst.Ledger().Installs); n != 1 {
		t.Fatalf("默认行为下成功的工具应落盘，台账应有 1 条，实际 %d", n)
	}
}

// TestAcceptance_Par_AtomicAbortEvent：被放弃的目标各发一条 abort（tool + msg），
// 且 abort 出现在该目标的任何其它事件之后（GUI 按 tool 分组，遇到 abort 就整组作废）。
func TestAcceptance_Par_AtomicAbortEvent(t *testing.T) {
	var events []jsonout.Event
	inst, _ := parSetup(t, []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		func(o *install.Options) {
			o.Atomic = true
			o.Events = func(ev jsonout.Event) { events = append(events, ev) }
		})
	inst.InstallMany(context.Background(), []install.Target{{Tool: "node"}, {Tool: "ghost"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	var aborts []jsonout.Event
	for _, ev := range events {
		if ev.Ev == "abort" {
			aborts = append(aborts, ev)
		}
		if ev.Ev == "done" {
			t.Fatalf("--atomic 失败时不该出现 done 事件: %+v", ev)
		}
	}
	if len(aborts) != 1 || aborts[0].Tool != "node" {
		t.Fatalf("应为 node 发一条 abort，实际 %+v", aborts)
	}
	if aborts[0].Msg == "" {
		t.Fatal("abort 事件必须带 msg（说清为什么整批放弃）")
	}
}
