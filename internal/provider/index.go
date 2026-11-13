package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/spec"
)

// DefaultIndexTTL 是索引缓存有效期（方案 §6.2：首次 + 每 24h 一次 304）。
const DefaultIndexTTL = 24 * time.Hour

// RegistryAbbrevAccept 是 npm registry 的 abbreviated 元数据 MIME：
// 只含 versions/dist-tags/dist/engines，不含 readme/description，体积小很多。
const RegistryAbbrevAccept = "application/vnd.npm.install-v1+json"

// IndexFile 是 JSON 索引里列出的产物文件（go 的 dl 页有 sha256）。
type IndexFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

// Version 是索引里的一条可安装版本。
type Version struct {
	Version string
	LTS     string
	Date    string
	Engines map[string]string
	Shasum  string // npm registry 的 dist.shasum（sha1）
	// Integrity 是 npm registry 的 dist.integrity（sha512 SRI，比 shasum 强）。
	Integrity string
	// ArtifactName 是索引给出的精确产物文件名（go 的 go1.27.3.linux-amd64.tar.gz、
	// python 的 cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz）。
	ArtifactName string
	// Tag 是发布标签（python-build-standalone 的 20261026）。
	Tag   string
	Files []IndexFile
}

// FileFor 返回匹配 OS/Arch 的产物条目（go 用）。
func (v Version) FileFor(goos, goarch string) (IndexFile, bool) {
	for _, f := range v.Files {
		if f.OS == goos && f.Arch == goarch && (f.Kind == "" || f.Kind == "archive") {
			return f, true
		}
	}
	return IndexFile{}, false
}

// Index 是某工具在某个档位下的版本清单。
type Index struct {
	Provider *Provider
	Versions []Version
	// Stale 表示这份索引来自过期缓存（镜像拉不到，但还是能用）。
	Stale bool
	// Source 是实际命中/缓存的索引 URL。
	Source string
}

// IndexLoader 负责拉取与解析索引（Client 与缓存目录均可注入）。
type IndexLoader struct {
	Client   *fetch.Client
	CacheDir string
	TTL      time.Duration
	// Custom 是自定义索引形态的钩子（目前只有 pbs 两级目录列举，实现在 internal/toolchain，
	// 这样 provider 不必反过来依赖 toolchain，避免成环）。
	Custom func(ctx context.Context, p *Provider, profile string, client *fetch.Client, cacheDir string) ([]Version, error)
	// Stable 为真时只要稳定版的轻量索引（清单声明了 stable_urls 才有效），
	// 缓存文件也分开存（<id>.stable.json），不覆盖完整索引。
	Stable bool
}

// LoadResult 是 LoadAll 的一项结果（Provider 一定非 nil，Index/Err 二选一）。
type LoadResult struct {
	Provider *Provider
	Index    *Index
	Err      error
}

// LoadAll 并发加载多个工具的索引（`pk list --available` / `doctor --probe` 这类“一次问所有工具”的场景）。
//
// 为什么并发：一个工具的索引就一次 HTTP；串行时总耗时 = 所有工具之和（实测 6 个工具 3.8s，
// 最慢的 go 就占 2.0s）。并发后接近“最慢那个”，代价只是多几个 goroutine。
// 单个工具失败只写到它自己的 Err 上，不影响其他工具（调用方继续展示能拿到的）。
// 结果顺序与 ps 一致 —— 输出稳定才能被测试和脚本依赖。
func (l *IndexLoader) LoadAll(ctx context.Context, ps []*Provider, profile string) []LoadResult {
	res := make([]LoadResult, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p *Provider) {
			defer wg.Done()
			idx, err := l.Load(ctx, p, profile)
			res[i] = LoadResult{Provider: p, Index: idx, Err: err}
		}(i, p)
	}
	wg.Wait()
	return res
}

// Load 按镜像回退链依次尝试索引源。
func (l *IndexLoader) Load(ctx context.Context, p *Provider, profile string) (*Index, error) {
	urls, cacheName := p.IndexURLs(profile), p.ID+".json"
	if l.Stable && len(p.Index.StableURLs) > 0 { // 没声明轻量索引的工具照旧用完整索引（也不多存一份缓存）
		urls, cacheName = p.StableIndexURLs(profile), p.ID+".stable.json"
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("%s: 没有可用的索引源", p.ID)
	}
	if p.Index.Kind == IndexPBS {
		if l.Custom == nil {
			return nil, fmt.Errorf("%s: pbs 索引需要 toolchain 钩子（内部错误）", p.ID)
		}
		versions, err := l.Custom(ctx, p, profile, l.client(), l.CacheDir)
		if err != nil {
			return nil, err
		}
		return &Index{Provider: p, Versions: versions}, nil
	}

	var errs []string
	for _, url := range urls {
		bodyPath := filepath.Join(l.CacheDir, cacheName)
		metaPath := filepath.Join(l.CacheDir, p.ID+".meta.json")
		// registry 用 abbreviated 元数据：同一个包的完整元数据可能有几十 MB，
		// 而我们只要 versions[].version/engines/dist —— 小一个数量级，慢源上差别很大。
		accept := ""
		if p.Index.Kind == IndexRegistry {
			accept = RegistryAbbrevAccept
		}
		res, err := l.client().GetIndexAccept(ctx, url, bodyPath, metaPath, l.ttl(), accept)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		versions, err := parseIndexFor(p, res.Body)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		return &Index{Provider: p, Versions: versions, Stale: res.Stale, Source: url}, nil
	}
	return nil, fmt.Errorf("%s 的版本索引全部不可用：\n  %s\n建议：`pk mirror use cn` 换档，或检查代理设置", p.ID, strings.Join(errs, "\n  "))
}

// Resolve 把说明符解析成具体版本。
func (i *Index) Resolve(sp spec.Spec) (Version, error) {
	list := i.VersionList()
	if len(list) == 0 {
		return Version{}, fmt.Errorf("%s 的索引是空的（镜像异常？）", i.Provider.ID)
	}
	if sp.IsAlias() {
		if sp.Alias() == "lts" {
			best, ok := i.latestLTS()
			if ok {
				return best, nil
			}
			return Version{}, fmt.Errorf("%s 没有标记 LTS 的版本（镜像可能未同步）", i.Provider.ID)
		}
		latest, ok := spec.Latest(list)
		if !ok {
			return Version{}, fmt.Errorf("%s 索引里没有稳定版本", i.Provider.ID)
		}
		return i.mustFind(latest), nil
	}
	if best, ok := spec.Best(list, sp); ok {
		return i.mustFind(best), nil
	}
	return Version{}, i.notFound(sp)
}

// VersionList 返回全部版本号（保持索引顺序）。
func (i *Index) VersionList() []string {
	out := make([]string, 0, len(i.Versions))
	for _, v := range i.Versions {
		out = append(out, v.Version)
	}
	return out
}

// Find 按精确版本查找。
func (i *Index) Find(version string) (Version, bool) {
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(version)), "v")
	for _, v := range i.Versions {
		if strings.TrimPrefix(strings.ToLower(v.Version), "v") == want {
			return v, true
		}
	}
	return Version{}, false
}

// LatestLTS 返回最高的 LTS 稳定版本（`pk list <tool>` 单工具视图用）。
func (i *Index) LatestLTS() (Version, bool) {
	return i.latestLTS()
}

// Latest 返回最高稳定版本。
func (i *Index) Latest() (Version, bool) {
	latest, ok := spec.Latest(i.VersionList())
	if !ok {
		return Version{}, false
	}
	return i.mustFind(latest), true
}

func (i *Index) latestLTS() (Version, bool) {
	var best Version
	var bestVer *specSortKey
	for _, v := range i.Versions {
		if v.LTS == "" {
			continue
		}
		key := newSortKey(v.Version)
		if key == nil || spec.IsPrerelease(v.Version) {
			continue
		}
		if bestVer == nil || key.greater(bestVer) {
			best, bestVer = v, key
		}
	}
	return best, bestVer != nil
}

func (i *Index) mustFind(version string) Version {
	if v, ok := i.Find(version); ok {
		return v
	}
	return Version{Version: strings.TrimPrefix(version, "v")}
}

// notFound 给出"镜像滞后"这类可执行建议，而不是干巴巴的 404。
func (i *Index) notFound(sp spec.Spec) error {
	latest, hasLatest := i.Latest()
	hint := ""
	if hasLatest {
		hint = fmt.Sprintf("；当前索引最新为 v%s", latest.Version)
		if i.Stale {
			hint += "（来自过期缓存，镜像可能未同步）"
		}
		hint += fmt.Sprintf("，可用 `pk install %s@%s` 或 `pk mirror use default`", i.Provider.ID, latest.Version)
	}
	return fmt.Errorf("%s 没有满足 %q 的版本%s", i.Provider.ID, sp.String(), hint)
}

// parseIndexFor 在 parseIndex 之上做 provider 声明的版本号前缀剥离（go 的 "go1.27.3"）。
func parseIndexFor(p *Provider, data []byte) ([]Version, error) {
	versions, err := parseIndex(p.Index.Kind, data)
	if err != nil {
		return nil, err
	}
	prefix := p.Index.VersionPrefix
	if prefix == "" {
		return versions, nil
	}
	for i := range versions {
		versions[i].Version = strings.TrimPrefix(versions[i].Version, prefix)
	}
	return versions, nil
}

// parseIndex 按 provider 声明的索引形态解析。
func parseIndex(kind string, data []byte) ([]Version, error) {
	switch kind {
	case IndexJSON:
		return parseJSONIndex(data)
	case IndexRegistry:
		return parseRegistryIndex(data)
	case IndexDirList:
		return parseDirList(data)
	case IndexGitHub:
		return parseGitHubReleases(data)
	}
	return nil, fmt.Errorf("未知索引形态 %q", kind)
}

// parseJSONIndex 解析数组式索引（node 的 index.json、go 的 ?mode=json）。
func parseJSONIndex(data []byte) ([]Version, error) {
	var raw []struct {
		Version string          `json:"version"`
		LTS     json.RawMessage `json:"lts"`
		Date    string          `json:"date"`
		// node 的 files 是字符串数组（"linux-x64"、"headers"…），go 的是对象数组；
		// 两者都要能读，所以这里保留原始 JSON 再按形状解析。
		Files  json.RawMessage `json:"files"`
		SHA256 string          `json:"sha256"`
		Stable bool            `json:"stable"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("索引不是 JSON 数组（%v）", err)
	}
	out := make([]Version, 0, len(raw))
	for _, r := range raw {
		if r.Version == "" {
			continue
		}
		v := Version{
			Version: strings.TrimPrefix(r.Version, "v"),
			Date:    r.Date,
			Files:   parseFiles(r.Files),
			LTS:     parseLTS(r.LTS),
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("索引里没有任何版本")
	}
	return out, nil
}

// parseFiles 兼容两种 files 形状：对象数组（go）与字符串数组（node）。
func parseFiles(raw json.RawMessage) []IndexFile {
	if len(raw) == 0 {
		return nil
	}
	var objects []IndexFile
	if err := json.Unmarshal(raw, &objects); err == nil {
		return objects
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}
	out := make([]IndexFile, 0, len(names))
	for _, name := range names {
		out = append(out, IndexFile{Filename: name})
	}
	return out
}

// parseLTS 处理 "lts": "Jod" / true / false 三种写法。
func parseLTS(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil && b {
		return "lts"
	}
	return ""
}

// versionPattern 从目录名/标签里抠出语义化版本（"bun-v1.4.4/" → 1.4.4）。
// 版本后面的 -xxx 可能是预发布标记（rc.1/beta.2），也可能是平台词（darwin/arm64），
// 所以先全部抠下来，再按 platformWords 判定（不再靠“只认几个前缀”的名单）。
var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*))?`)

// platformWords 是文件名里常见的平台/架构词：出现在版本后的第一段就不是预发布。
var platformWords = map[string]bool{
	"darwin": true, "linux": true, "windows": true, "win32": true, "win64": true,
	"arm64": true, "amd64": true, "x64": true, "x86": true, "x86_64": true,
	"aarch64": true, "armv7l": true, "armv6l": true, "i686": true, "musl": true,
}

// extractVersion 从任意名字里取第一个版本号（取不到返回空串）。
func extractVersion(name string) string {
	m := versionPattern.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	v := m[1] + "." + m[2] + "." + m[3]
	suffix := strings.Split(m[4], ".")[0] // "rc.1" → "rc"
	if m[4] != "" && !platformWords[strings.ToLower(suffix)] {
		v += "-" + m[4]
	}
	return v
}

// parseDirList 解析 npmmirror 的目录列举 JSON：
// [{"name":"bun-v1.4.4/","type":"dir","url":"…"}, …]（只取目录条目）。
func parseDirList(data []byte) ([]Version, error) {
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Date string `json:"date"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("目录列举不是 JSON 数组（%v）", err)
	}
	out := make([]Version, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Type != "" && e.Type != "dir" {
			continue
		}
		if e.Type == "" && !strings.HasSuffix(e.Name, "/") {
			continue
		}
		version := extractVersion(e.Name)
		if version == "" || seen[version] {
			continue
		}
		seen[version] = true
		out = append(out, Version{Version: version, Date: e.Date})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("目录列举里没有版本目录")
	}
	return out, nil
}

// parseGitHubReleases 解析 GitHub Releases API 的数组（只看 tag_name）。
func parseGitHubReleases(data []byte) ([]Version, error) {
	var releases []struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		Prerelease  bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("GitHub releases 不是 JSON 数组（%v）", err)
	}
	out := make([]Version, 0, len(releases))
	seen := map[string]bool{}
	for _, r := range releases {
		version := extractVersion(r.TagName)
		if version == "" || seen[version] {
			continue
		}
		seen[version] = true
		out = append(out, Version{Version: version, Date: r.PublishedAt})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("GitHub releases 里没有可用版本")
	}
	return out, nil
}

// nodeEngines 从 engines 里只取 node 一项（别的字段可能是复杂对象）。
func nodeEngines(engines map[string]json.RawMessage) map[string]string {
	if len(engines) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, raw := range engines {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseRegistryIndex 解析 npm registry 元数据（外部调用方：install 取平台包的 dist.integrity）。
func ParseRegistryIndex(data []byte) ([]Version, error) { return parseRegistryIndex(data) }

// parseRegistryIndex 解析 npm registry 元数据（versions + engines + dist）。
func parseRegistryIndex(data []byte) ([]Version, error) {
	var doc struct {
		Versions map[string]struct {
			Version string `json:"version"`
			// engines 里不只有字符串：pnpm 11 的 engines.runtime 是对象，所以先收原样再挑 node。
			Engines map[string]json.RawMessage `json:"engines"`
			Dist    struct {
				Shasum    string `json:"shasum"`
				Integrity string `json:"integrity"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("registry 元数据解析失败（%v）", err)
	}
	if len(doc.Versions) == 0 {
		return nil, fmt.Errorf("registry 元数据里没有 versions")
	}
	out := make([]Version, 0, len(doc.Versions))
	for key, meta := range doc.Versions {
		version := meta.Version
		if version == "" {
			version = key
		}
		out = append(out, Version{
			Version:   strings.TrimPrefix(version, "v"),
			Engines:   nodeEngines(meta.Engines),
			Shasum:    meta.Dist.Shasum,
			Integrity: meta.Dist.Integrity,
		})
	}
	// 稳定顺序：按 semver 降序，保证输出可复现（golden / 测试友好）。
	sorted := spec.SortDesc(versionStrings(out))
	byVersion := make(map[string]Version, len(out))
	for _, v := range out {
		byVersion[v.Version] = v
	}
	ordered := make([]Version, 0, len(out))
	for _, vs := range sorted {
		if v, ok := byVersion[vs]; ok {
			ordered = append(ordered, v)
		}
	}
	// 预发布版本也要保留（显式钉预发布时才用得上）。
	for _, v := range out {
		if spec.IsPrerelease(v.Version) {
			ordered = append(ordered, v)
		}
	}
	return ordered, nil
}

func versionStrings(vs []Version) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
}

// specSortKey 是一个只用于比较的轻量包装（避免在 provider 里再引 semver 类型）。
type specSortKey struct {
	major, minor, patch int
}

func newSortKey(version string) *specSortKey {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 3 {
		return nil
	}
	nums := make([]int, 3)
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return &specSortKey{nums[0], nums[1], nums[2]}
}

func (k *specSortKey) greater(other *specSortKey) bool {
	if k.major != other.major {
		return k.major > other.major
	}
	if k.minor != other.minor {
		return k.minor > other.minor
	}
	return k.patch > other.patch
}

func (l *IndexLoader) client() *fetch.Client {
	if l.Client != nil {
		return l.Client
	}
	return fetch.New()
}

func (l *IndexLoader) ttl() time.Duration {
	if l.TTL > 0 {
		return l.TTL
	}
	return DefaultIndexTTL
}

// SortVersionsDesc 返回降序版本号（展示用）。
func SortVersionsDesc(vs []Version) []string {
	return spec.SortDesc(versionStrings(vs))
}
