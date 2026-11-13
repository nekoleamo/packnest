// M7 验收：`pk upgrade`（1.4 的第 10 个命令）。
package acceptance

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/testkit"
)

// TestAcceptance_M7_Upgrade：升到索引最新 + 切默认 + 旧版本保留；dry-run 不下载；npm 跳过。
func TestAcceptance_M7_Upgrade(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.99", NPMVersion: "11.20.0"},
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
	ctx := context.Background()

	old, err := inst.Install(ctx, "node", "22.98.0")
	if err != nil {
		t.Fatalf("装 22.98.0: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}

	// dry-run：只算不装
	items, err := inst.Upgrade(ctx, nil, true)
	if err != nil {
		t.Fatalf("upgrade --dry-run: %v", err)
	}
	var nodeItem *install.UpgradeItem
	for i := range items {
		if items[i].Tool == "node" {
			nodeItem = &items[i]
		}
	}
	if nodeItem == nil || !nodeItem.Changed || nodeItem.From != "22.98.0" || nodeItem.To != "22.99.99" {
		t.Fatalf("dry-run 应报告 22.98.0 → 22.99.99: %+v", items)
	}
	if _, err := os.Stat(p.ToolVersion("node", "22.99.99")); !os.IsNotExist(err) {
		t.Fatal("dry-run 不该真的装上")
	}

	// 真升级：装新版 + 切默认 + 旧版本留着
	items, err = inst.Upgrade(ctx, nil, false)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if got := inst.ActiveVersion("node"); got != "22.99.99" {
		t.Fatalf("升级后默认应切到 22.99.99，得到 %q", got)
	}
	if _, err := os.Stat(old.Path); err != nil {
		t.Fatal("旧版本目录应保留（想清用 pk gc --keep）")
	}
	for _, it := range items {
		if it.Tool == "npm" && it.Skipped == "" {
			t.Fatalf("npm 应被跳过并说明原因: %+v", it)
		}
	}

	// 再跑一次：已是最新，不该重复下载
	again, err := inst.Upgrade(ctx, []string{"node"}, false)
	if err != nil {
		t.Fatalf("upgrade 二次: %v", err)
	}
	if len(again) != 1 || again[0].Changed || again[0].Skipped != "已是最新" {
		t.Fatalf("二次升级应报已是最新: %+v", again)
	}

	// 未安装的工具：不报错，给指路
	miss, err := inst.Upgrade(ctx, []string{"pnpm"}, false)
	if err != nil {
		t.Fatalf("upgrade 未装工具不该报错: %v", err)
	}
	if len(miss) != 1 || miss[0].Skipped == "" {
		t.Fatalf("未装工具应给 skipped 提示: %+v", miss)
	}
}

// TestAcceptance_M7_PackageJSONEngines：进一个已有前端项目 → `pk install` 不带参数时按
// package.json 的 engines.node 选 node 版本；pk.toml 有声明时以 pk.toml 为准。
func TestAcceptance_M7_PackageJSONEngines(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "20.19.5", NPMVersion: "11.20.0"},
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "24.1.0", NPMVersion: "12.2.2"},
		},
		NPM: npmMatrixAcceptance(),
	})
	inst, _ := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return fm.NodeProvider, nil
		case "npm":
			return fm.NPMProvider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/package.json", []byte(`{"engines":{"node":">=22.9 <23"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, _, err := install.ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("ProjectSpecs: %v", err)
	}
	res, err := inst.Install(context.Background(), "node", specs["node"])
	if err != nil {
		t.Fatalf("按 engines.node 装: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if res.Version != "22.98.0" {
		t.Fatalf("engines `>=22.9 <23` 应选出 22.98.0，得到 %q", res.Version)
	}

	// pk.toml 的声明优先：同一个 package.json，pk.toml 说 20 就装 20。
	if err := os.WriteFile(dir+"/pk.toml", []byte("[tools]\nnode = \"20\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, _, err = install.ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("ProjectSpecs: %v", err)
	}
	if specs["node"] != "20" {
		t.Fatalf("pk.toml 应优先，得到 %q", specs["node"])
	}
}
