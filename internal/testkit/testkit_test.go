package testkit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestMakeNodeTarEntries(t *testing.T) {
	data := MakeNode(NodeSpec{Version: "22.22.2", NPMVersion: "11.20.0"})
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	seen := map[string]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		seen[hdr.Name] = hdr.Typeflag
	}
	prefix := "node-v22.22.2-darwin-arm64/"
	for _, want := range []string{"bin/node", "lib/node_modules/npm/package.json"} {
		if _, ok := seen[prefix+want]; !ok {
			t.Errorf("缺少 %s，实际: %v", want, seen)
		}
	}
	if typ := seen[prefix+"bin/npm"]; typ != tar.TypeSymlink {
		t.Errorf("bin/npm 应为符号链接（Windows 侧必须取 zip，方案 §18.3），实际 %v", typ)
	}
}

func TestMakeNodeZipEntries(t *testing.T) {
	data := MakeNodeZip(NodeSpec{Version: "22.22.2"})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	prefix := "node-v22.22.2-darwin-arm64/"
	if !found[prefix+"node.exe"] || !found[prefix+"npm.cmd"] {
		t.Fatalf("zip 内容不全: %v", found)
	}
}

func TestMakeNpmRegistry(t *testing.T) {
	data := MakeNpmRegistry(map[string]string{
		"11.21.2":        "^20.17.0 || >=22.9.0",
		"12.2.2":         "^22.22.2 || ^24.15.0 || >=26.0.0",
		"12.0.0-pre.0.0": "^20.17.0 || >=22.9.0",
	})
	var doc struct {
		Versions map[string]struct {
			Engines map[string]string `json:"engines"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("registry JSON 无法解析: %v", err)
	}
	if len(doc.Versions) != 3 {
		t.Fatalf("版本数 = %d", len(doc.Versions))
	}
	if doc.Versions["12.0.0-pre.0.0"].Engines["node"] == "" {
		t.Fatal("预发布版本的 engines 必须保留（这是要测的陷阱）")
	}
}

func TestSumsFile(t *testing.T) {
	sums := SumsFile(map[string][]byte{"node.tar.gz": []byte("abc")})
	if len(sums) == 0 || bytes.Count(sums, []byte("\n")) != 1 {
		t.Fatalf("校验和文件格式异常: %q", sums)
	}
	if Sha256Hex([]byte("abc")) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("Sha256Hex 结果不符")
	}
}

func TestMirrorServesFiles(t *testing.T) {
	payload := []byte("0123456789")
	srv := Mirror(t, map[string][]byte{"/node/index.json": payload})

	resp, err := http.Get(srv.URL + "/node/index.json")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(payload) {
		t.Fatalf("body = %q", body)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatal("默认应支持 Range（模拟 npmmirror CDN）")
	}

	resp, err = http.Get(srv.URL + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("缺失文件应 404，得到 %d", resp.StatusCode)
	}
}

func TestMirrorBehaviorStatus(t *testing.T) {
	srv := Mirror(t, map[string][]byte{"/a": []byte("x")}, Status(503))
	resp, err := http.Get(srv.URL + "/a")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMirrorBehaviorNoRange(t *testing.T) {
	srv := Mirror(t, map[string][]byte{"/a": []byte("0123456789")}, NoRange())
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/a", nil)
	req.Header.Set("Range", "bytes=2-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("NoRange 时应忽略 Range 返回 200，得到 %d", resp.StatusCode)
	}
}

func TestMirrorBehaviorTruncate(t *testing.T) {
	srv := Mirror(t, map[string][]byte{"/a.tar.gz": bytes.Repeat([]byte("x"), 100)}, TruncateAt(10))
	resp, err := http.Get(srv.URL + "/a.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 10 {
		t.Fatalf("应截断到 10 字节，得到 %d", len(body))
	}
	if resp.ContentLength != 100 {
		t.Fatalf("Content-Length 仍是全长，得到 %d", resp.ContentLength)
	}
}

func TestMirrorBehaviorCorruptAndSums(t *testing.T) {
	files := map[string][]byte{
		"/v1/node.tar.gz":    []byte("payload-bytes"),
		"/v1/SHASUMS256.txt": SumsFile(map[string][]byte{"node.tar.gz": []byte("payload-bytes")}),
	}
	srv := Mirror(t, files, Corrupt(), ChecksumMismatch())

	resp, _ := http.Get(srv.URL + "/v1/node.tar.gz")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) == "payload-bytes" {
		t.Fatal("Corrupt 应翻转产物字节")
	}

	resp, _ = http.Get(srv.URL + "/v1/SHASUMS256.txt")
	sums, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(sums, bytes.Repeat([]byte("0"), 64)) {
		t.Fatalf("ChecksumMismatch 应写入全 0 哈希: %q", sums)
	}
}

func TestMirrorMissingChecksum(t *testing.T) {
	srv := Mirror(t, map[string][]byte{"/v1/SHASUMS256.txt": []byte("x")}, MissingChecksum())
	resp, _ := http.Get(srv.URL + "/v1/SHASUMS256.txt")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("校验和应 404，得到 %d", resp.StatusCode)
	}
}

func TestMirrorStaleIndex(t *testing.T) {
	srv := Mirror(t, map[string][]byte{}, StaleIndex([]byte(`{"versions":["22.21.1"]}`)))
	resp, _ := http.Get(srv.URL + "/dist/index.json")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(body, []byte("22.21.1")) {
		t.Fatalf("应返回过期索引: %q", body)
	}
}

func TestNewFakeToolBun(t *testing.T) {
	ft := NewFakeTool(t, FakeToolOptions{
		Tool:         "bun",
		Versions:     []string{"1.4.4"},
		ProviderTOML: BunProviderTOML,
		MakeArtifact: func(platform, ext string) []byte { return MakeFakeBunZip(platform) },
		Platforms: []FakePlatform{
			{"darwin", "arm64", "zip", "darwin-aarch64"},
			{"linux", "amd64", "zip", "linux-x64"},
		},
		ChecksumPath:   "SHASUMS256.txt",
		ChecksumFormat: "gnu-sums",
		VersionDir:     "bun-v%s/",
	})
	if ft.Artifact == nil {
		t.Fatal("应生成 darwin-arm64 产物")
	}
	// 目录列举能被 provider 索引解析
	idx, err := (&providerLoader{ft.Server.URL}).load(t, ft.Provider)
	if err != nil {
		t.Fatalf("索引加载失败: %v", err)
	}
	if len(idx.Versions) != 1 || idx.Versions[0].Version != "1.4.4" {
		t.Fatalf("版本解析错误: %+v", idx.Versions)
	}
	urls, err := ft.Provider.ArtifactURLs("1.4.4", "darwin", "arm64", "default")
	if err != nil || len(urls) == 0 {
		t.Fatalf("产物 URL 渲染失败: %v %v", urls, err)
	}
}

func TestNewFakeToolSingleFile(t *testing.T) {
	ft := NewFakeTool(t, FakeToolOptions{
		Tool:         "singlefile",
		Versions:     []string{"2.9.8"},
		ProviderTOML: SingleFileProviderTOML,
		MakeArtifact: func(platform, ext string) []byte { return MakeFakeSingleFileZip() },
		Platforms: []FakePlatform{
			{"darwin", "arm64", "zip", "aarch64-apple-darwin"},
			{"linux", "amd64", "zip", "x86_64-unknown-linux-gnu"},
		},
		ChecksumPath:   "singlefile-%s.zip.sha256sum",
		ChecksumFormat: "per-artifact",
	})
	if ft.Artifact == nil {
		t.Fatal("应生成产物")
	}
	sums, err := ft.Provider.ChecksumURLs("2.9.8", "darwin", "arm64", "default")
	if err != nil || len(sums) == 0 {
		t.Fatalf("校验和 URL 渲染失败: %v %v", sums, err)
	}
}

func TestNewFakePnpm(t *testing.T) {
	fp := NewFakePnpm(t, map[string]string{"10.4.1": ">=18.12"})
	if _, ok := fp.Files["/pnpm/-/pnpm-10.4.1.tgz"]; !ok {
		t.Fatalf("应有 pnpm tarball: %v", fp.Files)
	}
}
