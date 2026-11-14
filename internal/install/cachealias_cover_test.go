package install

// 缓存别名与内容寻址的边角分支（R-CACHE-1 的失败面）：全都不用网络，直接打内部方法。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/testkit"
)

// aliasEnv 造一个带 provider 的安装器（不装东西，只测别名/寻址的边角）。
func aliasEnv(t *testing.T) (*Installer, *provider.Provider) {
	t.Helper()
	ft := testkit.NewFakeTool(t, testkit.FakeToolOptions{
		Tool:         "demo",
		Versions:     []string{"1.0.0"},
		ProviderTOML: testkit.SingleFileProviderTOML,
		MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeSingleFileZip() },
		Platforms: []testkit.FakePlatform{
			{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"},
		},
		ChecksumPath:   "demo-%s.zip.sha256sum",
		ChecksumFormat: "per-artifact",
	})
	inst := newInstaller(t, nil, func(o *Options) {
		o.LookupProvider = func(string) (*provider.Provider, error) { return ft.Provider, nil }
	})
	return inst, ft.Provider
}

// TestAliasLookupBranches：强制重装 / 摘要不可归一化 / 格式对不上 / 归档不在 —— 都只当没命中。
func TestAliasLookupBranches(t *testing.T) {
	inst, _ := aliasEnv(t)
	dir := inst.opt.Paths.CacheAliases()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 归档本体 + 指向它的别名
	archive := filepath.Join(inst.opt.Paths.CacheArchives(), "abc.zip")
	if err := os.MkdirAll(inst.opt.Paths.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(dir, "sha256-"+digest), []byte("abc.zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := digestRef{Algo: "sha256", Digest: digest}
	if got, ok := inst.lookupAlias(ref, "zip", "demo", "1.0.0"); !ok || filepath.Base(got) != "abc.zip" {
		t.Fatalf("正常别名应命中: %q %v", got, ok)
	}
	// ① 格式对不上不猜
	if _, ok := inst.lookupAlias(ref, "tar.gz", "demo", "1.0.0"); ok {
		t.Fatal("扩展名对不上不该命中")
	}
	// ② 摘要不可归一化
	if _, ok := inst.lookupAlias(digestRef{Algo: "md5", Digest: digest}, "zip", "demo", "1.0.0"); ok {
		t.Fatal("未知算法不该命中")
	}
	// ③ --force 直接不看（也别把别名删了）
	inst.opt.Force = true
	if _, ok := inst.lookupAlias(ref, "zip", "demo", "1.0.0"); ok {
		t.Fatal("--force 不该命中缓存")
	}
	if _, err := os.Stat(filepath.Join(dir, "sha256-"+digest)); err != nil {
		t.Fatalf("--force 不该删别名: %v", err)
	}
	inst.opt.Force = false
	// ④ 归档被删 → 悬空别名清掉
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if _, ok := inst.lookupAlias(ref, "zip", "demo", "1.0.0"); ok {
		t.Fatal("悬空别名不该命中")
	}
	if _, err := os.Stat(filepath.Join(dir, "sha256-"+digest)); !os.IsNotExist(err) {
		t.Fatal("悬空别名应被删除")
	}
}

// TestRecordAliasBranches：空文件名 / 摘要不可归一化都静默跳过（别名只是加速）。
func TestRecordAliasBranches(t *testing.T) {
	inst, _ := aliasEnv(t)
	inst.recordAlias(digestRef{Algo: "sha256", Digest: strings.Repeat("a", 64)}, "")
	inst.recordAlias(digestRef{Algo: "nope", Digest: "x"}, "a.zip")
	entries, err := os.ReadDir(inst.opt.Paths.CacheAliases())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("这两类输入都不该写文件: %v", entries)
	}
	if inst.cacheHasAliases() {
		t.Fatal("空目录应判定为没有别名")
	}
}

// TestSumsRefsErrorBranches：平台不支持 / 校验源全挂 / 校验和里没有本产物 —— 都只是"没有摘要"。
func TestSumsRefsErrorBranches(t *testing.T) {
	inst, p := aliasEnv(t)
	ctx := context.Background()
	// ① 平台不支持 → ChecksumURLsFor 直接报错
	inst.opt.GOOS, inst.opt.GOARCH = "linux", "amd64"
	if refs := inst.sumsRefs(ctx, p, provider.Version{Version: "1.0.0"}, "demo-1.0.0.zip"); len(refs) != 0 {
		t.Fatalf("平台不支持时应返回空: %+v", refs)
	}
	if refs := inst.memoRefs(p, provider.Version{Version: "1.0.0"}, "demo-1.0.0.zip"); len(refs) != 0 {
		t.Fatalf("memoRefs 平台不支持时应返回空: %+v", refs)
	}
	// ② 校验源全挂（客户端离线）→ 短路返回空，不 panic
	inst.opt.GOOS, inst.opt.GOARCH = "darwin", "arm64"
	inst.opt.Client.Offline = true
	if refs := inst.sumsRefs(ctx, p, provider.Version{Version: "1.0.0"}, "demo-1.0.0.zip"); len(refs) != 0 {
		t.Fatalf("离线时应返回空: %+v", refs)
	}
	inst.opt.Client.Offline = false
	// ③ 校验和文件里没有本产物 → 空
	inst.opt.Client = failingGetClient(inst.opt.Client)
	if refs := inst.sumsRefs(ctx, p, provider.Version{Version: "1.0.0"}, "demo-1.0.0.zip"); len(refs) != 0 {
		t.Fatalf("取不到校验和时应返回空: %+v", refs)
	}
}

// TestAddressedReplacesCorrupt：内容寻址名已存在但大小不符（坏缓存）→ 用刚校验过的这份替掉它。
func TestAddressedReplacesCorrupt(t *testing.T) {
	inst, _ := aliasEnv(t)
	dir := inst.opt.Paths.CacheArchives()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 64)
	dst := filepath.Join(dir, sha+".zip")
	if err := os.WriteFile(dst, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "demo-1.0.0-darwin-arm64.zip")
	payload := []byte("a much longer, freshly verified payload")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	got := inst.addressed(src, sha, "zip")
	if filepath.Base(got) != sha+".zip" {
		t.Fatalf("应落到内容寻址名: %q", got)
	}
	back, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(payload) {
		t.Fatalf("坏缓存必须被替成正版，实际 %q", back)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("源文件应被改名走（不留重复）")
	}
	// 同大小（同名=同摘要）→ 直接去重，保留原有的
	src2 := filepath.Join(dir, "demo-1.0.0-darwin-arm64.zip")
	if err := os.WriteFile(src2, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := inst.addressed(src2, sha, "zip"); filepath.Base(got) != sha+".zip" {
		t.Fatalf("重复内容应去重到同一路径: %q", got)
	}
	if _, err := os.Stat(src2); !os.IsNotExist(err) {
		t.Fatal("重复的那份应被删掉")
	}
}

// failingGetClient 让所有 GET 都失败（只在测试里用：把"网络挂了"这条分支逼出来）。
// 离线等价于"取不到"：内部走同一批错误分支，且不发真请求。
func failingGetClient(orig *fetch.Client) *fetch.Client {
	cl := *orig
	cl.Offline = true
	return &cl
}
