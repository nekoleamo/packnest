// Package paths 统一管理 PK_ROOT 下的目录布局。
//
// 项目铁律：业务代码不许直接拼 ~ 路径，一律走这里 —— 这样测试能用 t.TempDir()
// 完整隔离，绝不碰真实 HOME。布局见方案 §8.1。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// RootEnv 是覆盖根目录的环境变量。测试全用它指向 t.TempDir()。
const RootEnv = "PK_ROOT"

// CacheEnv 把缓存目录从 PK_ROOT 里抽出来（CI 挂载缓存、多机预热用）。
const CacheEnv = "PK_CACHE"

// Paths 是 ~/.packnest 的目录视图（单根布局）。
type Paths struct {
	// Root 为绝对路径的根目录。
	Root string
	// CacheRoot 是缓存根；为空时用 <Root>/cache（默认形态）。只有 PK_CACHE 能改它。
	CacheRoot string
}

// Resolve 按 PK_ROOT → ~/.packnest 的顺序解析根目录，再按 PK_CACHE 覆盖缓存根。
func Resolve() (Paths, error) {
	root := strings.TrimSpace(os.Getenv(RootEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("解析用户主目录失败: %w", err)
		}
		root = filepath.Join(home, ".packnest")
	}
	p, err := New(root)
	if err != nil {
		return Paths{}, err
	}
	if cache := strings.TrimSpace(os.Getenv(CacheEnv)); cache != "" {
		c, err := New(cache)
		if err != nil {
			return Paths{}, fmt.Errorf("解析 %s 失败: %w", CacheEnv, err)
		}
		p.CacheRoot = c.Root
	}
	return p, nil
}

// New 以给定根目录构造 Paths：展开 ~、转绝对路径，不做任何磁盘操作。
func New(root string) (Paths, error) {
	if root == "" {
		return Paths{}, fmt.Errorf("根目录不能为空")
	}
	if root == "~" || strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("解析用户主目录失败: %w", err)
		}
		root = filepath.Join(home, strings.TrimPrefix(root, "~"))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("解析根目录 %q 失败: %w", root, err)
	}
	return Paths{Root: abs}, nil
}

func (p Paths) join(parts ...string) string {
	return filepath.Join(append([]string{p.Root}, parts...)...)
}

// Bin 是 shim 目录（PATH 第一段）。
func (p Paths) Bin() string { return p.join("bin") }

// Tools 是所有工具的解压根。
func (p Paths) Tools() string { return p.join("tools") }

// Tool 是某工具的解压根：tools/<tool>。
func (p Paths) Tool(name string) string { return p.join("tools", name) }

// ToolVersion 是某工具某版本目录：tools/<tool>/<ver>。
func (p Paths) ToolVersion(name, version string) string {
	return p.join("tools", name, version)
}

// Cache 是缓存根：PK_CACHE 覆盖，默认 <Root>/cache。
func (p Paths) Cache() string {
	if p.CacheRoot != "" {
		return p.CacheRoot
	}
	return p.join("cache")
}

// CacheDir 是缓存根下的子目录（corepack/npm 等派生缓存跟着走）。
func (p Paths) CacheDir(name string) string { return filepath.Join(p.Cache(), name) }

// CacheArchives 是内容寻址的产物缓存。
func (p Paths) CacheArchives() string { return p.CacheDir("archives") }

// CacheMeta 是索引元数据缓存（ETag/Last-Modified）。
func (p Paths) CacheMeta() string { return p.CacheDir("meta") }

// State 是台账与锁。
func (p Paths) State() string { return p.join("state") }

// Ledger 是安装台账路径（唯一事实源）。
func (p Paths) Ledger() string { return p.join("state", "installs.json") }

// Known 是 TOFU 校验和表。
func (p Paths) Known() string { return p.join("state", "known.json") }

// InstallInfo 记录"pk 自己是怎么装的"（由安装脚本写入）。
func (p Paths) InstallInfo() string { return p.join("state", "install.json") }

// Probe 是镜像探测结果缓存。
func (p Paths) Probe() string { return p.join("state", "probe.json") }

// CacheIndex 是缓存使用记录（LRU）。
func (p Paths) CacheIndex() string { return p.join("state", "cache.json") }

// Lock 是全局锁文件。
func (p Paths) Lock() string { return p.join("state", "lock") }

// Config 是全局配置。
func (p Paths) Config() string { return p.join("config.toml") }

// Logs 是日志目录。
func (p Paths) Logs() string { return p.join("logs") }

// Providers 是外部 provider 清单目录（$PK_ROOT/providers，缺失即跳过）。
func (p Paths) Providers() string { return p.join("providers") }

// Global 是共享全局包前缀（shared global prefix）。
func (p Paths) Global() string { return p.join("global") }

// GlobalBin 是全局包的 bin 目录（PATH 第二段，固定目录 → 无需 reshim）。
func (p Paths) GlobalBin() string { return p.join("global", "bin") }

// GlobalEtc 是引出的 npm globalconfig 目录。
func (p Paths) GlobalEtc() string { return p.join("global", "etc") }

// GlobalPnpmBin 是 PNPM_HOME 对应的 bin 目录。
func (p Paths) GlobalPnpmBin() string { return p.join("global", "pnpm") }

// GlobalBunBin 是 BUN_INSTALL 对应的 bin 目录。
func (p Paths) GlobalBunBin() string { return p.join("global", "bun", "bin") }

// GlobalCorepackBin 是 corepack 的 shim 目录（PATH 第 2 段，方案 B2-2）：
// 独立一段，provider 装的 pnpm 永远优先，corepack 只补 yarn/yarnpkg 和 pnpm 兜底。
func (p Paths) GlobalCorepackBin() string { return p.join("global", "corepack", "bin") }

// PathSegments 返回固定顺序的四段 PATH（方案 §6.7，doctor 会断言顺序）。
// PathSegments 返回 PATH 的五段（顺序即优先级，方案 B2-2）：
// shim → corepack → npm 全局 bin → PNPM_HOME → BUN_INSTALL/bin。
func (p Paths) PathSegments() []string {
	return []string{p.Bin(), p.GlobalCorepackBin(), p.GlobalBin(), p.GlobalPnpmBin(), p.GlobalBunBin()}
}

// SafeJoin 把归档内的相对路径安全地拼到 base 下，拒绝越界（zip-slip / tar 绝对路径）。
func SafeJoin(base, name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) || (len(name) > 1 && name[1] == ':') {
		return "", fmt.Errorf("归档内路径非法（绝对路径）: %q", name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("归档内路径非法（越界）: %q", name)
	}
	return filepath.Join(base, cleaned), nil
}

// ---- 原子落盘（原 atomic.go）：全项目只有这一份实现 ----

var tmpSeq atomic.Uint64 // 临时文件编号（进程内唯一即可）

// WriteAtomic 原子写文件（自动建父目录）。data 已完整在内存里，失败不留半个文件。
func WriteAtomic(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %q 失败: %w", dir, err)
		}
	}
	// 临时名带随机后缀：同一文件可能被并发写（索引预取、并发下载的探测缓存），
	// 固定 ".tmp" 会让两个 goroutine 抢同一个临时文件，rename 必有一个失败。
	tmp := fmt.Sprintf("%s.tmp.%d", path, tmpSeq.Add(1))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写 %q 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %q 失败: %w", path, err)
	}
	return nil
}
