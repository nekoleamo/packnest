// pbs 索引（python-build-standalone 两级目录列举）的端到端用例。
//
// 放在 acceptance 而不是 provider 里：它们要用 internal/testkit 起的假镜像，
// 而 testkit 依赖 provider（测试期成环），所以走真实的 IndexLoader.Load 入口。
package acceptance

import (
	"context"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/testkit"
)

func TestLoadPBSIndexEndToEnd(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261020", "20261026"},
		Versions: []string{"3.12.16", "3.13.18"},
	})
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	idx, err := loader.Load(context.Background(), fp.Provider, "cn")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(idx.Versions) != 2 {
		t.Fatalf("应有 2 个 cpython 版本: %+v", idx.Versions)
	}
	for _, v := range idx.Versions {
		if v.Tag != "20261026" {
			t.Fatalf("应采用最新标签 20261026: %+v", v)
		}
	}
	resolved, err := idx.Resolve(spec.MustParse("3.13"))
	if err != nil || resolved.Version != "3.13.18" {
		t.Fatalf("解析 3.13 → %+v/%v", resolved, err)
	}
	urls, err := fp.Provider.ArtifactURLsFor(resolved, "darwin", "arm64", "cn")
	if err != nil || !strings.Contains(urls[0], "/20261026/cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz") {
		t.Fatalf("产物 URL 错误: %v %v", urls, err)
	}
}

func TestLoadPBSIndexFallbacks(t *testing.T) {
	// 根目录列举失败（死地址）→ 报错并带出原因
	dead := testkit.Mirror(t, map[string][]byte{}, testkit.Status(500))
	p, err := provider.Parse([]byte(testkit.PBSProviderTOML(dead.URL)))
	if err != nil {
		t.Fatal(err)
	}
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	if _, err := loader.Load(context.Background(), p, "cn"); err == nil {
		t.Fatal("根列举全挂应报错")
	} else if !strings.Contains(err.Error(), "标签") {
		t.Fatalf("错误应说明标签列表拉不到: %v", err)
	}

	// 根列举正常但标签目录全 404 → 报错
	files := map[string][]byte{
		"/pbs/": []byte(`[{"name":"20261026/","type":"dir"}]`),
	}
	broken := testkit.Mirror(t, files)
	p2, err := provider.Parse([]byte(testkit.PBSProviderTOML(broken.URL)))
	if err != nil {
		t.Fatal(err)
	}
	loader2 := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	if _, err := loader2.Load(context.Background(), p2, "cn"); err == nil {
		t.Fatal("标签目录缺失应报错")
	}
}

func TestLoadPBSIndexSkipsEmptyNewestTag(t *testing.T) {
	// 最新标签没有 install_only 产物（还没同步完）→ 退到上一个有产物的标签。
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{Tags: []string{"20261020"}, Versions: []string{"3.13.18"}})
	// 手工追加一个空的更新标签
	fp.Files["/pbs/"] = []byte(`[{"name":"20261026/","type":"dir"},{"name":"20261020/","type":"dir"}]`)
	fp.Files["/pbs/20261026/"] = []byte(`[{"name":"SHA256SUMS","type":"file"}]`)
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	idx, err := loader.Load(context.Background(), fp.Provider, "cn")
	if err != nil {
		t.Fatalf("应回退到有产物的标签: %v", err)
	}
	if len(idx.Versions) == 0 || idx.Versions[0].Tag != "20261020" {
		t.Fatalf("应使用 20261020 标签: %+v", idx.Versions)
	}
}

func TestLoadPBSIndexCachesTagListing(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{Tags: []string{"20261026"}, Versions: []string{"3.13.18"}})
	cacheDir := t.TempDir()
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: cacheDir}
	if _, err := loader.Load(context.Background(), fp.Provider, "cn"); err != nil {
		t.Fatal(err)
	}
	// 缓存落盘后，断网也能再解析
	offline := fetch.New()
	offline.Offline = true
	loader2 := &provider.IndexLoader{Client: offline, CacheDir: cacheDir}
	if _, err := loader2.Load(context.Background(), fp.Provider, "cn"); err != nil {
		t.Fatalf("缓存应可离线读: %v", err)
	}
}
