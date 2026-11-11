// Package store 管全局配置（config.toml）与安装台账（installs.json）。
//
// 台账是唯一事实源：uninstall 只回收台账里的记录，不在台账里的目录只提示不删
// （方案 §8.2 第 3 条）。所有写盘都是"先 .bak 再原子替换"。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
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
	Tool        string    `json:"tool"`
	Version     string    `json:"version"`
	Path        string    `json:"path"`
	Source      string    `json:"source,omitempty"` // 镜像档名
	SHA256      string    `json:"sha256,omitempty"`
	Verified    string    `json:"verified,omitempty"`  // strong | weak | unverified
	Transport   string    `json:"transport,omitempty"` // 空=全 https；http=清单里有放行的 http 源
	CompanionOf string    `json:"companion_of,omitempty"`
	NpmBundled  string    `json:"npm_bundled,omitempty"` // node 自带 npm 版本
	NpmVersion  string    `json:"npm_version,omitempty"` // 实际生效的 npm 版本
	Channel     string    `json:"install_channel,omitempty"`
	References  []Ref     `json:"references,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
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

// WriteAtomic 写临时文件后 rename（同目录，保证原子）。
func WriteAtomic(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %q 失败: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写 %q 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %q 失败: %w", path, err)
	}
	return nil
}

// WriteAtomicWithBackup 先备份现有文件为 .bak，再原子写。
func WriteAtomicWithBackup(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		if err := WriteAtomic(path+".bak", old); err != nil {
			return fmt.Errorf("备份 %q 失败: %w", path, err)
		}
	}
	return WriteAtomic(path, data)
}
