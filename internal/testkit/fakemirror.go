package testkit

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/packnest/packnest/internal/provider"
)

// fakePlatform 是假镜像支持的平台（与 node 真实命名一致）。
type fakePlatform struct {
	goos, goarch string
	ext          string
	name         string // 产物名里的平台串
}

var fakePlatforms = []fakePlatform{
	{"darwin", "arm64", "tar.gz", "darwin-arm64"},
	{"linux", "amd64", "tar.gz", "linux-x64"},
	{"windows", "amd64", "zip", "win-x64"},
}

// FakeNodeOptions 描述一个假 node 镜像的内容。
type FakeNodeOptions struct {
	// Versions 是要提供的 node 版本（含各自自带的 npm 版本）。
	Versions []NodeSpec
	// NPM 是 npm registry 里的版本 → engines.node（含预发布用于测陷阱）。
	NPM map[string]string
	// LTS 是 node 版本 → LTS 代号（用于 latest/lts 别名）。
	LTS map[string]string
	// Behaviors 是故障注入（对全部路径生效）。
	Behaviors []Option
}

// FakeNode 是一个可安装的假 node + npm 镜像。
type FakeNode struct {
	Server        *httptest.Server
	Files         map[string][]byte
	NodeProvider  *provider.Provider
	NPMProvider   *provider.Provider
	Artifacts     map[string][]byte // 版本 → 产物（当前平台 darwin-arm64）
	NodeChecksums map[string]string // 版本 → sha256（darwin-arm64 产物）
}

// NewFakeNode 造一个假镜像：node index.json、各平台产物、SHASUMS256.txt、npm registry 与 tarball。
func NewFakeNode(t testing.TB, opts FakeNodeOptions) *FakeNode {
	t.Helper()
	files := map[string][]byte{}
	fm := &FakeNode{Files: files, Artifacts: map[string][]byte{}, NodeChecksums: map[string]string{}}

	type idxEntry struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"`
		Date    string `json:"date"`
	}
	var idx []idxEntry
	for _, n := range opts.Versions {
		lts := any(false)
		if name, ok := opts.LTS[n.Version]; ok && name != "" {
			lts = name
		}
		idx = append(idx, idxEntry{Version: "v" + n.Version, LTS: lts, Date: "2026-10-01"})

		sums := map[string][]byte{}
		for _, plat := range fakePlatforms {
			spec := n
			spec.OS, spec.Arch = plat.goos, plat.goarch
			var data []byte
			if plat.ext == "zip" {
				data = MakeNodeZip(spec)
			} else {
				data = MakeNode(spec)
			}
			name := fmt.Sprintf("node-v%s-%s.%s", n.Version, plat.name, plat.ext)
			path := fmt.Sprintf("/node/v%s/%s", n.Version, name)
			files[path] = data
			sums[name] = data
			if plat.goos == "darwin" && plat.goarch == "arm64" {
				fm.Artifacts[n.Version] = data
				fm.NodeChecksums[n.Version] = Sha256Hex(data)
			}
		}
		files[fmt.Sprintf("/node/v%s/SHASUMS256.txt", n.Version)] = SumsFile(sums)
	}
	if idx == nil {
		idx = []idxEntry{}
	}
	indexJSON, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("构造 node 索引失败: %v", err)
	}
	files["/node/index.json"] = indexJSON

	// npm registry + tarball（含 dist.shasum，供 registry-integrity 校验）。
	registry := MapNpmRegistry(opts.NPM)
	for version := range opts.NPM {
		tarball := MakeNpmTarball(version)
		files["/npm/-/npm-"+version+".tgz"] = tarball
		registry.SetTarball(version, "npm-"+version+".tgz", Sha1Hex(tarball))
		registry.SetIntegrity(version, Sha512SRI(tarball))
	}
	registryJSON, err := registry.Encode()
	if err != nil {
		t.Fatalf("构造 npm registry 失败: %v", err)
	}
	files["/npm"] = registryJSON

	srv := Mirror(t, files, opts.Behaviors...)
	fm.Server = srv

	nodeP, err := provider.Parse([]byte(NodeProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("测试用 node provider 清单非法: %v", err)
	}
	npmP, err := provider.Parse([]byte(NPMProviderTOML(srv.URL)))
	if err != nil {
		t.Fatalf("测试用 npm provider 清单非法: %v", err)
	}
	fm.NodeProvider, fm.NPMProvider = nodeP, npmP
	return fm
}

// NodeProviderTOML 返回索引与产物都指向 baseURL 的 node 清单。
func NodeProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "node"
display = "Node.js"
kind = "binary"

[index]
kind = "json"
[index.urls]
default = "%[1]s/node/index.json"
cn = "%[1]s/node/index.json"

[artifact]
[artifact.urls]
default = "%[1]s/node/v{version}/node-v{version}-{platform}.{ext}"
cn = "%[1]s/node/v{version}/node-v{version}-{platform}.{ext}"
[artifact.ext]
windows = "zip"
darwin = "tar.gz"
linux = "tar.gz"
[artifact.platform]
darwin-amd64 = "darwin-x64"
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-x64"
linux-arm64 = "linux-arm64"
windows-amd64 = "win-x64"

[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[1]s/node/v{version}/SHASUMS256.txt"

[layout]
strip = 1
bins = ["node", "npm", "npx"]

[companions.npm]
mode = "latest-compatible"
registry = "npm"
target = "lib/node_modules/npm"
`, baseURL)
}

// NodeProviderTOMLForTest 返回本假镜像使用的 node 清单原文（测试里可做替换）。
func (f *FakeNode) NodeProviderTOMLForTest() string { return NodeProviderTOML(f.Server.URL) }

// NPMProviderTOMLForTest 返回本假镜像使用的 npm 清单原文（测试里可做替换）。
func (f *FakeNode) NPMProviderTOMLForTest() string { return NPMProviderTOML(f.Server.URL) }

// NPMProviderTOML 返回索引与 tarball 都指向 baseURL 的 npm 清单。
func NPMProviderTOML(baseURL string) string {
	return fmt.Sprintf(`id = "npm"
display = "npm"
kind = "registry"

[index]
kind = "registry"
[index.urls]
default = "%[1]s/npm"
cn = "%[1]s/npm"

[artifact]
[artifact.urls]
default = "%[1]s/npm/-/npm-{version}.tgz"
cn = "%[1]s/npm/-/npm-{version}.tgz"
[artifact.ext]
darwin = "tgz"
linux = "tgz"
windows = "tgz"
[artifact.platform]
darwin-amd64 = "any"
darwin-arm64 = "any"
linux-amd64 = "any"
linux-arm64 = "any"
windows-amd64 = "any"
windows-arm64 = "any"

[checksum]
format = "registry-integrity"
mode = "tofu-then-verify"

[layout]
strip = 1
bins = ["npm", "npx"]
[layout.bin_targets]
npm = "bin/npm-cli.js"
npx = "bin/npx-cli.js"
`, baseURL)
}
