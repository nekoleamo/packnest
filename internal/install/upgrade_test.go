package install

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// TestUpgradeDryRunAndApply：dry-run 只算不装；真升装新版、切默认、旧版本保留；二次报"已是最新"。
func TestUpgradeDryRunAndApply(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.99", NPMVersion: "11.20.0"},
		},
		NPM: npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	ctx := context.Background()
	old, err := inst.Install(ctx, "node", "22.98.0")
	if err != nil {
		t.Fatal(err)
	}

	items, err := inst.Upgrade(ctx, nil, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var node *UpgradeItem
	for i := range items {
		if items[i].Tool == "node" {
			node = &items[i]
		}
	}
	if node == nil || node.From != "22.98.0" || node.To != "22.99.99" || !node.Changed {
		t.Fatalf("dry-run 结果不对: %+v", items)
	}
	if _, err := os.Stat(inst.opt.Paths.ToolVersion("node", "22.99.99")); !os.IsNotExist(err) {
		t.Fatal("dry-run 不该装上新版本")
	}

	items, err = inst.Upgrade(ctx, nil, false)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if got := inst.ActiveVersion("node"); got != "22.99.99" {
		t.Fatalf("升级后应切默认，得到 %q", got)
	}
	if _, err := os.Stat(old.Path); err != nil {
		t.Fatal("旧版本目录应保留")
	}
	if changed, failed := UpgradeSummary(items); changed != 1 || failed != 0 {
		t.Fatalf("摘要不对: changed=%d failed=%d %+v", changed, failed, items)
	}
	// npm 显式点名时被跳过并说明原因（伴生包没进台账，不会被自动带上）
	npmItems, err := inst.Upgrade(ctx, []string{"npm"}, true)
	if err != nil {
		t.Fatalf("upgrade npm: %v", err)
	}
	if len(npmItems) != 1 || npmItems[0].Skipped == "" || npmItems[0].Changed {
		t.Fatalf("npm 应被跳过并说明原因: %+v", npmItems)
	}

	again, err := inst.Upgrade(ctx, []string{"node"}, false)
	if err != nil {
		t.Fatalf("二次 upgrade: %v", err)
	}
	if len(again) != 1 || again[0].Changed || again[0].Skipped != "已是最新" {
		t.Fatalf("二次应报已是最新: %+v", again)
	}
}

// TestUpgradeErrorBranches：provider 查不到 / 索引拿不到 / 索引里没稳定版 / 未安装工具都不该让整条失败。
func TestUpgradeErrorBranches(t *testing.T) {
	// 只认 node；brokentool 的 provider 故意没有索引源（loadIndex 必失败），ghost 根本不存在
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	noIndex := &provider.Provider{ID: "noindex"}
	preOnly := &provider.Provider{ID: "preonly"}
	inst := newInstaller(t, fm, func(o *Options) {
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			switch id {
			case "node":
				return fm.NodeProvider, nil
			case "noindex":
				return noIndex, nil
			case "preonly":
				return preOnly, nil
			}
			return nil, errors.New("未知工具 " + id)
		}
	})
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.98.0"); err != nil {
		t.Fatal(err)
	}
	// 手工塞两条"已装但工具本身有问题"的台账记录
	for _, tool := range []string{"noindex", "preonly"} {
		dir := inst.opt.Paths.ToolVersion(tool, "1.0.0")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		inst.ledger.Upsert(store.Install{Tool: tool, Version: "1.0.0", Path: dir})
	}

	items, err := inst.Upgrade(ctx, []string{"noindex", "ghost"}, true)
	if err != nil {
		t.Fatalf("个别工具失败不该整体报错: %v", err)
	}
	byTool := map[string]UpgradeItem{}
	for _, it := range items {
		byTool[it.Tool] = it
	}
	if byTool["noindex"].Err == "" {
		t.Fatalf("没有索引源的工具应报错: %+v", byTool["noindex"])
	}
	if byTool["ghost"].Skipped == "" {
		t.Fatalf("未安装的工具应给 skipped 提示: %+v", byTool["ghost"])
	}
	if _, failed := UpgradeSummary(items); failed != 1 {
		t.Fatalf("失败计数不对: %+v", items)
	}
}
