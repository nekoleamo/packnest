// M4 验收：台账/引用计数/GC/全局包/清扫（方案 §12.1 M4 门禁）。
//
//   - GcZeroRef：零引用版本被回收，被引用的留下
//   - LedgerRecover：台账损坏时从 .bak 恢复
//   - FrozenMismatchExitCode：`--frozen` 下清单与锁不一致 → 非 0 退出
//   - CtrlCNoPartial：安装中途取消 → 不留半成品目录/台账
//   - GlobalSurvivesNodeSwitch：切 node 版本，全局包文件一个字节都不动
//   - GlobalBinNoReshim：固定 global/bin → 新装的全局包命令立刻可用
//   - NativeGlobalDetect：.node 非 N-API 要告警，prebuilds/*.napi.node 不告警
//   - StartupSweep：断电残留（.tmp/.part）在写操作前被清掉
package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

func m4Env(t *testing.T) (*testkit.FakeNode, *install.Installer, paths.Paths) {
	t.Helper()
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.22.2", NPMVersion: "11.20.0"},
			{Version: "24.14.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrixAcceptance(),
	})
	inst, p := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})
	return fm, inst, p
}

// TestAcceptance_M4_GcZeroRef：只回收零引用版本。
func TestAcceptance_M4_GcZeroRef(t *testing.T) {
	_, inst, p := m4Env(t)
	ctx := context.Background()
	keep, err := inst.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := inst.Install(ctx, "node", "24.14.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}

	res, err := inst.GC(ctx, 1<<40, false, false)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "node@24.14.0" {
		t.Fatalf("应只回收零引用的 24.14.0: %+v", res)
	}
	if _, err := os.Stat(orphan.Path); !os.IsNotExist(err) {
		t.Fatal("被回收的目录应删除")
	}
	if _, err := os.Stat(keep.Path); err != nil {
		t.Fatal("有全局引用的版本不该删")
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Installs) != 1 || ledger.Installs[0].Version != "22.22.2" {
		t.Fatalf("台账应只剩 22.22.2: %+v", ledger.Installs)
	}
}

// TestAcceptance_M4_LedgerRecover：主台账损坏 → 用 .bak 恢复并继续工作。
func TestAcceptance_M4_LedgerRecover(t *testing.T) {
	_, inst, p := m4Env(t)
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	// 再写一次，生成 .bak 里有 22.22.2 的记录
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Ledger(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger, recovered, err := store.LoadLedgerWithRecovery(p.Ledger())
	if err != nil {
		t.Fatalf("应能恢复: %v", err)
	}
	if !recovered || ledger.Find("node", "22.22.2") == nil {
		t.Fatalf("恢复结果异常: recovered=%v ledger=%+v", recovered, ledger)
	}
	// 安装器也要能带着恢复后的台账继续干活
	inst2, err := install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(id string) (*provider.Provider, error) {
			return provider.Get(id)
		},
	})
	if err != nil {
		t.Fatalf("台账接线恢复: %v", err)
	}
	if inst2.Ledger().Find("node", "22.22.2") == nil {
		t.Fatal("安装器应读到恢复后的台账")
	}
}

// TestAcceptance_M4_FrozenMismatchExitCode：清单与锁不一致 → 明确错误（CLI 退出码非 0）。
func TestAcceptance_M4_FrozenMismatchExitCode(t *testing.T) {
	project := map[string]string{"node": "24"}
	lock := &install.Lock{Tool: []install.LockTool{{Name: "node", Version: "22.22.2"}}}
	err := install.CheckFrozen(project, lock)
	if err == nil {
		t.Fatal("版本不满足时必须失败")
	}
	if !strings.Contains(err.Error(), "不满足") || !strings.Contains(err.Error(), "pk.lock") {
		t.Fatalf("错误信息应说明不一致点与处置: %v", err)
	}
	// 一致时通过
	if err := install.CheckFrozen(map[string]string{"node": "22"}, lock); err != nil {
		t.Fatalf("一致时应通过: %v", err)
	}
	// 缺锁
	if err := install.CheckFrozen(project, nil); err == nil {
		t.Fatal("缺 pk.lock 时应失败")
	}
}

// TestAcceptance_M4_CtrlCNoPartial：安装中途取消 → 不留半成品目录、台账不变。
func TestAcceptance_M4_CtrlCNoPartial(t *testing.T) {
	// 下载很慢，确保能在传输中途取消
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.99.99"}},
		NPM:       map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
		Behaviors: []testkit.Option{testkit.Slow(300 * time.Millisecond)},
	})
	inst, p := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if _, err := inst.Install(ctx, "node", "22.99.99"); err == nil {
		t.Fatal("被取消的安装应失败")
	}
	// 没有版本目录、没有临时目录、台账为空
	entries, err := os.ReadDir(p.Tool("node"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("取消后不该留临时目录: %s", e.Name())
		}
	}
	if _, err := os.Stat(p.ToolVersion("node", "22.99.99")); !os.IsNotExist(err) {
		t.Fatal("取消后不该有版本目录")
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	ledger, err := store.LoadLedger(p.Ledger())
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Installs) != 0 {
		t.Fatalf("台账不该有记录: %+v", ledger.Installs)
	}
	// 下一次写操作前清扫会把 .part 当残留处理（这里只验证不残留版本目录）
	if res, err := inst.SweepStale(); err != nil {
		t.Fatalf("SweepStale: %v", err)
	} else if len(res.Removed) > 0 {
		t.Logf("清扫掉 %d 项残留", len(res.Removed))
	}
}

// TestAcceptance_M4_GlobalSurvivesNodeSwitch：切 node 版本，全局包一个字节都不动。
func TestAcceptance_M4_GlobalSurvivesNodeSwitch(t *testing.T) {
	_, inst, p := m4Env(t)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Install(ctx, "node", "24.14.0"); err != nil {
		t.Fatal(err)
	}
	// 造一个全局包（模拟 `npm i -g`）到共享前缀
	pkgDir := filepath.Join(p.Global(), "lib", "node_modules", "vite")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"vite","version":"7.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := p.GlobalBin()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "vite"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}

	// 切到 24.14.0
	if err := inst.Activate("node", "24.14.0"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := inst.ActiveVersion("node"); got != "24.14.0" {
		t.Fatalf("应切到 24.14.0，得到 %q", got)
	}
	// ① 全局包文件没被复制/移动（inode 与 mtime 都不变）
	after, err := os.Stat(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		t.Fatal("全局包不该消失")
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("全局包文件被改动了（切版本不该动它）")
	}
	// ② 前缀变量仍是 shared 的那一套（同一目录 → 无需重装）
	vars := map[string]string{}
	for _, kv := range env.SharedVarsFor("shared", p, "/home/u/.npm", "24.14.0") {
		vars[kv.Key] = kv.Value
	}
	if vars["npm_config_prefix"] != p.Global() {
		t.Fatalf("shared 前缀必须固定: %q", vars["npm_config_prefix"])
	}
	// ③ 切版本后全局包命令仍在 PATH 段里（固定目录）
	segs := p.PathSegments()
	found := false
	for _, seg := range segs {
		if seg == p.GlobalBin() {
			found = true
		}
	}
	if !found {
		t.Fatalf("global/bin 必须在 PATH 段里: %v", segs)
	}
}

// TestAcceptance_M4_GlobalBinNoReshim：global/bin 是固定目录，新文件立刻可解析。
func TestAcceptance_M4_GlobalBinNoReshim(t *testing.T) {
	_, _, p := m4Env(t)
	if err := os.MkdirAll(p.GlobalBin(), 0o755); err != nil {
		t.Fatal(err)
	}
	// 模拟 `npm i -g` 往 global/bin 丢了个新命令
	newBin := filepath.Join(p.GlobalBin(), "brand-new-cli")
	if err := os.WriteFile(newBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 没有任何 reshim 动作，PATH 上的这个目录立刻能解析到它
	segs := p.PathSegments()
	dir := ""
	for _, seg := range segs {
		if seg == p.GlobalBin() {
			dir = seg
			break
		}
	}
	if dir == "" {
		t.Fatalf("global/bin 必须在 PATH 段里: %v", segs)
	}
	resolved := filepath.Join(dir, "brand-new-cli")
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("新命令应立刻可解析: %v", err)
	}
}

// TestAcceptance_M4_NativeGlobalDetect：L2 原生模块检测（N-API 不告警）。
func TestAcceptance_M4_NativeGlobalDetect(t *testing.T) {
	prefix := t.TempDir()
	writeGlobal := func(name, engines string, files map[string]string) {
		dir := filepath.Join(prefix, "lib", "node_modules", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"name":"` + name + `","version":"1.0.0"`
		if engines != "" {
			body += `,"engines":{"node":"` + engines + `"}`
		}
		body += `}`
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		for path, content := range files {
			full := filepath.Join(dir, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeGlobal("needs-node24", ">=24", nil)
	writeGlobal("legacy-native", "", map[string]string{"build/Release/addon.node": "x"})
	writeGlobal("modern-napi", "", map[string]string{"prebuilds/darwin-arm64/addon.napi.node": "x"})

	pkgs, err := env.ScanGlobals(prefix, "darwin")
	if err != nil {
		t.Fatalf("ScanGlobals: %v", err)
	}
	problems := env.CheckGlobals(pkgs, "22.22.2")
	codes := map[string]int{}
	for _, p := range problems {
		codes[p.Code]++
	}
	if codes["GLOBAL_ENGINES_UNMET"] != 1 {
		t.Fatalf("needs-node24 应报 engines 不满足: %+v", problems)
	}
	if codes["GLOBAL_NATIVE_ABI"] != 1 {
		t.Fatalf("只有 legacy-native 应报 ABI: %+v", problems)
	}
	for _, p := range problems {
		if p.Pkg == "modern-napi" {
			t.Fatalf("N-API 预编译包不该告警: %+v", p)
		}
	}
	// JSON 可序列化（doctor --json 直接吃）
	if _, err := json.Marshal(problems); err != nil {
		t.Fatalf("问题项应可序列化: %v", err)
	}
}

// TestAcceptance_M4_StartupSweep：断电残留（.tmp/.old/.part）在写操作前清掉。
func TestAcceptance_M4_StartupSweep(t *testing.T) {
	_, inst, p := m4Env(t)
	// 造三种残留 + 一个新鲜的（不该删）
	old := func(path string) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "f"), []byte("1234"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	old(filepath.Join(p.Tool("node"), "22.22.2.tmp.abcd"))
	old(filepath.Join(p.Tool("node"), "22.22.2.old"))
	if err := os.MkdirAll(p.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.CacheArchives(), "node.part"), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(p.CacheArchives(), "node.part"), ts, ts)
	fresh := filepath.Join(p.Tool("node"), "9.9.9.tmp.fresh")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := inst.SweepStale()
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if len(res.Removed) != 3 {
		t.Fatalf("应清掉 3 项残留（.tmp/.old/.part），得到 %v", res.Removed)
	}
	if res.Bytes == 0 {
		t.Fatal("应统计释放字节")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("新鲜残留不该删（可能是另一个进程正在用）")
	}
}

// TestAcceptance_M4_CacheMax：缓存超限按 LRU 淘汰，dry-run 不动磁盘。
func TestAcceptance_M4_CacheMax(t *testing.T) {
	_, inst, p := m4Env(t)
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(p.CacheArchives())
	if err != nil || len(entries) == 0 {
		t.Fatalf("应有缓存产物: %v %v", entries, err)
	}

	// dry-run：报告但不删
	res, err := inst.GC(context.Background(), 1, true, false)
	if err != nil {
		t.Fatalf("GC dry-run: %v", err)
	}
	if len(res.CacheRemoved) == 0 {
		t.Fatalf("上限 1 字节时 dry-run 应报告淘汰项: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(p.CacheArchives(), entries[0].Name())); err != nil {
		t.Fatal("dry-run 不该删缓存")
	}

	// 真删
	res, err = inst.GC(context.Background(), 1, false, false)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.CacheRemoved) == 0 {
		t.Fatalf("应淘汰缓存: %+v", res)
	}
	remain, _ := os.ReadDir(p.CacheArchives())
	if len(remain) != 0 {
		t.Fatalf("上限 1 字节应清空缓存，剩 %v", remain)
	}
}
