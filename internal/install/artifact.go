package install

import (
	"context"
	"errors"
	"fmt"
	"github.com/packnest/packnest/internal/coded"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/verify"
)

// fetchArtifact 返回（本地缓存路径, 实际下载 URL, 是否来自缓存）。
//
// 命中顺序（越便宜越先）：
//  1. TOFU/台账 里的 sha256 → <sha256>.<ext>（同内容跨版本只存一份）；
//  2. 索引里自带的上游摘要（go 的 sha256、registry 的 integrity/shasum）→ 缓存别名（R-CACHE-1）；
//  3. 缓存里已经有别名时，花一次几 KB 的请求拿校验和文件 → 缓存别名（R-CACHE-1）；
//  4. 非内容寻址的老文件名（1.0 时代的缓存）→ 下载。
func (i *Installer) fetchArtifact(ctx context.Context, p *provider.Provider, resolved provider.Version, idx *provider.Index) (string, string, bool, error) {
	if _, err := p.Platform(i.opt.GOOS, i.opt.GOARCH); err != nil {
		return "", "", false, err
	}
	urls, err := p.ArtifactURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return "", "", false, err
	}
	version := resolved.Version
	ext := extOrDefault(p, i.opt.GOOS)
	platform := i.opt.GOOS + "-" + i.opt.GOARCH
	dir := i.opt.Paths.CacheArchives()
	// 内容寻址：TOFU 里已知摘要就按内容找（同内容跨版本只存一份）；--insecure 的"源矛盾"安装
	// 不写 TOFU，退回台账里的 SHA256，避免重复下载。
	if sha := i.knownDigest(p.ID, version, platform); sha != "" {
		if cp := filepath.Join(dir, sha+"."+ext); i.cacheHit(cp, p.ID, version) {
			return cp, urls[0], true, nil
		}
	}
	// R-CACHE-1：新机器没有 TOFU 也没有台账，但缓存目录里可能早就有这份归档。
	// 先看索引自带的摘要（零请求），再看校验和文件（一次几 KB），命中就不下 50MB。
	if !i.opt.Force && !i.offline() {
		for _, ref := range publishedRefs(resolved, idx, i.opt.GOOS, i.opt.GOARCH, version) {
			if cp, hit := i.lookupAlias(ref, ext, p.ID, version); hit {
				return cp, urls[0], true, nil
			}
		}
		if i.cacheHasAliases() {
			for _, ref := range i.sumsRefs(ctx, p, resolved, filepath.Base(urls[0])) {
				if cp, hit := i.lookupAlias(ref, ext, p.ID, version); hit {
					return cp, urls[0], true, nil
				}
			}
		}
	}
	cachePath := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.%s", p.ID, version, platform, ext))

	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 && !i.opt.Force {
		i.recordCacheTouch(filepath.Base(cachePath), i.now())
		i.emit(jsonout.Event{Ev: "cache", Tool: p.ID, Version: version, Bytes: st.Size()})
		return cachePath, urls[0], true, nil
	}

	source, _, err := i.downloadFromChain(ctx, p.ID, version, urls, cachePath)
	if err != nil {
		return "", "", false, err
	}
	return cachePath, source, false, nil
}

// cacheHit 判断缓存文件是否可用（存在、非空、本次没给 --force），命中时记一次 LRU。
//
// tool 只用于事件：`cache` 事件的 `tool` 必须是工具名（GUI 按 tool 分组画进度），
// 不能是 `<sha256>.tar.gz` 这种文件名（1.8 之前的实现就是这么错发的）。
func (i *Installer) cacheHit(path, tool, version string) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 || i.opt.Force {
		return false
	}
	i.recordCacheTouch(filepath.Base(path), i.now())
	i.emit(jsonout.Event{Ev: "cache", Tool: tool, Version: version, Bytes: st.Size()})
	return true
}

// knownDigest 返回这个版本已知的产物摘要：优先 TOFU 表，其次台账（--insecure 放行的安装只在台账里）。
func (i *Installer) knownDigest(tool, version, platform string) string {
	if e, ok := i.known.Get(verify.Key(tool, version, platform)); ok && len(e.Digest) == 64 {
		return e.Digest
	}
	if in := i.ledger.Find(tool, version); in != nil && len(in.SHA256) == 64 {
		return in.SHA256
	}
	return ""
}

// addressed 把缓存文件改成内容寻址名 <sha256>.<ext>：同名已存在说明同内容已在缓存里，
// 直接删掉这份重复的（跨版本/跨项目去重）。
//
// ⚠️ 同名已存在时**必须比一下大小**：文件名就是摘要，但文件可能被改坏/截断（磁盘错误、
// 中途断电、手工 tamper）。不比就直接把刚下载的好文件删了，坏文件留着，下一次会在解压阶段炸
// —— 2026-11-15 实测就是这条路径把“缓存被改坏”拖成了“先下 50MB 再失败”。
func (i *Installer) addressed(path, sha, ext string) string {
	if sha == "" || strings.HasPrefix(filepath.Base(path), sha+".") {
		return path
	}
	dst := filepath.Join(filepath.Dir(path), sha+"."+ext)
	if dstInfo, err := os.Stat(dst); err == nil {
		srcInfo, serr := os.Stat(path)
		if serr == nil && srcInfo.Size() == dstInfo.Size() {
			_ = os.Remove(path) // 同内容（同名=同摘要，大小也一致）→ 去重
			i.recordCacheTouch(filepath.Base(dst), i.now())
			return dst
		}
		// 大小不符：缓存里那份是坏的，用刚校验过的这份替掉它。
		if err := os.Rename(path, dst); err == nil {
			i.recordCacheTouch(filepath.Base(dst), i.now())
			return dst
		}
		_ = os.Remove(dst)
		if err := os.Rename(path, dst); err == nil {
			i.recordCacheTouch(filepath.Base(dst), i.now())
			return dst
		}
		return path
	}
	if err := os.Rename(path, dst); err != nil {
		return path
	}
	i.recordCacheTouch(filepath.Base(dst), i.now())
	return dst
}

// progressInterval 是 progress 事件的节流间隔：GUI 每 200ms 拿到一次就够画进度条了。
const progressInterval = 200 * time.Millisecond

// downloadFromChain 先探测再下载；探测全挂时退化到直接下载首选源。
//
// 理由：HEAD 探针只有 500ms（方案 §6.3），网络抖动时"探针超时"不等于"源不可用"，
// 直接拿真产物去试，比让用户手动切源更可靠。
func (i *Installer) downloadFromChain(ctx context.Context, tool, version string, urls []string, cachePath string) (string, int64, error) {
	prober := &mirror.Prober{Client: i.opt.Client, Path: i.opt.Paths.Probe(), TTL: mirror.DefaultTTL}
	probe, probeErr := prober.Select(ctx, urls)
	source := urls[0]
	if probeErr == nil {
		source = probe.URL
	}

	// 下载过程中的字节进度：CLI 的进度条照旧走 Client.Progress，这里再叠一层 --json 事件
	if i.opt.Events != nil {
		prev, last := i.opt.Client.Progress, time.Time{}
		i.opt.Client.Progress = func(done, total int64) {
			if prev != nil {
				prev(done, total)
			}
			if done < total && time.Since(last) < progressInterval {
				return
			}
			last = time.Now()
			i.emit(jsonout.Event{Ev: "progress", Tool: tool, Version: version, BytesDone: done, BytesTotal: total})
		}
		defer func() { i.opt.Client.Progress = prev }()
	}
	n, err := i.opt.Client.Download(ctx, source, cachePath)
	if err != nil {
		for _, alt := range urls {
			if alt == source {
				continue
			}
			n2, err2 := i.opt.Client.Download(ctx, alt, cachePath)
			if err2 == nil {
				source, n, err = alt, n2, nil
				break
			}
		}
	}
	if err != nil {
		if probeErr != nil {
			return "", 0, coded.Wrap(ErrNetwork, CodeNetworkProbe, probeErr, err)
		}
		return "", 0, coded.Wrap(ErrNetwork, CodeNetwork, err)
	}
	i.recordCacheTouch(filepath.Base(cachePath), i.now())
	i.emit(jsonout.Event{Ev: "download", Tool: tool, Version: version, Bytes: n, Source: source})
	return source, n, nil
}

// registryDigest 按 registry 元数据选出（算法, 期望摘要）：
// dist.integrity（sha512 SRI）优先，其次 dist.shasum（sha1）；都没有则回落 sha256。
func registryDigest(idx *provider.Index, version, archivePath, sha256 string) (string, string) {
	if idx != nil {
		if v, ok := idx.Find(version); ok {
			if v.Integrity != "" {
				if d, err := verify.DigestFile(archivePath, verify.AlgoSHA512); err == nil {
					return verify.AlgoSHA512, d
				}
			}
			if v.Shasum != "" {
				if d, err := verify.DigestFile(archivePath, verify.AlgoSHA1); err == nil {
					return verify.AlgoSHA1, d
				}
			}
		}
	}
	return verify.AlgoSHA256, sha256
}

// newVerifier 造接到注入式客户端与时钟上的校验器（TOFU 表在各安装路径间共享）。
func (i *Installer) newVerifier() *verify.Verifier {
	v := verify.New(i.known)
	v.Fetch = i.sums
	v.Now = i.opt.Now
	return v
}

// sumsMemo 是本轮已拉到的校验和/平台元数据（URL → 响应体）。
type sumsMemo struct {
	mu sync.Mutex
	m  map[string][]byte
}

// offline 汇总两处离线开关（Options.Offline 与 Client.Offline）。
func (i *Installer) offline() bool {
	return i.opt.Offline || (i.opt.Client != nil && i.opt.Client.Offline)
}

// sums 拉一份校验和/平台元数据，本轮内只请求一次。
// 下载前查缓存与随后的 verify 都要这份文件，不共用的话同一份几 KB 的东西会请求两遍。
func (i *Installer) sums(ctx context.Context, url string) ([]byte, error) {
	if data, ok := i.memoGet(url); ok {
		return data, nil
	}
	data, err := i.opt.Client.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	if i.sumsCache != nil {
		i.sumsCache.mu.Lock()
		i.sumsCache.m[url] = data
		i.sumsCache.mu.Unlock()
	}
	return data, nil
}

// memoGet 只看本轮已拉到的内容，不联网。
func (i *Installer) memoGet(url string) ([]byte, bool) {
	if i.sumsCache == nil {
		return nil, false
	}
	i.sumsCache.mu.Lock()
	defer i.sumsCache.mu.Unlock()
	data, ok := i.sumsCache.m[url]
	return data, ok
}

// fetchArtifactVerified = fetchArtifact + sha256 + verifyArtifact，含"缓存坏了就重下一次"；
// 返回（缓存路径, 实际 URL, 产物 sha256, 校验结果）。
func (i *Installer) fetchArtifactVerified(ctx context.Context, p *provider.Provider, resolved provider.Version, idx *provider.Index) (string, string, string, verify.Result, error) {
	platform := i.opt.GOOS + "-" + i.opt.GOARCH
	offline := i.opt.Offline || i.opt.Client.Offline
	archivePath, artifactURL, fromCache, err := i.fetchArtifact(ctx, p, resolved, idx)
	if err != nil {
		return "", "", "", verify.Result{}, err
	}
	run := func() (string, verify.Result, error) {
		sha, err := verify.SHA256File(archivePath)
		if err != nil {
			return "", verify.Result{}, err
		}
		vr, err := i.verifyArtifact(ctx, p, resolved, platform, artifactURL, archivePath, sha, idx)
		return sha, vr, err
	}
	sha, vr, err := run()
	// 只有"摘要不符"才算缓存坏了；离线/网络失败不能删缓存（否则离线重装救不回来）
	if err != nil && fromCache && !offline && errors.Is(err, verify.ErrChecksum) {
		_ = os.Remove(archivePath)
		if archivePath, artifactURL, _, err = i.fetchArtifact(ctx, p, resolved, idx); err != nil {
			return "", "", "", verify.Result{}, err
		}
		sha, vr, err = run()
	}
	if err != nil {
		if !fromCache && !offline && errors.Is(err, verify.ErrChecksum) {
			// 刚下的产物就没过校验：删掉脏缓存，下次重下（可能是投毒或截断）。
			_ = os.Remove(archivePath)
		}
		return "", "", "", verify.Result{}, err
	}
	final := i.addressed(archivePath, sha, extOrDefault(p, i.opt.GOOS))
	// R-CACHE-1：把这次拿到的上游摘要记成别名，下一台机器（或者 CI 的另一个 runner）
	// 只要拿到同一份几 KB 的校验和就能直接命中这个归档。
	i.recordRefs(p, resolved, sha, vr, artifactURL, final)
	return final, artifactURL, sha, vr, nil
}

// recordRefs 记下「上游摘要 → 内容寻址归档」的映射（只记这次运行里已经拿到的摘要，不额外联网）。
func (i *Installer) recordRefs(p *provider.Provider, resolved provider.Version, sha string, vr verify.Result, artifactURL, file string) {
	refs := []digestRef{{Algo: verify.AlgoSHA256, Digest: sha}}
	if vr.Digest != "" && vr.Algo != "" && !strings.EqualFold(vr.Digest, sha) {
		refs = append(refs, digestRef{Algo: vr.Algo, Digest: vr.Digest})
	}
	refs = append(refs, publishedRefs(resolved, nil, i.opt.GOOS, i.opt.GOARCH, resolved.Version)...)
	refs = append(refs, i.memoRefs(p, resolved, filepath.Base(artifactURL))...)
	for _, ref := range refs {
		i.recordAlias(ref, file)
	}
}

// verifyArtifact 按清单声明的校验格式选算法与期望摘要，交给 verify 包求值。
func (i *Installer) verifyArtifact(ctx context.Context, p *provider.Provider, resolved provider.Version, platform, artifactURL, archivePath, sha256 string, idx *provider.Index) (verify.Result, error) {
	version := resolved.Version
	sumsURLs, err := p.ChecksumURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return verify.Result{}, err
	}
	// 摘要改读平台包自己的元数据（pnpm 的 @pnpm/exe.*）；TOFU 已记过就不问网络，--force-rehash 才回源。
	if _, known := i.known.Get(verify.Key(p.ID, version, platform)); p.Checksum.Format == provider.ChecksumRegistry && len(sumsURLs) > 0 && (!known || i.opt.Rehash) {
		data, derr := i.sums(ctx, sumsURLs[0])
		if derr != nil {
			return verify.Result{}, derr
		}
		vs, derr := provider.ParseRegistryIndex(data)
		if derr != nil {
			return verify.Result{}, derr
		}
		idx = &provider.Index{Versions: vs}
		if _, ok := idx.Find(version); !ok {
			return verify.Result{}, coded.New(CodePlatformMissing, p.ID, version)
		}
	}
	algo, digest := registryDigest(idx, version, archivePath, sha256)
	inline := ""
	if p.Checksum.Format == provider.ChecksumIndexInline {
		// go 的 dl JSON 自带每个产物的 sha256：省掉一次校验和请求。
		algo, digest = verify.AlgoSHA256, sha256
		if f, ok := resolved.FileFor(i.opt.GOOS, i.opt.GOARCH); ok {
			inline = f.SHA256
		}
	} else if p.Checksum.Format != provider.ChecksumRegistry {
		algo, digest = verify.AlgoSHA256, sha256
	} else if idx != nil {
		if v, ok := idx.Find(version); ok {
			if v.Integrity != "" {
				inline = v.Integrity
			} else {
				inline = v.Shasum
			}
		}
	}
	v := i.newVerifier()
	req := verify.Request{
		Tool:              p.ID,
		Version:           version,
		Platform:          platform,
		Artifact:          filepath.Base(artifactURL),
		ArtifactURL:       artifactURL,
		Digest:            digest,
		Algo:              algo,
		SumsURLs:          sumsURLs,
		Format:            p.Checksum.Format,
		InlineShasum:      inline,
		Insecure:          i.opt.Insecure,
		InsecureTransport: p.InsecureTransport,
		Rehash:            i.opt.Rehash,
	}
	return v.Verify(ctx, req)
}

// applyCompanion 处理 node→npm 伴生（方案 §7）。返回（生效 npm 版本, 自带 npm 版本）。
func extOrDefault(p *provider.Provider, goos string) string {
	if ext, err := p.ExtFor(goos); err == nil && ext != "" {
		return ext
	}
	if ext, ok := p.Artifact.Ext[goos]; ok {
		return ext
	}
	return "tar.gz"
}

// readNpmVersion 读 package.json 的 version。

// checkMinVersion 拦住"产物形态换了、老版本没有对应产物"（pnpm 平台包从 11 起），并给可执行建议。
func checkMinVersion(p *provider.Provider, version string) error {
	if p.Artifact.MinVersion == "" {
		return nil
	}
	v, err := spec.ParseVersion(version)
	if err != nil {
		return nil // 版本解析不了就交给后面的流程报错
	}
	if min, err := spec.ParseVersion(p.Artifact.MinVersion); err == nil && v.LessThan(min) {
		return coded.New(CodeMinVersion, p.ID, p.Artifact.MinVersion, version)
	}
	return nil
}

// Target 是一个待安装目标（工具 + 版本说明符），批量预取下载用。
type Target struct{ Tool, Spec string }

// PrefetchArtifacts 并发把多个目标的产物先下到缓存："装 3 个工具"的网络时间从 3 次相加压成最慢那个。
// 每一路用独立的 Installer（独立 Client、单流、不发事件、不写台账），只让随后的串行 Install 命中缓存；
// 失败一律吞掉，真正的错误由串行 Install 带上下文报。
func (i *Installer) PrefetchArtifacts(ctx context.Context, targets []Target) {
	if i.opt.DryRun || i.opt.Force || i.opt.Offline || len(targets) < 2 {
		return // 预览 / 强制重装 / 离线都不预取
	}
	sem := make(chan struct{}, 3) // 同时最多 3 路，别把镜像与本机带宽打爆
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			i.fetchOnly(ctx, t)
		}(t)
	}
	wg.Wait()
}

// fetchOnly 只做"解析 + 下载到缓存"（自己的 Client、单流，不碰进度回调与分片配额）。
func (i *Installer) fetchOnly(ctx context.Context, t Target) {
	client := fetch.New()
	client.Jobs = 1
	sub, err := New(Options{
		Paths: i.opt.Paths, Client: client, Cfg: i.opt.Cfg, Profile: i.opt.Profile,
		GOOS: i.opt.GOOS, GOARCH: i.opt.GOARCH,
		Project: i.opt.Project, LookupProvider: i.opt.LookupProvider,
	})
	if err != nil {
		return
	}
	p, err := sub.opt.LookupProvider(t.Tool)
	if err != nil {
		return
	}
	v, idx, err := sub.resolveVersion(ctx, p, t.Spec)
	if err != nil {
		return
	}
	_, _, _, _ = sub.fetchArtifact(ctx, p, v, idx) // 只要文件落进缓存
}
