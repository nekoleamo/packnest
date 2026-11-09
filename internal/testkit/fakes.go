package testkit

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/packnest/packnest/internal/provider"
)

// FakeTool 是"一个目录列举式镜像 + 产物 + 校验和"的通用假镜像（bun/deno）。
type FakeTool struct {
	Server   *httptest.Server
	Files    map[string][]byte
	Provider *provider.Provider
	// Artifact 是当前平台（darwin-arm64）的产物字节。
	Artifact []byte
}

// FakeToolOptions 描述 bun/deno 这类"每版本一个目录"的镜像。
type FakeToolOptions struct {
	Tool string
	// Versions 是要提供的版本。
	Versions []string
	// ProviderTOML 生成清单（调用方传入 base URL）。
	ProviderTOML func(baseURL string) string
	// MakeArtifact 造某平台产物；ext 为 zip/tar.gz。
	MakeArtifact func(platform, ext string) []byte
	// Platforms 是平台名 → (goos, goarch, ext)。
	Platforms []FakePlatform
	// Checksum 是校验和文件路径模板（相对版本目录）；空表示该工具没有校验和文件。
	ChecksumPath string
	// ChecksumFormat 是 checksum 文件的格式：gnu-sums / per-artifact。
	ChecksumFormat string
	// VersionDir 生成版本目录名（如 "bun-v%s/"、"v%s/"）。
	VersionDir string
	// Behaviors 是故障注入。
	Behaviors []Option
}

// FakePlatform 是产物平台描述。
type FakePlatform struct {
	Goos, Goarch, Ext, Name string
}

// NewFakeTool 造一个目录列举式假镜像（目录列举走 JSON，与 npmmirror 一致）。
func NewFakeTool(t testing.TB, opts FakeToolOptions) *FakeTool {
	t.Helper()
	if opts.VersionDir == "" {
		opts.VersionDir = "v%s/"
	}
	files := map[string][]byte{}
	ft := &FakeTool{Files: files}

	type dirEntry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		URL  string `json:"url"`
		Date string `json:"date"`
	}
	listing := make([]dirEntry, 0, len(opts.Versions))
	for _, version := range opts.Versions {
		dirName := fmt.Sprintf(opts.VersionDir, version)
		listing = append(listing, dirEntry{Name: dirName, Type: "dir", Date: "2026-10-01T00:00:00Z"})

		sums := map[string][]byte{}
		for _, plat := range opts.Platforms {
			data := opts.MakeArtifact(plat.Name, plat.Ext)
			name := fmt.Sprintf("%s-%s.%s", opts.Tool, plat.Name, plat.Ext)
			path := fmt.Sprintf("/%s/%s%s", opts.Tool, dirName, name)
			files[path] = data
			sums[name] = data
			if plat.Goos == "darwin" && plat.Goarch == "arm64" {
				ft.Artifact = data
			}
		}
		if opts.ChecksumPath != "" {
			path := fmt.Sprintf("/%s/%s%s", opts.Tool, dirName, opts.ChecksumPath)
			if opts.ChecksumFormat == "per-artifact" {
				// 每平台一个 .sha256sum：文件名由模板决定，这里按模板逐平台写。
				for _, plat := range opts.Platforms {
					name := fmt.Sprintf("%s-%s.%s", opts.Tool, plat.Name, plat.Ext)
					files[fmt.Sprintf("/%s/%s%s", opts.Tool, dirName, fmt.Sprintf(opts.ChecksumPath, plat.Name))] =
						[]byte(Sha256Hex(sums[name]) + "\n")
				}
			} else {
				files[path] = SumsFile(sums)
			}
		}
	}
	indexJSON, err := json.Marshal(listing)
	if err != nil {
		t.Fatalf("构造目录列举失败: %v", err)
	}
	files["/"+opts.Tool+"/"] = indexJSON

	srv := Mirror(t, files, opts.Behaviors...)
	ft.Server = srv
	p, err := provider.Parse([]byte(opts.ProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("%s 的测试清单非法: %v", opts.Tool, err)
	}
	ft.Provider = p
	return ft
}

// BunProviderTOML 是 bun 的测试清单（目录列举 + zip + SHASUMS256.txt）。
func BunProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "bun"
display = "Bun"
kind = "binary"
[index]
kind = "dirlist"
[index.urls]
default = "%[1]s/bun/"
[artifact]
[artifact.urls]
default = "%[1]s/bun/bun-v{version}/bun-{platform}.zip"
[artifact.ext]
windows = "zip"
darwin = "zip"
linux = "zip"
[artifact.platform]
darwin-arm64 = "darwin-aarch64"
linux-amd64 = "linux-x64"
windows-amd64 = "windows-x64"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[1]s/bun/bun-v{version}/SHASUMS256.txt"
[layout]
strip = 1
bins = ["bun", "bunx"]
[layout.bin_targets]
bun = "bun"
bunx = "bun"
`, baseURL)
}

// DenoProviderTOML 是 deno 的测试清单（目录列举 + 单文件 zip + per-artifact 校验）。
func DenoProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "deno"
display = "Deno"
kind = "binary"
[index]
kind = "dirlist"
[index.urls]
default = "%[1]s/deno/"
[artifact]
[artifact.urls]
default = "%[1]s/deno/v{version}/deno-{platform}.zip"
[artifact.ext]
windows = "zip"
darwin = "zip"
linux = "zip"
[artifact.platform]
darwin-arm64 = "aarch64-apple-darwin"
linux-amd64 = "x86_64-unknown-linux-gnu"
windows-amd64 = "x86_64-pc-windows-msvc"
[checksum]
format = "per-artifact"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[1]s/deno/v{version}/deno-{platform}.zip.sha256sum"
[layout]
single = "bin/deno"
bins = ["deno"]
`, baseURL)
}

// MakeFakeBunZip 造 bun 的 zip：内部 bun-<platform>/bun（strip=1 后是 bun）。
func MakeFakeBunZip(platform string) []byte {
	return MakeZip([]ZipEntry{
		{Name: "bun-" + platform + "/", Mode: 0o40000 | 0o755},
		{Name: "bun-" + platform + "/bun", Content: "#!/bin/sh\necho bun 1.4.4\n"},
	})
}

// MakeFakeDenoZip 造 deno 的单文件 zip。
func MakeFakeDenoZip() []byte {
	return MakeZip([]ZipEntry{
		{Name: "deno", Content: "#!/bin/sh\necho deno 2.9.8\n"},
	})
}

// NewFakePnpm 造 pnpm 的假镜像：registry 元数据 + tarball（含 dist.integrity）。
func NewFakePnpm(t testing.TB, versions map[string]string) *FakePnpm {
	t.Helper()
	files := map[string][]byte{}
	registry := MapNpmRegistry(versions)
	for version := range versions {
		tarball := MakePnpmTarball(version)
		files["/pnpm/-/pnpm-"+version+".tgz"] = tarball
		registry.SetTarball(version, "pnpm-"+version+".tgz", Sha1Hex(tarball))
		registry.SetIntegrity(version, Sha512SRI(tarball))
	}
	data, err := registry.Encode()
	if err != nil {
		t.Fatalf("构造 pnpm registry 失败: %v", err)
	}
	files["/pnpm"] = data

	srv := Mirror(t, files)
	fp := &FakePnpm{Server: srv, Files: files}
	p, err := provider.Parse([]byte(PnpmProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("pnpm 测试清单非法: %v", err)
	}
	fp.Provider = p
	return fp
}

// FakePnpm 是 pnpm 的假镜像。
type FakePnpm struct {
	Server   *httptest.Server
	Files    map[string][]byte
	Provider *provider.Provider
}

// PnpmProviderTOML 是 pnpm 的测试清单。
func PnpmProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "pnpm"
display = "pnpm"
kind = "registry"
requires = ["node"]
[index]
kind = "registry"
[index.urls]
default = "%[1]s/pnpm"
[artifact]
[artifact.urls]
default = "%[1]s/pnpm/-/pnpm-{version}.tgz"
[artifact.ext]
darwin = "tgz"
linux = "tgz"
windows = "tgz"
[artifact.platform]
darwin-arm64 = "any"
linux-amd64 = "any"
windows-amd64 = "any"
[checksum]
format = "registry-integrity"
mode = "tofu-then-verify"
[layout]
strip = 1
bins = ["pnpm", "pnpx"]
[layout.bin_targets]
pnpm = "bin/pnpm.cjs"
pnpx = "bin/pnpx.cjs"
`, baseURL)
}

// MakePnpmTarball 造 pnpm 的 tgz（package/bin/pnpm.cjs 带 node shebang）。
func MakePnpmTarball(version string) []byte {
	files := map[string]string{
		"package/package.json": fmt.Sprintf(`{"name":"pnpm","version":%q,"bin":{"pnpm":"bin/pnpm.cjs","pnpx":"bin/pnpx.cjs"},"engines":{"node":">=18.12"}}`, version),
		"package/bin/pnpm.cjs": "#!/usr/bin/env node\n// fake pnpm\n",
		"package/bin/pnpx.cjs": "#!/usr/bin/env node\n// fake pnpx\n",
	}
	return MakeTarGz(files)
}
