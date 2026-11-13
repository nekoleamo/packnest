// Package store 管全局配置（config.toml）与安装台账（installs.json）。
//
// 台账是唯一事实源：uninstall 只回收台账里的记录，不在台账里的目录只提示不删
// （方案 §8.2 第 3 条）。所有写盘都是"先 .bak 再原子替换"。
package store

import (
	"encoding/json"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/packnest/packnest/internal/paths"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LedgerSchema 是台账格式版本（版本号字段 + 手写 up 函数，不做迁移框架）。
const LedgerSchema = 1

// 引用类型。
const (
	RefGlobal  = "global"  // 全局默认版本
	RefProject = "project" // 项目锁
	RefTool    = "tool"    // 被其他工具依赖（pnpm → node）
)

// Ref 是一条引用（引用计数用）。
type Ref struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

// Install 是台账里的一条安装记录。
type Install struct {
	Tool        string `json:"tool"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	Source      string `json:"source,omitempty"` // 镜像档名
	SHA256      string `json:"sha256,omitempty"`
	Verified    string `json:"verified,omitempty"`  // strong | weak | unverified
	Transport   string `json:"transport,omitempty"` // 空=全 https；http=清单里有放行的 http 源
	CompanionOf string `json:"companion_of,omitempty"`
	NpmBundled  string `json:"npm_bundled,omitempty"` // node 自带 npm 版本
	NpmVersion  string `json:"npm_version,omitempty"` // 实际生效的 npm 版本
	Channel     string `json:"install_channel,omitempty"`
	// SizeBytes 是安装目录占用（装完算一次，`pk list` 直接读 —— 别每次 walk 十几万文件）。
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	References  []Ref     `json:"references,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// Versions 返回某工具的已装版本（升序）。
func (l *Ledger) Versions(tool string) []string {
	var out []string
	for _, in := range l.Installs {
		if in.Tool == tool {
			out = append(out, in.Version)
		}
	}
	sort.Strings(out)
	return out
}

// Ledger 是 state/installs.json 的内容。
type Ledger struct {
	Schema   int       `json:"schema"`
	Installs []Install `json:"installs"`
}

// LoadLedger 读取台账；文件不存在返回空台账（不是错误）。
func LoadLedger(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Ledger{Schema: LedgerSchema}, nil
		}
		return nil, fmt.Errorf("读取台账失败: %w", err)
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("台账 %q 损坏（%v）；可执行 `pk doctor` 查看恢复建议", path, err)
	}
	if l.Schema == 0 {
		l.Schema = LedgerSchema
	}
	return &l, nil
}

// LoadLedgerWithRecovery 读取台账；主文件损坏时退回 .bak，并返回恢复标记。
// 两个都不可用才报错（doctor 会把错误码报出来）。
func LoadLedgerWithRecovery(path string) (*Ledger, bool, error) {
	l, err := LoadLedger(path)
	if err == nil {
		return l, false, nil
	}
	if _, statErr := os.Stat(path + ".bak"); statErr != nil {
		return nil, false, err // 没有备份可利用，原始错误就是结论
	}
	backup, bakErr := LoadLedger(path + ".bak")
	if bakErr != nil {
		return nil, false, err
	}
	return backup, true, nil
}

// Find 按工具与版本查记录。
func (l *Ledger) Find(tool, version string) *Install {
	for i := range l.Installs {
		if l.Installs[i].Tool == tool && l.Installs[i].Version == version {
			return &l.Installs[i]
		}
	}
	return nil
}

// Active 返回某工具被引用为全局默认的版本（空串表示没有）。
func (l *Ledger) Active(tool string) string {
	for _, in := range l.Installs {
		if in.Tool != tool {
			continue
		}
		for _, ref := range in.References {
			if ref.Kind == RefGlobal {
				return in.Version
			}
		}
	}
	return ""
}

// Upsert 插入或覆盖同工具同版本的记录。
func (l *Ledger) Upsert(in Install) {
	if in.InstalledAt.IsZero() {
		in.InstalledAt = time.Now().UTC()
	}
	for i := range l.Installs {
		if l.Installs[i].Tool == in.Tool && l.Installs[i].Version == in.Version {
			l.Installs[i] = in
			return
		}
	}
	l.Installs = append(l.Installs, in)
}

// Remove 删除一条记录，返回是否真的删掉。
func (l *Ledger) Remove(tool, version string) bool {
	for i := range l.Installs {
		if l.Installs[i].Tool == tool && l.Installs[i].Version == version {
			l.Installs = append(l.Installs[:i], l.Installs[i+1:]...)
			return true
		}
	}
	return false
}

// AddRef 追加一条引用（跳过重复）。
func (l *Ledger) AddRef(tool, version string, ref Ref) error {
	in := l.Find(tool, version)
	if in == nil {
		return fmt.Errorf("%s@%s 不在台账里", tool, version)
	}
	for _, existing := range in.References {
		if existing.Kind == ref.Kind && existing.Name == ref.Name {
			return nil
		}
	}
	in.References = append(in.References, ref)
	return nil
}

// Save 先备份为 .bak 再原子替换。
func (l *Ledger) Save(path string) error {
	if l.Schema == 0 {
		l.Schema = LedgerSchema
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化台账失败: %w", err)
	}
	return WriteAtomicWithBackup(path, data)
}

// Validate 检查台账自洽（doctor 用）。
func (l *Ledger) Validate() []string {
	var issues []string
	seen := make(map[string]bool, len(l.Installs))
	for _, in := range l.Installs {
		key := in.Tool + "@" + in.Version
		if seen[key] {
			issues = append(issues, fmt.Sprintf("台账有重复记录: %s", key))
		}
		seen[key] = true
		if in.Tool == "" || in.Version == "" {
			issues = append(issues, "台账存在缺少 tool/version 的记录")
		}
		if in.Path == "" {
			issues = append(issues, fmt.Sprintf("%s 缺少 path", key))
		} else if st, err := os.Stat(in.Path); err != nil || !st.IsDir() {
			issues = append(issues, fmt.Sprintf("%s 的目录不存在: %s", key, in.Path))
		}
	}
	return issues
}

// Config 是 ~/.packnest/config.toml。
type Config struct {
	// Mirror 是镜像档（default/cn/tuna/aliyun/ustc/auto）。
	Mirror string `toml:"mirror"`
	// GlobalMode 是全局包模式：shared（默认）| per-version。
	GlobalMode string `toml:"global_mode"`
	// CacheMax 是产物缓存上限（如 "5GB"）。
	CacheMax string `toml:"cache_max"`
	// CompanionModes 是 <工具> → bundled|latest-compatible|pinned。
	CompanionModes map[string]string `toml:"companion_modes"`
	// Corepack 是 node ≤24 的 corepack 开关（`pk install node --corepack` 写这里；默认关）。
	Corepack bool `toml:"corepack"`
	// Tools 是全局默认版本（`pk use` 写这里）。
	Tools map[string]string `toml:"tools"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() *Config {
	return &Config{
		Mirror:         "auto",
		GlobalMode:     "shared",
		CacheMax:       "5GB",
		CompanionModes: map[string]string{"npm": "latest-compatible"},
	}
}

// LoadConfig 读取配置；不存在返回默认值。
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("配置 %q 解析失败: %w", path, err)
	}
	if cfg.Mirror == "" {
		cfg.Mirror = "auto"
	}
	if cfg.GlobalMode == "" {
		cfg.GlobalMode = "shared"
	}
	if cfg.CacheMax == "" {
		cfg.CacheMax = "5GB"
	}
	return cfg, nil
}

// Save 原子写配置。
func (c *Config) Save(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return WriteAtomic(path, data)
}

// WriteAtomic 写临时文件后 rename（实现只有一份，在 paths）。
func WriteAtomic(path string, data []byte) error { return paths.WriteAtomic(path, data) }

// WriteAtomicWithBackup 先备份现有文件为 .bak，再原子写。
func WriteAtomicWithBackup(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		if err := WriteAtomic(path+".bak", old); err != nil {
			return fmt.Errorf("备份 %q 失败: %w", path, err)
		}
	}
	return WriteAtomic(path, data)
}

// InstallInfo 记录"pk 自己是怎么装进来的"（由 install.sh / install.ps1 写入
// state/install.json）。`pk uninstall --all` 用它给出**精确**的自我删除命令，
// 而不是猜 ~/.local/bin 之类。
type InstallInfo struct {
	Schema   int    `json:"schema"`
	Channel  string `json:"channel"`  // script / manual
	Platform string `json:"platform"` // darwin-arm64 …
	Version  string `json:"version"`
	Dir      string `json:"dir"` // 二进制所在目录
}

// LoadInstallInfo 读取安装来源记录；没有或损坏时返回 nil（不是错误）。
func LoadInstallInfo(path string) *InstallInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var info InstallInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}
	return &info
}

// Save 原子写记录。
func (i *InstallInfo) Save(path string) error {
	if i.Schema == 0 {
		i.Schema = 1
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化安装记录失败: %w", err)
	}
	return WriteAtomic(path, data)
}

// CacheSchema 是缓存索引的格式版本。
const CacheSchema = 1

// StaleMaxAge 是临时残留被视为"垃圾"的阈值。
const StaleMaxAge = 24 * time.Hour

// CacheEntry 是缓存里一个产物的使用记录。
type CacheEntry struct {
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"last_used"`
}

// CacheIndex 是 state/cache.json。
type CacheIndex struct {
	Schema  int                   `json:"schema"`
	Entries map[string]CacheEntry `json:"entries"`
}

// LoadCacheIndex 读取缓存索引；缺失返回空索引。
func LoadCacheIndex(path string) (*CacheIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CacheIndex{Schema: CacheSchema, Entries: map[string]CacheEntry{}}, nil
		}
		return nil, fmt.Errorf("读取缓存索引失败: %w", err)
	}
	var idx CacheIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		// 缓存索引坏掉不是致命问题：当成空索引重建。
		return &CacheIndex{Schema: CacheSchema, Entries: map[string]CacheEntry{}}, nil
	}
	if idx.Entries == nil {
		idx.Entries = map[string]CacheEntry{}
	}
	idx.Schema = CacheSchema
	return &idx, nil
}

// Save 原子写缓存索引。
func (c *CacheIndex) Save(path string) error {
	c.Schema = CacheSchema
	if c.Entries == nil {
		c.Entries = map[string]CacheEntry{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, data)
}

// Touch 记录一次使用（命中缓存时调用）。
func (c *CacheIndex) Touch(name string, size int64, now time.Time) {
	if c.Entries == nil {
		c.Entries = map[string]CacheEntry{}
	}
	c.Entries[name] = CacheEntry{Size: size, LastUsed: now.UTC()}
}

// Total 返回缓存的已记录总大小。
func (c *CacheIndex) Total() int64 {
	var total int64
	for _, e := range c.Entries {
		total += e.Size
	}
	return total
}

// ParseSize 解析 "5GB" / "500MB" / "1073741824" 这类写法。
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("容量为空")
	}
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1},
	} {
		if strings.HasSuffix(s, unit.suffix) {
			s = strings.TrimSpace(strings.TrimSuffix(s, unit.suffix))
			multiplier = unit.mult
			break
		}
	}
	value, err := strconv.ParseFloat(s, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("容量 %q 无法解析（示例：5GB / 500MB / 1073741824）", s)
	}
	return int64(value * float64(multiplier)), nil
}

// SweepResult 是清扫结果。
type SweepResult struct {
	Removed []string
	Bytes   int64
}

// SweepStale 清理崩溃/强杀留下的临时残留（超过 maxAge 才删）。
func SweepStale(p paths.Paths, now time.Time, maxAge time.Duration) (SweepResult, error) {
	if maxAge <= 0 {
		maxAge = StaleMaxAge
	}
	var res SweepResult
	patterns := []string{
		filepath.Join(p.Tools(), "*", "*.tmp.*"),
		filepath.Join(p.Tools(), "*", "*.old"),
		filepath.Join(p.CacheArchives(), "*.part"),
		filepath.Join(p.CacheArchives(), "*.part.meta"),
		filepath.Join(p.CacheMeta(), "*.tmp"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			st, err := os.Lstat(match)
			if err != nil || now.Sub(st.ModTime()) < maxAge {
				continue
			}
			size := DirSize(match)
			if err := os.RemoveAll(match); err != nil {
				continue
			}
			res.Removed = append(res.Removed, match)
			res.Bytes += size
		}
	}
	sort.Strings(res.Removed)
	return res, nil
}

// PruneCache 把产物缓存压到 limit 以内（LRU，按 last_used 从旧到新删）。
//
// dryRun 时只报告要删什么，不动磁盘。
func PruneCache(p paths.Paths, limit int64, now time.Time, dryRun bool) (SweepResult, error) {
	idx, err := LoadCacheIndex(p.CacheIndex())
	if err != nil {
		return SweepResult{}, err
	}
	dir := p.CacheArchives()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return SweepResult{}, err
	}

	// 先把磁盘上存在但索引里没有的文件补进来（按文件 mtime 当 last_used）。
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".meta") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if _, ok := idx.Entries[name]; !ok {
			idx.Entries[name] = CacheEntry{Size: info.Size(), LastUsed: info.ModTime().UTC()}
		}
	}
	// 索引里有但磁盘上没有的，顺手清掉。
	for name := range idx.Entries {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			delete(idx.Entries, name)
		}
	}

	total := idx.Total()
	var res SweepResult
	if total <= limit {
		if err := idx.Save(p.CacheIndex()); err != nil {
			return res, err
		}
		return res, nil
	}

	type candidate struct {
		name  string
		entry CacheEntry
	}
	cands := make([]candidate, 0, len(idx.Entries))
	for name, e := range idx.Entries {
		cands = append(cands, candidate{name, e})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].entry.LastUsed.Before(cands[j].entry.LastUsed) })

	for _, c := range cands {
		if total <= limit {
			break
		}
		path := filepath.Join(dir, c.name)
		if !dryRun {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				// 删不掉（Windows 占用常见）：不计入已释放，台账留着下次再试。
				continue
			}
		}
		res.Removed = append(res.Removed, path)
		res.Bytes += c.entry.Size
		total -= c.entry.Size
		delete(idx.Entries, c.name)
	}
	if !dryRun {
		if err := idx.Save(p.CacheIndex()); err != nil {
			return res, err
		}
	}
	sort.Strings(res.Removed)
	return res, nil
}

// DirSize 递归算目录大小。
func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
