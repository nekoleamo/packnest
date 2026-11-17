package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/packnest/packnest/internal/jsonout"
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

// TestInstallManyProgressPerTool（E4）：并行安装时每个目标各自一个进度回调，
// 回调由 install 挂在 worker 的 Client 副本上（闭包 tool），否则多行进度会互相踩。
func TestInstallManyProgressPerTool(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	var mu sync.Mutex
	var gotTools []string
	calls := 0
	inst := newInstaller(t, fm, func(o *Options) {
		o.ProgressFor = func(tool string) func(done, total int64) {
			mu.Lock()
			gotTools = append(gotTools, tool)
			mu.Unlock()
			return func(done, total int64) {
				mu.Lock()
				calls++
				mu.Unlock()
				if total > 0 && done > total {
					t.Errorf("%s: done(%d) > total(%d)", tool, done, total)
				}
			}
		}
	})
	out := inst.InstallMany(context.Background(), []Target{{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("node@%s 失败: %v", oc.Spec, oc.Err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotTools) != 2 {
		t.Fatalf("两个目标应各挂一次回调，得到 %d 次: %v", len(gotTools), gotTools)
	}
	for _, tool := range gotTools {
		if tool != "node" {
			t.Fatalf("回调应闭包各自的 tool，得到 %q", tool)
		}
	}
	if calls == 0 {
		t.Fatal("进度回调一次都没触发（下载没走 Client.Progress？）")
	}
}

// TestInstallSingleNoProgressFor（E4）：单目标走 Install()，不经过并行层的 ProgressFor
// （回调在 CLI 直接挂在共享 client 上，行为与 1.6 一致）。
func TestInstallSingleNoProgressFor(t *testing.T) {
	fm := testkitNode(t)
	var mu sync.Mutex
	n := 0
	inst := newInstaller(t, fm, func(o *Options) {
		o.ProgressFor = func(tool string) func(done, total int64) {
			mu.Lock()
			n++
			mu.Unlock()
			return func(done, total int64) {}
		}
	})
	if _, err := inst.Install(context.Background(), "npm", ""); err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Fatalf("单目标不该调用 ProgressFor（会与 CLI 的回调重复）: %d 次", n)
	}
}

// TestInstallManyAtomicAllSucceed（E5）：全部 prepare 成功时按层提交，台账两行。
func TestInstallManyAtomicAllSucceed(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, func(o *Options) { o.Atomic = true })
	out := inst.InstallMany(context.Background(), []Target{{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.99.0"}})
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	for _, oc := range out {
		if oc.Err != nil {
			t.Fatalf("%s@%s 失败: %v", oc.Tool, oc.Spec, oc.Err)
		}
	}
	if n := len(inst.Ledger().Installs); n < 2 {
		t.Fatalf("--atomic 全成功时应全部落盘，台账 %d 条", n)
	}
}

// TestAbortAllCleansTmp（E5）：被放弃的目标要删临时目录，并带上 AbortError（可 errors.Is 到根因）。
func TestAbortAllCleansTmp(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	tmp := filepath.Join(inst.opt.Paths.Root, "tools", "node", "22.1.0.tmp.deadbeef")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	targets := []Target{{Tool: "node"}, {Tool: "ghost"}}
	out := make([]Outcome, len(targets))
	preps := []*prepared{{tmpDir: tmp}, nil}
	cause := errors.New("ghost 失败")

	inst.abortAll(targets, out, preps, nil, cause)

	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("临时目录必须删掉: %v", err)
	}
	var ab *AbortError
	if !errors.As(out[0].Err, &ab) || ab.Tool != "node" {
		t.Fatalf("应为 node 返回 AbortError: %+v", out[0].Err)
	}
	if !errors.Is(out[0].Err, cause) {
		t.Fatal("AbortError 必须能 unwrap 到根因")
	}
	// 文案是给人看的，判定用类型（errors.As 上面已经做过）
	var ab2 *AbortError
	if !errors.As(out[0].Err, &ab2) || ab2.Tool != "node" {
		t.Fatalf("应为 AbortError: %+v", out[0].Err)
	}
}

// TestFirstErr 按顺序取第一个错误（--atomic 的"哪个先失败"必须可复现）。
func TestFirstErr(t *testing.T) {
	if got := firstErr([]error{nil, nil}); got != nil {
		t.Fatalf("全 nil 应返回 nil: %v", got)
	}
	e1, e2 := errors.New("a"), errors.New("b")
	if got := firstErr([]error{nil, e1, e2}); got != e1 {
		t.Fatalf("应取第一个错误: %v", got)
	}
}

// TestInstallManyFlushesFailedWorkerEvents：并行路径里某个目标 prepare 失败时，
// 它已经发生的 resolve / download / verify 事件必须补发。
// 0.14 之前失败分支直接丢掉 prepared.events，消费方按 tool 分组只会看到"这个工具什么都没发生"，
// 而 --json 契约说 abort / 失败排在"该目标其它事件之后"。
func TestInstallManyFlushesFailedWorkerEvents(t *testing.T) {
	mkTool := func(id string, opts ...testkit.Option) *provider.Provider {
		ft := testkit.NewFakeTool(t, testkit.FakeToolOptions{
			Tool:         id, // 两个工具的产物路径必须不同：一样的话会命中同一份缓存，投毒字节被发现不了
			Versions:     []string{"1.0.0"},
			ProviderTOML: testkit.SingleFileProviderTOML,
			MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeSingleFileZip() },
			Platforms:    []testkit.FakePlatform{{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"}},
			ChecksumPath: id + "-%s.zip.sha256sum", ChecksumFormat: "per-artifact",
			Behaviors: opts,
		})
		toml := strings.Replace(testkit.SingleFileProviderTOML(ft.Server.URL), `id = "singlefile"`, `id = "`+id+`"`, 1)
		p, err := provider.Parse([]byte(toml))
		if err != nil {
			t.Fatalf("parse %s: %v", id, err)
		}
		return p
	}
	good := mkTool("good")
	bad := mkTool("bad", testkit.Corrupt())

	var evs []jsonout.Event
	inst := newInstaller(t, nil, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "bad" {
				return bad, nil
			}
			return good, nil
		}
		o.Events = func(ev jsonout.Event) { evs = append(evs, ev) }
	})
	out := inst.InstallMany(context.Background(), []Target{
		{Tool: "good", Spec: "1.0.0"},
		{Tool: "bad", Spec: "1.0.0"},
	})
	if len(out) != 2 || out[1].Err == nil {
		t.Fatalf("第二个目标应失败: %+v", out)
	}
	var badEvs []string
	for _, ev := range evs {
		if ev.Tool == "bad" {
			badEvs = append(badEvs, ev.Ev)
		}
	}
	if len(badEvs) == 0 {
		t.Fatalf("失败目标的已发生事件被整段丢弃（事件总数 %d）", len(evs))
	}
	if badEvs[0] != "resolve" {
		t.Fatalf("失败目标的第一条事件应是 resolve，实际 %v", badEvs)
	}
}

// TestInstallManyKeepsCompanionTOFUOut 记录一条**故意不做**的事：
// worker 的 TOFU 副本不能整体并回主表（那样 npm 伴生条目能留下来、省一次求校验和），
// 因为 npm 伴生包用 sha1（有 shasum 时）、独立 npm 包用 sha512 integrity，
// 同一个 key 两种算法 → 合并后下一次校验报 sum.algo_changed（见 TestAcceptance_Par_NPMConflict）。
// 这条用例钉住现状：合并或统一算法时必须同时改这里。
func TestInstallManyKeepsCompanionTOFUOut(t *testing.T) {
	fm := testkitNode(t) // node 22.98.0 + 伴生 npm
	inst := newInstaller(t, fm, nil)
	out := inst.InstallMany(context.Background(), []Target{
		{Tool: "node", Spec: "22.98.0"}, {Tool: "node", Spec: "22.98.0"},
	})
	for _, o := range out {
		if o.Err != nil {
			t.Fatalf("%s 安装失败: %v", o.Tool, o.Err)
		}
	}
	for k := range inst.known.Entries {
		if strings.HasPrefix(k, "npm@") {
			t.Fatalf("npm 伴生条目被并进主表了（%s）：先统一两条路径的算法，否则会 sum.algo_changed", k)
		}
	}
}
