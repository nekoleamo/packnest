package install

import (
	"context"
	"errors"
	"fmt"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/verify"
	"os"
	"path/filepath"
	"strings"
)

// fetchArtifact 返回（本地缓存路径, 实际下载 URL, 是否来自缓存）。
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
	// 内容寻址：TOFU 表里已知道摘要时直接找 <sha256>.<ext>（同一份字节跨版本只存一份）
	if e, ok := i.known.Get(verify.Key(p.ID, version, platform)); ok && len(e.Digest) == 64 {
		if cp := filepath.Join(dir, e.Digest+"."+ext); i.cacheHit(cp) {
			return cp, urls[0], true, nil
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
func (i *Installer) cacheHit(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 || i.opt.Force {
		return false
	}
	i.recordCacheTouch(filepath.Base(path), i.now())
	i.emit(jsonout.Event{Ev: "cache", Tool: filepath.Base(path), Bytes: st.Size()})
	return true
}

// addressed 把缓存文件改成内容寻址名 <sha256>.<ext>：同名已存在说明同内容已在缓存里，
// 直接删掉这份重复的（跨版本/跨项目去重）。
func (i *Installer) addressed(path, sha, ext string) string {
	if sha == "" || strings.HasPrefix(filepath.Base(path), sha+".") {
		return path
	}
	dst := filepath.Join(filepath.Dir(path), sha+"."+ext)
	if _, err := os.Stat(dst); err == nil {
		_ = os.Remove(path)
	} else if err := os.Rename(path, dst); err != nil {
		return path
	}
	i.recordCacheTouch(filepath.Base(dst), i.now())
	return dst
}

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
			return "", 0, fmt.Errorf("%w: 探测与直连都失败：%v；%v", ErrNetwork, probeErr, err)
		}
		return "", 0, fmt.Errorf("%w: %v", ErrNetwork, err)
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
	v.Fetch = func(ctx context.Context, url string) ([]byte, error) { return i.opt.Client.Get(ctx, url) }
	v.Now = i.opt.Now
	return v
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
	return i.addressed(archivePath, sha, extOrDefault(p, i.opt.GOOS)), artifactURL, sha, vr, nil
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
		data, derr := i.opt.Client.Get(ctx, sumsURLs[0])
		if derr != nil {
			return verify.Result{}, derr
		}
		vs, derr := provider.ParseRegistryIndex(data)
		if derr != nil {
			return verify.Result{}, derr
		}
		idx = &provider.Index{Versions: vs}
		if _, ok := idx.Find(version); !ok {
			return verify.Result{}, fmt.Errorf("%s@%s 没有对应的平台包（版本号须与本体一致）", p.ID, version)
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
