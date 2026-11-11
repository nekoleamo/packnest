// Package testkit 是测试专用夹具：用代码现场造小微发行包与假镜像。
//
// 原则（方案 §9.2）：不手工维护二进制夹具，也不联网 —— 让"真安装"这类
// 看起来必须联网的逻辑变成纯本地断言。故障注入用可组合的 Option。
package testkit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ───────────────────────── 合成发行包 ─────────────────────────

// NodeSpec 描述一个假 node 发行包。
type NodeSpec struct {
	Version    string // 如 22.22.2
	NPMVersion string // 自带 npm 版本，如 11.20.0
	OS         string // darwin（默认）
	Arch       string // arm64（默认）
	// Corepack 为 true 时包里带 lib/node_modules/corepack（node ≤24 的真实布局；
	// node 25+ 已移除，用它造"不自带 corepack"的反例）。
	Corepack bool
}

// MakeNode 造一个 tar.gz 形式的假 node 发行包：
// 真的 bin/node（sh 脚本）、真的 lib/node_modules/npm/package.json、
// bin/npm 符号链接（模拟官方 tar 包结构，Windows 侧必须走 zip 才不踩坑）。
func MakeNode(spec NodeSpec) []byte {
	spec = withNodeDefaults(spec)
	prefix := fmt.Sprintf("node-v%s-%s-%s/", spec.Version, spec.OS, spec.Arch)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	writeTarEntry(tw, prefix+"bin/node", shellScript(spec.Version), 0o755, tar.TypeReg)
	writeTarSymlink(tw, prefix+"bin/npm", "../lib/node_modules/npm/bin/npm-cli.js")
	writeTarSymlink(tw, prefix+"bin/npx", "../lib/node_modules/npm/bin/npx-cli.js")
	writeTarEntry(tw, prefix+"lib/node_modules/npm/package.json", npmPackageJSON(spec.NPMVersion), 0o644, tar.TypeReg)
	writeTarEntry(tw, prefix+"lib/node_modules/npm/bin/npm-cli.js", "#!/usr/bin/env node\nconsole.log('npm');\n", 0o755, tar.TypeReg)
	if spec.Corepack {
		writeTarEntry(tw, prefix+"lib/node_modules/corepack/package.json", `{"name":"corepack","version":"0.34.0"}`, 0o644, tar.TypeReg)
		writeTarEntry(tw, prefix+"lib/node_modules/corepack/dist/corepack.js", "#!/usr/bin/env node\n", 0o755, tar.TypeReg)
	}
	writeTarEntry(tw, prefix+"README.md", "fake node for tests\n", 0o644, tar.TypeReg)

	closeAll(tw, gz)
	return buf.Bytes()
}

// MakeNodeZip 造 zip 形式的假 node 发行包（Windows 用；里面没有符号链接）。
func MakeNodeZip(spec NodeSpec) []byte {
	spec = withNodeDefaults(spec)
	prefix := fmt.Sprintf("node-v%s-%s-%s/", spec.Version, spec.OS, spec.Arch)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipEntry(zw, prefix+"node.exe", "[fake node.exe]\n", 0o755)
	writeZipEntry(zw, prefix+"npm.cmd", "@ECHO off\r\nnode \"%~dp0\\node_modules\\npm\\bin\\npm-cli.js\" %*\r\n", 0o755)
	writeZipEntry(zw, prefix+"npx.cmd", "@ECHO off\r\nnode \"%~dp0\\node_modules\\npm\\bin\\npx-cli.js\" %*\r\n", 0o755)
	writeZipEntry(zw, prefix+"node_modules/npm/package.json", npmPackageJSON(spec.NPMVersion), 0o644)
	writeZipEntry(zw, prefix+"node_modules/npm/bin/npm-cli.js", "#!/usr/bin/env node\n", 0o755)
	closeAll(zw)
	return buf.Bytes()
}

func withNodeDefaults(spec NodeSpec) NodeSpec {
	if spec.Version == "" {
		spec.Version = "22.22.2"
	}
	if spec.NPMVersion == "" {
		spec.NPMVersion = "11.20.0"
	}
	if spec.OS == "" {
		spec.OS = "darwin"
	}
	if spec.Arch == "" {
		spec.Arch = "arm64"
	}
	return spec
}

func shellScript(version string) string {
	return "#!/bin/sh\necho v" + version + "\n"
}

func npmPackageJSON(version string) string {
	return fmt.Sprintf(`{"name":"npm","version":%q,"bin":{"npm":"bin/npm-cli.js","npx":"bin/npx-cli.js"},"engines":{"node":"^20.17.0 || >=22.9.0"}}`, version)
}

func writeTarEntry(tw *tar.Writer, name, content string, mode int64, typ byte) {
	hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: typ, ModTime: time.Unix(1700000000, 0)}
	if err := tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		panic(err)
	}
}

func writeTarSymlink(tw *tar.Writer, name, target string) {
	hdr := &tar.Header{Name: name, Linkname: target, Mode: 0o777, Typeflag: tar.TypeSymlink, ModTime: time.Unix(1700000000, 0)}
	if err := tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
}

func writeZipEntry(zw *zip.Writer, name, content string, mode uint32) {
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetMode(os.FileMode(mode))
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		panic(err)
	}
}

func closeAll(closers ...io.Closer) {
	for _, c := range closers {
		if err := c.Close(); err != nil {
			panic(err)
		}
	}
}

// MakeNpmRegistry 造 npm registry 元数据 JSON：版本 → engines.node。
//
// 用它可以纯本地验证 node↔npm 反查（含预发布陷阱，方案 §9.2）。
func MakeNpmRegistry(versions map[string]string) []byte {
	type dist struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity,omitempty"`
		Shasum    string `json:"shasum,omitempty"`
	}
	type versionMeta struct {
		Version string            `json:"version"`
		Engines map[string]string `json:"engines,omitempty"`
		Dist    dist              `json:"dist"`
	}
	doc := struct {
		Name     string                 `json:"name"`
		Versions map[string]versionMeta `json:"versions"`
		DistTags map[string]string      `json:"dist-tags"`
	}{
		Name:     "npm",
		Versions: map[string]versionMeta{},
		DistTags: map[string]string{},
	}
	for v, engines := range versions {
		vm := versionMeta{Version: v, Dist: dist{Tarball: "npm-" + v + ".tgz"}}
		if engines != "" {
			vm.Engines = map[string]string{"node": engines}
		}
		doc.Versions[v] = vm
		doc.DistTags["latest"] = v
	}
	data, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return data
}

// SumsFile 造 GNU-style 校验和文件（"<sha256>  <filename>" 每行一条）。
func SumsFile(files map[string][]byte) []byte {
	var b strings.Builder
	for name, content := range files {
		sum := sha256.Sum256(content)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(b.String())
}

// Sha256Hex 返回十六进制 sha256。
func Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Sha1Hex 返回十六进制 sha1（npm registry 的 dist.shasum 就是它）。
func Sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// Sha512SRI 返回 sha512 的 SRI 形式（"sha512-<base64>"，npm dist.integrity 的格式）。
func Sha512SRI(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// ───────────────────────── 假镜像 ─────────────────────────

// Options 是故障注入矩阵（方案 §9.3）。
type Options struct {
	status      int
	statusFor   map[string]int
	truncateAt  int64
	dropAfter   int64
	noRange     bool
	wrongETag   bool
	delay       time.Duration
	corrupt     bool
	missingSum  bool
	sumMismatch bool
	staleIndex  []byte
}

// Option 是故障注入开关。
type Option func(*Options)

// Status 让所有请求返回指定状态码。
func Status(code int) Option { return func(o *Options) { o.status = code } }

// StatusFor 让某个路径返回指定状态码。
func StatusFor(path string, code int) Option {
	return func(o *Options) {
		if o.statusFor == nil {
			o.statusFor = map[string]int{}
		}
		o.statusFor[path] = code
	}
}

// TruncateAt 只发前 n 字节（模拟传输中断）。
func TruncateAt(n int64) Option { return func(o *Options) { o.truncateAt = n } }

// DropAfter 在第 n 字节后断开连接（模拟断流）。
func DropAfter(n int64) Option { return func(o *Options) { o.dropAfter = n } }

// NoRange 让服务器不支持 Range（不返回 Accept-Ranges，忽略 Range 头）。
func NoRange() Option { return func(o *Options) { o.noRange = true } }

// WrongETag 每次返回不同的 ETag（模拟镜像刷新）。
func WrongETag() Option { return func(o *Options) { o.wrongETag = true } }

// Slow 每个请求延迟 d。
func Slow(d time.Duration) Option { return func(o *Options) { o.delay = d } }

// Corrupt 翻转产物字节（模拟投毒/损坏）。
func Corrupt() Option { return func(o *Options) { o.corrupt = true } }

// MissingChecksum 让校验和文件 404。
func MissingChecksum() Option { return func(o *Options) { o.missingSum = true } }

// ChecksumMismatch 让校验和文件里的哈希全是错的。
func ChecksumMismatch() Option { return func(o *Options) { o.sumMismatch = true } }

// StaleIndex 用给定的过期索引内容替换 index.json（模拟镜像滞后）。
func StaleIndex(body []byte) Option { return func(o *Options) { o.staleIndex = body } }

// Mirror 起一个假镜像服务器（默认支持 Range，像 npmmirror 的 CDN）。
func Mirror(t testing.TB, files map[string][]byte, opts ...Option) *httptest.Server {
	t.Helper()
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o.delay > 0 {
			time.Sleep(o.delay)
		}
		if o.status != 0 {
			w.WriteHeader(o.status)
			return
		}
		if code, ok := o.statusFor[r.URL.Path]; ok {
			w.WriteHeader(code)
			return
		}
		if o.missingSum && isChecksumPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		body, ok := lookup(files, r.URL.Path)
		// 过期索引优先于真实文件：模拟"镜像还没同步"。
		if o.staleIndex != nil && strings.HasSuffix(r.URL.Path, "index.json") {
			body, ok = o.staleIndex, true
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if o.corrupt && isArtifactPath(r.URL.Path) && len(body) > 0 {
			body = flipByte(body)
		}
		if o.sumMismatch && isChecksumPath(r.URL.Path) {
			body = replaceSums(body)
		}
		if o.wrongETag {
			hits++
			w.Header().Set("ETag", strconv.Itoa(int(hits)))
		}
		if o.noRange {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if o.truncateAt > 0 && int64(len(body)) > o.truncateAt {
				body = body[:o.truncateAt]
			}
			_, _ = w.Write(body)
			return
		}
		if o.truncateAt > 0 && int64(len(body)) > o.truncateAt {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body[:o.truncateAt])
			return
		}
		if o.dropAfter > 0 && int64(len(body)) > o.dropAfter {
			// 断流：写出 Content-Length 与前半截内容后直接掐断连接。
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body[:o.dropAfter])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		http.ServeContent(w, r, pathBase(r.URL.Path), time.Unix(1700000000, 0), bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func lookup(files map[string][]byte, path string) ([]byte, bool) {
	if body, ok := files[path]; ok {
		return body, true
	}
	trimmed := strings.TrimPrefix(path, "/")
	if body, ok := files[trimmed]; ok {
		return body, true
	}
	return nil, false
}

func pathBase(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return p
	}
	return p[idx+1:]
}

func isChecksumPath(p string) bool {
	upper := strings.ToUpper(p)
	return strings.Contains(upper, "SHASUMS") || strings.Contains(upper, "SHA256SUMS") ||
		strings.HasSuffix(upper, ".SHA256SUM")
}

func isArtifactPath(p string) bool {
	return strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".zip") || strings.HasSuffix(p, ".tgz")
}

func flipByte(b []byte) []byte {
	out := append([]byte(nil), b...)
	out[len(out)/2] ^= 0xFF
	return out
}

func replaceSums(b []byte) []byte {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			lines[i] = strings.Repeat("0", 64) + "  " + fields[1]
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// MakeSingleFileZip 造只有单个文件的 zip（layout.single 用的单文件布局夹具）。
func MakeSingleFileZip(name, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipEntry(zw, name, content, 0o755)
	closeAll(zw)
	return buf.Bytes()
}

// MakeTarGz 把 name→content 打成 tar.gz（不带 strip 前缀），用于构造非法/边界归档。
func MakeTarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		writeTarEntry(tw, name, content, 0o755, tar.TypeReg)
	}
	closeAll(tw, gz)
	return buf.Bytes()
}

// ZipEntry 是构造 zip 的一条目（支持目录与符号链接）。
type ZipEntry struct {
	Name    string
	Content string
	// Mode 为 0 时按常规文件 0755 处理；设为 os.ModeSymlink 时 Content 是链接目标。
	Mode os.FileMode
}

// MakeZip 按条目构造 zip（用于覆盖目录/符号链接等边界）。
func MakeZip(entries []ZipEntry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		mode := e.Mode
		if mode == 0 {
			mode = 0o755
		}
		hdr := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(e.Content)); err != nil {
			panic(err)
		}
	}
	closeAll(zw)
	return buf.Bytes()
}
