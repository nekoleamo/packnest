package provider

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestAllProvidersValidate(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	want := []string{"bun", "deno", "go", "node", "npm", "pnpm", "python"}
	if len(all) != len(want) {
		t.Fatalf("provider 数量 = %d，期望 %d（%v）", len(all), len(want), IDs())
	}
	for i, p := range all {
		if p.ID != want[i] {
			t.Errorf("第 %d 个 provider = %q，期望 %q", i, p.ID, want[i])
		}
		if err := p.Validate(); err != nil {
			t.Errorf("%s: Validate 失败: %v", p.ID, err)
		}
	}
}

func TestGetProvider(t *testing.T) {
	p, err := Get("node")
	if err != nil {
		t.Fatalf("Get(node): %v", err)
	}
	if p.Index.Kind != IndexJSON || p.Layout.Strip != 1 {
		t.Fatalf("node 清单异常: %+v", p)
	}
	if _, err := Get("rust"); err == nil {
		t.Fatal("未知工具应报错")
	} else if !strings.Contains(err.Error(), "node") {
		t.Errorf("错误信息应列出可用工具: %v", err)
	}
}

func TestNodeCompanionDeclared(t *testing.T) {
	p, err := Get("node")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := p.Companions["npm"]
	if !ok {
		t.Fatal("node 必须声明 npm 伴生（方案 §7）")
	}
	if c.Mode != "latest-compatible" || c.Target != "lib/node_modules/npm" || c.Registry != "npm" {
		t.Fatalf("伴生声明异常: %+v", c)
	}
}

func TestPnpmRequiresNode(t *testing.T) {
	p, err := Get("pnpm")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Requires) != 1 || p.Requires[0] != "node" {
		t.Fatalf("pnpm 必须 requires node（方案 §18.1）: %v", p.Requires)
	}
}

func TestGoToolchainLocal(t *testing.T) {
	p, err := Get("go")
	if err != nil {
		t.Fatal(err)
	}
	if p.Toolchain == nil || p.Toolchain.Env["GOTOOLCHAIN"] != "local" {
		t.Fatalf("go 必须写 GOTOOLCHAIN=local（方案 §6.6）: %+v", p.Toolchain)
	}
}

func TestArtifactURLNode(t *testing.T) {
	p, _ := Get("node")
	u, err := p.ArtifactURL("22.22.2", "darwin", "arm64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURL: %v", err)
	}
	want := "https://registry.npmmirror.com/-/binary/node/v22.22.2/node-v22.22.2-darwin-arm64.tar.gz"
	if u != want {
		t.Fatalf("got %q\nwant %q", u, want)
	}
	win, err := p.ArtifactURL("22.22.2", "windows", "amd64", "default")
	if err != nil {
		t.Fatalf("ArtifactURL windows: %v", err)
	}
	if !strings.HasSuffix(win, "node-v22.22.2-win-x64.zip") {
		t.Fatalf("windows 必须取 zip（方案 §18.3）: %q", win)
	}
}

func TestArtifactURLRegistryPlaceholder(t *testing.T) {
	p, _ := Get("pnpm")
	u, err := p.ArtifactURL("10.4.1", "linux", "amd64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURL: %v", err)
	}
	if u != "https://registry.npmmirror.com/pnpm/-/pnpm-10.4.1.tgz" {
		t.Fatalf("registry 占位符渲染错误: %q", u)
	}
}

func TestArtifactURLDenoSingle(t *testing.T) {
	p, _ := Get("deno")
	if p.Layout.Single != "bin/deno" {
		t.Fatalf("deno 必须单文件布局（方案 §18.7）: %q", p.Layout.Single)
	}
	u, err := p.ArtifactURL("2.1.9", "darwin", "arm64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURL: %v", err)
	}
	if !strings.HasSuffix(u, "deno-aarch64-apple-darwin.zip") {
		t.Fatalf("平台映射错误: %q", u)
	}
}

func TestArtifactURLUnsupportedPlatform(t *testing.T) {
	p, _ := Get("bun")
	if _, err := p.ArtifactURL("1.2.0", "windows", "arm64", "cn"); err == nil {
		t.Fatal("bun 没有 windows-arm64，应报错并列出可用平台")
	} else if !strings.Contains(err.Error(), "darwin-arm64") {
		t.Errorf("错误应列出可用平台: %v", err)
	}
}

func TestPythonPBSManifest(t *testing.T) {
	// python 的索引是 pbs 两级列举；产物名与校验和都依赖 {tag}/{filename}。
	p, _ := Get("python")
	if p.Index.Kind != IndexPBS {
		t.Fatalf("python 索引必须是 pbs: %q", p.Index.Kind)
	}
	v := Version{
		Version: "3.13.18", Tag: "20261026",
		Files: []IndexFile{{Filename: "cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz",
			OS: "darwin", Arch: "arm64", Kind: "archive"}},
	}
	urls, err := p.ArtifactURLsFor(v, "darwin", "arm64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURLsFor: %v", err)
	}
	if !strings.Contains(urls[0], "/20261026/cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz") {
		t.Fatalf("{tag}/{filename} 未渲染: %v", urls)
	}
	sums, err := p.ChecksumURLsFor(v, "darwin", "arm64", "cn")
	if err != nil || len(sums) == 0 || !strings.HasSuffix(sums[0], "/20261026/SHA256SUMS") {
		t.Fatalf("校验和 URL 应带标签目录: %v %v", sums, err)
	}
	// 不带元数据时 {filename} 为空，URL 会退化成目录形式（调用方必须传 Version）
	plain, err := p.ArtifactURL("3.13.18", "darwin", "arm64", "cn")
	if err != nil || !strings.HasSuffix(plain, "/20261026/") && strings.Contains(plain, "20261026") {
		t.Logf("无元数据的渲染结果: %q", plain)
	}
}

func TestGoIndexInlineChecksum(t *testing.T) {
	p, _ := Get("go")
	if p.Checksum.Format != ChecksumIndexInline {
		t.Fatalf("go 的校验和应来自索引（index-inline）: %q", p.Checksum.Format)
	}
	if p.Index.VersionPrefix != "go" {
		t.Fatalf("go 的版本前缀应剥离: %q", p.Index.VersionPrefix)
	}
	if !strings.Contains(p.Index.URLs["default"], "include=all") {
		t.Fatalf("索引必须带 include=all（否则只有 2 条）: %q", p.Index.URLs["default"])
	}
	v := Version{Version: "1.27.3", Files: []IndexFile{{Filename: "go1.27.3.linux-amd64.tar.gz",
		OS: "linux", Arch: "amd64", SHA256: "abc", Kind: "archive"}}}
	urls, err := p.ArtifactURLsFor(v, "linux", "amd64", "cn")
	if err != nil || urls[0] != "https://mirrors.aliyun.com/golang/go1.27.3.linux-amd64.tar.gz" {
		t.Fatalf("go 产物 URL（{filename}）错误: %v %v", urls, err)
	}
	if f, ok := v.FileFor("linux", "amd64"); !ok || f.SHA256 != "abc" {
		t.Fatalf("go 的产物 sha256 应可读: %+v", f)
	}
}

func TestNodeChecksumExcludesCN(t *testing.T) {
	p, _ := Get("node")
	urls, err := p.ChecksumURLs("22.22.2", "darwin", "arm64", "cn")
	if err != nil {
		t.Fatalf("ChecksumURLs: %v", err)
	}
	if len(urls) == 0 {
		t.Fatal("node 必须有校验和源")
	}
	for _, u := range urls {
		if strings.Contains(u, "npmmirror") {
			t.Fatalf("npmmirror 没有 SHASUMS256.txt（实测 404），不该出现在校验源里: %q", u)
		}
	}
	if !strings.HasPrefix(urls[0], "https://mirrors.tuna.tsinghua.edu.cn/") &&
		!strings.HasPrefix(urls[0], "https://mirrors.aliyun.com/") &&
		!strings.HasPrefix(urls[0], "https://mirrors.ustc.edu.cn/") {
		t.Fatalf("cn 档应优先国内校验源: %q", urls[0])
	}
}

func TestIndexURLsOrder(t *testing.T) {
	p, _ := Get("node")
	urls := p.IndexURLs("cn")
	if len(urls) < 2 {
		t.Fatalf("索引应有默认与国内两条: %v", urls)
	}
	if !strings.Contains(urls[0], "npmmirror") {
		t.Fatalf("cn 档首选国内索引: %v", urls)
	}
	if !strings.Contains(urls[len(urls)-1], "nodejs.org") {
		t.Fatalf("官方源应最后: %v", urls)
	}
}

func TestParseRejectsBadManifests(t *testing.T) {
	base := `
id = "x"
display = "X"
kind = "binary"
[index]
kind = "json"
[index.urls]
default = "https://example.com/index.json"
[artifact]
[artifact.urls]
default = "https://example.com/{version}.{ext}"
[artifact.ext]
windows = "zip"
darwin  = "tar.gz"
linux   = "tar.gz"
[artifact.platform]
darwin-arm64  = "darwin-arm64"
linux-amd64   = "linux-amd64"
windows-amd64 = "win-x64"
[checksum]
format = "gnu-sums"
mode   = "tofu-then-verify"
[checksum.urls]
default = "https://example.com/SHASUMS"
[layout]
strip = 1
bins = ["x"]
`
	if _, err := Parse([]byte(base)); err != nil {
		t.Fatalf("基线清单应合法: %v", err)
	}

	cases := map[string]string{
		"缺 id":         strings.Replace(base, `id = "x"`, "", 1),
		"缺 display":    strings.Replace(base, `display = "X"`, "", 1),
		"坏 index.kind": strings.Replace(base, `kind = "json"`, `kind = "soap"`, 1),
		"空 index.url":  strings.Replace(base, `default = "https://example.com/index.json"`, `default = ""`, 1),
		"坏 ext":        strings.Replace(base, "linux   = \"tar.gz\"", "linux   = \"tar.xz\"", 1),
		"缺平台":          strings.Replace(base, `windows-amd64 = "win-x64"`, ``, 1),
		"缺 bins":       strings.Replace(base, `bins = ["x"]`, `bins = []`, 1),
		"坏 checksum":   strings.Replace(base, `format = "gnu-sums"`, `format = "md5"`, 1),
		"坏 mode":       strings.Replace(base, `mode   = "tofu-then-verify"`, `mode   = "yolo"`, 1),
		"缺 checksum":   strings.Replace(base, "default = \"https://example.com/SHASUMS\"", "", 1),
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: 应报错", name)
		}
	}
}

func TestRenderUnknownPlaceholder(t *testing.T) {
	if _, err := render("https://x/{version}/{bogus}", map[string]string{"version": "1"}); err == nil {
		t.Fatal("未知占位符应报错")
	}
	if _, err := render("https://x/{version", map[string]string{"version": "1"}); err == nil {
		t.Fatal("未闭合大括号应报错")
	}
	got, err := render("https://x/{version}/{ext}", map[string]string{"version": "1", "ext": "zip"})
	if err != nil || got != "https://x/1/zip" {
		t.Fatalf("render = %q/%v", got, err)
	}
}

func TestCompileSpec(t *testing.T) {
	p, _ := Get("node")
	if _, err := p.Compile("22.22.2"); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := p.Compile("nonsense spec"); err == nil {
		t.Fatal("非法说明符应报错")
	}
}

func TestPickProfileKeyFallback(t *testing.T) {
	urls := map[string]string{"default": "d", "cn": "c"}
	if got := pickProfileKey(urls, "tuna"); got != "cn" {
		t.Fatalf("tuna 缺档时应回落到 cn，得到 %q", got)
	}
	if got := pickProfileKey(urls, ""); got != "cn" {
		t.Fatalf("空档应回落 cn，得到 %q", got)
	}
	if got := pickProfileKey(map[string]string{}, "cn"); got != "" {
		t.Fatalf("空表应返回空: %q", got)
	}
}

func TestArtifactURLsChain(t *testing.T) {
	p, _ := Get("node")
	urls, err := p.ArtifactURLs("22.22.2", "linux", "amd64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURLs: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("node 有 default/cn 两个档位: %v", urls)
	}
	if !strings.Contains(urls[0], "npmmirror") || !strings.Contains(urls[1], "nodejs.org") {
		t.Fatalf("回退顺序错误: %v", urls)
	}
	// 不支持的平台要报错
	if _, err := p.ArtifactURLs("22.22.2", "plan9", "amd64", "cn"); err == nil {
		t.Fatal("不支持的平台应报错")
	}
	if p, _ := Get("deno"); p != nil {
		if _, err := p.ChecksumURLs("1.0.0", "plan9", "amd64", "cn"); err == nil {
			t.Fatal("不支持的平台应报错")
		}
	}
}

func TestExtFor(t *testing.T) {
	p, _ := Get("node")
	if ext, err := p.ExtFor("windows"); err != nil || ext != "zip" {
		t.Fatalf("windows 应取 zip: %q %v", ext, err)
	}
	if _, err := p.ExtFor("plan9"); err == nil {
		t.Fatal("未知平台应报错")
	}
	single := &Provider{ID: "x", Layout: LayoutBlock{Single: "bin/x"}}
	if ext, err := single.ExtFor("linux"); err != nil || ext != "" {
		t.Fatalf("单文件布局不需要 ext: %q %v", ext, err)
	}
}

func TestLoadFSRejectsBadFiles(t *testing.T) {
	good := `
id = "x"
display = "X"
kind = "binary"
[index]
kind = "json"
[index.urls]
default = "https://example.com/i.json"
[artifact]
[artifact.urls]
default = "https://example.com/{version}.{ext}"
[artifact.ext]
darwin = "tar.gz"
linux = "tar.gz"
windows = "zip"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-amd64"
windows-amd64 = "win-x64"
[checksum]
format = "gnu-sums"
mode = "none"
[layout]
bins = ["x"]
strip = 1
`
	fsys := fstest.MapFS{"x.toml": &fstest.MapFile{Data: []byte(good)}}
	if ps, err := loadFS(fsys); err != nil || len(ps) != 1 {
		t.Fatalf("应加载成功: %v %v", ps, err)
	}

	fsys = fstest.MapFS{"y.toml": &fstest.MapFile{Data: []byte(good)}}
	if _, err := loadFS(fsys); err == nil {
		t.Fatal("文件名与 id 不一致应报错")
	}
	fsys = fstest.MapFS{"x.toml": &fstest.MapFile{Data: []byte("id = [")}}
	if _, err := loadFS(fsys); err == nil {
		t.Fatal("非法 TOML 应报错")
	}
	if _, err := loadFS(fstest.MapFS{}); err == nil {
		t.Fatal("空目录应报错")
	}
}

func TestPickProfileKeyUnknown(t *testing.T) {
	if got := pickProfileKey(map[string]string{"ustc": "u"}, "tuna"); got != "ustc" {
		t.Fatalf("应回落到链上第一个存在的档位: %q", got)
	}
}

func TestEnvMirrorOverride(t *testing.T) {
	p, _ := Get("node")
	base, _ := p.ArtifactURL("22.22.2", "darwin", "arm64", "cn")
	reg := func() string { u, _ := Get("pnpm"); v, _ := u.ArtifactURL("10.0.0", "linux", "amd64", "cn"); return v }()
	_ = base

	t.Setenv("PK_MIRROR_REGISTRY", "http://nexus.local/npm/")
	if got := reg; got == "" {
		t.Fatal("基线 registry 渲染失败")
	}
	pnpm, _ := Get("pnpm")
	got, err := pnpm.ArtifactURL("10.0.0", "linux", "amd64", "cn")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://nexus.local/npm/pnpm/-/pnpm-10.0.0.tgz" {
		t.Fatalf("PK_MIRROR_REGISTRY 未生效: %q", got)
	}

	t.Setenv("PK_MIRROR_NODE_INDEX", "http://nexus.local/index.json")
	if urls := p.IndexURLs("cn"); urls[0] != "http://nexus.local/index.json" {
		t.Fatalf("PK_MIRROR_<TOOL>_INDEX 应最优先: %v", urls)
	}
	t.Setenv("PK_MIRROR_NODE_ARTIFACT", "http://nexus.local/v{version}/node-{platform}.{ext}")
	urls, err := p.ArtifactURLs("22.22.2", "darwin", "arm64", "cn")
	if err != nil || urls[0] != "http://nexus.local/v22.22.2/node-darwin-arm64.tar.gz" {
		t.Fatalf("PK_MIRROR_<TOOL>_ARTIFACT 未生效: %v %v", urls, err)
	}
	t.Setenv("PK_MIRROR_NODE_CHECKSUM", "http://nexus.local/sums/{version}")
	sums, err := p.ChecksumURLs("22.22.2", "darwin", "arm64", "cn")
	if err != nil || sums[0] != "http://nexus.local/sums/22.22.2" {
		t.Fatalf("PK_MIRROR_<TOOL>_CHECKSUM 未生效: %v %v", sums, err)
	}
}

func TestRegistryTemplatesRenderPerProfile(t *testing.T) {
	// {registry} 模板必须按回退链各渲染一次，否则国内档挂了不会退到官方 registry。
	pnpm, _ := Get("pnpm")
	urls, err := pnpm.ArtifactURLs("10.4.1", "linux", "amd64", "cn")
	if err != nil {
		t.Fatalf("ArtifactURLs: %v", err)
	}
	if len(urls) < 2 {
		t.Fatalf("应至少渲染出 2 个 registry（cn + 官方），得到 %v", urls)
	}
	if !strings.HasPrefix(urls[0], "https://registry.npmmirror.com/") {
		t.Fatalf("首个应是国内 registry: %v", urls)
	}
	foundOfficial := false
	for _, u := range urls {
		if strings.HasPrefix(u, "https://registry.npmjs.org/") {
			foundOfficial = true
		}
	}
	if !foundOfficial {
		t.Fatalf("回退链里必须有官方 registry: %v", urls)
	}
}
