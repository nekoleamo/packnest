// Package toolchain 处理"非标准语义"的两个工具：go 与 python（方案 §4.0 / §18）。
//
//   - go：装完必须把 GOTOOLCHAIN=local 写进环境，否则 go 会自己去下一套工具链
//     （goproxy.cn 确实能提供 golang.org/toolchain，所以这条不是形式主义）。
//   - python：只装 python-build-standalone 预编译版，永不源码编译；它的索引是
//     **两级目录列举**（release 标签目录 → 目录下的文件），不是 index.json。
package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/provider"
)

// GoToolchainEnv 是 go 必须写进环境的那一条（方案 §6.6）。
const GoToolchainEnv = "GOTOOLCHAIN=local"

// CheckGoEnv 检查已生效的环境里是否有 GOTOOLCHAIN=local（doctor 用）。
//
// 返回空字符串表示没问题；否则返回给人看的告警文本。
func CheckGoEnv(lookup func(string) string) string {
	if lookup == nil {
		return ""
	}
	if v := strings.TrimSpace(lookup("GOTOOLCHAIN")); v != "local" {
		return "GOTOOLCHAIN 不是 local：go 会按 go.mod 的 toolchain 声明自己再下一次工具链（多一个下载面）。" +
			"用 `eval \"$(pk env --shell bash)\"` 让 GOTOOLCHAIN=local 生效"
	}
	return ""
}

// pbsFile 是 python-build-standalone 的文件名解析结果。
type pbsFile struct {
	Version  string
	Tag      string
	Platform string
	Filename string
}

// pbsPattern 匹配 cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz。
var pbsPattern = regexp.MustCompile(`^cpython-(\d+\.\d+\.\d+)\+(\d+)-(.+)-install_only\.tar\.gz$`)

// versionDirs 解析 npmmirror 的目录列举，返回 release 标签（"20261026"），新的在前。
func versionDirs(data []byte) ([]string, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("目录列举不是 JSON 数组（%v）", err)
	}
	var tags []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name, "/")
		if e.Type != "" && e.Type != "dir" {
			continue
		}
		if !isTag(name) {
			continue
		}
		tags = append(tags, name)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("目录列举里没有 release 标签目录")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(tags)))
	return tags, nil
}

// isTag 判断是否是 pbs 的发布标签（8 位日期）。
func isTag(name string) bool {
	if len(name) != 8 {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseFiles 从 tag 目录列举里挑出 install_only 的 cpython 产物。
func parseFiles(data []byte, tag string, platformByName map[string]string) ([]pbsFile, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("tag 目录列举不是 JSON 数组（%v）", err)
	}
	var out []pbsFile
	for _, e := range entries {
		if e.Type != "" && e.Type != "file" {
			continue
		}
		m := pbsPattern.FindStringSubmatch(e.Name)
		if m == nil {
			continue // 跳过 debug/full/stripped/bsdiff 等变体
		}
		if _, ok := platformByName[m[3]]; !ok {
			continue // 只留清单声明过的平台
		}
		out = append(out, pbsFile{Version: m[1], Tag: m[2], Platform: m[3], Filename: e.Name})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tag %s 里没有 install_only 产物", tag)
	}
	return out, nil
}

// LoadPBSIndex 是 provider.IndexLoader.Custom 的实现：
// 两级目录列举（标签目录 → 文件），同版本的多平台产物合成一条 Version。
func LoadPBSIndex(ctx context.Context, p *provider.Provider, profile string, client *fetch.Client, cacheDir string) ([]provider.Version, error) {
	urls := p.IndexURLs(profile)
	if len(urls) == 0 {
		return nil, fmt.Errorf("%s: 没有可用的索引源", p.ID)
	}
	root := strings.TrimRight(urls[0], "/")

	// 平台串 → "goos-goarch" 的反查表（只留清单声明过的）。
	platformByName := map[string]string{}
	for key, name := range p.Artifact.Platform {
		platformByName[name] = key
	}
	var lastErr error

	// 1. 找最新的 release 标签（用缓存与 ETag，避免每次全量拉）。
	res, err := client.GetIndex(ctx, root+"/", filepath.Join(cacheDir, p.ID+"-tags.json"),
		filepath.Join(cacheDir, p.ID+"-tags.meta.json"), provider.DefaultIndexTTL)
	if err != nil {
		return nil, fmt.Errorf("%s 的 release 标签列表拉不到: %w", p.ID, err)
	}
	tags, err := versionDirs(res.Body)
	if err != nil {
		return nil, err
	}

	// 2. 逐个标签往下看，直到拿到产物（最新标签可能还没同步完）。
	for _, tag := range tags {
		tagBody, err := client.GetIndex(ctx, fmt.Sprintf("%s/%s/", root, tag),
			filepath.Join(cacheDir, p.ID+"-"+tag+".json"),
			filepath.Join(cacheDir, p.ID+"-"+tag+".meta.json"), provider.DefaultIndexTTL)
		if err != nil {
			lastErr = err
			continue
		}
		files, err := parseFiles(tagBody.Body, tag, platformByName)
		if err != nil {
			lastErr = err
			continue
		}
		versions, err := groupPBS(files, platformByName, tag)
		if err != nil {
			lastErr = err
			continue
		}
		return versions, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 release 标签")
	}
	return nil, fmt.Errorf("%s: 目录列举里没找到可用产物: %w", p.ID, lastErr)
}

// groupPBS 把文件列表按 cpython 版本聚合成 Version（每版本记下各平台文件名）。
func groupPBS(files []pbsFile, platformByName map[string]string, tag string) ([]provider.Version, error) {
	byVersion := map[string]*provider.Version{}
	order := make([]string, 0, len(files))
	for _, f := range files {
		v, ok := byVersion[f.Version]
		if !ok {
			v = &provider.Version{Version: f.Version, Tag: tag}
			byVersion[f.Version] = v
			order = append(order, f.Version)
		}
		key := platformByName[f.Platform]
		goos, goarch := splitPlatform(key)
		v.Files = append(v.Files, provider.IndexFile{
			Filename: f.Filename, OS: goos, Arch: goarch, Kind: "archive",
		})
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("没有解析出任何 cpython 版本")
	}
	out := make([]provider.Version, 0, len(order))
	for _, version := range order {
		out = append(out, *byVersion[version])
	}
	return out, nil
}

// splitPlatform 把 "darwin-arm64" 拆成 goos/goarch。
func splitPlatform(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '-' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
