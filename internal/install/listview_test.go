package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/store"
)

// ActiveVersions：config 的全局默认优先，台账里的全局引用兜底。
func TestActiveVersions(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.Tools = map[string]string{"bun": "1.0.0"}
	ledger := &store.Ledger{Installs: []store.Install{
		{Tool: "node", Version: "22.22.2", References: []store.Ref{{Kind: store.RefGlobal}}},
		{Tool: "bun", Version: "0.9.0", References: []store.Ref{{Kind: store.RefGlobal}}},
		{Tool: "go", Version: "1.27.3"},
	}}
	active := ActiveVersions(cfg, ledger)
	if active["node"] != "22.22.2" {
		t.Fatalf("台账引用应成为兜底默认: %+v", active)
	}
	if active["bun"] != "1.0.0" {
		t.Fatalf("config 应优先于台账引用: %+v", active)
	}
	if _, ok := active["go"]; ok {
		t.Fatalf("没有全局引用的工具不该出现在默认里: %+v", active)
	}
	if got := ActiveVersions(nil, &store.Ledger{}); len(got) != 0 {
		t.Fatalf("nil config 应给空表，得到 %+v", got)
	}
}

// InstalledEntries：排序稳定、node 带 npm 伴生信息、目录大小算得出。
func TestInstalledEntries(t *testing.T) {
	dir := t.TempDir()
	nodeDir := filepath.Join(dir, "tools", "node", "22.22.2")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "bin"), []byte("xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := &store.Ledger{Installs: []store.Install{
		{Tool: "node", Version: "22.22.2", Path: nodeDir, NpmVersion: "11.20.0", NpmBundled: "10.9.10", Verified: "strong"},
		{Tool: "bun", Version: "1.3.0", Path: "", Verified: "weak"},
	}}
	entries := InstalledEntries(ledger, map[string]string{"node": "22.22.2"})
	if len(entries) != 2 || entries[0].Name != "bun" || entries[1].Name != "node" {
		t.Fatalf("应按工具名排序: %+v", entries)
	}
	nodeEntry := entries[1]
	if !nodeEntry.Active || nodeEntry.NPM == nil || nodeEntry.NPM.Version != "11.20.0" || nodeEntry.NPM.Replaced != "10.9.10" {
		t.Fatalf("node 行异常: %+v", nodeEntry)
	}
	if nodeEntry.SizeBytes <= 0 {
		t.Fatalf("应算出目录占用: %+v", nodeEntry)
	}
	if nodeEntry.Source == "" && nodeEntry.Requires == nil {
		t.Fatalf("Requires 要序列化成 []（不能是 null）: %+v", nodeEntry)
	}
	if entries[0].NPM != nil || entries[0].SizeBytes != 0 || entries[0].Verified != "weak" {
		t.Fatalf("非 node / 无路径的行不该有 npm 与大小: %+v", entries[0])
	}
}

// OutdatedHints 只读索引缓存：有缓存才提示，没缓存静默跳过（零网络）。
func TestOutdatedHintsFromCache(t *testing.T) {
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 手写一份"以前拉过"的索引缓存（Load 的缓存就是 <meta>/<id>.json + .meta.json）
	if err := os.MkdirAll(p.CacheMeta(), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `[{"version":"v22.22.2","lts":"Jod"},{"version":"v24.15.0","lts":"Krypton"}]`
	if err := os.WriteFile(filepath.Join(p.CacheMeta(), "node.json"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"url":"https://example.invalid/index.json","fetched_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(filepath.Join(p.CacheMeta(), "node.meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	hints := OutdatedHints(context.Background(), p, "cn", []jsonout.ToolEntry{{Name: "node", Version: "22.22.2"}})
	if len(hints) != 1 || hints[0].Name != "node" || hints[0].Installed != "22.22.2" || hints[0].Latest != "24.15.0" {
		t.Fatalf("应提示 22.22.2 → 24.15.0: %+v", hints)
	}
	if hints[0].Advice != "pk upgrade node" {
		t.Fatalf("建议文案应指向 pk upgrade: %q", hints[0].Advice)
	}

	// 已是最新 → 不提示
	if got := OutdatedHints(context.Background(), p, "cn", []jsonout.ToolEntry{{Name: "node", Version: "24.15.0"}}); len(got) != 0 {
		t.Fatalf("已最新不该提示: %+v", got)
	}
	// 没有任何缓存 → 静默跳过（离线零网络）
	empty, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := OutdatedHints(context.Background(), empty, "cn", []jsonout.ToolEntry{{Name: "node", Version: "22.22.2"}}); len(got) != 0 {
		t.Fatalf("没缓存不该提示: %+v", got)
	}
}
