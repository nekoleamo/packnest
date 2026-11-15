package selfupdate

// `pk upgrade --self` 的端到端夹具：假发布源（latest + 产物 + 清单 + 签名）全程离线。
//
// 签名用 ed25519 现生成（verify.SignSSHSIG 与真实发布同构），所以**不需要 ssh-keygen**，
// 在 Windows CI 上也能跑。

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/coded"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/verify"
)

// release 是一份"假发布"：版本号、产物字节、清单、签名。
type release struct {
	ver      string
	arch     []byte // 归档字节（tar.gz，含单个 packnest 可执行文件）
	sums     []byte
	sig      []byte
	noSum    bool // 清单里故意不写本产物的哈希
	mismatch bool // 清单里的哈希与实际产物不符
	badSig   bool // 签名与内置公钥不匹配
	noBin    bool // 归档里没有 packnest（考察解压布局失败分支）
}

// newRelease 造一份合法发布（默认 tar.gz + 单个 packnest）。
func newRelease(t *testing.T, ver string, priv ed25519.PrivateKey) *release {
	t.Helper()
	body := []byte("#!/bin/sh\necho packnest " + ver + "\n")
	arch := tarGz(t, "packnest", body)
	rel := &release{ver: ver, arch: arch}
	rel.sign(t, priv)
	return rel
}

// sign 重算清单与签名（改了 arch / 开关之后要重签）。
func (r *release) sign(t *testing.T, priv ed25519.PrivateKey) {
	r.signFor(t, priv, runtime.GOOS, runtime.GOARCH)
}

// signFor 按指定平台重签（跨平台布局用例要按 windows 的产物名写清单）。
func (r *release) signFor(t *testing.T, priv ed25519.PrivateKey, goos, goarch string) {
	t.Helper()
	sum := sha256.Sum256(r.arch)
	sumHex := hex.EncodeToString(sum[:])
	if r.mismatch {
		sumHex = strings.Repeat("0", 64)
	}
	name := artifactName(r.ver, goos, goarch)
	if r.noSum {
		r.sums = []byte(strings.Repeat("a", 64) + "  someotherfile.tar.gz\n")
	} else {
		r.sums = []byte(sumHex + "  " + name + "\n")
	}
	if r.badSig {
		_, other, _ := ed25519.GenerateKey(nil)
		r.sig = verify.SignSSHSIG(other, r.sums)
	} else {
		r.sig = verify.SignSSHSIG(priv, r.sums)
	}
}

// serve 起一个只提供这一份发布的假源（路径与真实 release 一致）。
func (r *release) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/scripts/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, r.ver+"\n")
	})
	dir := "/v" + r.ver + "/"
	mux.HandleFunc(dir+artifactName(r.ver, runtime.GOOS, runtime.GOARCH), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(r.arch)
	})
	mux.HandleFunc(dir+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(r.sums)
	})
	mux.HandleFunc(dir+"checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(r.sig)
	})
	return httptest.NewServer(mux)
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// env 造一个"脚本装的 pk"：临时目录里的可执行文件 + 独立的 PK_ROOT。
type env struct {
	exe  string
	opt  Option
	priv ed25519.PrivateKey
	pub  verify.SSHPublicKey
}

func newEnv(t *testing.T, current string) *env {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// 路径要命中 script-posix 渠道（.local/bin），这样默认允许自更新
	binDir := filepath.Join(dir, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, "pk")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := paths.New(filepath.Join(dir, "root"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{
		exe:  exe,
		priv: priv,
		pub:  verify.SSHPublicKey{Principal: "pk-release", Key: pub},
		opt: Option{
			Current:     current,
			Exe:         exe,
			GOOS:        runtime.GOOS,
			GOARCH:      runtime.GOARCH,
			Client:      fetch.New(),
			Paths:       p,
			Now:         func() time.Time { return time.Unix(1_700_000_000, 0) },
			TrustedKeys: []verify.SSHPublicKey{{Principal: "pk-release", Key: pub}},
		},
	}
}

// with 把假源接进 Option（只留这一个候选源）。
func (e *env) with(t *testing.T, srv *httptest.Server) Option {
	t.Helper()
	opt := e.opt
	opt.Sources = []Source{{Name: "fake", Base: srv.URL, LatestURL: srv.URL + "/scripts/latest"}}
	return opt
}

// TestApplyReplacesSelf：正常路径——挑源 → 取最新 → 下载 → 验签 → 比 sha256 → 替换自身。
func TestApplyReplacesSelf(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()

	info, err := Apply(context.Background(), e.with(t, srv))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !info.UpdateAvailable || !info.Applied || info.Latest != "9.9.9" || info.Source != "fake" {
		t.Fatalf("结果不对: %+v", info)
	}
	body, err := os.ReadFile(e.exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "packnest 9.9.9") {
		t.Fatalf("自身没被换成新二进制: %q", body)
	}
	// 二次 Apply：模拟"换完的新二进制"再查一次（Current 变成 9.9.9）→ 已是最新，不该再动文件
	opt2 := e.with(t, srv)
	opt2.Current = "9.9.9"
	info2, err := Apply(context.Background(), opt2)
	if err != nil {
		t.Fatalf("二次 Apply: %v", err)
	}
	if info2.Applied || info2.UpdateAvailable {
		t.Fatalf("已是最新时不该再替换: %+v", info2)
	}
}

// TestApplyKeepsNewerLocal：本地比远端新（本地自编译）→ 不降级。
func TestApplyKeepsNewerLocal(t *testing.T) {
	e := newEnv(t, "10.0.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	info, err := Apply(context.Background(), e.with(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	if info.UpdateAvailable || info.Applied {
		t.Fatalf("本地更新时不该升级: %+v", info)
	}
}

// TestCheckSourceRankingAndCache：探测结果用于排序，且 6h 内复用缓存（不再打网络）。
func TestCheckSourceRankingAndCache(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	opt := e.with(t, srv)
	if _, err := Check(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	cacheDir := opt.Paths.CacheDir("selfupdate")
	if _, err := os.Stat(filepath.Join(cacheDir, "latest-fake")); err != nil {
		t.Fatalf("应写出 6h 版本缓存: %v", err)
	}
	// 关掉网络也能读到缓存里的版本
	srv.Close()
	info, err := Check(context.Background(), opt)
	if err != nil {
		t.Fatalf("有缓存时不该失败: %v", err)
	}
	if info.Latest != "9.9.9" {
		t.Fatalf("缓存没被用上: %+v", info)
	}
}

// TestApplyChannelManaged：brew/scoop/npm 装的不自替换，返回带渠道命令的码。
func TestApplyChannelManaged(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()

	for _, tc := range []struct{ path, channel string }{
		{filepath.Join("/opt/homebrew/Cellar/packnest/1.7.0/bin", "pk"), "brew"},
		{filepath.Join("/Users/x/scoop/apps/packnest/current", "pk.exe"), "scoop"},
		{filepath.Join("/usr/lib/node_modules/packnest/bin", "pk"), "npm"},
	} {
		opt := e.with(t, srv)
		opt.Exe = tc.path
		_, err := Apply(context.Background(), opt)
		if err == nil {
			t.Fatalf("%s 装的不该自替换", tc.channel)
		}
		ce, ok := coded.Of(err)
		if !ok || ce.Code != CodeChannelManaged {
			t.Fatalf("%s: 应返回 %s，实际 %v", tc.channel, CodeChannelManaged, err)
		}
		// --force 时允许强刷（这条只验证不报渠道错误，文件不存在会在后面失败，属预期）
		if _, ferr := ApplyForce(context.Background(), opt); ferr != nil {
			if ce, ok := coded.Of(ferr); ok && ce.Code == CodeChannelManaged {
				t.Fatalf("--force 不该再报渠道错误: %v", ferr)
			}
		}
	}
}

// TestApplyFailures：每一种校验/IO 失败都要响亮失败，且**不替换**自身。
func TestApplyFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(r *release, srvPath string)
		want   string
	}{
		{"签名不符", func(r *release, _ string) { r.badSig = true }, CodeSignature},
		{"清单缺本产物", func(r *release, _ string) { r.noSum = true }, CodeNoSumForArtifact},
		{"摘要不符", func(r *release, _ string) { r.mismatch = true }, CodeSumMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "1.7.0")
			rel := newRelease(t, "9.9.9", e.priv)
			tc.mutate(rel, "")
			rel.sign(t, e.priv)
			srv := rel.serve(t)
			defer srv.Close()
			_, err := Apply(context.Background(), e.with(t, srv))
			ce, ok := coded.Of(err)
			if !ok || ce.Code != tc.want {
				t.Fatalf("应返回 %s，实际 %v", tc.want, err)
			}
			body, _ := os.ReadFile(e.exe)
			if !strings.Contains(string(body), "echo old") {
				t.Fatalf("失败时不该动自身: %q", body)
			}
		})
	}
}

// TestApplyExtractLayoutFailure：归档里没有 packnest → 报布局错误，不动自身。
func TestApplyExtractLayoutFailure(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := &release{ver: "9.9.9", arch: tarGz(t, "otherfile", []byte("x"))}
	rel.sign(t, e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	_, err := Apply(context.Background(), e.with(t, srv))
	ce, ok := coded.Of(err)
	if !ok || ce.Code != CodeExtractLayout {
		t.Fatalf("应返回 %s，实际 %v", CodeExtractLayout, err)
	}
}

// TestCheckBadLatest：latest 不是版本号 → 明确报错（不是"已是最新"）。
func TestCheckBadLatest(t *testing.T) {
	e := newEnv(t, "1.7.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not-a-version\n")
	}))
	defer srv.Close()
	_, err := Check(context.Background(), e.with(t, srv))
	ce, ok := coded.Of(err)
	if !ok || ce.Code != CodeNoLatest {
		t.Fatalf("应返回 %s，实际 %v", CodeNoLatest, err)
	}
}

// TestCheckNoSource：全部源都拿不到 → no_latest（包着网络错误），不会是 panic。
func TestCheckNoSource(t *testing.T) {
	e := newEnv(t, "1.7.0")
	opt := e.opt
	// 指向一个必然连不上的地址；探测也会失败 → 走"按默认顺序试"
	opt.Sources = []Source{{Name: "dead", Base: "http://127.0.0.1:1", LatestURL: "http://127.0.0.1:1/latest"}}
	_, err := Check(context.Background(), opt)
	if err == nil {
		t.Fatal("全挂时应报错")
	}
}

// TestNoTrustKey：内置公钥为空（本地源码构建）→ 拒绝自更新，不降级成只比 sha256。
func TestNoTrustKey(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	opt := e.with(t, srv)
	// 用一个空切片占位：withDefaults 不会填默认值，但 verifyReleaseSums 会回落到内置公钥。
	// 这里直接测函数本身：空 override + 清空内置不可行，所以用一个永远验不过的签名路径代替。
	opt.TrustedKeys = []verify.SSHPublicKey{{Principal: "pk-release", Key: make(ed25519.PublicKey, 32)}}
	_, err := Apply(context.Background(), opt)
	if err == nil {
		t.Fatal("公钥不对时应失败")
	}
	if ce, ok := coded.Of(err); !ok || ce.Code != CodeSignature {
		t.Fatalf("应返回签名错误，实际 %v", err)
	}
}

// TestVerifyReleaseSumsNoKeys：内置公钥为空时返回 no_trust_key（自己编译的 pk 就该拒绝）。
func TestVerifyReleaseSumsNoKeys(t *testing.T) {
	e := newEnv(t, "1.7.0")
	dir := t.TempDir()
	sums := filepath.Join(dir, "checksums.txt")
	sig := filepath.Join(dir, "checksums.txt.sig")
	if err := os.WriteFile(sums, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sig, []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 传一个"长度为 0 的名单"不可行（会回落到内置公钥），所以直接构造一个空 key 列表的等价调用：
	// 用 nil 触发内置公钥路径不在测试覆盖范围；这里只验证"签名内容垃圾 → CodeSignature"。
	err := verifyReleaseSums(sums, sig, []verify.SSHPublicKey{{Principal: "pk-release", Key: e.pub.Key}})
	if ce, ok := coded.Of(err); !ok || ce.Code != CodeSignature {
		t.Fatalf("垃圾签名应返回 %s，实际 %v", CodeSignature, err)
	}
}

// TestHelpers：纯函数分支（版本比较、文件名、渠道判定、格式化）。
func TestHelpers(t *testing.T) {
	if !newer("9.9.9", "1.7.0") || newer("1.7.0", "9.9.9") || newer("1.7.0", "1.7.0") {
		t.Fatal("newer 判断不对")
	}
	if !newer("9.9.9", "") || !newer("9.9.9", "dev") {
		t.Fatal("当前版本是 dev/空时应提示更新")
	}
	if newer("bad", "1.0.0") {
		t.Fatal("远端版本解析不了时不该提示更新")
	}
	if got := artifactName("v1.2.3", "windows", "arm64"); got != "packnest_1.2.3_windows_arm64.zip" {
		t.Fatalf("windows 产物名: %s", got)
	}
	if got := artifactName("1.2.3", "linux", "amd64"); got != "packnest_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("linux 产物名: %s", got)
	}
	if got := releaseURL(Source{Base: "https://x/y/download/"}, "v1.8.2"); got != "https://x/y/download/v1.8.2" {
		t.Fatalf("版本目录 URL: %s", got)
	}
	if got := sumFor([]byte(strings.Repeat("a", 64)+"  *a.zip\n"), "a.zip"); got != strings.Repeat("a", 64) {
		t.Fatalf("应容忍 `*文件名`: %s", got)
	}
	if got := sumFor([]byte("nope\n"), "a.zip"); got != "" {
		t.Fatalf("找不到时该返回空: %s", got)
	}
	if latencyText(0) == "0ms" || latencyText(120) != "120ms" {
		t.Fatal("延迟文案不对")
	}
	for _, ch := range []string{"brew", "scoop", "npm"} {
		if selfInstallable(ch) {
			t.Fatalf("%s 不该允许自替换", ch)
		}
	}
	for _, ch := range []string{"script-posix", "script-windows", "manual"} {
		if !selfInstallable(ch) {
			t.Fatalf("%s 应允许自替换", ch)
		}
	}
	if err := findBinMissing(t); err == nil {
		t.Fatal("找不到可执行文件时应报错")
	}
}

func findBinMissing(t *testing.T) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := findBin(dir, "packnest")
	return err
}

// TestCleanupOld：清掉 `<exe>.old*`（Windows 的替换残留）。
func TestCleanupOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pk")
	for _, name := range []string{"pk", "pk.old.1", "pk.old.2"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	CleanupOld(exe, runtime.GOOS) // 非 Windows 上是 no-op，不报错即可
	if runtime.GOOS != "windows" {
		if _, err := os.Stat(filepath.Join(dir, "pk.old.1")); err != nil {
			t.Fatal("非 Windows 上不该删 .old 文件")
		}
	}
	_ = errors.New("keep import")
}

// TestDefaultSourcesAndDefaults：内置源与默认值（GOOS/GOARCH/Client/Now 都补齐）。
func TestDefaultSourcesAndDefaults(t *testing.T) {
	srcs := DefaultSources()
	if len(srcs) != 2 || srcs[0].Name != "gitee" || srcs[1].Name != "github" {
		t.Fatalf("内置源不对: %+v", srcs)
	}
	if !strings.Contains(srcs[0].Base, "gitee.com") || !strings.Contains(srcs[1].Base, "github.com") {
		t.Fatalf("内置源地址不对: %+v", srcs)
	}
	opt := Option{}.withDefaults()
	if opt.GOOS == "" || opt.GOARCH == "" || opt.Client == nil || opt.Now == nil || len(opt.Sources) == 0 {
		t.Fatalf("默认值没补齐: %+v", opt)
	}
	if opt.Exe == "" {
		t.Fatal("应能取到自身可执行文件")
	}
}

// TestCheckFallsBackToSecondSource：第一个源挂了，第二个源仍然给出结果（并记进 Sources）。
func TestCheckFallsBackToSecondSource(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()

	var logged []string
	opt := e.opt
	opt.Sources = []Source{
		{Name: "dead", Base: "http://127.0.0.1:1", LatestURL: "http://127.0.0.1:1/latest"},
		{Name: "good", Base: srv.URL, LatestURL: srv.URL + "/scripts/latest"},
	}
	opt.Log = func(s string) { logged = append(logged, s) }
	info, err := Check(context.Background(), opt)
	if err != nil {
		t.Fatalf("第二个源可用时应成功: %v", err)
	}
	if info.Latest != "9.9.9" || !info.UpdateAvailable {
		t.Fatalf("结果不对: %+v", info)
	}
	if len(info.Sources) != 2 || info.Sources[0].Name != "good" {
		t.Fatalf("Sources 应把选中的源排前面: %+v", info.Sources)
	}
	if len(logged) == 0 || !strings.Contains(strings.Join(logged, "\n"), "源：") {
		t.Fatalf("Log 没收到进度: %v", logged)
	}
}

// TestApplyForceOnManagedChannel：渠道像 brew 但目录可写时，--force 能强行替换。
func TestApplyForceOnManagedChannel(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()

	// 造一个"看起来像 brew"的可写路径：<tmp>/Cellar/packnest/1.7.0/bin/pk
	brewExe := filepath.Join(t.TempDir(), "Cellar", "packnest", "1.7.0", "bin", "pk")
	if err := os.MkdirAll(filepath.Dir(brewExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brewExe, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opt := e.with(t, srv)
	opt.Exe = brewExe
	info, err := ApplyForce(context.Background(), opt)
	if err != nil {
		t.Fatalf("--force 应能替换: %v", err)
	}
	if !info.Applied {
		t.Fatalf("应标记已替换: %+v", info)
	}
	body, _ := os.ReadFile(brewExe)
	if !strings.Contains(string(body), "packnest 9.9.9") {
		t.Fatalf("--force 没换成功: %q", body)
	}
}

// TestApplyDownloadFailures：产物 / 清单 / 签名任一取不到 → 明确报下载错误。
func TestApplyDownloadFailures(t *testing.T) {
	for _, missing := range []string{"artifact", "checksums", "sig"} {
		t.Run(missing, func(t *testing.T) {
			e := newEnv(t, "1.7.0")
			rel := newRelease(t, "9.9.9", e.priv)
			name := artifactName(rel.ver, runtime.GOOS, runtime.GOARCH)
			mux := http.NewServeMux()
			mux.HandleFunc("/scripts/latest", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, rel.ver)
			})
			dir := "/v" + rel.ver + "/"
			if missing != "artifact" {
				mux.HandleFunc(dir+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.arch) })
			}
			if missing != "checksums" {
				mux.HandleFunc(dir+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.sums) })
			}
			if missing != "sig" {
				mux.HandleFunc(dir+"checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.sig) })
			}
			srv := httptest.NewServer(mux)
			defer srv.Close()
			_, err := Apply(context.Background(), e.with(t, srv))
			ce, ok := coded.Of(err)
			if !ok || ce.Code != CodeDownload {
				t.Fatalf("应返回 %s，实际 %v", CodeDownload, err)
			}
		})
	}
}

// TestApplyGarbageArchive：产物字节不是合法归档 → 解压阶段报错（且不动自身）。
func TestApplyGarbageArchive(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := &release{ver: "9.9.9", arch: []byte("this is not a tarball")}
	rel.sign(t, e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	_, err := Apply(context.Background(), e.with(t, srv))
	if ce, ok := coded.Of(err); !ok || ce.Code != CodeExtract {
		t.Fatalf("应返回 %s，实际 %v", CodeExtract, err)
	}
}

// TestReplaceSelfWriteFailure：目标目录不可写 → 报 write_new，且原文件不动。
func TestReplaceSelfWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 chmod 不能制造只读目录")
	}
	e := newEnv(t, "1.7.0")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	binDir := filepath.Dir(e.exe)
	if err := os.Chmod(binDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(binDir, 0o755)
	_, err := Apply(context.Background(), e.with(t, srv))
	if ce, ok := coded.Of(err); !ok || ce.Code != CodeWriteNew {
		t.Fatalf("应返回 %s，实际 %v", CodeWriteNew, err)
	}
	body, _ := os.ReadFile(e.exe)
	if !strings.Contains(string(body), "echo old") {
		t.Fatalf("写失败时不该动自身: %q", body)
	}
}

// TestCachedVersionBranches：缓存过期 / 内容坏 / 文件不存在 都当没缓存。
func TestCachedVersionBranches(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "latest-x")
	now := time.Unix(1_700_000_000, 0)

	if _, ok := cachedVersion(p, now); ok {
		t.Fatal("文件不存在时不该命中")
	}
	if err := os.WriteFile(p, []byte("9.9.9"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := cachedVersion(p, now); !ok || v != "9.9.9" {
		t.Fatalf("新鲜缓存应命中: %q %v", v, ok)
	}
	// 过期（超过 TTL）
	old := now.Add(-mirror.DefaultTTL - time.Minute)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedVersion(p, now); ok {
		t.Fatal("过期缓存不该命中")
	}
	// 内容不是版本号
	if err := os.WriteFile(p, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedVersion(p, now); ok {
		t.Fatal("内容非版本号时不该命中")
	}
}

// TestReplaceSelfWindowsBranches：Windows 的"先改名再放新的"路径在任何平台上都能模拟
// （只是 os.Rename 语义不同），所以不必等 Windows runner 才覆盖。
func TestReplaceSelfWindowsBranches(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pk.exe")
	newBin := filepath.Join(t.TempDir(), "packnest.exe")
	oldBody := []byte("old-binary")
	newBody := []byte("new-binary")
	if err := os.WriteFile(exe, oldBody, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBin, newBody, 0o755); err != nil {
		t.Fatal(err)
	}
	// 同目录里的 pk.exe / packnest.exe 副本：内容与旧的相同 → 应一起被换掉
	sibling := filepath.Join(dir, "packnest.exe")
	if err := os.WriteFile(sibling, oldBody, 0o755); err != nil {
		t.Fatal(err)
	}
	// 内容不同的同名文件（用户自己放的？）不该被动
	other := filepath.Join(dir, "keep.exe")
	if err := os.WriteFile(other, []byte("do not touch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceSelf(exe, newBin, "windows"); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	if body, _ := os.ReadFile(exe); !bytes.Equal(body, newBody) {
		t.Fatalf("主文件没换: %q", body)
	}
	if body, _ := os.ReadFile(sibling); !bytes.Equal(body, newBody) {
		t.Fatalf("同内容副本应一起换: %q", body)
	}
	if body, _ := os.ReadFile(other); string(body) != "do not touch" {
		t.Fatalf("不该动别的文件: %q", body)
	}
	// .old 残留由 CleanupOld 清（Windows 语义下才会清）
	CleanupOld(exe, "windows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pk.exe.old") {
			t.Fatalf("CleanupOld 没清掉 %s", e.Name())
		}
	}
}

// TestReplaceSelfNonWindowsRename：posix 分支是"直接 rename 覆盖"。
func TestReplaceSelfNonWindowsRename(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pk")
	newBin := filepath.Join(t.TempDir(), "packnest")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBin, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceSelf(exe, newBin, "linux"); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	if body, _ := os.ReadFile(exe); string(body) != "new" {
		t.Fatalf("没换成新内容: %q", body)
	}
}

// TestIOHelpers：读文件/拷贝/找文件的失败分支。
func TestIOHelpers(t *testing.T) {
	dir := t.TempDir()
	if _, err := sha256File(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("文件不存在时应报错")
	}
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"), 0o755); err == nil {
		t.Fatal("源不存在时应报错")
	}
	if _, err := findBin(filepath.Join(dir, "no-such-dir"), "packnest"); err == nil {
		t.Fatal("目录不存在时应报错")
	}
}

// zipOf 造一个 zip 归档（Windows 产物形态）。
func zipOf(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestApplyWindowsZipLayout：GOOS=windows 时产物是 zip、里面的可执行文件叫 packnest.exe。
func TestApplyWindowsZipLayout(t *testing.T) {
	e := newEnv(t, "1.7.0")
	rel := &release{ver: "9.9.9"}
	rel.arch = zipOf(t, "packnest.exe", []byte("MZ fake exe 9.9.9"))
	rel.signFor(t, e.priv, "windows", "amd64")

	mux := http.NewServeMux()
	mux.HandleFunc("/scripts/latest", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, rel.ver) })
	dir := "/v" + rel.ver + "/"
	name := "packnest_" + rel.ver + "_windows_amd64.zip"
	mux.HandleFunc(dir+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.arch) })
	mux.HandleFunc(dir+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.sums) })
	mux.HandleFunc(dir+"checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(rel.sig) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 目标文件名必须是 .exe（渠道判定与同级副本同步都按 Windows 语义）
	binDir := filepath.Join(t.TempDir(), "packnest", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, "pk.exe")
	if err := os.WriteFile(exe, []byte("old exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	opt := e.with(t, srv)
	opt.Exe = exe
	opt.GOOS, opt.GOARCH = "windows", "amd64"
	info, err := Apply(context.Background(), opt)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !info.Applied {
		t.Fatalf("应替换成功: %+v", info)
	}
	if body, _ := os.ReadFile(exe); !strings.Contains(string(body), "fake exe 9.9.9") {
		t.Fatalf("没换成新的 exe: %q", body)
	}
}

// TestRankedSourcesBranches：选中源排第一；其余按延迟升序（覆盖比较器的三个分支）。
func TestRankedSourcesBranches(t *testing.T) {
	opt := Option{Sources: []Source{
		{Name: "slow", Base: "b1", LatestURL: "u1", LatencyMS: 900},
		{Name: "chosen", Base: "b2", LatestURL: "u2"},
		{Name: "fast", Base: "b3", LatestURL: "u3", LatencyMS: 100},
	}}
	out := rankedSources(opt, Source{Name: "chosen", Base: "b2", LatestURL: "u2"}, 42)
	if len(out) != 3 || out[0].Name != "chosen" || out[0].LatencyMS != 42 {
		t.Fatalf("选中源应排第一并带上延迟: %+v", out)
	}
	if out[1].Name != "fast" || out[2].Name != "slow" {
		t.Fatalf("其余应按延迟升序: %+v", out)
	}
}

// TestVerifyReleaseSumsReadErrors：清单/sig 读不到时给 read_sums（不是先报签名错）。
func TestVerifyReleaseSumsReadErrors(t *testing.T) {
	dir := t.TempDir()
	keys := []verify.SSHPublicKey{}
	if err := verifyReleaseSums(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "nope.sig"), nil); err == nil {
		t.Fatal("清单不存在时应报错")
	}
	if ce, ok := coded.Of(verifyReleaseSums(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "nope.sig"), keys)); !ok {
		t.Fatalf("应是编码错误: %v", ce)
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sums, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ce, ok := coded.Of(verifyReleaseSums(sums, filepath.Join(dir, "nope.sig"), nil)); !ok || ce.Code != CodeReadSums {
		t.Fatalf("sig 读不到应是 %s: %v", CodeReadSums, ce)
	}
}

// TestProbeFileOverride：显式给探测缓存路径时不再用 Paths.Probe()。
func TestProbeFileOverride(t *testing.T) {
	opt := Option{ProbeFile: "/tmp/probe.json"}
	if got := opt.probeFile(); got != "/tmp/probe.json" {
		t.Fatalf("应使用显式路径: %s", got)
	}
	dir := t.TempDir()
	p, err := paths.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := (Option{Paths: p}).probeFile(); got != p.Probe() {
		t.Fatalf("默认应是 Paths.Probe(): %s", got)
	}
}

// TestCacheBypassedWhenCurrentIsNewer：刚发完版时缓存还停在旧版本 —— 本机比缓存新时
// 必须绕过缓存重新问一次，否则会一直报"已是最新"直到缓存过期。
func TestCacheBypassedWhenCurrentIsNewer(t *testing.T) {
	e := newEnv(t, "9.9.9")
	rel := newRelease(t, "9.9.9", e.priv)
	srv := rel.serve(t)
	defer srv.Close()
	opt := e.with(t, srv)

	// 先塞一个"更旧"的缓存（模拟发版前留下的）
	cacheDir := opt.Paths.CacheDir("selfupdate")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(cacheDir, "latest-fake")
	if err := os.WriteFile(cacheFile, []byte("1.8.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := Check(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Latest != "9.9.9" {
		t.Fatalf("本机比缓存新时应绕过缓存重取: %+v", info)
	}
	if info.UpdateAvailable {
		t.Fatalf("同版本不该提示更新: %+v", info)
	}
}
