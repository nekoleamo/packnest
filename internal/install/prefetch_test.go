package install

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/packnest/packnest/internal/provider"
)

// 伴生索引预取：解压是纯 CPU 的秒级工作，期间先把 npm 索引拉回来；
// 取用时必须复用这一次结果（不能再发一遍请求）。
func TestCompanionIndexPrefetchedOnce(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, `[{"version":"11.20.0"},{"version":"12.2.2"}]`)
	}))
	defer srv.Close()

	npmP := &provider.Provider{ID: "npm", Index: provider.IndexBlock{
		Kind: provider.IndexJSON, URLs: map[string]string{"cn": srv.URL + "/index.json"},
	}}
	nodeP := &provider.Provider{ID: "node", Companions: map[string]provider.Companion{
		"npm": {Mode: "latest-compatible", Registry: "npm", Target: "lib/node_modules/npm"},
	}}
	inst := newInstaller(t, nil, func(o *Options) {
		o.Profile = "cn"
		o.LookupProvider = func(id string) (*provider.Provider, error) {
			if id == "npm" {
				return npmP, nil
			}
			return nodeP, nil
		}
	})
	ctx := context.Background()

	inst.prefetchCompanionIndex(ctx, nodeP)
	idx, err := inst.loadOrTakeIndex(ctx, npmP)
	if err != nil {
		t.Fatalf("取预取结果: %v", err)
	}
	if idx == nil || len(idx.Versions) < 2 {
		t.Fatalf("索引异常: %+v", idx)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("预取应只发一次请求，实际 %d 次", got)
	}

	// 预取只能被消费一次：再取会走正常路径（这里命中 24h 索引缓存，所以仍然只有 1 次请求）
	if _, err := inst.loadOrTakeIndex(ctx, npmP); err != nil {
		t.Fatalf("二次加载: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("索引只该拉一次，实际 %d 次", got)
	}
	// 预取槽已被消费（不会无限复用同一个结果）
	if _, ok := inst.prefetched[npmP.ID]; ok {
		t.Fatal("预取结果消费后应清出槽位")
	}

	// dry-run 不预取（预览不额外发请求）
	hits = 0
	dry := newInstaller(t, nil, func(o *Options) { o.Profile = "cn"; o.DryRun = true })
	dry.opt.LookupProvider = inst.opt.LookupProvider
	dry.prefetchCompanionIndex(ctx, nodeP)
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("dry-run 不该预取，实际请求 %d 次", got)
	}
}
