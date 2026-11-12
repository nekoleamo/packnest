package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/store"
)

// seedInstalled 造一个"已装 node@22.22.2"的现场（目录 + 台账），返回 installer 与路径。
func seedInstalled(t *testing.T) (*Installer, paths.Paths, string) {
	t.Helper()
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inst, err := New(Options{Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), GOOS: "darwin", GOARCH: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(p.Tools(), "node", "22.22.2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.22.2", Path: dir})
	return inst, p, dir
}

// TestUninstallAllPlanIsReadOnly：plan 只汇总、绝不碰盘（预览与单测都靠这个保证）。
func TestUninstallAllPlanIsReadOnly(t *testing.T) {
	inst, _, dir := seedInstalled(t)
	plan := inst.PlanUninstallAll(UninstallAllOptions{Purge: true})
	if len(plan.Tools) != 1 || plan.Tools[0] != "node@22.22.2" {
		t.Fatalf("plan 该列出 node@22.22.2: %+v", plan.Tools)
	}
	if _, ok := plan.Active["node"]; !ok {
		t.Fatalf("plan 应带上各工具的当前默认（打印 * 用）: %+v", plan.Active)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Fatalf("plan 不该动盘: %v", err)
	}
	if len(inst.Ledger().Installs) != 1 {
		t.Fatalf("plan 不该改台账: %+v", inst.Ledger().Installs)
	}
}

// TestUninstallAllApply：删运行时与目录，不留台账；不带 purge 时 PK_ROOT 还在。
func TestUninstallAllApply(t *testing.T) {
	inst, p, dir := seedInstalled(t)
	res := inst.ApplyUninstallAll(inst.PlanUninstallAll(UninstallAllOptions{}))
	if len(res.Removed) != 1 || res.Removed[0] != "node@22.22.2" || len(res.Failures) != 0 {
		t.Fatalf("应删掉 1 个运行时且无失败: %+v", res)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("版本目录应被删: %v", err)
	}
	if len(inst.Ledger().Installs) != 0 {
		t.Fatalf("台账应清空: %+v", inst.Ledger().Installs)
	}
	if _, err := os.Stat(p.Root); err != nil {
		t.Fatalf("没加 purge 不该删 PK_ROOT: %v", err)
	}
	if res.Purged {
		t.Fatal("没加 purge 时 Purged 应为 false")
	}
}

// TestUninstallAllPurge：purge 删整个根；keepCache 时只留 cache/。
func TestUninstallAllPurge(t *testing.T) {
	for _, keepCache := range []bool{false, true} {
		inst, p, _ := seedInstalled(t)
		for _, sub := range []string{"cache", "state"} {
			dir := filepath.Join(p.Root, sub)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		res := inst.ApplyUninstallAll(inst.PlanUninstallAll(UninstallAllOptions{Purge: true, KeepCache: keepCache}))
		if !res.Purged || len(res.Failures) != 0 {
			t.Fatalf("keepCache=%v 应连根删成功: %+v", keepCache, res)
		}
		if keepCache {
			if _, err := os.Stat(filepath.Join(p.Root, "cache", "f")); err != nil {
				t.Fatalf("keepCache 应保留 cache/: %v", err)
			}
			if _, err := os.Stat(filepath.Join(p.Root, "state")); !os.IsNotExist(err) {
				t.Fatalf("keepCache 只该留 cache/: %v", err)
			}
			continue
		}
		if _, err := os.Stat(p.Root); !os.IsNotExist(err) {
			t.Fatalf("purge 应删掉整个 PK_ROOT: %v", err)
		}
	}
}
