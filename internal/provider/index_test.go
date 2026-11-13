package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/spec"
)

// 本文件不引 internal/testkit：testkit 依赖本包，若这里反向 import 会形成测试期环。

// localNpmRegistry 造最小的 npm registry 元数据（version → engines.node）。
func localNpmRegistry(versions map[string]string) []byte {
	doc := map[string]any{"name": "npm", "versions": map[string]any{}}
	vs := doc["versions"].(map[string]any)
	for v, engines := range versions {
		rec := map[string]any{"version": v, "dist": map[string]any{"tarball": "npm-" + v + ".tgz"}}
		if engines != "" {
			rec["engines"] = map[string]any{"node": engines}
		}
		vs[v] = rec
	}
	data, _ := json.Marshal(doc)
	return data
}

// localMirror 起一个只按路径返回静态内容的小服务。
func localMirror(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestParseJSONIndexNode(t *testing.T) {
	data := []byte(`[
	  {"version":"v22.22.2","lts":"Jod","date":"2026-09-01"},
	  {"version":"v23.1.0","lts":false,"date":"2026-10-01"},
	  {"version":"v22.23.0-rc.1","lts":false}
	]`)
	vs, err := parseIndex(IndexJSON, data)
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("版本数 = %d", len(vs))
	}
	if vs[0].Version != "22.22.2" || vs[0].LTS != "Jod" {
		t.Fatalf("应去掉 v 前缀并保留 lts: %+v", vs[0])
	}
	if vs[1].LTS != "" {
		t.Fatalf("lts=false 应解析为空: %+v", vs[1])
	}
}

func TestParseJSONIndexLTSBool(t *testing.T) {
	vs, err := parseIndex(IndexJSON, []byte(`[{"version":"v20.18.1","lts":true}]`))
	if err != nil || len(vs) != 1 || vs[0].LTS != "lts" {
		t.Fatalf("lts:true 应解析为 lts: %+v %v", vs, err)
	}
}

func TestParseJSONIndexErrors(t *testing.T) {
	for _, bad := range []string{`{}`, `[]`, `not json`} {
		if _, err := parseIndex(IndexJSON, []byte(bad)); err == nil {
			t.Errorf("parseIndex(%q) 应报错", bad)
		}
	}
}

func TestParseRegistryIndex(t *testing.T) {
	vs, err := parseIndex(IndexRegistry, localNpmRegistry(map[string]string{
		"11.21.2":        "^20.17.0 || >=22.9.0",
		"12.2.2":         "^22.22.2 || ^24.15.0 || >=26.0.0",
		"12.0.0-pre.0.0": "^20.17.0 || >=22.9.0",
	}))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("版本数 = %d", len(vs))
	}
	// 稳定版按降序在前，预发布垫底。
	if vs[0].Version != "12.2.2" || vs[1].Version != "11.21.2" {
		t.Fatalf("排序异常: %+v", vs[:2])
	}
	if vs[2].Version != "12.0.0-pre.0.0" {
		t.Fatalf("预发布应垫底: %+v", vs[2])
	}
	if vs[0].Engines["node"] == "" {
		t.Fatal("应保留 engines")
	}
}

func TestParseDirList(t *testing.T) {
	// 真实形状（npmmirror /-/binary/bun/）：
	data := []byte(`[
	  {"name":"bun-v0.0.19/","type":"dir","date":"2021-09-17T23:16:44Z"},
	  {"name":"bun-v1.4.4/","type":"dir","date":"2026-10-22T08:58:37Z"},
	  {"name":"canary/","type":"dir"},
	  {"name":"SHASUMS256.txt","type":"file"}
	]`)
	vs, err := parseIndex(IndexDirList, data)
	if err != nil {
		t.Fatalf("parseDirList: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("应只取版本目录，得到 %+v", vs)
	}
	if vs[0].Version != "0.0.19" || vs[1].Version != "1.4.4" {
		t.Fatalf("版本提取错误: %+v", vs)
	}
	if vs[1].Date == "" {
		t.Fatal("日期应保留")
	}
	if _, err := parseIndex(IndexDirList, []byte("x")); err == nil {
		t.Fatal("非 JSON 应报错")
	}
	if _, err := parseIndex(IndexDirList, []byte(`[{"name":"canary/","type":"dir"}]`)); err == nil {
		t.Fatal("没有版本目录时应报错")
	}
	// 没有 type 字段时按结尾斜杠判断
	vs, err = parseIndex(IndexDirList, []byte(`[{"name":"v2.9.8/"},{"name":"not-a-version"}]`))
	if err != nil || len(vs) != 1 || vs[0].Version != "2.9.8" {
		t.Fatalf("无 type 的目录列举解析失败: %+v %v", vs, err)
	}
}

func TestParseGitHubReleases(t *testing.T) {
	data := []byte(`[
	  {"tag_name":"bun-v1.4.4","published_at":"2026-10-22T08:00:00Z"},
	  {"tag_name":"bun-v1.4.3","prerelease":false},
	  {"tag_name":"canary","prerelease":true},
	  {"tag_name":"bun-v1.4.3"}
	]`)
	vs, err := parseIndex(IndexGitHub, data)
	if err != nil {
		t.Fatalf("parseGitHubReleases: %v", err)
	}
	if len(vs) != 2 || vs[0].Version != "1.4.4" || vs[1].Version != "1.4.3" {
		t.Fatalf("tag_name 解析/去重错误: %+v", vs)
	}
	if _, err := parseIndex(IndexGitHub, []byte(`{}`)); err == nil {
		t.Fatal("非数组应报错")
	}
	if _, err := parseIndex("soap", nil); err == nil {
		t.Fatal("未知形态应报错")
	}
}

func TestExtractVersion(t *testing.T) {
	cases := map[string]string{
		"bun-v1.4.4/":                       "1.4.4",
		"v2.9.8/":                           "2.9.8",
		"node-v22.22.2-darwin-arm64.tar.gz": "22.22.2",
		"v2.9.8-rc.1/":                      "2.9.8-rc.1",
		"canary/":                           "",
		"":                                  "",
	}
	for in, want := range cases {
		if got := extractVersion(in); got != want {
			t.Errorf("extractVersion(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestIndexResolve(t *testing.T) {
	idx := &Index{Provider: &Provider{ID: "node"}, Versions: []Version{
		{Version: "22.21.1"},
		{Version: "22.22.2"},
		{Version: "24.14.0", LTS: "Krypton"},
		{Version: "26.13.0"},
	}}
	v, err := idx.Resolve(spec.MustParse("22"))
	if err != nil || v.Version != "22.22.2" {
		t.Fatalf("22 → %q/%v", v.Version, err)
	}
	v, err = idx.Resolve(spec.MustParse("latest"))
	if err != nil || v.Version != "26.13.0" {
		t.Fatalf("latest → %q/%v", v.Version, err)
	}
	v, err = idx.Resolve(spec.MustParse("lts"))
	if err != nil || v.Version != "24.14.0" {
		t.Fatalf("lts → %q/%v", v.Version, err)
	}
	if _, err := idx.Resolve(spec.MustParse("14")); err == nil {
		t.Fatal("没有的版本应报错")
	} else if !contains(err.Error(), "26.13.0") || !contains(err.Error(), "pk mirror") {
		t.Fatalf("错误信息应给出最新版本与换档建议: %v", err)
	}
}

func TestIndexResolveStaleHint(t *testing.T) {
	idx := &Index{Provider: &Provider{ID: "node"}, Stale: true, Versions: []Version{{Version: "22.21.1"}}}
	_, err := idx.Resolve(spec.MustParse("22.22.2"))
	if err == nil || !contains(err.Error(), "过期缓存") {
		t.Fatalf("过期缓存要提示: %v", err)
	}
}

func TestIndexResolveEmpty(t *testing.T) {
	idx := &Index{Provider: &Provider{ID: "node"}}
	if _, err := idx.Resolve(spec.MustParse("22")); err == nil {
		t.Fatal("空索引应报错")
	}
	idx.Versions = []Version{{Version: "22.0.0-rc.1"}}
	if _, err := idx.Resolve(spec.MustParse("latest")); err == nil {
		t.Fatal("只有预发布时应报错")
	}
}

func TestIndexLatestAndFind(t *testing.T) {
	idx := &Index{Provider: &Provider{ID: "node"}, Versions: []Version{{Version: "22.22.2"}, {Version: "24.14.0"}}}
	if v, ok := idx.Latest(); !ok || v.Version != "24.14.0" {
		t.Fatalf("Latest = %+v/%v", v, ok)
	}
	if v, ok := idx.Find("v22.22.2"); !ok || v.Version != "22.22.2" {
		t.Fatalf("Find 应容忍 v 前缀: %+v/%v", v, ok)
	}
	if _, ok := idx.Find("1.0.0"); ok {
		t.Fatal("不存在的版本应返回 false")
	}
	if got := idx.mustFind("9.9.9"); got.Version != "9.9.9" {
		t.Fatalf("mustFind 兜底应构造版本: %+v", got)
	}
}

func TestIndexLoaderNode(t *testing.T) {
	index := []byte(`[{"version":"v22.22.2","lts":"Jod"},{"version":"v24.14.0","lts":"Krypton"}]`)
	srv := localMirror(t, map[string][]byte{"/dist/index.json": index})

	p := &Provider{
		ID: "node",
		Index: IndexBlock{
			Kind: IndexJSON,
			URLs: map[string]string{
				"default": srv.URL + "/offline/index.json",
				"cn":      srv.URL + "/dist/index.json",
			},
		},
	}
	loader := &IndexLoader{
		Client:   fetch.New(),
		CacheDir: filepath.Join(t.TempDir(), "meta"),
		TTL:      time.Hour,
	}
	idx, err := loader.Load(context.Background(), p, "cn")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(idx.Versions) != 2 || idx.Stale || idx.Source == "" {
		t.Fatalf("索引异常: %+v", idx)
	}

	// 第二次：命中缓存，零网络也读得到。
	offline := fetch.New()
	offline.Offline = true
	loader.Client = offline
	idx2, err := loader.Load(context.Background(), p, "cn")
	if err != nil {
		t.Fatalf("缓存应可离线读: %v", err)
	}
	if len(idx2.Versions) != 2 {
		t.Fatalf("缓存索引异常: %+v", idx2)
	}
}

func TestIndexLoaderAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	p := &Provider{ID: "node", Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": srv.URL + "/x"}}}
	loader := &IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	_, err := loader.Load(context.Background(), p, "cn")
	if err == nil || !contains(err.Error(), "版本索引全部不可用") {
		t.Fatalf("全失败应报错并给建议: %v", err)
	}

	none := &Provider{ID: "node"}
	if _, err := loader.Load(context.Background(), none, "cn"); err == nil {
		t.Fatal("没有索引源应报错")
	}
}

func TestSortVersionsDesc(t *testing.T) {
	out := SortVersionsDesc([]Version{{Version: "22.21.1"}, {Version: "22.22.2"}, {Version: "24.14.0"}})
	if len(out) != 3 || out[0] != "24.14.0" || out[2] != "22.21.1" {
		t.Fatalf("SortVersionsDesc = %v", out)
	}
}

func TestFileFor(t *testing.T) {
	v := Version{Files: []IndexFile{
		{Filename: "go1.23.4.linux-amd64.tar.gz", OS: "linux", Arch: "amd64", Kind: "archive"},
		{Filename: "go1.23.4.src.tar.gz", OS: "linux", Arch: "amd64", Kind: "source"},
	}}
	f, ok := v.FileFor("linux", "amd64")
	if !ok || f.Filename != "go1.23.4.linux-amd64.tar.gz" {
		t.Fatalf("FileFor = %+v/%v", f, ok)
	}
	if _, ok := v.FileFor("darwin", "arm64"); ok {
		t.Fatal("没有的平台应返回 false")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestParseJSONIndexFilesShapes(t *testing.T) {
	// node 的形状：files 是字符串数组
	node := []byte(`[{"version":"v22.22.2","files":["linux-x64","headers","win-x64-zip"]}]`)
	vs, err := parseIndex(IndexJSON, node)
	if err != nil || len(vs) != 1 || len(vs[0].Files) != 3 {
		t.Fatalf("node 形状解析失败: %+v %v", vs, err)
	}
	if vs[0].Files[0].Filename != "linux-x64" {
		t.Fatalf("文件名未保留: %+v", vs[0].Files[0])
	}
	// go 的形状：files 是对象数组
	golang := []byte(`[{"version":"go1.23.4","files":[{"filename":"go1.23.4.linux-amd64.tar.gz","os":"linux","arch":"amd64","sha256":"abc","kind":"archive"}]}]`)
	vs, err = parseIndex(IndexJSON, golang)
	if err != nil || len(vs) != 1 {
		t.Fatalf("go 形状解析失败: %v", err)
	}
	f, ok := vs[0].FileFor("linux", "amd64")
	if !ok || f.SHA256 != "abc" {
		t.Fatalf("go 的 files 应保留 sha256: %+v", f)
	}
	// 其它形状（数字/对象）不该 panic
	if got := parseFiles(json.RawMessage(`123`)); got != nil {
		t.Fatalf("非法 files 应返回 nil: %+v", got)
	}
	if got := parseFiles(nil); got != nil {
		t.Fatalf("空 files 应返回 nil: %+v", got)
	}
}

func TestParseRegistryIndexEngineObjects(t *testing.T) {
	// pnpm 11 的 engines.runtime 是对象，不能因此整份索引解析失败。
	data := []byte(`{"versions":{
	  "11.0.0-alpha.0":{"version":"11.0.0-alpha.0","engines":{"node":">=20","runtime":{"foo":"bar"}}},
	  "10.4.1":{"version":"10.4.1","engines":{"node":">=18.12"}}
	}}`)
	vs, err := parseIndex(IndexRegistry, data)
	if err != nil {
		t.Fatalf("对象型 engines 不该让索引失败: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("版本数 = %d", len(vs))
	}
	for _, v := range vs {
		if v.Engines["node"] == "" {
			t.Fatalf("node engines 应被保留: %+v", v)
		}
		if _, ok := v.Engines["runtime"]; ok {
			t.Fatalf("对象型字段应被丢弃: %+v", v.Engines)
		}
	}
}

// TestLoadAllConcurrent：6 个工具的索引要并发拉（串行时总耗时是"所有工具之和"）。
// 用慢服务 + 最大并发计数断言，不看墙钟，避免 CI 抖动。
func TestLoadAllConcurrent(t *testing.T) {
	index := []byte(`[{"version":"v22.22.2","lts":"Jod"}]`)
	var cur, maxSeen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&cur, 1)
		for {
			old := atomic.LoadInt32(&maxSeen)
			if n <= old || atomic.CompareAndSwapInt32(&maxSeen, old, n) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(index)
	}))
	defer srv.Close()

	ps := make([]*Provider, 6)
	for i := range ps {
		ps[i] = &Provider{
			ID:    fmt.Sprintf("tool%d", i),
			Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": srv.URL + "/index.json"}},
		}
	}
	loader := &IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), TTL: time.Hour}
	start := time.Now()
	res := loader.LoadAll(context.Background(), ps, "cn")
	if len(res) != len(ps) {
		t.Fatalf("结果数不对: %d", len(res))
	}
	for i, r := range res {
		if r.Err != nil || r.Index == nil {
			t.Fatalf("第 %d 项失败: %v", i, r.Err)
		}
		if r.Provider != ps[i] {
			t.Fatalf("结果顺序必须与入参一致（第 %d 项）", i)
		}
	}
	if got := atomic.LoadInt32(&maxSeen); got < 2 {
		t.Fatalf("应并发拉取，实际最大并发 %d", got)
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Fatalf("6×80ms 并发应在 400ms 内完成，实际 %v", d)
	}

	// 单个工具失败不影响其他（只写在它自己的 Err 上）
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer dead.Close()
	badP := &Provider{ID: "bad", Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": dead.URL + "/a"}}}
	mixed := loader.LoadAll(context.Background(), []*Provider{badP, ps[0]}, "cn")
	if mixed[0].Err == nil || mixed[1].Err != nil {
		t.Fatalf("一个失败不该影响其他: %v / %v", mixed[0].Err, mixed[1].Err)
	}
}

// 轻量索引（stable_urls）：只有声明了它的工具才另走一条 URL、另存一份缓存，
// 完整索引（`pk list <tool>` / 按精确版本装）不受影响。
func TestStableIndexView(t *testing.T) {
	full := []byte(`[{"version":"v1.26.0"},{"version":"v1.27.1"},{"version":"v1.27.4"}]`)
	stable := []byte(`[{"version":"v1.27.4"}]`)
	srv := localMirror(t, map[string][]byte{
		"/all/index.json":    full,
		"/stable/index.json": stable,
	})
	p := &Provider{
		ID: "go",
		Index: IndexBlock{
			Kind:       IndexJSON,
			URLs:       map[string]string{"cn": srv.URL + "/all/index.json"},
			StableURLs: map[string]string{"cn": srv.URL + "/stable/index.json"},
		},
	}
	if got := p.StableIndexURLs("cn"); len(got) != 1 || got[0] != srv.URL+"/stable/index.json" {
		t.Fatalf("StableIndexURLs = %v", got)
	}
	// 没声明 stable_urls → 退回完整索引（老清单必须照常工作）
	bare := &Provider{ID: "node", Index: IndexBlock{Kind: IndexJSON, URLs: map[string]string{"cn": srv.URL + "/all/index.json"}}}
	if got := bare.StableIndexURLs("cn"); len(got) != 1 || got[0] != bare.IndexURLs("cn")[0] {
		t.Fatalf("没有 stable_urls 应退回完整索引，得到 %v", got)
	}

	cache := t.TempDir()
	fullIdx, err := (&IndexLoader{Client: fetch.New(), CacheDir: cache, TTL: time.Hour}).Load(context.Background(), p, "cn")
	if err != nil {
		t.Fatalf("完整索引: %v", err)
	}
	if len(fullIdx.Versions) != 3 {
		t.Fatalf("完整索引应有 3 个版本: %+v", fullIdx.Versions)
	}
	stableIdx, err := (&IndexLoader{Client: fetch.New(), CacheDir: cache, TTL: time.Hour, Stable: true}).Load(context.Background(), p, "cn")
	if err != nil {
		t.Fatalf("轻量索引: %v", err)
	}
	if len(stableIdx.Versions) != 1 || stableIdx.Versions[0].Version != "1.27.4" {
		t.Fatalf("轻量索引应只含最新稳定版，得到 %+v", stableIdx.Versions)
	}
	// 两份缓存互不覆盖
	for _, name := range []string{"go.json", "go.stable.json"} {
		if _, err := os.Stat(filepath.Join(cache, name)); err != nil {
			t.Fatalf("缺少缓存 %s: %v", name, err)
		}
	}
	// 没声明 stable_urls 的工具不该平白多存一份
	if _, err := (&IndexLoader{Client: fetch.New(), CacheDir: cache, TTL: time.Hour, Stable: true}).Load(context.Background(), bare, "cn"); err != nil {
		t.Fatalf("bare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "node.stable.json")); !os.IsNotExist(err) {
		t.Fatal("没有 stable_urls 时不该另存轻量缓存")
	}
}
