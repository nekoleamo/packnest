package install

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// TestContentAddressedCacheDedupes：两个工具发布**同样的字节**时，缓存里只留一份 <sha256>.zip。
func TestContentAddressedCacheDedupes(t *testing.T) {
	mk := func(providerID string) *provider.Provider {
		ft := testkit.NewFakeTool(t, testkit.FakeToolOptions{
			Tool:         "singlefile", // 镜像上的路径仍叫 singlefile，只有 provider id 不同
			Versions:     []string{"1.0.0"},
			ProviderTOML: testkit.SingleFileProviderTOML,
			MakeArtifact: func(platform, _ string) []byte { return testkit.MakeFakeSingleFileZip() },
			Platforms:    []testkit.FakePlatform{{Goos: "darwin", Goarch: "arm64", Ext: "zip", Name: "aarch64-apple-darwin"}},
			ChecksumPath: "singlefile-%s.zip.sha256sum", ChecksumFormat: "per-artifact",
		})
		toml := strings.Replace(testkit.SingleFileProviderTOML(ft.Server.URL), `id = "singlefile"`, `id = "`+providerID+`"`, 1)
		p, err := provider.Parse([]byte(toml))
		if err != nil {
			t.Fatalf("parse %s: %v", providerID, err)
		}
		return p
	}
	providers := map[string]*provider.Provider{"demo": mk("demo"), "demo2": mk("demo2")}
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inst, err := New(Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(id string) (*provider.Provider, error) { return providers[id], nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, id := range []string{"demo", "demo2"} {
		if _, err := inst.Install(context.Background(), id, "1.0.0"); err != nil {
			t.Fatalf("装 %s: %v", id, err)
		}
	}
	entries, err := os.ReadDir(p.CacheArchives())
	if err != nil {
		t.Fatal(err)
	}
	var zips int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			zips++
		}
	}
	if zips != 1 {
		t.Fatalf("同样字节只该留一份缓存，实际 %d 份: %v", zips, entries)
	}
}

// TestCacheLookupViaLedger：--insecure 因"源矛盾"放行的安装不写 TOFU，但台账有 SHA256，
// 缓存仍要能按内容命中（否则每次重装都重下）。
func TestCacheLookupViaLedger(t *testing.T) {
	inst, ft, cache := cachedInstaller(t, true) // 离线客户端 + 缓存里已有产物
	sha, err := verify.DigestFile(cache, verify.AlgoSHA256)
	if err != nil {
		t.Fatal(err)
	}
	// 只写台账（模拟 --insecure 放行后的状态），不写 TOFU
	inst.ledger.Upsert(store.Install{Tool: ft.Provider.ID, Version: "1.0.0", Path: "/tmp/x", SHA256: sha})
	if got := inst.knownDigest(ft.Provider.ID, "1.0.0", "darwin-arm64"); got != sha {
		t.Fatalf("knownDigest 应回落到台账: %q", got)
	}
}
