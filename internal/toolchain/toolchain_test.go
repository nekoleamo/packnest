package toolchain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/testkit"
)

func TestCheckGoEnv(t *testing.T) {
	// 没设 → 告警并给处置
	msg := CheckGoEnv(func(string) string { return "" })
	if !strings.Contains(msg, "GOTOOLCHAIN") || !strings.Contains(msg, "pk env") {
		t.Fatalf("应告警并给建议: %q", msg)
	}
	if msg := CheckGoEnv(func(k string) string { return "local" }); msg != "" {
		t.Fatalf("local 时不该告警: %q", msg)
	}
	if msg := CheckGoEnv(func(k string) string { return "auto" }); msg == "" {
		t.Fatal("auto 也应告警（会多下一次工具链）")
	}
	if CheckGoEnv(nil) != "" {
		t.Fatal("nil lookup 应安全返回空")
	}
	if GoToolchainEnv != "GOTOOLCHAIN=local" {
		t.Fatalf("常量必须与方案一致: %q", GoToolchainEnv)
	}
}

func TestVersionDirs(t *testing.T) {
	data := []byte(`[
	  {"name":"20181218/","type":"dir"},
	  {"name":"20261026/","type":"dir"},
	  {"name":"20261020/","type":"dir"},
	  {"name":"README.md","type":"file"}
	]`)
	tags, err := versionDirs(data)
	if err != nil {
		t.Fatalf("versionDirs: %v", err)
	}
	if len(tags) != 3 || tags[0] != "20261026" {
		t.Fatalf("应按日期降序，得到 %v", tags)
	}
	if _, err := versionDirs([]byte(`[]`)); err == nil {
		t.Fatal("没有标签目录应报错")
	}
	if _, err := versionDirs([]byte(`{}`)); err == nil {
		t.Fatal("非数组应报错")
	}
}

func TestParseFiles(t *testing.T) {
	names := []struct{ name string }{}
	for _, n := range []string{
		"cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz",
		"cpython-3.13.18+20261026-aarch64-apple-darwin-install_only_stripped.tar.gz",
		"cpython-3.13.18+20261026-aarch64-apple-darwin-debug-full.tar.zst",
		"cpython-3.12.16+20261026-aarch64-apple-darwin-install_only.tar.gz",
		"cpython-3.13.18+20261026-riscv64-unknown-linux-gnu-install_only.tar.gz",
		"SHA256SUMS",
	} {
		names = append(names, struct{ name string }{n})
	}
	body, err := json.Marshal(func() []map[string]string {
		out := make([]map[string]string, 0, len(names))
		for _, n := range names {
			out = append(out, map[string]string{"name": n.name, "type": "file"})
		}
		return out
	}())
	if err != nil {
		t.Fatal(err)
	}
	platforms := map[string]string{
		"aarch64-apple-darwin":     "darwin-arm64",
		"x86_64-unknown-linux-gnu": "linux-amd64",
	}
	files, err := parseFiles(body, "20261026", platforms)
	if err != nil {
		t.Fatalf("parseFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应只留 install_only（非 stripped、声明过的平台），得到 %+v", files)
	}
	for _, f := range files {
		if f.Tag != "20261026" {
			t.Fatalf("tag 应带上: %+v", f)
		}
		if strings.Contains(f.Filename, "stripped") {
			t.Fatalf("stripped 变体不该入选: %+v", f)
		}
	}
}

func TestGroupPBS(t *testing.T) {
	platforms := map[string]string{
		"aarch64-apple-darwin":     "darwin-arm64",
		"x86_64-unknown-linux-gnu": "linux-amd64",
	}
	files := []pbsFile{
		{Version: "3.13.18", Tag: "20261026", Platform: "aarch64-apple-darwin", Filename: "a.gz"},
		{Version: "3.13.18", Tag: "20261026", Platform: "x86_64-unknown-linux-gnu", Filename: "b.gz"},
		{Version: "3.12.16", Tag: "20261026", Platform: "aarch64-apple-darwin", Filename: "c.gz"},
	}
	versions, err := groupPBS(files, platforms, "20261026")
	if err != nil {
		t.Fatalf("groupPBS: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("应聚合成 2 个版本: %+v", versions)
	}
	if len(versions[0].Files) != 2 {
		t.Fatalf("3.13.18 应有两个平台产物: %+v", versions[0])
	}
	if f, ok := versions[0].FileFor("linux", "amd64"); !ok || f.Filename != "b.gz" {
		t.Fatalf("FileFor 应能按平台取到文件名: %+v", f)
	}
	if versions[0].Tag != "20261026" {
		t.Fatalf("版本应带 tag: %+v", versions[0])
	}
	if _, err := groupPBS(nil, platforms, "x"); err == nil {
		t.Fatal("空文件列表应报错")
	}
}

// TestLoadPBSIndexEndToEnd 走假镜像，验证两级列举 + 新标签优先。
func TestLoadPBSIndexEndToEnd(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261020", "20261026"},
		Versions: []string{"3.12.16", "3.13.18"},
	})
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), Custom: LoadPBSIndex}
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

func TestLoadPBSIndexNoCustom(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{Tags: []string{"20261026"}, Versions: []string{"3.13.18"}})
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir()}
	if _, err := loader.Load(context.Background(), fp.Provider, "cn"); err == nil {
		t.Fatal("没有 pbs 钩子时应报错（内部错误）")
	}
}

func TestSplitPlatform(t *testing.T) {
	os, arch := splitPlatform("darwin-arm64")
	if os != "darwin" || arch != "arm64" {
		t.Fatalf("splitPlatform = %q/%q", os, arch)
	}
	if os, arch := splitPlatform("nohyphen"); os != "nohyphen" || arch != "" {
		t.Fatalf("无连字符时应整串作为 os: %q/%q", os, arch)
	}
}

func TestIsTag(t *testing.T) {
	for _, ok := range []string{"20261026", "20181218"} {
		if !isTag(ok) {
			t.Errorf("%q 应是合法标签", ok)
		}
	}
	for _, bad := range []string{"2026102", "202610266", "canary", "2026-10-26", ""} {
		if isTag(bad) {
			t.Errorf("%q 不该是合法标签", bad)
		}
	}
}

func TestVersionDirsSkipsFilesAndJunk(t *testing.T) {
	data := []byte(`[
	  {"name":"20261026","type":"dir"},
	  {"name":"README.md","type":"file"},
	  {"name":"canary/","type":"dir"},
	  {"name":"20261020/"}
	]`)
	tags, err := versionDirs(data)
	if err != nil {
		t.Fatalf("versionDirs: %v", err)
	}
	if len(tags) != 2 || tags[0] != "20261026" || tags[1] != "20261020" {
		t.Fatalf("应只留 8 位日期目录: %v", tags)
	}
}

func TestParseFilesErrors(t *testing.T) {
	platforms := map[string]string{"aarch64-apple-darwin": "darwin-arm64"}
	if _, err := parseFiles([]byte(`{}`), "20261026", platforms); err == nil {
		t.Fatal("非数组应报错")
	}
	// 只有未声明平台/变体 → 报错
	body := []byte(`[{"name":"cpython-3.13.18+20261026-riscv64-unknown-linux-gnu-install_only.tar.gz","type":"file"}]`)
	if _, err := parseFiles(body, "20261026", platforms); err == nil {
		t.Fatal("没有匹配平台时应报错")
	}
}

func TestLoadPBSIndexFallbacks(t *testing.T) {
	// 根目录列举失败（死地址）→ 报错并带出原因
	dead := testkit.Mirror(t, map[string][]byte{}, testkit.Status(500))
	p, err := provider.Parse([]byte(testkit.PBSProviderTOML(dead.URL)))
	if err != nil {
		t.Fatal(err)
	}
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), Custom: LoadPBSIndex}
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
	loader2 := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), Custom: LoadPBSIndex}
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
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), Custom: LoadPBSIndex}
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
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: cacheDir, Custom: LoadPBSIndex}
	if _, err := loader.Load(context.Background(), fp.Provider, "cn"); err != nil {
		t.Fatal(err)
	}
	// 缓存落盘后，断网也能再解析
	offline := fetch.New()
	offline.Offline = true
	loader2 := &provider.IndexLoader{Client: offline, CacheDir: cacheDir, Custom: LoadPBSIndex}
	if _, err := loader2.Load(context.Background(), fp.Provider, "cn"); err != nil {
		t.Fatalf("缓存应可离线读: %v", err)
	}
}
