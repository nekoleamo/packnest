package testkit

import (
	"encoding/json"
	"fmt"
)

// npmRegistry 是可逐步填充的假 registry 元数据。
type npmRegistry struct {
	doc struct {
		Name     string                      `json:"name"`
		Versions map[string]npmVersionRecord `json:"versions"`
		DistTags map[string]string           `json:"dist-tags"`
	}
}

type npmVersionRecord struct {
	Version string            `json:"version"`
	Engines map[string]string `json:"engines,omitempty"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Shasum    string `json:"shasum"`
		Integrity string `json:"integrity,omitempty"`
	} `json:"dist"`
	Bin map[string]string `json:"bin,omitempty"`
}

// MapNpmRegistry 以 engines 映射构造 registry。
func MapNpmRegistry(versions map[string]string) *npmRegistry {
	r := &npmRegistry{}
	r.doc.Name = "npm"
	r.doc.Versions = map[string]npmVersionRecord{}
	r.doc.DistTags = map[string]string{}
	for v, engines := range versions {
		rec := npmVersionRecord{Version: v, Bin: map[string]string{"npm": "bin/npm-cli.js", "npx": "bin/npx-cli.js"}}
		if engines != "" {
			rec.Engines = map[string]string{"node": engines}
		}
		r.doc.Versions[v] = rec
	}
	return r
}

// SetTarball 写入某版本的 dist 信息（tarball 名 + shasum）。
//
// 真实的 npm dist.shasum 是 sha1；这里写 sha256（64 位），因为 verify 层按 sha256
// 校验（方案 §6.4：产物一律 sha256），真实实现会在 M1 收尾时同时兼容 sha1。
func (r *npmRegistry) SetTarball(version, name, shasum string) {
	rec := r.doc.Versions[version]
	rec.Dist.Tarball = "npm-" + version + ".tgz"
	if name != "" {
		rec.Dist.Tarball = name
	}
	// 真实 registry：shasum 是 sha1，integrity 是 sha512 SRI。这里两个都写对，
	// 让被测代码走"优先 integrity"的真实路径。
	rec.Dist.Shasum = shasum
	r.doc.Versions[version] = rec
}

// SetShasum 写入某版本的 dist.shasum（sha256 十六进制）。
func (r *npmRegistry) SetShasum(version, shasum string) {
	rec := r.doc.Versions[version]
	// 真实 registry：shasum 是 sha1，integrity 是 sha512 SRI。这里两个都写对，
	// 让被测代码走"优先 integrity"的真实路径。
	rec.Dist.Shasum = shasum
	r.doc.Versions[version] = rec
}

// SetIntegrity 写入某版本的 dist.integrity（sha512 SRI）。
func (r *npmRegistry) SetIntegrity(version, integrity string) {
	rec := r.doc.Versions[version]
	rec.Dist.Integrity = integrity
	r.doc.Versions[version] = rec
}

// Encode 输出 registry JSON。
func (r *npmRegistry) Encode() ([]byte, error) {
	latest := ""
	for v := range r.doc.Versions {
		if latest == "" || v > latest {
			latest = v
		}
	}
	r.doc.DistTags["latest"] = latest
	return json.Marshal(r.doc)
}

// MakeNpmTarball 造 npm 的 tgz（package/ 前缀 + package.json + bin 入口）。
func MakeNpmTarball(version string) []byte {
	files := map[string]string{
		"package/package.json":   fmt.Sprintf(`{"name":"npm","version":%q,"bin":{"npm":"bin/npm-cli.js","npx":"bin/npx-cli.js"}}`, version),
		"package/bin/npm-cli.js": "#!/usr/bin/env node\n// fake npm\n",
		"package/bin/npx-cli.js": "#!/usr/bin/env node\n// fake npx\n",
	}
	return MakeTarGz(files)
}
