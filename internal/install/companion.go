package install

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/companion"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/verify"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 伴生包与工具链自带工具：node → npm 的反查与替换、corepack 的 shim 落位。

func (i *Installer) applyCompanion(ctx context.Context, p *provider.Provider, nodeVersion, treeDir string, nodeIdx *provider.Index) (string, string, error) {
	c, ok := p.Companions["npm"]
	if !ok {
		return "", "", nil
	}
	npmDir := filepath.Join(treeDir, filepath.FromSlash(companionTarget(c.Target, i.opt.GOOS)))
	bundled := readNpmVersion(filepath.Join(npmDir, "package.json"))
	if bundled == "" {
		// 拿不到自带版本（zip 布局差异）也不该让安装失败。
		return "", "", nil
	}

	mode := c.Mode
	if override, ok := i.opt.Cfg.CompanionModes["npm"]; ok && override != "" {
		mode = override
	}
	explicit := ""
	if v, ok := i.opt.Project["npm"]; ok && !strings.EqualFold(v, "auto") {
		explicit = v
	}

	var candidates []companion.Candidate
	indexAvailable := false
	npmP, err := i.opt.LookupProvider(c.Registry)
	var npmIdx *provider.Index // 一次索引加载复用两处（候选版本 + shasum）
	if err == nil {
		if idx, err := i.loadOrTakeIndex(ctx, npmP); err == nil {
			npmIdx, indexAvailable = idx, true
			for _, v := range idx.Versions {
				candidates = append(candidates, companion.Candidate{Version: v.Version, Engines: v.Engines["node"]})
			}
		}
	}

	dec, err := companion.Resolve(companion.Request{
		NodeVersion:    nodeVersion,
		Bundled:        bundled,
		Explicit:       explicit,
		Mode:           mode,
		Candidates:     candidates,
		IndexAvailable: indexAvailable,
	})
	if err != nil {
		return "", "", err
	}
	if !dec.Replaced {
		return bundled, bundled, nil
	}

	npmVersion, shasum := dec.Version, ""
	if npmIdx != nil {
		if v, ok := npmIdx.Find(npmVersion); ok {
			shasum = v.Shasum
		}
	}
	if err := i.installNpmPackage(ctx, npmP, npmVersion, shasum, npmDir); err != nil {
		return "", "", err
	}
	i.emit(jsonout.Event{Ev: "companion", Tool: "npm", Version: npmVersion, Msg: "replaced " + bundled})
	return npmVersion, bundled, nil
}

func (i *Installer) installNpmPackage(ctx context.Context, npmP *provider.Provider, version, shasum, npmDir string) error {
	if npmP == nil {
		return fmt.Errorf("找不到 npm 的 provider 清单，无法替换伴生包")
	}
	// 缓存/下载走与主产物同一条路径（fetchArtifact 已含缓存命中与回退链）。
	cachePath, _, _, err := i.fetchArtifact(ctx, npmP, provider.Version{Version: version}, nil)
	if err != nil {
		return err
	}
	sha, err := verify.SHA256File(cachePath)
	if err != nil {
		return err
	}
	algo, digest := verify.AlgoSHA256, sha
	if shasum != "" {
		if d, err := verify.DigestFile(cachePath, verify.AlgoSHA1); err == nil {
			algo, digest = verify.AlgoSHA1, d
		}
	}
	if _, err := i.newVerifier().Verify(ctx, verify.Request{
		Tool: "npm", Version: version, Platform: i.opt.GOOS + "-" + i.opt.GOARCH,
		Digest: digest, Algo: algo, Format: "registry-integrity", InlineShasum: shasum,
		Insecure: i.opt.Insecure, Rehash: i.opt.Rehash,
	}); err != nil {
		_ = os.Remove(cachePath)
		return err
	}

	tmp := npmDir + ".tmp." + randomSuffix()
	format, err := archive.FormatFor(extOrDefault(npmP, i.opt.GOOS))
	if err != nil {
		return err
	}
	if err := archive.Extract(cachePath, tmp, format, archive.Layout{Strip: npmP.Layout.Strip, Bins: npmP.Layout.Bins, BinTargets: npmP.Layout.BinTargets}, i.opt.GOOS); err != nil {
		_ = os.RemoveAll(tmp)
		_ = os.Remove(cachePath)
		return err
	}
	if err := archive.Replace(npmDir, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// applyGenericCompanions 处理除 npm 之外的 [companions]（1.1：伴生包不再只认 node→npm）。
//
// 语义：把 registry 指向的 provider 装进 <工具根>/<target>（和 node 里的 npm 一样属于宿主的一部分，
// 因此不单独写台账 —— 卸载/回收宿主时它跟着一起走）。mode = bundled 表示产物自带、不动。
func (i *Installer) applyGenericCompanions(ctx context.Context, p *provider.Provider, version, treeDir string) error {
	for key, c := range p.Companions {
		if key == "npm" || c.Mode == "bundled" {
			continue // npm 有 engines 反查语义，走 applyCompanion
		}
		want := c.Version
		cp, err := i.opt.LookupProvider(c.Registry)
		if err != nil {
			return fmt.Errorf("%s 的伴生包 %s 找不到清单 %q: %w", p.ID, key, c.Registry, err)
		}
		if want == "" {
			idx, ierr := i.loadIndex(ctx, cp)
			if ierr != nil {
				return fmt.Errorf("伴生包 %s 的版本索引不可用: %w", key, ierr)
			}
			latest, ok := idx.Latest()
			if !ok {
				return fmt.Errorf("伴生包 %s 的索引里没有可用版本", key)
			}
			want = latest.Version
		}
		cachePath, _, _, err := i.fetchArtifact(ctx, cp, provider.Version{Version: want}, nil)
		if err != nil {
			return err
		}
		dst := filepath.Join(treeDir, filepath.FromSlash(companionTarget(c.Target, i.opt.GOOS)))
		format, err := archive.FormatFor(extOrDefault(cp, i.opt.GOOS))
		if err == nil {
			tmp := dst + ".tmp." + randomSuffix()
			err = archive.Extract(cachePath, tmp, format, archive.Layout{Strip: cp.Layout.Strip, Bins: cp.Layout.Bins, BinTargets: cp.Layout.BinTargets}, i.opt.GOOS)
			if err == nil {
				err = archive.Replace(dst, tmp)
			}
			if err != nil {
				_ = os.RemoveAll(tmp)
				return fmt.Errorf("伴生包 %s 落地失败: %w", key, err)
			}
		}
		if err != nil {
			return err
		}
		i.emit(jsonout.Event{Ev: "companion", Tool: key, Version: want, Msg: "into " + c.Target})
	}
	return nil
}

// enableCorepack 把该 node 自带的 corepack 的 shim 指到独立目录（方案 B2-2）。
//
// node 25+ 已移除 corepack —— 这种情况必须明确报错，而不是静默跳过：
// 用户敲了 --corepack 就得知道"这个版本没有"，并被告知 pnpm/yarn 该怎么装。

func (i *Installer) enableCorepack(ctx context.Context, toolDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(toolDir, filepath.FromSlash("lib/node_modules/corepack"))); err != nil {
		return false, fmt.Errorf("该 node 不自带 corepack（node 25+ 已移除）：pnpm/yarn 请用 `pk install pnpm`")
	}
	bin := i.opt.CorepackBin
	if bin == "" {
		bin = i.opt.Paths.GlobalCorepackBin()
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return false, err
	}
	pruneDanglingShims(bin) // gc 回收过 node 版本后旧 shim 会悬空，corepack enable 会因 realpath 失败中断
	run := i.opt.RunCommand
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w（%s）", name, err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	node := filepath.Join(toolDir, "bin", "node")
	if _, err := os.Stat(node); err != nil {
		node = filepath.Join(toolDir, "node") // 单文件布局兜底
	}
	if err := run(ctx, node, filepath.Join(toolDir, filepath.FromSlash("lib/node_modules/corepack/dist/corepack.js")),
		"enable", "--install-directory", bin); err != nil {
		return false, fmt.Errorf("corepack enable 失败: %w", err)
	}
	i.emit(jsonout.Event{Ev: "corepack", Tool: "node", Version: filepath.Base(filepath.Dir(toolDir)), Source: bin})
	return true, nil
}

// pruneDanglingShims 删掉 shim 目录里指向已不存在文件的软链（Windows 的 .cmd 不是软链，自然跳过）。
func pruneDanglingShims(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if p := filepath.Join(dir, e.Name()); e.Type()&os.ModeSymlink != 0 {
			if _, err := os.Stat(p); err != nil { // Stat 跟随软链：悬空即报错
				_ = os.Remove(p)
			}
		}
	}
}

// transportOf 记录清单走哪种传输：内网 http 源在台账里留痕（只增字段）。

// companionTarget 处理 Windows 与 Unix 的目录差异：
// Unix 是 lib/node_modules/npm，Windows 官方 zip 是 node_modules/npm。
func companionTarget(target, goos string) string {
	if goos == "windows" {
		return strings.TrimPrefix(target, "lib/")
	}
	return target
}

func readNpmVersion(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return pkg.Version
}

// indexFuture 是一次后台索引加载的结果槽。
type indexFuture struct {
	done chan struct{}
	idx  *provider.Index
}

// PrefetchIndexes 并发预热多个工具的索引（网络并行，装/写台账仍串行）：逐个工具走
// "拉索引 → 下载 → 解压"，索引那段彼此无关，串着做就是把 N 次延迟相加。失败不报错，真到那一步会重试。
func (i *Installer) PrefetchIndexes(ctx context.Context, tools []string) {
	for _, tool := range tools {
		p, err := i.opt.LookupProvider(tool)
		if err != nil {
			continue
		}
		// 先把 auto 档位定下来：不改的话后台协程会读到 "auto"，主协程又在 Install 里改 i.opt.Profile
		// （race），而且预取也会拿错档位的 URL。resolveAutoProfile 自己幂等，第二次起直接返回。
		i.resolveAutoProfile(ctx, p)
		i.startPrefetch(ctx, p)
	}
}

// prefetchCompanionIndex 在解压（纯 CPU 的秒级工作）期间先拉伴生包的索引；--dry-run 不预取。
func (i *Installer) prefetchCompanionIndex(ctx context.Context, p *provider.Provider) {
	if c, ok := p.Companions["npm"]; ok && !i.opt.DryRun {
		if npmP, err := i.opt.LookupProvider(c.Registry); err == nil {
			i.startPrefetch(ctx, npmP)
		}
	}
}

// startPrefetch 起一个后台索引加载（同一工具只起一次），返回它的结果槽。
//
// prefetched 这张表只在"安装协程"里读写（cmd → Install/Upgrade → 这里 / loadOrTakeIndex），
// 后台协程只写自己的 f.idx 再 close(f.done)，读侧靠 channel 的 happens-before，所以不需要锁。
func (i *Installer) startPrefetch(ctx context.Context, p *provider.Provider) *indexFuture {
	f, ok := i.prefetched[p.ID]
	if ok { // 已经起过：直接复用它的结果槽（绝不能再起一个 goroutine 去写同一个槽）
		return f
	}
	f = &indexFuture{done: make(chan struct{})}
	i.prefetched[p.ID] = f
	go func() {
		defer close(f.done)
		if idx, err := i.loadIndex(ctx, p); err == nil {
			f.idx = idx
		}
	}()
	return f
}

// loadOrTakeIndex 优先用预取结果（等它结束）；没有预取就现加载。
func (i *Installer) loadOrTakeIndex(ctx context.Context, p *provider.Provider) (*provider.Index, error) {
	f, ok := i.prefetched[p.ID]
	delete(i.prefetched, p.ID)
	if !ok {
		return i.loadIndex(ctx, p)
	}
	select {
	case <-f.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.idx != nil {
		return f.idx, nil
	}
	return i.loadIndex(ctx, p) // 预取失败：再试一次，报错信息才算准确
}
