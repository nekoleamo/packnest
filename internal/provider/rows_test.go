package provider

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
)

// AvailableRows：一次问所有工具 → 每个工具一行（最新、LTS、近 5 个、失败只记自己）。
func TestAvailableRows(t *testing.T) {
	index := []byte(`[{"version":"v21.0.0","lts":false},{"version":"v22.22.2","lts":"Jod"},
	  {"version":"v23.0.0","lts":false},{"version":"v24.14.0","lts":"Krypton"},
	  {"version":"v25.0.0","lts":false},{"version":"v26.1.0","lts":false}]`)
	srv := localMirror(t, map[string][]byte{"/dist/index.json": index})
	node := &Provider{
		ID: "node", Source: SourceEmbedded, Requires: []string{},
		Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": srv.URL + "/dist/index.json"}},
	}
	bad := &Provider{
		ID: "bun", Source: SourceEmbedded, Requires: []string{"node"},
		Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": srv.URL + "/nope.json"}},
	}
	loader := &IndexLoader{Client: fetch.New(), CacheDir: filepath.Join(t.TempDir(), "meta"), TTL: time.Hour}
	rows := AvailableRows(context.Background(), loader, []*Provider{node, bad}, "cn")
	if len(rows) != 2 {
		t.Fatalf("应有 2 行，得到 %d", len(rows))
	}
	if rows[0].Name != "node" || rows[0].Latest != "26.1.0" || rows[0].LTS != "24.14.0" {
		t.Fatalf("node 行异常: %+v", rows[0])
	}
	if len(rows[0].Recent) != 5 || rows[0].Recent[0] != "26.1.0" {
		t.Fatalf("Recent 应是降序的 5 个: %v", rows[0].Recent)
	}
	if rows[0].Err != "" || rows[0].Source != SourceEmbedded || rows[0].Requires == nil {
		t.Fatalf("成功行不该有 Err，且 Requires 要序列化成 []: %+v", rows[0])
	}
	// 失败只影响自己那一行：其余工具照常显示
	if rows[1].Err == "" || rows[1].Latest != "" || rows[1].Requires[0] != "node" {
		t.Fatalf("失败行异常: %+v", rows[1])
	}

	// nil → 现取内嵌清单；离线且无缓存时也只给一行错误，不 panic
	offline := &IndexLoader{Client: &fetch.Client{Offline: true}, CacheDir: t.TempDir()}
	if got := AvailableRows(context.Background(), offline, nil, "cn"); len(got) == 0 {
		t.Fatal("nil 清单也应给出结果（内嵌 + 每个工具的 Err）")
	}
}

// Requires / SourceOf 是 nil 安全的取值：外部工具取不到清单时也要有稳定输出。
func TestRequiresAndSourceNilSafe(t *testing.T) {
	if got := Requires(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil 清单应给空切片，得到 %#v", got)
	}
	if got := SourceOf(nil); got != "" {
		t.Fatalf("nil 清单应给空来源，得到 %q", got)
	}
	if got := Requires(&Provider{}); len(got) != 0 {
		t.Fatalf("没有 requires 应给空切片，得到 %v", got)
	}
	p := &Provider{Source: SourceExternal, Requires: []string{"node"}}
	if got := Requires(p); len(got) != 1 || got[0] != "node" {
		t.Fatalf("Requires = %v", got)
	}
	if got := SourceOf(p); got != SourceExternal {
		t.Fatalf("SourceOf = %q", got)
	}
}
