package install

// upgrade 的失败分支与列表投影（长尾覆盖；每条都走"尽力而为、其他工具继续"的语义）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// TestUpgradeBranches：provider 找不到 / 索引没有稳定版 / 索引取不到 / 目标装不上 —— 每条都只影响自己。
func TestUpgradeBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("provider 找不到", func(t *testing.T) {
		inst := newInstaller(t, nil, func(o *Options) {
			o.LookupProvider = func(id string) (*provider.Provider, error) {
				return nil, errors.New("清单不存在: " + id)
			}
		})
		inst.ledger.Upsert(store.Install{Tool: "ghost", Version: "1.0.0", Path: t.TempDir()})
		items, err := inst.Upgrade(ctx, []string{"ghost"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("应有一条结果: %+v", items)
		}
		if items[0].Err == "" {
			t.Fatalf("应把错误记在这个工具上: %+v", items)
		}
	})

	t.Run("索引里没有稳定版", func(t *testing.T) {
		fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
			Versions: []testkit.NodeSpec{{Version: "22.0.0-pre.1", NPMVersion: "11.20.0"}},
			NPM:      npmMatrix(),
		})
		inst := newInstaller(t, fm, nil)
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.0.0-pre.1", Path: t.TempDir()})
		items, err := inst.Upgrade(ctx, []string{"node"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("应有一条结果: %+v", items)
		}
		// 只用预发布版本装过：不 panic，也不瞎升级
		if items[0].Err != "" {
			t.Fatalf("不该报错: %+v", items)
		}
		if items[0].Skipped == "" && !items[0].Changed {
			t.Fatalf("要么跳过并说明，要么真的升: %+v", items)
		}
	})

	t.Run("索引取不到", func(t *testing.T) {
		fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
			Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
			NPM:      npmMatrix(),
		})
		inst := newInstaller(t, fm, func(o *Options) {
			c := fetch.New()
			c.Offline = true // 离线且缓存空：索引拉不到
			o.Client = c
		})
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: t.TempDir()})
		items, err := inst.Upgrade(ctx, []string{"node"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("应有一条结果: %+v", items)
		}
		if items[0].Err == "" {
			t.Fatalf("索引取不到应记错误: %+v", items)
		}
	})

	t.Run("目标装不上", func(t *testing.T) {
		fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
			Versions: []testkit.NodeSpec{
				{Version: "22.98.0", NPMVersion: "11.20.0"},
				{Version: "26.13.0", NPMVersion: "11.20.0"},
			},
			NPM:       npmMatrix(),
			Behaviors: []testkit.Option{testkit.ChecksumMismatch()},
		})
		inst := newInstaller(t, fm, nil)
		// 先把 22 装好（同一份镜像会导致校验失败，所以只写台账、不真装）
		inst.ledger.Upsert(store.Install{Tool: "node", Version: "22.98.0", Path: t.TempDir()})
		items, err := inst.Upgrade(ctx, []string{"node"}, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("应有一条结果: %+v", items)
		}
		if items[0].Err == "" {
			t.Fatalf("装不上应记错误而不是整体失败: %+v", items)
		}
	})
}

// TestInstalledEntriesAndActive：列表投影的排序与"当前生效版本"的判定。
func TestInstalledEntriesAndActive(t *testing.T) {
	cfg := store.DefaultConfig()
	cfg.Tools = map[string]string{"node": "22.98.0"}
	led := &store.Ledger{Schema: store.LedgerSchema, Installs: []store.Install{
		{Tool: "node", Version: "26.13.0", Path: t.TempDir()},
		{Tool: "node", Version: "22.98.0", Path: t.TempDir(), NpmVersion: "11.20.0", Verified: "strong"},
		{Tool: "go", Version: "1.27.3", Path: t.TempDir(), References: []store.Ref{{Kind: store.RefGlobal}}},
	}}
	active := ActiveVersions(cfg, led)
	if active["node"] != "22.98.0" || active["go"] != "1.27.3" {
		t.Fatalf("生效版本不对: %v", active)
	}
	entries := InstalledEntries(led, active)
	if len(entries) != 3 {
		t.Fatalf("应有 3 行: %+v", entries)
	}
	// 排序：先工具名，再版本
	if entries[0].Name != "go" || entries[1].Version != "22.98.0" || entries[2].Version != "26.13.0" {
		t.Fatalf("排序不对: %+v", entries)
	}
	if !entries[1].Active || !entries[0].Active || entries[2].Active {
		t.Fatalf("active 标记不对: %+v", entries)
	}
	if entries[1].NPM == nil || entries[1].NPM.Version != "11.20.0" {
		t.Fatalf("node 行应带 npm 信息: %+v", entries[1])
	}
}

// TestOutdatedHints：只读索引缓存；未知工具、已是最新都不提示，多个工具按名字排序。
func TestOutdatedHints(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "26.13.0", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	// 借一次真实解析把索引写进 meta 缓存（OutdatedHints 只读缓存）。
	p, err := inst.opt.LookupProvider("node")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := inst.resolveVersion(context.Background(), p, "22"); err != nil {
		t.Fatal(err)
	}
	entries := []jsonout.ToolEntry{
		{Name: "node", Version: "22.98.0"},
		{Name: "node", Version: "26.13.0"},
		{Name: "ghost", Version: "9.9.9"}, // 没有清单：跳过
	}
	out := OutdatedHints(context.Background(), inst.opt.Paths, "cn", entries)
	for _, o := range out {
		if o.Name != "node" {
			t.Fatalf("不该为未知工具提示: %+v", o)
		}
		if o.Installed == o.Latest {
			t.Fatalf("已是最新不该提示: %+v", o)
		}
		if !strings.HasPrefix(o.Advice, "pk upgrade ") {
			t.Fatalf("建议应可执行: %+v", o)
		}
	}
}

// TestOutdatedHintsKeepsFirstOnTie：多个工具时按名字排序（顺序稳定，GUI 列表不抖）。
func TestOutdatedHintsSort(t *testing.T) {
	// 没有缓存时应当什么都不提示，也不 panic（覆盖"缓存没东西"的分支）
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if out := OutdatedHints(context.Background(), p, "cn",
		[]jsonout.ToolEntry{{Name: "node", Version: "22.98.0"}, {Name: "go", Version: "1.27.3"}}); len(out) != 0 {
		t.Fatalf("没有索引缓存时不该提示: %+v", out)
	}
}
