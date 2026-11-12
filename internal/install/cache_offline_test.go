package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
	"github.com/packnest/packnest/internal/verify"
)

// 两个回归点（2026-11-11 review 时发现）：
//  1. 离线重装：TOFU 表里记过的版本不该再去问网络（pnpm 的平台包元数据曾经每次都拉）；
//  2. 校验失败时不能把缓存删掉 —— 只有"摘要不符"才算缓存脏了，网络/离线失败必须留住缓存。

// cachedInstaller 造一个"缓存里已经有产物"的安装器（客户端离线）。
func cachedInstaller(t *testing.T, offline bool) (*Installer, *testkit.FakeTool, string) {
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
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := fetch.New()
	client.Offline = offline
	inst, err := New(Options{
		Paths: p, Client: client, Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64",
		LookupProvider: func(string) (*provider.Provider, error) { return ft.Provider, nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 把产物放进缓存（文件名规则与 fetchArtifact 一致）
	if err := os.MkdirAll(p.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(p.CacheArchives(), ft.Provider.ID+"-1.0.0-darwin-arm64.zip")
	if err := os.WriteFile(cache, ft.Artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	return inst, ft, cache
}

// TestFetchVerifiedOfflineKeepsCache：离线 + 拿不到校验和 → 报错但缓存必须还在（否则永远救不回来）。
func TestFetchVerifiedOfflineKeepsCache(t *testing.T) {
	inst, ft, cache := cachedInstaller(t, true)
	_, _, _, _, err := inst.fetchArtifactVerified(context.Background(), ft.Provider, provider.Version{Version: "1.0.0"}, nil)
	if err == nil {
		t.Fatal("离线且没有 TOFU 记录时应该失败")
	}
	if !errors.Is(err, verify.ErrChecksum) { // 离线时表现为"拿不到校验和"，但缓存必须留住
		t.Fatalf("错误应是校验类错误: %v", err)
	}
	if _, statErr := os.Stat(cache); statErr != nil {
		t.Fatalf("离线失败不该删缓存: %v", statErr)
	}
}

// TestFetchVerifiedTOFUSkipsNetwork：TOFU 表里已有该版本 → 离线也能装上（走缓存、零网络）。
func TestFetchVerifiedTOFUSkipsNetwork(t *testing.T) {
	inst, ft, _ := cachedInstaller(t, true)
	digest, err := verify.DigestFile(filepath.Join(inst.opt.Paths.CacheArchives(), ft.Provider.ID+"-1.0.0-darwin-arm64.zip"), verify.AlgoSHA256)
	if err != nil {
		t.Fatal(err)
	}
	inst.known.Entries[verify.Key(ft.Provider.ID, "1.0.0", "darwin-arm64")] =
		verify.Entry{Digest: digest, Algo: verify.AlgoSHA256, Verified: verify.VerifiedWeak, Sources: 1}

	_, _, sha, vr, err := inst.fetchArtifactVerified(context.Background(), ft.Provider, provider.Version{Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("TOFU 命中时离线应该能装: %v", err)
	}
	if sha != digest || vr.Mode != "tofu" {
		t.Fatalf("应走 TOFU: sha=%s mode=%s", sha, vr.Mode)
	}
}

// TestCheckMinVersion：产物形态换了的老版本必须**在解析后立刻**报错并给建议，而不是撞产物 404。
func TestCheckMinVersion(t *testing.T) {
	p := &provider.Provider{ID: "pnpm", Artifact: provider.ArtifactBlock{MinVersion: "11.0.0"}}
	err := checkMinVersion(p, "10.4.1")
	if err == nil || !strings.Contains(err.Error(), "11.0.0") || !strings.Contains(err.Error(), "npm i -g pnpm@10.4.1") {
		t.Fatalf("应报最低版本并给建议: %v", err)
	}
	if err := checkMinVersion(p, "11.0.0"); err != nil {
		t.Fatalf("等于下限应放行: %v", err)
	}
	if err := checkMinVersion(p, "12.19.0"); err != nil {
		t.Fatalf("高于下限应放行: %v", err)
	}
	if err := checkMinVersion(&provider.Provider{ID: "x"}, "1.0.0"); err != nil {
		t.Fatalf("没声明下限就不管: %v", err)
	}
}
