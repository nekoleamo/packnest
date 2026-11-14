package install

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// TestGenericCompanionInstalled：外部清单声明 [companions.xx] 时，伴生包要真的落到宿主的
// <target> 目录里（1.1：伴生包不再只认 node→npm），并吐一条 companion 事件。
func TestGenericCompanionInstalled(t *testing.T) {
	mk := func(id string) *provider.Provider {
		ft := testkit.NewFakeTool(t, testkit.FakeToolOptions{
			Tool:         "singlefile",
			Versions:     []string{"1.0.0"},
			ProviderTOML: testkit.SingleFileProviderTOML,
			MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeSingleFileZip() },
			Platforms:    []testkit.FakePlatform{{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"}},
			ChecksumPath: "singlefile-%s.zip.sha256sum", ChecksumFormat: "per-artifact",
		})
		toml := strings.Replace(testkit.SingleFileProviderTOML(ft.Server.URL), `id = "singlefile"`, `id = "`+id+`"`, 1)
		if id == "demo" {
			toml += "\n[companions.cc]\nmode = \"latest-compatible\"\nregistry = \"cc\"\ntarget = \"lib/cc\"\n"
		}
		p, err := provider.Parse([]byte(toml))
		if err != nil {
			t.Fatalf("parse %s: %v", id, err)
		}
		return p
	}
	providers := map[string]*provider.Provider{"demo": mk("demo"), "cc": mk("cc")}
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var events []jsonout.Event
	inst, err := New(Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(id string) (*provider.Provider, error) { return providers[id], nil },
		Events:         func(ev jsonout.Event) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := inst.Install(context.Background(), "demo", "1.0.0"); err != nil {
		t.Fatalf("装 demo: %v", err)
	}

	// 伴生包应落在 <工具根>/lib/cc 下，宿主卸载时跟着一起走（所以不单独写台账）。
	ccDir := filepath.Join(p.Tools(), "demo", "1.0.0", "lib", "cc")
	found := false
	_ = filepath.WalkDir(ccDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("伴生包没有落到 %s（目录不存在或为空）", ccDir)
	}
	var companionEvents int
	for _, ev := range events {
		if ev.Ev == "companion" && ev.Tool == "cc" {
			companionEvents++
		}
	}
	if companionEvents == 0 {
		t.Fatalf("应吐一条 companion 事件，实际事件: %+v", events)
	}
	if _, err := os.Stat(filepath.Join(p.Tools(), "demo", "1.0.0", "lib", "cc")); err != nil {
		t.Fatalf("伴生目录不存在: %v", err)
	}
}

// TestPrefetchWaitDrains：后台索引预取必须在操作返回前被等干净。
//
// 回归点（2026-11-15 CI 偶发）：`pk install` 失败早退时，预取 goroutine 还在往缓存目录写，
// 而调用方（或测试的 TempDir 清理）已经把目录删了 —— 表现为 `directory not empty` 清理报错，
// 生产里则是"卸载/清理之后目录又被写回来"。
func TestPrefetchWaitDrains(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	p, err := inst.opt.LookupProvider("npm")
	if err != nil {
		t.Fatal(err)
	}
	f := inst.startPrefetch(context.Background(), p)
	inst.waitPrefetch()
	select {
	case <-f.done:
	default:
		t.Fatal("waitPrefetch 返回时预取还没结束（goroutine 会活过这次操作）")
	}
	if f.idx == nil {
		// 预取失败也算结束；这里只是确认结果槽已被写入或确实是 nil（无第三个状态）
		t.Log("预取没有拿到索引（假镜像的 npm 路径可能不同），但 goroutine 已结束")
	}
	// 预取写过的缓存目录此刻必须存在且稳定：再等一拍不该有新文件冒出来
	before, _ := os.ReadDir(inst.opt.Paths.CacheMeta())
	time.Sleep(50 * time.Millisecond)
	after, _ := os.ReadDir(inst.opt.Paths.CacheMeta())
	if len(after) != len(before) {
		t.Fatalf("操作返回后缓存目录还在变（%d → %d）", len(before), len(after))
	}
}
