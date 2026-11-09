package testkit

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/packnest/packnest/internal/provider"
)

// FakeGo 是 go 的假镜像：dl JSON（files 带 sha256）+ 产物 tar.gz/zip。
type FakeGo struct {
	Server   *httptest.Server
	Files    map[string][]byte
	Provider *provider.Provider
	Sha256   map[string]string // "1.27.3" → 产物 sha256（darwin-arm64）
}

// NewFakeGo 造 go 的假镜像（索引带 sha256，产物命名与官方一致）。
func NewFakeGo(t testing.TB, versions []string) *FakeGo {
	t.Helper()
	files := map[string][]byte{}
	sha := map[string]string{}
	type goFile struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Version  string `json:"version"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	}
	type goRelease struct {
		Version string   `json:"version"`
		Stable  bool     `json:"stable"`
		Files   []goFile `json:"files"`
	}
	var index []goRelease
	platforms := []struct{ os, arch string }{{"darwin", "arm64"}, {"linux", "amd64"}, {"windows", "amd64"}}
	for _, v := range versions {
		rel := goRelease{Version: "go" + v, Stable: true}
		for _, plat := range platforms {
			ext := "tar.gz"
			if plat.os == "windows" {
				ext = "zip"
			}
			name := fmt.Sprintf("go%s.%s-%s.%s", v, plat.os, plat.arch, ext)
			var data []byte
			if ext == "zip" {
				data = MakeZip([]ZipEntry{{Name: "go/bin/go.exe", Content: "fake go " + v}})
			} else {
				data = MakeTarGz(map[string]string{
					"go/bin/go":    "#!/bin/sh\necho go version go" + v + "\n",
					"go/bin/gofmt": "#!/bin/sh\n",
				})
			}
			files["/dl/"+name] = data
			rel.Files = append(rel.Files, goFile{Filename: name, OS: plat.os, Arch: plat.arch,
				Version: "go" + v, SHA256: Sha256Hex(data), Kind: "archive"})
			if plat.os == "darwin" && plat.arch == "arm64" {
				sha[v] = Sha256Hex(data)
			}
		}
		index = append(index, rel)
	}
	body, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("构造 go 索引失败: %v", err)
	}
	files["/dl/index.json"] = body

	srv := Mirror(t, files)
	fg := &FakeGo{Server: srv, Files: files, Sha256: sha}
	p, err := provider.Parse([]byte(GoProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("go 测试清单非法: %v", err)
	}
	fg.Provider = p
	return fg
}

// GoProviderTOML 是 go 的测试清单。
func GoProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "go"
display = "Go"
kind = "binary"
[index]
kind = "json"
version_prefix = "go"
[index.urls]
default = "%[1]s/dl/index.json"
[artifact]
[artifact.urls]
default = "%[1]s/dl/{filename}"
[artifact.ext]
windows = "zip"
darwin = "tar.gz"
linux = "tar.gz"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-amd64"
windows-amd64 = "windows-amd64"
[checksum]
format = "index-inline"
mode = "tofu-then-verify"
[layout]
strip = 1
bins = ["go", "gofmt"]
[toolchain.env]
GOTOOLCHAIN = "local"
`, baseURL)
}

// FakePBS 是 python-build-standalone 的假镜像：标签目录 → 文件 + SHA256SUMS。
type FakePBS struct {
	Server   *httptest.Server
	Files    map[string][]byte
	Provider *provider.Provider
	Tags     []string
	// Artifacts 是 "version" → darwin-arm64 产物
	Artifacts map[string][]byte
}

// PBSOptions 描述假 pbs 镜像的内容。
type PBSOptions struct {
	// Tags 是 release 标签（如 ["20261020","20261026"]），后面的是新的。
	Tags []string
	// Versions 是标签下的 cpython 版本（每个标签都放同样的版本）。
	Versions []string
	// Platforms 是平台串 → (os, arch)；为空时用 darwin-arm64/linux-amd64/windows-amd64。
	Platforms map[string][2]string
}

// NewFakePBS 造 pbs 假镜像（目录列举 + 每标签 SHA256SUMS + install_only 产物）。
func NewFakePBS(t testing.TB, opts PBSOptions) *FakePBS {
	t.Helper()
	files := map[string][]byte{}
	fp := &FakePBS{Files: files, Tags: opts.Tags, Artifacts: map[string][]byte{}}
	if len(opts.Platforms) == 0 {
		opts.Platforms = map[string][2]string{
			"aarch64-apple-darwin":     {"darwin", "arm64"},
			"x86_64-unknown-linux-gnu": {"linux", "amd64"},
			"x86_64-pc-windows-msvc":   {"windows", "amd64"},
		}
	}

	type dirEntry struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var rootListing []dirEntry
	for _, tag := range opts.Tags {
		rootListing = append(rootListing, dirEntry{Name: tag + "/", Type: "dir"})
	}
	rootBody, err := json.Marshal(rootListing)
	if err != nil {
		t.Fatal(err)
	}
	files["/pbs/"] = rootBody

	for _, tag := range opts.Tags {
		var listing []dirEntry
		sums := map[string][]byte{}
		for _, version := range opts.Versions {
			for platform, osarch := range opts.Platforms {
				name := fmt.Sprintf("cpython-%s+%s-%s-install_only.tar.gz", version, tag, platform)
				data := MakeTarGz(map[string]string{
					"python/bin/python3": "#!/bin/sh\necho Python " + version + "\n",
					"python/bin/pip3":    "#!/bin/sh\n",
				})
				files[fmt.Sprintf("/pbs/%s/%s", tag, name)] = data
				sums[name] = data
				listing = append(listing, dirEntry{Name: name, Type: "file"})
				// 干扰项：stripped 变体不该被选中
				stripped := fmt.Sprintf("cpython-%s+%s-%s-install_only_stripped.tar.gz", version, tag, platform)
				listing = append(listing, dirEntry{Name: stripped, Type: "file"})
				if osarch[0] == "darwin" && osarch[1] == "arm64" {
					fp.Artifacts[version] = data
				}
			}
		}
		body, err := json.Marshal(listing)
		if err != nil {
			t.Fatal(err)
		}
		files[fmt.Sprintf("/pbs/%s/", tag)] = body
		files[fmt.Sprintf("/pbs/%s/SHA256SUMS", tag)] = SumsFile(sums)
	}

	srv := Mirror(t, files)
	fp.Server = srv
	p, err := provider.Parse([]byte(PBSProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("python 测试清单非法: %v", err)
	}
	fp.Provider = p
	return fp
}

// PBSProviderTOML 是 python（pbs）的测试清单。
func PBSProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "python"
display = "Python"
kind = "binary"
[index]
kind = "pbs"
[index.urls]
cn = "%[1]s/pbs"
[artifact]
[artifact.urls]
cn = "%[1]s/pbs/{tag}/{filename}"
[artifact.ext]
windows = "tar.gz"
darwin = "tar.gz"
linux = "tar.gz"
[artifact.platform]
darwin-amd64 = "x86_64-apple-darwin"
darwin-arm64 = "aarch64-apple-darwin"
linux-amd64 = "x86_64-unknown-linux-gnu"
linux-arm64 = "aarch64-unknown-linux-gnu"
windows-amd64 = "x86_64-pc-windows-msvc"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
cn = "%[1]s/pbs/{tag}/SHA256SUMS"
[layout]
strip = 1
bins = ["python3", "pip3"]
`, baseURL)
}
