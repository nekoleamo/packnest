package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/testkit"
)

func TestParseGNUSums(t *testing.T) {
	data := []byte(`# comment
ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  node-v22.22.2-darwin-arm64.tar.gz
BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD *node-v22.22.2-win-x64.zip
not-a-hash  junk
short  x
`)
	sums := ParseGNUSums(data)
	if len(sums) != 2 {
		t.Fatalf("应解析出 2 条: %v", sums)
	}
	got := sums["node-v22.22.2-darwin-arm64.tar.gz"]
	if got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("哈希应小写归一: %q", got)
	}
	if sums["node-v22.22.2-win-x64.zip"] == "" {
		t.Fatal("`*文件名` 形式应支持")
	}
}

func TestParsePerArtifact(t *testing.T) {
	sum := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got, ok := ParsePerArtifact([]byte(sum + "  deno.zip\n")); !ok || got != sum {
		t.Fatalf("ParsePerArtifact = %q/%v", got, ok)
	}
	if _, ok := ParsePerArtifact([]byte("nonsense")); ok {
		t.Fatal("非法内容应返回 false")
	}
}

func TestParseRegistryShasum(t *testing.T) {
	if got := NormalizeRegistryShasum(" BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD "); got != strings.ToLower(got) {
		t.Fatalf("应小写归一: %q", got)
	}
}

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SHA256File(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("SHA256File = %q", got)
	}
	if _, err := SHA256File(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在应报错")
	}
}

func TestKnownRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known.json")
	k, err := LoadKnown(path)
	if err != nil || k.Schema != KnownSchema || len(k.Entries) != 0 {
		t.Fatalf("缺失表应返回空表: %+v %v", k, err)
	}
	if err := k.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadKnown(path)
	if err != nil || back.Schema != KnownSchema {
		t.Fatalf("往返失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKnown(path); err == nil {
		t.Fatal("损坏表应报错")
	}
}

// fakeFetcher 按 URL 返回内容（不存在的 URL 报错），并记录调用次数。
type fakeFetcher struct {
	files map[string][]byte
	calls int
}

func (f *fakeFetcher) fetch(_ context.Context, url string) ([]byte, error) {
	f.calls++
	if data, ok := f.files[url]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("404: %s", url)
}

func TestVerifyFirstInstallStrong(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	ff := &fakeFetcher{files: map[string][]byte{
		"https://a/SHASUMS256.txt": []byte(sum + "  node.tar.gz\n"),
		"https://b/SHASUMS256.txt": []byte(sum + "  node.tar.gz\n"),
	}}
	v := New(nil)
	v.Fetch = ff.fetch

	res, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "22.22.2", Platform: "darwin-arm64",
		Artifact: "node.tar.gz", Digest: sum, Format: "gnu-sums",
		SumsURLs: []string{"https://a/SHASUMS256.txt", "https://b/SHASUMS256.txt"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Verified != VerifiedStrong || res.Mode != "network" || res.Sources != 2 {
		t.Fatalf("应 strong/network/2: %+v", res)
	}
	if entry, ok := v.Known.Get("node@22.22.2/darwin-arm64"); !ok || entry.Digest != sum {
		t.Fatalf("应写入 TOFU 表: %+v", entry)
	}
}

func TestVerifySingleSourceWeak(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	ff := &fakeFetcher{files: map[string][]byte{
		"https://a/SHASUMS256.txt": []byte(sum + "  node.tar.gz\n"),
		"https://dead/x":           nil,
	}}
	v := New(nil)
	v.Fetch = ff.fetch
	res, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Artifact: "node.tar.gz",
		Digest: sum, Format: "gnu-sums",
		SumsURLs: []string{"https://a/SHASUMS256.txt", "https://dead/x"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Verified != VerifiedWeak || res.Sources != 1 {
		t.Fatalf("单源应 weak: %+v", res)
	}
}

func TestVerifyMismatchFails(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	ff := &fakeFetcher{files: map[string][]byte{
		"https://a/SHASUMS256.txt": []byte(testkit.Sha256Hex([]byte("evil")) + "  node.tar.gz\n"),
	}}
	v := New(nil)
	v.Fetch = ff.fetch
	_, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Artifact: "node.tar.gz",
		Digest: sum, Format: "gnu-sums", SumsURLs: []string{"https://a/SHASUMS256.txt"},
	})
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("哈希不符应返回 ErrChecksum，得到 %v", err)
	}
}

func TestVerifySourcesDisagree(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	other := testkit.Sha256Hex([]byte("other"))
	ff := &fakeFetcher{files: map[string][]byte{
		"https://a/SHASUMS256.txt": []byte(sum + "  node.tar.gz\n"),
		"https://b/SHASUMS256.txt": []byte(other + "  node.tar.gz\n"),
	}}
	v := New(nil)
	v.Fetch = ff.fetch
	_, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Artifact: "node.tar.gz",
		Digest: sum, Format: "gnu-sums",
		SumsURLs: []string{"https://a/SHASUMS256.txt", "https://b/SHASUMS256.txt"},
	})
	if !errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), "--insecure") {
		t.Fatalf("源之间不一致应硬失败并给出处置: %v", err)
	}
	// --insecure 可以放行，但必须标记 unverified
	res, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "2.0.0", Platform: "linux-amd64", Artifact: "node.tar.gz",
		Digest: sum, Algo: AlgoSHA256, Format: "gnu-sums",
		SumsURLs: []string{"https://a/SHASUMS256.txt", "https://b/SHASUMS256.txt"},
		Insecure: true,
	})
	if err != nil || res.Verified != VerifiedUnverified || res.Mode != "insecure" {
		t.Fatalf("--insecure 应放行并标记 unverified: %+v %v", res, err)
	}
}

func TestVerifyTOFUOffline(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	known := &Known{Schema: KnownSchema, Entries: map[string]Entry{
		"node@1.0.0/linux-amd64": {Digest: sum, Verified: VerifiedStrong, Sources: 2},
	}}
	ff := &fakeFetcher{}
	v := New(known)
	v.Fetch = ff.fetch
	res, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Digest: sum,
	})
	if err != nil {
		t.Fatalf("已知版本应离线通过: %v", err)
	}
	if res.Mode != "tofu" || res.Verified != VerifiedStrong {
		t.Fatalf("应走 tofu: %+v", res)
	}
	if ff.calls != 0 {
		t.Fatalf("TOFU 命中不该发请求，实际 %d 次", ff.calls)
	}
}

func TestVerifyTOFUMismatch(t *testing.T) {
	known := &Known{Schema: KnownSchema, Entries: map[string]Entry{
		"node@1.0.0/linux-amd64": {Digest: strings.Repeat("a", 64), Verified: VerifiedStrong},
	}}
	v := New(known)
	_, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Digest: strings.Repeat("b", 64),
	})
	if !errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), "--force-rehash") {
		t.Fatalf("TOFU 不符应硬失败并给出处置: %v", err)
	}
}

func TestVerifyNoSourcesInsecure(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("artifact"))
	v := New(nil)
	v.Fetch = (&fakeFetcher{}).fetch
	_, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Digest: sum,
		Artifact: "node.tar.gz", Format: "gnu-sums", SumsURLs: []string{"https://dead/x"},
	})
	if !errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), "--insecure") {
		t.Fatalf("全部源不可用应拒绝并提示 --insecure: %v", err)
	}

	res, err := v.Verify(context.Background(), Request{
		Tool: "node", Version: "1.0.0", Platform: "linux-amd64", Digest: sum,
		Artifact: "node.tar.gz", Format: "gnu-sums", SumsURLs: []string{"https://dead/x"},
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("--insecure 应放行: %v", err)
	}
	if res.Verified != VerifiedUnverified || res.Mode != "insecure" {
		t.Fatalf("应标记 unverified: %+v", res)
	}
}

func TestVerifyRegistryIntegrity(t *testing.T) {
	sum := testkit.Sha256Hex([]byte("tarball"))
	v := New(nil)
	res, err := v.Verify(context.Background(), Request{
		Tool: "npm", Version: "12.2.2", Platform: "any", Digest: sum,
		Format: "registry-integrity", InlineShasum: sum, Algo: AlgoSHA256,
	})
	if err != nil {
		t.Fatalf("registry 自带 shasum 应可直接校验: %v", err)
	}
	if res.Verified != VerifiedWeak || res.Sources != 1 {
		t.Fatalf("单源应 weak: %+v", res)
	}

	_, err = v.Verify(context.Background(), Request{
		Tool: "npm", Version: "9.0.0", Platform: "any", Digest: sum,
		Format: "registry-integrity", InlineShasum: testkit.Sha256Hex([]byte("other")), Algo: AlgoSHA256,
	})
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("registry shasum 不符应失败: %v", err)
	}
}

func TestVerifyMissingSHA(t *testing.T) {
	if _, err := New(nil).Verify(context.Background(), Request{Tool: "x", Version: "1"}); !errors.Is(err, ErrChecksum) {
		t.Fatalf("缺少产物哈希应报错: %v", err)
	}
}

func TestVerifierDefaults(t *testing.T) {
	v := &Verifier{}
	if v.minSources() != DefaultMinSources {
		t.Fatalf("默认最小源数 = %d", v.minSources())
	}
	if v.now().IsZero() {
		t.Fatal("now 应有默认实现")
	}
	now := time.Now()
	v2 := &Verifier{Now: func() time.Time { return now }}
	if !v2.now().Equal(now.UTC()) {
		t.Fatal("应使用注入时钟")
	}
}

func TestKey(t *testing.T) {
	if got := Key("node", "22.22.2", "darwin-arm64"); got != "node@22.22.2/darwin-arm64" {
		t.Fatalf("Key = %q", got)
	}
}

func FuzzSHASUMS(f *testing.F) {
	f.Add([]byte("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  node.tar.gz\n"))
	f.Add([]byte("garbage\n\n  \t"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		sums := ParseGNUSums(data)
		for name, sum := range sums {
			if len(sum) != 64 || !isHex(sum) {
				t.Fatalf("解析出非法哈希: %q → %q", name, sum)
			}
		}
		if sum, ok := ParsePerArtifact(data); ok && (len(sum) != 64 || !isHex(sum)) {
			t.Fatalf("per-artifact 解析出非法哈希: %q", sum)
		}
	})
}

func TestDigestFileAlgos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha1("abc")
	if got, err := DigestFile(path, AlgoSHA1); err != nil || got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("sha1 = %q/%v", got, err)
	}
	sri, err := DigestFile(path, AlgoSHA512)
	if err != nil || !strings.HasPrefix(sri, "sha512-") || len(sri) < 90 {
		t.Fatalf("sha512 SRI = %q/%v", sri, err)
	}
	// 未知算法默认 sha256
	if got, err := DigestFile(path, "md5"); err != nil || got != testkit.Sha256Hex([]byte("abc")) {
		t.Fatalf("未知算法应回落 sha256: %q/%v", got, err)
	}
	if _, err := DigestFile(filepath.Join(t.TempDir(), "missing"), AlgoSHA256); err == nil {
		t.Fatal("文件不存在应报错")
	}
}

func TestKnownSaveError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := &Known{Schema: KnownSchema, Entries: map[string]Entry{}}
	if err := k.Save(filepath.Join(blocker, "known.json")); err == nil {
		t.Fatal("写不进去时应报错")
	}
}

func TestVerifyRegistrySHA1(t *testing.T) {
	digest := testkit.Sha1Hex([]byte("tarball"))
	res, err := New(nil).Verify(context.Background(), Request{
		Tool: "npm", Version: "1.0.0", Platform: "any",
		Digest: digest, Algo: AlgoSHA1,
		Format: "registry-integrity", InlineShasum: digest,
	})
	if err != nil || res.Sources != 1 {
		t.Fatalf("sha1 shasum 应可用: %+v %v", res, err)
	}
	// 算法不匹配时不该误用
	if _, err := New(nil).Verify(context.Background(), Request{
		Tool: "npm", Version: "2.0.0", Platform: "any",
		Digest: digest, Algo: AlgoSHA1,
		Format: "registry-integrity", InlineShasum: strings.Repeat("a", 64),
	}); !errors.Is(err, ErrChecksum) {
		t.Fatalf("sha256 值配 sha1 算法应报缺校验和: %v", err)
	}
}

func TestVerifyRegistrySHA512(t *testing.T) {
	digest := testkit.Sha512SRI([]byte("tarball"))
	res, err := New(nil).Verify(context.Background(), Request{
		Tool: "npm", Version: "3.0.0", Platform: "any",
		Digest: digest, Algo: AlgoSHA512,
		Format: "registry-integrity", InlineShasum: digest,
	})
	if err != nil || res.Sources != 1 {
		t.Fatalf("sha512 integrity 应可用: %+v %v", res, err)
	}
	if _, err := New(nil).Verify(context.Background(), Request{
		Tool: "npm", Version: "4.0.0", Platform: "any",
		Digest: digest, Algo: AlgoSHA512,
		Format: "registry-integrity", InlineShasum: "sha512-not-a-real-digest!!!",
	}); !errors.Is(err, ErrChecksum) {
		t.Fatalf("非法 integrity 应报错: %v", err)
	}
}

func TestVerifyAlgorithmChanged(t *testing.T) {
	known := &Known{Schema: KnownSchema, Entries: map[string]Entry{
		"npm@1.0.0/any": {Digest: "abc", Algo: AlgoSHA1, Verified: VerifiedWeak},
	}}
	_, err := New(known).Verify(context.Background(), Request{
		Tool: "npm", Version: "1.0.0", Platform: "any", Digest: "abc", Algo: AlgoSHA256,
	})
	if !errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), "--force-rehash") {
		t.Fatalf("算法变了应硬失败: %v", err)
	}
}

func TestRecordNilKnown(t *testing.T) {
	v := &Verifier{}
	v.record("k", Entry{Digest: "d", Algo: AlgoSHA256})
	if v.Known == nil || v.Known.Entries["k"].Digest != "d" {
		t.Fatalf("record 应自建表: %+v", v.Known)
	}
}
