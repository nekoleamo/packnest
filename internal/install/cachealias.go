package install

// 缓存别名（R-CACHE-1）：PK_CACHE 里的归档是**内容寻址**名（<sha256>.<ext>），
// 想按摘要找回来就必须先知道 sha256；而新机器（CI 的新 runner）state 是空的，
// 于是只能"先下 50MB 产物、再算摘要"—— 缓存等于白存。
//
// 解法：把**上游公布的摘要**（gnu-sums 里的 sha256、registry 的 dist.integrity/shasum、
// go 索引里的 sha256）也当成一把钥匙，记成缓存目录里的一个别名文件：
//
//	<cache>/alias/<algo>-<hex>   内容是一行 <sha256>.<ext> 文件名
//
// 为什么不放进 state/cache.json：那份 LRU 记录在 PK_ROOT/state 里（台账与锁必须留在根里），
// 而 CI 只缓存 PK_CACHE —— 别名必须跟着缓存一起搬走才有意义。
// 为什么不用符号链接：Windows 上建软链要开发者模式/管理员权限，普通用户直接失败；
// 一行文本 + 原子写，三平台行为一致。
//
// 别名的失效是「静默」的：目标文件不在、大小不对、`--force` 重装，都只当作没命中，
// 顺手把悬空别名删掉。别名永远不参与正确性判断 —— 命中之后照样走完整的 sha256 + 校验源比对。

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/verify"
)

// digestRef 是「上游公布的（算法, 摘要）」，把摘要和算法绑在一起，避免把 sha1 当 sha256 用。
type digestRef struct {
	Algo   string
	Digest string
}

// aliasPath 返回某个摘要对应的别名文件路径；摘要无法归一化时 ok=false。
func (i *Installer) aliasPath(ref digestRef) (string, bool) {
	key := verify.AliasKey(ref.Algo, ref.Digest)
	if key == "" {
		return "", false
	}
	return filepath.Join(i.opt.Paths.CacheAliases(), key), true
}

// lookupAlias 按上游摘要查缓存；命中返回内容寻址的归档路径。
//
// 命中判定与 <sha256>.<ext> 那条路径一致（存在、非空、非 --force），
// 命中时记一次 LRU 并发出 cache 事件，行为对齐老的非内容寻址缓存。
func (i *Installer) lookupAlias(ref digestRef, ext, tool, version string) (string, bool) {
	if i.opt.Force {
		return "", false // 强制重装：不看缓存，也别把好别名当悬空删掉
	}
	path, ok := i.aliasPath(ref)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(string(data))
	// 别名内容只允许是一个纯文件名：带路径分隔符的一律当脏数据（防止被缓存目录里的东西牵着走）。
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		_ = os.Remove(path)
		return "", false
	}
	if !strings.HasSuffix(name, "."+ext) {
		return "", false // 同名版本的归档格式不该变；对不上就不猜
	}
	archive := filepath.Join(i.opt.Paths.CacheArchives(), name)
	if !i.cacheHit(archive, tool, version) {
		_ = os.Remove(path) // 悬空别名（归档被 gc 掉/被手删）：顺手清掉
		return "", false
	}
	return archive, true
}

// recordAlias 记一条「上游摘要 → 归档文件名」。
//
// 失败一律吞掉：别名只是加速，写不进去（缓存目录只读之类）不该影响安装本身。
func (i *Installer) recordAlias(ref digestRef, file string) {
	if file == "" {
		return
	}
	path, ok := i.aliasPath(ref)
	if !ok {
		return
	}
	_ = paths.WriteAtomic(path, []byte(filepath.Base(file)))
}

// cacheHasAliases 判断缓存里是否已经有可复用的归档别名。
// 只有"已经有东西可命中"时才值得为查缓存多发一次校验和请求（几 KB）。
func (i *Installer) cacheHasAliases() bool {
	entries, err := os.ReadDir(i.opt.Paths.CacheAliases())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

// publishedRefs 是**不用网络**就能知道的上游摘要：go 索引内联的 sha256、
// registry 元数据里的 dist.integrity（sha512 SRI）/ dist.shasum（sha1）。
func publishedRefs(resolved provider.Version, idx *provider.Index, goos, goarch, version string) []digestRef {
	var refs []digestRef
	if f, ok := resolved.FileFor(goos, goarch); ok && f.SHA256 != "" {
		refs = append(refs, digestRef{Algo: verify.AlgoSHA256, Digest: f.SHA256})
	}
	if idx != nil {
		if v, ok := idx.Find(version); ok {
			refs = append(refs, registryRefs(v)...)
		}
	}
	return refs
}

// registryRefs 从一条 registry 版本记录里取摘要：dist.integrity 优先（可能多段），其次 dist.shasum。
func registryRefs(v provider.Version) []digestRef {
	var refs []digestRef
	for _, tok := range strings.Fields(v.Integrity) {
		if strings.HasPrefix(strings.ToLower(tok), verify.AlgoSHA512+"-") {
			refs = append(refs, digestRef{Algo: verify.AlgoSHA512, Digest: tok})
		}
	}
	if len(v.Shasum) == 40 {
		refs = append(refs, digestRef{Algo: verify.AlgoSHA1, Digest: v.Shasum})
	}
	return refs
}

// refsFromSums 从一份「校验和文件 / 平台包元数据」里取本产物的上游摘要。
func refsFromSums(format string, data []byte, version, artifact string) []digestRef {
	if format == provider.ChecksumRegistry {
		vs, err := provider.ParseRegistryIndex(data)
		if err != nil {
			return nil
		}
		idx := &provider.Index{Versions: vs}
		if v, ok := idx.Find(version); ok {
			return registryRefs(v)
		}
		return nil
	}
	if sum, ok := verify.PublishedSum(format, data, artifact); ok {
		return []digestRef{{Algo: verify.AlgoSHA256, Digest: sum}}
	}
	return nil
}

// sumsRefs 逐个校验和源去取（几 KB），短路在第一份能解析出摘要的来源。
// 只在「缓存里已经有别名」时调用 —— 否则纯属白发请求。
func (i *Installer) sumsRefs(ctx context.Context, p *provider.Provider, resolved provider.Version, artifact string) []digestRef {
	urls, err := p.ChecksumURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return nil
	}
	for _, u := range urls {
		data, err := i.sums(ctx, u)
		if err != nil {
			continue
		}
		if refs := refsFromSums(p.Checksum.Format, data, resolved.Version, artifact); len(refs) > 0 {
			return refs
		}
	}
	return nil
}

// memoRefs 是「这次运行里已经拉到手、不用再发请求」的上游摘要（下载后记别名用）。
//
// 关键点：稳态（TOFU 命中）下 verify 根本不上网，这里也就拿不到任何东西 ——
// 不会为了记别名偷偷加一次网络请求。
func (i *Installer) memoRefs(p *provider.Provider, resolved provider.Version, artifact string) []digestRef {
	urls, err := p.ChecksumURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return nil
	}
	var refs []digestRef
	for _, u := range urls {
		data, ok := i.memoGet(u)
		if !ok {
			continue
		}
		refs = append(refs, refsFromSums(p.Checksum.Format, data, resolved.Version, artifact)...)
	}
	return refs
}
