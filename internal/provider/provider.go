// Package provider 加载内嵌的 provider 清单，并做 URL 模板渲染（方案 §5.3）。
//
// 一个 provider 描述"某个运行时从哪取版本索引、从哪下产物、怎么校验、怎么解压落地"。
// 全部是声明式 TOML，不执行任何代码。
package provider

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/providers"
)

// 归档扩展名。
const (
	ExtTarGz = "tar.gz"
	ExtZip   = "zip"
	ExtTgz   = "tgz"
)

// 清单来源。
const (
	SourceEmbedded = "embedded" // //go:embed 编进二进制的那 6 份
	SourceExternal = "external" // 用户 / 内网自己放的 TOML
)

// 索引形态。
const (
	IndexJSON     = "json"     // 一次性 JSON（node index.json、npm registry 元数据）
	IndexRegistry = "registry" // npm registry 元数据（versions/engines/dist）
	IndexDirList  = "dirlist"  // 目录列举式 JSON（npmmirror 的 /-/binary/<tool>/）
	// IndexGitHub 是 GitHub Releases API（bun 的"官方源"）。
	IndexGitHub = "github-releases"
	// IndexPBS 是 python-build-standalone 的两级目录列举（release 标签 → 文件）。
	// 解析在 internal/toolchain 里，provider 只负责声明。
	IndexPBS = "pbs"
)

// 校验和形态。
const (
	ChecksumGNUSums     = "gnu-sums"           // SHASUMS256.txt 风格
	ChecksumRegistry    = "registry-integrity" // npm registry 的 dist.integrity/shasum
	ChecksumPerArtifact = "per-artifact"       // 与产物同目录的 <name>.sha256
	// ChecksumIndexInline 表示校验和就在版本索引里（go 的 dl JSON 自带 sha256）。
	ChecksumIndexInline = "index-inline"
)

// Provider 是一个工具的声明式清单。
type Provider struct {
	ID       string   `toml:"id"`
	Display  string   `toml:"display"`
	Requires []string `toml:"requires"`
	// Source / Dir / InsecureTransport 是加载期算出来的，不来自 TOML。
	Source string `toml:"-"`
	Dir    string `toml:"-"`
	// InsecureTransport 表示清单里有被 PK_ALLOW_HTTP 放行的 http:// 源（校验按弱处理）。
	InsecureTransport bool `toml:"-"`
	// Kind 区分普通工具（binary）与 registry 上的 JS 包（registry）。
	Kind       string               `toml:"kind"`
	Index      IndexBlock           `toml:"index"`
	Artifact   ArtifactBlock        `toml:"artifact"`
	Checksum   ChecksumBlock        `toml:"checksum"`
	Layout     LayoutBlock          `toml:"layout"`
	Companions map[string]Companion `toml:"companions"`
	Toolchain  *ToolchainBlock      `toml:"toolchain"`
}

// IndexBlock 描述版本索引的来源。
type IndexBlock struct {
	Kind string            `toml:"kind"`
	URLs map[string]string `toml:"urls"`
	// Path 是索引里的字段路径（如 "versions"），dirlist 时不用。
	Path string `toml:"path"`
	// VersionPrefix 是版本号前缀（go 的 "go1.27.3" → 去掉 "go"）。
	VersionPrefix string `toml:"version_prefix"`
}

// ArtifactBlock 描述产物下载地址与平台命名。
type ArtifactBlock struct {
	URLs map[string]string `toml:"urls"`
	// Ext 是 os → 归档扩展名。
	Ext map[string]string `toml:"ext"`
	// Platform 是 "<goos>-<goarch>" → 产物命名里的平台串。
	Platform map[string]string `toml:"platform"`
	// MinVersion 是最低可安装版本（产物地址换了形态时用，例如 pnpm 的平台包从 11 才有）。
	// 低于它的版本在解析后立刻报错，给可执行建议，而不是让用户撞 404。
	MinVersion string `toml:"min_version"`
}

// ChecksumBlock 描述校验和来源。校验源必须与产物源解耦（方案 §6.4）。
type ChecksumBlock struct {
	Format string            `toml:"format"`
	Mode   string            `toml:"mode"`
	URLs   map[string]string `toml:"urls"`
}

// LayoutBlock 描述解压布局。
type LayoutBlock struct {
	// Strip 是剥掉的路径层级（node tar 是 1）。
	Strip int `toml:"strip"`
	// Single 非空表示产物是单文件，落到该相对路径（layout.single）。
	Single string `toml:"single"`
	// Bins 是要暴露到 shim 的命令名。
	Bins []string `toml:"bins"`
	// BinTargets 是 bin 名 → 解压后树内的相对路径（npm/pnpm 的入口是 .cjs）。
	BinTargets map[string]string `toml:"bin_targets"`
}

// Companion 描述伴生包（node ↔ npm，以及外部清单自定义的任意伴生包）。
type Companion struct {
	Mode     string `toml:"mode"`              // bundled | latest-compatible | pinned
	Registry string `toml:"registry"`          // 索引来源 provider id（npm）
	Target   string `toml:"target"`            // 相对工具根目录的目标路径
	Version  string `toml:"version,omitempty"` // 仅 mode = "pinned" 时用
}

// ToolchainBlock 描述装完必须写进环境的东西（go 的 GOTOOLCHAIN）。
type ToolchainBlock struct {
	Env map[string]string `toml:"env"`
}

var (
	once  sync.Once
	cache []*Provider
	err   error
)

// All 返回全部 provider（内嵌 + 外部，按 ID 排序），结果缓存在进程内。
//
// 外部清单出任何问题都不影响本函数的成功返回（方案 A.7）：问题记在 ExternalIssues()。
func All() ([]*Provider, error) {
	once.Do(func() {
		if cache, err = loadFS(providers.FS); err != nil {
			return
		}
		cache = append(cache, loadExternal(ExternalDirs(), cache)...)
		sort.Slice(cache, func(i, j int) bool { return cache[i].ID < cache[j].ID })
	})
	return cache, err
}

// Get 按 ID 取 provider。
func Get(id string) (*Provider, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("未知工具 %q，可用: %s", id, strings.Join(IDs(), " / "))
}

// IDs 返回全部工具 ID（排序后）。
func IDs() []string {
	all, err := All()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(all))
	for _, p := range all {
		ids = append(ids, p.ID)
	}
	return ids
}

func loadFS(fsys fs.FS) ([]*Provider, error) {
	entries, err := fs.Glob(fsys, "*.toml")
	if err != nil {
		return nil, fmt.Errorf("扫描内嵌 provider 失败: %w", err)
	}
	sort.Strings(entries)
	out := make([]*Provider, 0, len(entries))
	for _, name := range entries {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", name, err)
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		want := strings.TrimSuffix(name, ".toml")
		if p.ID != want {
			return nil, fmt.Errorf("%s: id = %q 与文件名不一致", name, p.ID)
		}
		p.Source = SourceEmbedded
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有内嵌任何 provider")
	}
	return out, nil
}

// Parse 解析并校验单个 provider TOML。
func Parse(data []byte) (*Provider, error) {
	var p Provider
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("TOML 解析失败: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Compile 把工具版本说明符编译成 spec.Spec，并在 provider 不存在时立刻报错。
func (p *Provider) Compile(versionSpec string) (spec.Spec, error) {
	sp, err := spec.Parse(versionSpec)
	if err != nil {
		return spec.Spec{}, err
	}
	return sp, nil
}

// Validate 校验清单自洽（启动即失败，别等运行时）。
func (p *Provider) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("缺少 id")
	}
	if p.Display == "" {
		return fmt.Errorf("%s: 缺少 display", p.ID)
	}
	switch p.Index.Kind {
	case IndexJSON, IndexRegistry, IndexDirList, IndexGitHub, IndexPBS:
	default:
		return fmt.Errorf("%s: index.kind = %q 非法（json/registry/dirlist/github-releases/pbs）", p.ID, p.Index.Kind)
	}
	if len(p.Index.URLs) == 0 {
		return fmt.Errorf("%s: 缺少 index.urls", p.ID)
	}
	for name, u := range p.Index.URLs {
		if strings.TrimSpace(u) == "" {
			return fmt.Errorf("%s: index.urls.%s 为空", p.ID, name)
		}
	}
	if len(p.Artifact.URLs) == 0 {
		return fmt.Errorf("%s: 缺少 artifact.urls", p.ID)
	}
	if p.Layout.Single == "" {
		for _, goos := range []string{"darwin", "linux", "windows"} {
			ext, ok := p.Artifact.Ext[goos]
			if !ok || ext == "" {
				return fmt.Errorf("%s: artifact.ext 缺少 %s（只允许 tar.gz/zip/tgz）", p.ID, goos)
			}
			switch ext {
			case ExtTarGz, ExtZip, ExtTgz:
			default:
				return fmt.Errorf("%s: artifact.ext.%s = %q 非法（只允许 tar.gz/zip/tgz）", p.ID, goos, ext)
			}
		}
	}
	for _, key := range []string{"darwin-arm64", "linux-amd64", "windows-amd64"} {
		if p.Artifact.Platform[key] == "" {
			return fmt.Errorf("%s: artifact.platform 缺少 %s", p.ID, key)
		}
	}
	if len(p.Layout.Bins) == 0 {
		return fmt.Errorf("%s: layout.bins 不能为空", p.ID)
	}
	switch p.Checksum.Format {
	case ChecksumGNUSums, ChecksumRegistry, ChecksumPerArtifact, ChecksumIndexInline:
	default:
		return fmt.Errorf("%s: checksum.format = %q 非法", p.ID, p.Checksum.Format)
	}
	switch p.Checksum.Mode {
	case "tofu-then-verify", "verify", "none":
	default:
		return fmt.Errorf("%s: checksum.mode = %q 非法（tofu-then-verify/verify/none）", p.ID, p.Checksum.Mode)
	}
	if p.Checksum.Mode != "none" && p.Checksum.Format != ChecksumRegistry && p.Checksum.Format != ChecksumIndexInline && len(p.Checksum.URLs) == 0 {
		return fmt.Errorf("%s: 缺少 checksum.urls（校验源必须与产物源解耦）", p.ID)
	}
	for name, c := range p.Companions {
		switch c.Mode {
		case "bundled", "latest-compatible", "pinned":
		default:
			return fmt.Errorf("%s: companions.%s.mode = %q 非法", p.ID, name, c.Mode)
		}
		if c.Registry == "" || c.Target == "" {
			return fmt.Errorf("%s: companions.%s 缺少 registry/target", p.ID, name)
		}
		if c.Mode == "pinned" && c.Version == "" {
			return fmt.Errorf("%s: companions.%s 是 pinned，需同时给 version", p.ID, name)
		}
	}
	return nil
}

// 内网/自定义源覆盖（方案 §6.5）：PK_MIRROR_<TOOL>_<KIND> 优先级高于一切。
const (
	envMirrorRegistry = "PK_MIRROR_REGISTRY"
	envMirrorPrefix   = "PK_MIRROR_"
)

func envOverride(tool, kind string) string {
	return strings.TrimSpace(os.Getenv(envMirrorPrefix + strings.ToUpper(tool) + "_" + kind))
}

// registryBase 返回 npm registry 基地址（PK_MIRROR_REGISTRY 优先）。
func registryBase(profile string) string {
	if v := strings.TrimSpace(os.Getenv(envMirrorRegistry)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return mirror.RegistryFor(profile)
}

// IndexURLs 返回按镜像回退链排好序的索引 URL 列表。
func (p *Provider) IndexURLs(profile string) []string {
	urls := mirror.PickURLs(p.Index.URLs, mirror.Chain(profile))
	if override := envOverride(p.ID, "INDEX"); override != "" {
		urls = append([]string{override}, urls...)
	}
	return urls
}

// ArtifactURL 渲染单个产物 URL。
func (p *Provider) ArtifactURL(version, goos, goarch, profile string) (string, error) {
	tmpl, ok := p.Artifact.URLs[pickProfileKey(p.Artifact.URLs, profile)]
	if !ok {
		return "", fmt.Errorf("%s: artifact.urls 缺少可用档位", p.ID)
	}
	vars, err := p.urlVars(Version{Version: version}, goos, goarch, profile)
	if err != nil {
		return "", err
	}
	return render(tmpl, vars)
}

// urlVars 构造模板变量：{version} {platform} {ext} {registry} {filename} {tag}。
func (p *Provider) urlVars(v Version, goos, goarch, profile string) (map[string]string, error) {
	platform, err := p.Platform(goos, goarch)
	if err != nil {
		return nil, err
	}
	ext, err := p.ExtFor(goos)
	if err != nil {
		return nil, err
	}
	filename := v.ArtifactName
	if f, ok := v.FileFor(goos, goarch); filename == "" && ok {
		filename = f.Filename
	}
	vars := map[string]string{
		"version":  v.Version,
		"platform": platform,
		"ext":      ext,
		"registry": registryBase(profile),
		"tool":     p.ID,
		"filename": filename,
		"tag":      v.Tag,
	}
	if filename == "" {
		// 索引没有文件清单（github-releases / dirlist）时 {filename} 会渲染成空串（抓 `.../.sha256` 404）。
		// 用产物 URL 的末段兜底：产物名本来就只由 {version}/{platform}/{ext} 决定，不引入新信息。
		if tmpl := p.Artifact.URLs[pickProfileKey(p.Artifact.URLs, profile)]; !strings.Contains(tmpl, "{filename}") {
			if u, err := render(tmpl, vars); err == nil {
				vars["filename"] = path.Base(u)
			}
		}
	}
	return vars, nil
}

// ArtifactURLs 返回按回退链排好序的产物 URL 列表（同一版本所有档位）。
func (p *Provider) ArtifactURLs(version, goos, goarch, profile string) ([]string, error) {
	return p.ArtifactURLsFor(Version{Version: version}, goos, goarch, profile)
}

// ArtifactURLsFor 与 ArtifactURLs 相同，但带着索引里的版本元数据（{filename}/{tag}）。
func (p *Provider) ArtifactURLsFor(v Version, goos, goarch, profile string) ([]string, error) {
	vars, err := p.urlVars(v, goos, goarch, profile)
	if err != nil {
		return nil, err
	}
	urls, err := renderAll(p.Artifact.URLs, vars, profile)
	if err != nil {
		return nil, fmt.Errorf("%s 产物 URL 渲染失败: %w", p.ID, err)
	}
	if override := envOverride(p.ID, "ARTIFACT"); override != "" {
		if u, err := render(override, vars); err == nil {
			urls = append([]string{u}, urls...)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("%s: 没有可用的产物源", p.ID)
	}
	return urls, nil
}

// ChecksumURLs 返回按回退链排好序的校验和 URL 列表。
func (p *Provider) ChecksumURLs(version, goos, goarch, profile string) ([]string, error) {
	return p.ChecksumURLsFor(Version{Version: version}, goos, goarch, profile)
}

// ChecksumURLsFor 携带索引元数据（{filename}/{tag}）的校验和 URL 列表。
func (p *Provider) ChecksumURLsFor(v Version, goos, goarch, profile string) ([]string, error) {
	if len(p.Checksum.URLs) == 0 {
		return nil, nil
	}
	vars, err := p.urlVars(v, goos, goarch, profile)
	if err != nil {
		return nil, err
	}
	urls, err := renderAll(p.Checksum.URLs, vars, profile)
	if err != nil {
		return nil, fmt.Errorf("%s 校验和 URL 渲染失败: %w", p.ID, err)
	}
	if override := envOverride(p.ID, "CHECKSUM"); override != "" {
		if u, err := render(override, vars); err == nil {
			urls = append([]string{u}, urls...)
		}
	}
	return urls, nil
}

// renderAll 渲染一组档位模板，返回按回退链排好序的 URL 列表。
//
// 含 {registry} 的模板（npm/pnpm 的 registry tarball）要**按链上每个档位各渲染一次**：
// 否则"国内档挂了我们退回官方 registry"这件事根本不会发生（实例：npmmirror 的
// tarball 路径返回 500 时，必须能自动退到 registry.npmjs.org）。
func renderAll(templates map[string]string, vars map[string]string, profile string) ([]string, error) {
	hasRegistry := false
	for _, tmpl := range templates {
		if strings.Contains(tmpl, "{registry}") {
			hasRegistry = true
			break
		}
	}
	if !hasRegistry {
		rendered := make(map[string]string, len(templates))
		for k, tmpl := range templates {
			u, err := render(tmpl, vars)
			if err != nil {
				return nil, err
			}
			rendered[k] = u
		}
		return mirror.PickURLs(rendered, mirror.Chain(profile)), nil
	}

	var out []string
	seen := map[string]bool{}
	for _, name := range mirror.Chain(profile) {
		for _, key := range sortedKeys(templates) {
			tmpl := templates[key]
			if !strings.Contains(tmpl, "{registry}") {
				continue
			}
			local := make(map[string]string, len(vars)+1)
			for k, v := range vars {
				local[k] = v
			}
			local["registry"] = registryBase(name)
			u, err := render(tmpl, local)
			if err != nil {
				return nil, err
			}
			if seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	// 不含 {registry} 的模板（自建源）追加在最后。
	for _, key := range sortedKeys(templates) {
		tmpl := templates[key]
		if strings.Contains(tmpl, "{registry}") {
			continue
		}
		u, err := render(tmpl, vars)
		if err != nil {
			return nil, err
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Platform 返回产物命名里的平台串。
func (p *Provider) Platform(goos, goarch string) (string, error) {
	key := goos + "-" + goarch
	if v, ok := p.Artifact.Platform[key]; ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s 不支持平台 %s（可支持: %s）", p.ID, key, strings.Join(PlatformKeys(p), " / "))
}

// PlatformKeys 返回清单里支持的平台键（排序）。
func PlatformKeys(p *Provider) []string {
	keys := make([]string, 0, len(p.Artifact.Platform))
	for k := range p.Artifact.Platform {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ExtFor 返回某平台的归档扩展名。
func (p *Provider) ExtFor(goos string) (string, error) {
	if p.Layout.Single != "" && p.Artifact.Ext == nil {
		return "", nil
	}
	if e, ok := p.Artifact.Ext[goos]; ok && e != "" {
		return e, nil
	}
	return "", fmt.Errorf("%s: 不支持平台 %s", p.ID, goos)
}

// pickProfileKey 选模板档位：先精确档位，再依次回落。
func pickProfileKey(urls map[string]string, profile string) string {
	if profile == "" {
		profile = mirror.AutoProfile
	}
	if profile == mirror.AutoProfile {
		profile = "cn"
	}
	if _, ok := urls[profile]; ok {
		return profile
	}
	for _, name := range mirror.Chain(profile) {
		if _, ok := urls[name]; ok {
			return name
		}
	}
	return ""
}

// render 渲染 {placeholder} 模板，遇到未知占位符直接报错（不静默留空）。
func render(tmpl string, vars map[string]string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '{' {
			b.WriteByte(tmpl[i])
			continue
		}
		end := strings.IndexByte(tmpl[i:], '}')
		if end < 0 {
			return "", fmt.Errorf("URL 模板 %q 有大括号未闭合", tmpl)
		}
		key := tmpl[i+1 : i+end]
		val, ok := vars[key]
		if !ok {
			return "", fmt.Errorf("URL 模板 %q 含未知占位符 {%s}", tmpl, key)
		}
		b.WriteString(val)
		i += end
	}
	return b.String(), nil
}
