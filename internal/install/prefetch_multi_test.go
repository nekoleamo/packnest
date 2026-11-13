package install

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/provider"
)

// 多工具索引预取：`pk install a b c` / `pk upgrade` 之前先把索引并发拉回来，
// 别让 N 次索引延迟串成加法（慢速网络上这就是主要耗时）。
func TestPrefetchIndexesParallel(t *testing.T) {
	var inflight, peak int32
	slow := func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(120 * time.Millisecond) // 每个索引都慢（模拟国内到官方源）
		atomic.AddInt32(&inflight, -1)
		_, _ = io.WriteString(w, `[{"version":"1.0.0"}]`)
	}
	srvA := httptest.NewServer(http.HandlerFunc(slow))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(slow))
	defer srvB.Close()

	pa := &provider.Provider{ID: "aaa", Index: provider.IndexBlock{Kind: provider.IndexJSON, URLs: map[string]string{"cn": srvA.URL}}}
	pb := &provider.Provider{ID: "bbb", Index: provider.IndexBlock{Kind: provider.IndexJSON, URLs: map[string]string{"cn": srvB.URL}}}
	inst := newInstaller(t, nil, func(o *Options) {
		o.Profile = "cn"
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "bbb" {
				return pb, nil
			}
			return pa, nil
		}
	})
	ctx := context.Background()

	start := time.Now()
	inst.PrefetchIndexes(ctx, []string{"aaa", "bbb"})
	for _, p := range []*provider.Provider{pa, pb} {
		if _, err := inst.loadOrTakeIndex(ctx, p); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
	}
	elapsed := time.Since(start)
	// 串行要 240ms；并行应该贴着单个的 120ms（放宽到 200ms 容忍调度抖动）
	if elapsed > 200*time.Millisecond {
		t.Fatalf("两个索引应并行拉取，实际耗时 %s", elapsed)
	}
	if got := atomic.LoadInt32(&peak); got < 2 {
		t.Fatalf("最大并发应 ≥2，实际 %d", got)
	}
}
