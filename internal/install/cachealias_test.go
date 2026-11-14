package install

// R-CACHE-1 的验收：**新机器（state 全空）+ 热缓存**不该再下那 50MB。
//
// 场景就是 CI：`actions/cache` 只搬 PK_CACHE（archives/meta/alias），state 留在 runner 上，
// 而每个 job 都是全新 runner。缓存别名必须跟着缓存走，否则内容寻址的归档永远找不回来。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// eventRecorder 收集事件（判定"有没有真下载"）。
type eventRecorder struct {
	evs []jsonout.Event
}

func (r *eventRecorder) hook(ev jsonout.Event) { r.evs = append(r.evs, ev) }

func (r *eventRecorder) count(kind string) int {
	n := 0
	for _, ev := range r.evs {
		if ev.Ev == kind {
			n++
		}
	}
	return n
}

// freshInstaller 造一个新的安装器（模拟"新 runner"：state 目录全空，只有缓存还在）。
func freshInstaller(t *testing.T, fm *testkit.FakeNode, dir string, rec *eventRecorder) *Installer {
	t.Helper()
	root := t.TempDir()
	// 只搬缓存（归档 + 别名），state 一律不搬。
	cache := filepath.Join(dir, "cache")
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTree(cache, p.Cache()); err != nil {
		t.Fatalf("搬缓存: %v", err)
	}
	inst, err := New(Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", LookupProvider: fakeLookup(fm),
		Events: rec.hook,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return inst
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// TestCacheAliasReuseRegistry：registry 摘要（npm 的 dist.integrity/shasum）也能当缓存钥匙。
func TestCacheAliasReuseRegistry(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	ctx := context.Background()
	first := newInstaller(t, fm, nil)
	if _, err := first.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatal(err)
	}
	// 别名必须落在**缓存**目录里（跟着 PK_CACHE 走），不能落在 state。
	aliases, err := os.ReadDir(first.opt.Paths.CacheAliases())
	if err != nil || len(aliases) == 0 {
		t.Fatalf("缓存别名应有内容: %v (n=%d)", err, len(aliases))
	}
	if !strings.HasPrefix(first.opt.Paths.CacheAliases(), first.opt.Paths.Cache()) {
		t.Fatalf("别名目录必须在缓存根下: %s", first.opt.Paths.CacheAliases())
	}

	rec := &eventRecorder{}
	second := freshInstaller(t, fm, first.opt.Paths.Root, rec)
	res, err := second.Install(ctx, "npm", "12.2.2")
	if err != nil {
		t.Fatalf("新机器 + 热缓存应能直接装: %v", err)
	}
	if rec.count("download") != 0 {
		t.Fatalf("不该再下载产物（已按上游摘要命中缓存），download 事件 %d 次", rec.count("download"))
	}
	if rec.count("cache") == 0 {
		t.Fatal("命中缓存时应发 cache 事件")
	}
	// cache 事件的 tool 必须是工具名（GUI 按 tool 分组画进度），不能是 `<sha256>.tar.gz`
	for _, ev := range rec.evs {
		if ev.Ev == "cache" && ev.Tool != "npm" {
			t.Fatalf("cache 事件的 tool 应是工具名，实际 %q", ev.Tool)
		}
	}
	if res.SHA256 == "" {
		t.Fatal("结果里应带 sha256")
	}
}

// TestCacheAliasReuseGNUSums：gnu-sums 型的工具（node）靠一次几 KB 的 SHASUMS256.txt 命中缓存。
func TestCacheAliasReuseGNUSums(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "26.13.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	ctx := context.Background()
	first := newInstaller(t, fm, nil)
	if _, err := first.Install(ctx, "node", "26.13.0"); err != nil {
		t.Fatal(err)
	}
	rec := &eventRecorder{}
	second := freshInstaller(t, fm, first.opt.Paths.Root, rec)
	if _, err := second.Install(ctx, "node", "26.13.0"); err != nil {
		t.Fatalf("新机器 + 热缓存应能直接装: %v", err)
	}
	// node 与伴生 npm 都该命中缓存：一个产物字节都不该重新下。
	if rec.count("download") != 0 {
		t.Fatalf("node/npm 都该命中缓存，download 事件 %d 次", rec.count("download"))
	}
	if rec.count("cache") == 0 {
		t.Fatal("命中缓存时应发 cache 事件")
	}
}

// TestCacheAliasForceBypass：--force 不看缓存，也不会把好别名当悬空删掉。
func TestCacheAliasForceBypass(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	ctx := context.Background()
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(inst.opt.Paths.CacheAliases())
	inst.opt.Force = true
	rec := &eventRecorder{}
	inst.opt.Events = rec.hook
	if _, err := inst.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatal(err)
	}
	if rec.count("download") == 0 {
		t.Fatal("--force 必须重新下载")
	}
	after, _ := os.ReadDir(inst.opt.Paths.CacheAliases())
	if len(after) < len(before) {
		t.Fatalf("--force 不该删掉别名: before=%d after=%d", len(before), len(after))
	}
}

// TestCacheAliasDanglingAndDirty：悬空别名与脏别名都只当没命中，并且会被清掉。
func TestCacheAliasDanglingAndDirty(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.98.0", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	ctx := context.Background()
	inst := newInstaller(t, fm, nil)
	if _, err := inst.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatal(err)
	}
	dir := inst.opt.Paths.CacheAliases()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("应已有别名: %v", err)
	}
	// ① 悬空：别名指向一个不存在的归档。
	dangling := filepath.Join(dir, "sha256-"+strings.Repeat("a", 64))
	if err := os.WriteFile(dangling, []byte("nope.zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ② 脏：别名里塞了路径（防止被缓存目录里的东西牵着走）。
	dirty := filepath.Join(dir, "sha1-"+strings.Repeat("b", 40))
	if err := os.WriteFile(dirty, []byte("../../etc/passwd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, hit := inst.lookupAlias(digestRef{Algo: "sha256", Digest: strings.Repeat("a", 64)}, "zip", "demo", "1.0.0"); hit {
		t.Fatal("悬空别名不该命中")
	}
	if _, hit := inst.lookupAlias(digestRef{Algo: "sha1", Digest: strings.Repeat("b", 40)}, "zip", "demo", "1.0.0"); hit {
		t.Fatal("脏别名不该命中")
	}
	if _, err := os.Stat(dirty); !os.IsNotExist(err) {
		t.Fatalf("脏别名应被删掉: %v", err)
	}
	if _, err := os.Stat(dangling); !os.IsNotExist(err) {
		t.Fatalf("悬空别名应被删掉: %v", err)
	}
	// 别名坏掉不影响安装：照常下。
	if _, err := inst.Install(ctx, "npm", "12.2.2"); err != nil {
		t.Fatalf("别名脏了不该影响安装: %v", err)
	}
}

// TestAliasKeyNormalization：三套算法都能折成文件名安全的键，坏输入返回空。
func TestAliasKeyNormalization(t *testing.T) {
	if got := verify.AliasKey("sha256", strings.Repeat("A", 64)); got != "sha256-"+strings.Repeat("a", 64) {
		t.Fatalf("sha256 应小写十六进制: %q", got)
	}
	if got := verify.AliasKey("sha1", strings.Repeat("c", 40)); got != "sha1-"+strings.Repeat("c", 40) {
		t.Fatalf("sha1 应原样: %q", got)
	}
	// SRI 的 sha512 会被折成 128 位十六进制（去掉 base64 的 +/= 之类字符）。
	if got := verify.AliasKey("sha512", "sha512-i9JC/LJGL5vE+1UebxIf3VGfh85z8m55HVgNrJqlcAfZ/vseOM30ZHL8HZqZUzMLe9W5hG+ZtiFvIMN6LnP2Tg=="); !strings.HasPrefix(got, "sha512-") || len(got) != len("sha512-")+128 {
		t.Fatalf("sha512 应折成 128 位十六进制: %q", got)
	}
	for _, bad := range []struct{ algo, digest string }{
		{"sha256", "not-hex"},
		{"sha512", "sha512-zzz"},
		{"md5", strings.Repeat("a", 64)},
	} {
		if got := verify.AliasKey(bad.algo, bad.digest); got != "" {
			t.Fatalf("%s/%s 应返回空: %q", bad.algo, bad.digest, got)
		}
	}
}

// TestRefsFromSums：三种校验源都能被翻译成「上游摘要」候选。
func TestRefsFromSums(t *testing.T) {
	sum := strings.Repeat("d", 64)
	// gnu-sums：一行 <sha256>  <文件名>
	gnu := []byte(sum + "  node-v22.0.0-darwin-arm64.tar.gz\n")
	refs := refsFromSums("gnu-sums", gnu, "22.0.0", "node-v22.0.0-darwin-arm64.tar.gz")
	if len(refs) != 1 || refs[0].Algo != "sha256" || refs[0].Digest != sum {
		t.Fatalf("gnu-sums 应给出 sha256: %+v", refs)
	}
	if refs := refsFromSums("gnu-sums", gnu, "22.0.0", "别的文件.tar.gz"); len(refs) != 0 {
		t.Fatalf("文件名对不上就不该给摘要: %+v", refs)
	}
	// per-artifact：整份文件只有一个哈希
	if refs := refsFromSums("per-artifact", []byte(sum+"\n"), "1.0.0", "x.zip"); len(refs) != 1 || refs[0].Digest != sum {
		t.Fatalf("per-artifact 应给出 sha256: %+v", refs)
	}
	// registry 平台包元数据：integrity（sha512 SRI）+ shasum（sha1）都算钥匙
	packument := []byte(`{"versions":{"10.4.1":{"version":"10.4.1","dist":{"integrity":"sha512-AAAA","shasum":"` + strings.Repeat("e", 40) + `"}}}}`)
	refs = refsFromSums(provider.ChecksumRegistry, packument, "10.4.1", "unused.tgz")
	if len(refs) != 2 {
		t.Fatalf("registry 元数据应给出两种摘要: %+v", refs)
	}
	if refs[0].Algo != "sha512" || refs[1].Algo != "sha1" {
		t.Fatalf("顺序应是 integrity 优先: %+v", refs)
	}
	// 坏 JSON / 未知格式都不该 panic
	if refs := refsFromSums(provider.ChecksumRegistry, []byte("not json"), "1.0.0", "x"); len(refs) != 0 {
		t.Fatalf("坏元数据应返回空: %+v", refs)
	}
	if refs := refsFromSums("index-inline", gnu, "1.0.0", "x"); len(refs) != 0 {
		t.Fatalf("索引内联格式这里不该给别名: %+v", refs)
	}
}

// TestPublishedRefsFromIndex：索引自带的摘要（go 的 file.sha256 / registry 的 dist）都能直接用。
func TestPublishedRefsFromIndex(t *testing.T) {
	resolved := provider.Version{
		Version: "1.27.3",
		Files:   []provider.IndexFile{{OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("f", 64)}},
	}
	idx := &provider.Index{Versions: []provider.Version{{
		Version:   "1.27.3",
		Integrity: "sha512-QQQQ",
		Shasum:    strings.Repeat("a", 40),
	}}}
	refs := publishedRefs(resolved, idx, "darwin", "arm64", "1.27.3")
	if len(refs) != 3 {
		t.Fatalf("应给出 3 个摘要候选（sha256 + sha512 + sha1）: %+v", refs)
	}
	if refs[0].Algo != "sha256" || refs[1].Algo != "sha512" || refs[2].Algo != "sha1" {
		t.Fatalf("顺序应是「索引内联优先」: %+v", refs)
	}
	// 平台不匹配时不该瞎给摘要
	if refs := publishedRefs(resolved, nil, "linux", "amd64", "1.27.3"); len(refs) != 0 {
		t.Fatalf("平台不匹配应返回空: %+v", refs)
	}
}
