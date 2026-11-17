// Package paths 统一管理 PK_ROOT 下的目录布局。
//
// 项目铁律：业务代码不许直接拼 ~ 路径，一律走这里 —— 这样测试能用 t.TempDir()
// 完整隔离，绝不碰真实 HOME。布局见方案 §8.1。
package paths

import (
	"fmt"
	"github.com/packnest/packnest/internal/coded"
	"os"
	"path"
	"path/filepath"
	"runtime"
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
			return Paths{}, coded.Wrap(err, CodeHomeUnresolved)
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
			return Paths{}, coded.Wrap(err, CodeCacheUnresolved, CacheEnv)
		}
		p.CacheRoot = c.Root
	}
	return p, nil
}

// New 以给定根目录构造 Paths：展开 ~、转绝对路径，不做任何磁盘操作。
func New(root string) (Paths, error) {
	if root == "" {
		return Paths{}, coded.New(CodeRootEmpty)
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
		return Paths{}, coded.Wrap(err, CodeRootUnresolved, root)
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

// CacheAliases 是"上游公布的摘要 → 内容寻址归档"的别名目录（R-CACHE-1）。
// 放在缓存根下，跟着 PK_CACHE 一起被搬走/被 CI 缓存，这样没有 TOFU 记录的新机器
// 也能按上游摘要直接命中已有归档。
func (p Paths) CacheAliases() string { return p.CacheDir("alias") }

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
		return "", coded.New(CodeArchiveAbs, name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", coded.New(CodeArchiveEscape, name)
	}
	return filepath.Join(base, cleaned), nil
}

// ---- Windows 长路径（\\?\ 前缀化）----
//
// Windows 的 260 字符 MAX_PATH 限制在 Win10 1607 之后可以绕过，但**必须**用 `\\?\`
// 绝对路径前缀（UNC 是 `\\?\UNC\server\share`）。只有真正碰文件系统时才加前缀：
// 前缀不能出现在 `--json` 输出、shim 内容、PATH 里（用户看不懂，别的工具也不认）。
//
// 典型触发点：`<PK_ROOT>/tools/node/<ver>/lib/node_modules/<pkg>/node_modules/...` 这类深层
// 目录（解压 npm 伴生包时），PK_ROOT 稍长一点就超 260。
const winMaxPath = 240 // 留 20 字符余量给 `\\?\` 与即将拼接的文件名

// Long 返回"可以安全交给 Windows 文件 API"的路径：
// 非 Windows、相对路径、已经带前缀、或长度没超阈值时原样返回。
func Long(p string) string { return longPath(p, runtime.GOOS) }

// longPath 是 Long 的纯函数版（便于在任意平台测 Windows 的逻辑）。
func longPath(p, goos string) string {
	if goos != "windows" || len(p) < winMaxPath || !winAbs(p) {
		return p
	}
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	// 归一化不依赖平台：先把分隔符统一成 `/` 交给 path.Clean（解决 `..`/重复分隔符），
	// 再换回反斜杠 —— `\\?\` 前缀要求路径已归一化。
	clean := func(s string) string {
		return strings.ReplaceAll(path.Clean(strings.ReplaceAll(s, `\`, "/")), "/", `\`)
	}
	// UNC（\\server\share\...）不能整段交给 path.Clean：它会把开头的 `//` 收成一个 `/`。
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + clean(strings.TrimPrefix(p, `\\`))
	}
	return `\\?\` + clean(p)
}

// winAbs 判 Windows 绝对路径（盘符或 UNC），与运行平台无关（测试要在 mac 上跑这段）。
func winAbs(p string) bool {
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0]
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	return strings.HasPrefix(p, `\\`)
}

// ---- 原子落盘（原 atomic.go）：全项目只有这一份实现 ----

var tmpSeq atomic.Uint64 // 临时文件编号（进程内唯一即可）

// WriteAtomic 原子写文件（自动建父目录）。data 已完整在内存里，失败不留半个文件。
func WriteAtomic(path string, data []byte) error { return WriteAtomicMode(path, data, 0) }

// WriteAtomicMode 是 WriteAtomic 的"保留权限"版本：mode 为 0 时沿用已有文件的权限
// （不存在则 0644）。写用户配置（`.zshrc` / `.npmrc` 常是 0600，里面有 token）必须走这个，
// 否则一次原子写就把权限放宽成 0644。
func WriteAtomicMode(path string, data []byte, mode os.FileMode) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return coded.Wrap(err, CodeMkdir, dir)
		}
	}
	// 临时名带随机后缀：同一文件可能被并发写（索引预取、并发下载的探测缓存），
	// 固定 ".tmp" 会让两个 goroutine 抢同一个临时文件，rename 必有一个失败。
	if mode == 0 {
		mode = 0o644
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, tmpSeq.Add(1))
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return coded.Wrap(err, CodeWrite, path)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return coded.Wrap(err, CodeRename, path)
	}
	return nil
}
