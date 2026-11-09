// M3 验收：go 与 python（方案 §12.1 M3 门禁）。
//
//   - go：校验和来自索引自带的 sha256，装完环境里必须是 GOTOOLCHAIN=local，
//     否则 go 会按 go.mod 的 toolchain 声明自己去下一套工具链（多一个下载面）
//   - python：只装 python-build-standalone 预编译产物，永不源码编译；
//     索引是两级目录列举（release 标签 → 文件）
package acceptance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/toolchain"
	"github.com/packnest/packnest/internal/verify"
)

func newToolInstaller(t *testing.T, goos, goarch string, lookup func(string) (*provider.Provider, error)) (*install.Installer, paths.Paths) {
	t.Helper()
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inst, err := install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: goos, GOARCH: goarch, LookupProvider: lookup,
	})
	if err != nil {
		t.Fatalf("install.New: %v", err)
	}
	return inst, p
}

// TestAcceptance_M3_GoNoDoubleDownload：go 的校验和来自索引，装完必然写 GOTOOLCHAIN=local。
func TestAcceptance_M3_GoNoDoubleDownload(t *testing.T) {
	fg := testkit.NewFakeGo(t, []string{"1.26.0", "1.27.3"})
	inst, p := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		if id == "go" {
			return fg.Provider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})

	res, err := inst.Install(context.Background(), "go", "1.27.3")
	if err != nil {
		t.Fatalf("装 go: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "bin", "go")); err != nil {
		t.Fatalf("缺少 bin/go: %v", err)
	}

	// ① 校验和来自索引本身：fixture 里没有任何 SHASUMS 文件，装成功即证明没多请求
	if res.Verified != verify.VerifiedWeak {
		t.Fatalf("单源（索引内联）应为 weak，得到 %q", res.Verified)
	}
	known, err := verify.LoadKnown(p.Known())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := known.Get("go@1.27.3/darwin-arm64")
	if !ok || entry.Digest != fg.Sha256["1.27.3"] {
		t.Fatalf("TOFU 摘要应与索引 sha256 一致: %+v", entry)
	}

	// ② 环境里必须有 GOTOOLCHAIN=local（否则 go 自己会再下一套工具链）
	vars := map[string]string{}
	for _, kv := range append(env.RegistryVars("cn"), env.ToolchainVars(map[string]string{"GOTOOLCHAIN": "local"})...) {
		vars[kv.Key] = kv.Value
	}
	if vars["GOTOOLCHAIN"] != "local" {
		t.Fatalf("pk env 必须导出 GOTOOLCHAIN=local（方案 §6.6）: %+v", vars)
	}
	if !strings.Contains(vars["GOPROXY"], "goproxy.cn") {
		t.Fatalf("cn 档的 GOPROXY 应指向国内代理: %q", vars["GOPROXY"])
	}
	if msg := toolchain.CheckGoEnv(func(k string) string { return vars[k] }); msg != "" {
		t.Fatalf("环境齐全时不该告警: %q", msg)
	}
	// 缺了就要告警并给出处置（doctor 对应用例在 cmd/pk 里）
	if msg := toolchain.CheckGoEnv(func(string) string { return "" }); !strings.Contains(msg, "GOTOOLCHAIN") {
		t.Fatalf("缺 GOTOOLCHAIN 时应告警: %q", msg)
	}
}

// TestAcceptance_M3_PythonNoCompile：只下预编译产物，树里是现成二进制，不编译。
func TestAcceptance_M3_PythonNoCompile(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261026"},
		Versions: []string{"3.12.16", "3.13.18"},
	})
	inst, p := newToolInstaller(t, "darwin", "arm64", func(id string) (*provider.Provider, error) {
		if id == "python" {
			return fp.Provider, nil
		}
		return nil, errors.New("未知工具 " + id)
	})
	res, err := inst.Install(context.Background(), "python", "3.13")
	if err != nil {
		t.Fatalf("装 python: %v", err)
	}
	if res.Version != "3.13.18" {
		t.Fatalf("3.13 → %q", res.Version)
	}
	// 树里是现成的解释器
	if _, err := os.Stat(filepath.Join(res.Path, "bin", "python3")); err != nil {
		t.Fatalf("缺少 bin/python3: %v", err)
	}
	// 没有源码编译的痕迹
	for _, marker := range []string{"Python-3.13.18", "configure", "Makefile.pre.in", "Modules/Setup"} {
		if _, err := os.Stat(filepath.Join(res.Path, marker)); err == nil {
			t.Fatalf("不该出现源码编译产物: %s", marker)
		}
	}
	// 缓存里只有一份预编译 tar.gz
	entries, err := os.ReadDir(p.CacheArchives())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".tar.gz") {
		t.Fatalf("只该下一份预编译产物，得到 %v", entries)
	}
	if strings.Contains(entries[0].Name(), "stripped") {
		t.Fatalf("不该选 stripped 变体: %v", entries[0].Name())
	}
}

// TestAcceptance_M3_PbsDirectoryIndex：两级目录列举（标签目录 → 文件）正确解析。
func TestAcceptance_M3_PbsDirectoryIndex(t *testing.T) {
	fp := testkit.NewFakePBS(t, testkit.PBSOptions{
		Tags:     []string{"20261020", "20261026"},
		Versions: []string{"3.11.18", "3.12.16", "3.13.18"},
	})
	loader := &provider.IndexLoader{Client: fetch.New(), CacheDir: t.TempDir(), Custom: toolchain.LoadPBSIndex}
	idx, err := loader.Load(context.Background(), fp.Provider, "cn")
	if err != nil {
		t.Fatalf("索引加载: %v", err)
	}
	if len(idx.Versions) != 3 {
		t.Fatalf("应有 3 个 cpython 版本: %+v", idx.Versions)
	}
	for _, v := range idx.Versions {
		if v.Tag != "20261026" {
			t.Fatalf("应采用最新标签: %+v", v)
		}
		if len(v.Files) < 2 {
			t.Fatalf("每版本应记录多平台文件名: %+v", v)
		}
	}
	cases := map[string]string{"3.11": "3.11.18", "3.12": "3.12.16", "3.13": "3.13.18"}
	for sp, want := range cases {
		v, err := idx.Resolve(spec.MustParse(sp))
		if err != nil || v.Version != want {
			t.Errorf("解析 %s → %+v/%v，期望 %s", sp, v, err, want)
		}
	}
	if _, err := idx.Resolve(spec.MustParse("3.14")); err == nil {
		t.Fatal("不存在的 3.14 应报错")
	} else if !strings.Contains(err.Error(), "3.13.18") {
		t.Fatalf("错误应给出最新可用版本: %v", err)
	}

	// 产物 URL 与校验和 URL 都要带标签目录与精确文件名
	v, _ := idx.Resolve(spec.MustParse("3.13"))
	urls, err := fp.Provider.ArtifactURLsFor(v, "linux", "amd64", "cn")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(urls[0], "/20261026/cpython-3.13.18+20261026-x86_64-unknown-linux-gnu-install_only.tar.gz") {
		t.Fatalf("产物 URL 应带 tag 与文件名: %v", urls)
	}
	sums, err := fp.Provider.ChecksumURLsFor(v, "linux", "amd64", "cn")
	if err != nil || len(sums) == 0 || !strings.HasSuffix(sums[0], "/20261026/SHA256SUMS") {
		t.Fatalf("校验和 URL 应带标签目录: %v %v", sums, err)
	}
}
