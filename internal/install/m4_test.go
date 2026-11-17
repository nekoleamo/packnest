package install

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/store"
)

// TestRecordCacheTouchConcurrent：并行 worker 会同时命中缓存，
// cache.json 的读-改-写必须串起来（否则各写各的，丢一条 LRU 记录，
// `pk gc` 可能把刚用过的归档当冷数据删掉）。
func TestRecordCacheTouchConcurrent(t *testing.T) {
	inst := newInstaller(t, nil, nil)
	archives := inst.opt.Paths.CacheArchives()
	if err := os.MkdirAll(archives, 0o755); err != nil {
		t.Fatal(err)
	}
	names := []string{"a.zip", "b.zip", "c.zip", "d.zip"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(archives, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(1700000000, 0)
	var wg sync.WaitGroup
	for round := 0; round < 5; round++ {
		for _, n := range names {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				inst.recordCacheTouch(n, now)
			}(n)
		}
	}
	wg.Wait()

	idx, err := store.LoadCacheIndex(inst.opt.Paths.CacheIndex())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if _, ok := idx.Entries[n]; !ok {
			t.Fatalf("缓存记录 %s 丢了（读-改-写没串起来）: %+v", n, idx)
		}
	}
}
