package install

import (
	"context"
	"os"
	"testing"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/testkit"
)

// 批量预取：多目标时把产物并发下到缓存（台账仍为空、不发事件），之后的串行 Install 命中缓存。
func TestPrefetchArtifactsFillsCache(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}, {Version: "24.1.0", NPMVersion: "12.2.2"}},
		NPM:      npmMatrix(),
	})
	inst := newInstaller(t, fm, nil)
	var events []string
	inst.opt.Events = func(ev jsonout.Event) { events = append(events, ev.Ev) }
	ctx := context.Background()

	inst.PrefetchArtifacts(ctx, []Target{{Tool: "node", Spec: "22.22.2"}, {Tool: "node", Spec: "24.1.0"}})

	root := rootOf(t, inst)
	entries, err := os.ReadDir(root.CacheArchives())
	if err != nil || len(entries) < 2 {
		t.Fatalf("应有两个产物落到缓存: %v %v", entries, err)
	}
	if n := len(inst.Ledger().Installs); n != 0 {
		t.Fatalf("预取不该写台账，得到 %d 条", n)
	}
	if len(events) != 0 {
		t.Fatalf("预取不该发事件（进度条/NDJSON 都属于随后的串行安装）: %v", events)
	}

	// 只有 1 个目标 / dry-run / force / 离线都不预取
	inst2 := newInstaller(t, fm, func(o *Options) { o.DryRun = true })
	inst2.PrefetchArtifacts(ctx, []Target{{Tool: "node", Spec: "22.22.2"}, {Tool: "node", Spec: "24.1.0"}})
	if es, _ := os.ReadDir(rootOf(t, inst2).CacheArchives()); len(es) != 0 {
		t.Fatalf("dry-run 不该预取: %v", es)
	}
	inst3 := newInstaller(t, fm, nil)
	inst3.PrefetchArtifacts(ctx, []Target{{Tool: "node", Spec: "22.22.2"}})
	if es, _ := os.ReadDir(rootOf(t, inst3).CacheArchives()); len(es) != 0 {
		t.Fatalf("单目标不该预取（装的时候自己下）: %v", es)
	}
}
