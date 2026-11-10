// Package install 是安装编排层：解析 → 索引 → 下载 → 校验 → 解压 → 伴生 → shim → 台账。
//
// 之所以单独成包：`cmd/pk` 只负责参数与输出（预算 1500 行），编排逻辑要能被 GUI
// 复用，也要能被用例直接调用（不经子进程）。预算从 cmd/pk 里划过来，总量不变。
package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/companion"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/toolchain"
	"github.com/packnest/packnest/internal/verify"
)

// ErrNetwork 表示网络类失败（CLI 映射退出码 3）。
var ErrNetwork = errors.New("网络失败")

// Options 是编排所需的全部依赖（全部可注入，测试零外网）。
type Options struct {
	Paths  paths.Paths
	Client *fetch.Client
	Cfg    *store.Config
	// Profile 是镜像档；为空时用 Cfg.Mirror。
	Profile  string
	Offline  bool
	Insecure bool
	Force    bool
	// Rehash 强制重新向校验源求值并覆盖 TOFU 记录（`--force-rehash`）。
	Rehash bool
	GOOS   string
	GOARCH string
	Now    func() time.Time
	// LookupProvider 默认 provider.Get（测试注入假清单）。
	LookupProvider func(id string) (*provider.Provider, error)
	// Events 用于 NDJSON 事件输出（nil 则静默）。
	Events func(jsonout.Event)
	// Project 是 pk.toml 里的工具声明（含 npm 显式 pin）。
	Project map[string]string
	// DryRun 为 true 时只解析并打印计划，不下载不落盘。
	DryRun bool
	// Frozen 为 true 时不解析版本，直接按 Lock 装（CI 用）。
	Frozen bool
	// Lock 是 pk.lock 的内容（--frozen 必需）。
	Lock *Lock
	// TTL 是索引缓存时长。
	TTL time.Duration
}

// Result 是一次安装的结果。
type Result struct {
	Tool             string
	Version          string
	Path             string
	Verified         string
	SHA256           string
	NPMVersion       string
	NPMReplaced      string
	AlreadyInstalled bool
	// BecameDefault 表示这次安装把它设成了全局默认版本。
	BecameDefault bool
	// Planned 表示这是 --dry-run 的解析结果（什么都没下）。
	Planned      bool
	PlanURL      string
	PlanFormat   string
	PlanChecksum string
}

// Installer 持有台账与校验表，Install 后需调用 Finalize 落盘。
type Installer struct {
	opt    Options
	ledger *store.Ledger
	known  *verify.Known
}

// New 加载台账与校验表。
func New(opt Options) (*Installer, error) {
	if opt.Client == nil {
		c := fetch.New()
		c.Offline = opt.Offline
		opt.Client = c
	}
	if opt.Cfg == nil {
		opt.Cfg = store.DefaultConfig()
	}
	if opt.Profile == "" {
		opt.Profile = opt.Cfg.Mirror
	}
	if opt.Profile == "" {
		opt.Profile = mirror.AutoProfile
	}
	if opt.GOOS == "" {
		opt.GOOS = "linux"
	}
	if opt.GOARCH == "" {
		opt.GOARCH = "amd64"
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.LookupProvider == nil {
		opt.LookupProvider = provider.Get
	}
	ledger, _, err := store.LoadLedgerWithRecovery(opt.Paths.Ledger())
	if err != nil {
		return nil, err
	}
	known, err := verify.LoadKnown(opt.Paths.Known())
	if err != nil {
		return nil, err
	}
	return &Installer{opt: opt, ledger: ledger, known: known}, nil
}

// Ledger 暴露台账（CLI 的 list 复用）。
func (i *Installer) Ledger() *store.Ledger { return i.ledger }

// Finalize 落盘台账、TOFU 表与全局配置（use/install 都可能改默认版本）。
func (i *Installer) Finalize() error {
	if err := i.ledger.Save(i.opt.Paths.Ledger()); err != nil {
		return err
	}
	if err := i.known.Save(i.opt.Paths.Known()); err != nil {
		return err
	}
	return i.opt.Cfg.Save(i.opt.Paths.Config())
}

// now 返回时钟（可注入）。
func (i *Installer) now() time.Time {
	if i.opt.Now != nil {
		return i.opt.Now()
	}
	return time.Now()
}

func (i *Installer) emit(ev jsonout.Event) {
	if i.opt.Events != nil {
		i.opt.Events(ev)
	}
}

// Install 安装一个工具。specStr 为空表示"最新稳定版"。
func (i *Installer) Install(ctx context.Context, tool, specStr string) (Result, error) {
	p, err := i.opt.LookupProvider(tool)
	if err != nil {
		return Result{}, err
	}
	if err := i.checkRequires(p); err != nil {
		return Result{}, err
	}

	var lockedSHA string
	var resolved provider.Version
	var idx *provider.Index
	if i.opt.Frozen {
		version, sha, err := i.lockedVersion(i.opt.Lock, tool)
		if err != nil {
			return Result{}, err
		}
		lockedSHA = sha
		resolved = provider.Version{Version: version}
	} else {
		i.resolveAutoProfile(ctx, p)
		resolved, idx, err = i.resolveVersion(ctx, p, specStr)
		if err != nil {
			return Result{}, err
		}
	}
	version := resolved.Version
	i.emit(jsonout.Event{Ev: "resolve", Tool: tool, Version: version})

	platform := i.opt.GOOS + "-" + i.opt.GOARCH
	target := i.opt.Paths.ToolVersion(tool, version)
	if existing := i.ledger.Find(tool, version); existing != nil && !i.opt.Force {
		if _, statErr := os.Stat(target); statErr == nil {
			return Result{Tool: tool, Version: version, Path: target, Verified: existing.Verified,
				AlreadyInstalled: true, NPMVersion: existing.NpmVersion, NPMReplaced: existing.NpmBundled}, nil
		}
	}

	res := Result{Tool: tool, Version: version, Path: target}

	if i.opt.DryRun {
		urls, err := p.ArtifactURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
		if err != nil {
			return Result{}, err
		}
		res.Planned, res.PlanURL, res.PlanFormat = true, urls[0], extOrDefault(p, i.opt.GOOS)
		res.PlanChecksum = fmt.Sprintf("%s(%s)", p.Checksum.Format, p.Checksum.Mode)
		i.emit(jsonout.Event{Ev: "plan", Tool: tool, Version: version, Source: res.PlanURL, Mode: res.PlanChecksum})
		return res, nil
	}

	archivePath, artifactURL, fromCache, err := i.fetchArtifact(ctx, p, resolved, idx)
	if err != nil {
		return Result{}, err
	}
	res.SHA256, err = verify.SHA256File(archivePath)
	if err != nil {
		return Result{}, err
	}

	vr, err := i.verifyArtifact(ctx, p, resolved, platform, artifactURL, archivePath, res.SHA256, idx)
	if err != nil && fromCache {
		// 缓存坏了（截断/篡改）：删掉重下一次，而不是让用户手动清缓存。
		_ = os.Remove(archivePath)
		archivePath, artifactURL, _, ferr := i.fetchArtifact(ctx, p, resolved, idx)
		if ferr != nil {
			return Result{}, ferr
		}
		res.SHA256, err = verify.SHA256File(archivePath)
		if err != nil {
			return Result{}, err
		}
		vr, err = i.verifyArtifact(ctx, p, resolved, platform, artifactURL, archivePath, res.SHA256, idx)
	}
	if err != nil {
		if !fromCache {
			// 刚下的产物就没过校验：删掉脏缓存，下次重下（可能是投毒或截断）。
			_ = os.Remove(archivePath)
		}
		return Result{}, err
	}
	res.Verified = vr.Verified
	i.emit(jsonout.Event{Ev: "verify", Tool: tool, Version: version, Mode: vr.Mode, OK: boolPtr(true)})

	if i.opt.Frozen && lockedSHA != "" {
		if err := verifyLockedSHA(archivePath, lockedSHA); err != nil {
			return Result{}, err
		}
	}

	tmpDir := target + ".tmp." + randomSuffix()
	layout := archive.Layout{Strip: p.Layout.Strip, Single: p.Layout.Single, Bins: p.Layout.Bins, BinTargets: p.Layout.BinTargets}
	format, err := archive.FormatFor(extOrDefault(p, i.opt.GOOS))
	if err != nil {
		return Result{}, err
	}
	// Ctrl-C / panic 也不许留半成品目录（方案 §12.1 `_M4_CtrlCNoPartial`）。
	placed := false
	defer func() {
		if !placed {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	if err := archive.Extract(archivePath, tmpDir, format, layout, i.opt.GOOS); err != nil {
		// 归档损坏：把缓存也删掉，下次重下（可能是截断的产物）。
		_ = os.Remove(archivePath)
		return Result{}, fmt.Errorf("解压失败（产物可能被截断或损坏）: %w", err)
	}

	npmVersion, npmBundled, err := i.applyCompanion(ctx, p, version, tmpDir, idx)
	if err != nil {
		return Result{}, err
	}
	res.NPMVersion, res.NPMReplaced = npmVersion, npmBundled

	if err := archive.Replace(target, tmpDir); err != nil {
		return Result{}, err
	}
	placed = true

	i.ledger.Upsert(store.Install{
		Tool: tool, Version: version, Path: target,
		Source: i.opt.Profile, SHA256: res.SHA256, Verified: res.Verified,
		NpmBundled: npmBundled, NpmVersion: npmVersion,
		Channel: "manual",
	})
	// 首次安装即设为全局默认；已有默认版本时不抢（切版本用 `pk use`）。
	if i.ActiveVersion(tool) == "" {
		if err := i.Activate(tool, version); err != nil {
			return Result{}, err
		}
		res.BecameDefault = true
	} else if i.ActiveVersion(tool) == version {
		if err := i.installShims(p, target); err != nil {
			return Result{}, err
		}
	}
	i.emit(jsonout.Event{Ev: "done", Tool: tool, Version: version, Path: target})
	return res, nil
}

// Uninstall 台账驱动卸载：只删自己记录过的目录（方案 §8.2 第 3 条）。
func (i *Installer) Uninstall(tool, version string, force bool) error {
	in := i.ledger.Find(tool, version)
	if in == nil {
		return fmt.Errorf("%s@%s 不在台账里；不在台账里的目录只提示不删除（确认后可手动清理）", tool, version)
	}
	if len(in.References) > 0 && !force {
		refs := make([]string, 0, len(in.References))
		for _, r := range in.References {
			refs = append(refs, r.Kind+":"+r.Name)
		}
		return fmt.Errorf("%s@%s 仍被引用（%s），确需删除加 --force", tool, version, strings.Join(refs, ", "))
	}
	if err := removeDirWithRetry(in.Path, 3); err != nil {
		return fmt.Errorf("删除 %q 失败（可能仍有进程占用，关掉后重试或用 --force 留残）: %w", in.Path, err)
	}
	// 顺手把空的 tools/<tool> 收掉：卸载干净不是“目录还在但不空”。
	pruneEmptyDirs(filepath.Dir(in.Path), i.opt.Paths.Root)
	if err := i.removeShimsFor(in.Path); err != nil {
		return err
	}
	wasActive := i.ActiveVersion(tool) == version
	i.ledger.Remove(tool, version)
	if wasActive {
		i.clearActive(tool)
		if err := i.repointAfterRemoval(tool); err != nil {
			return err
		}
	}
	i.emit(jsonout.Event{Ev: "uninstall", Tool: tool, Version: version})
	return nil
}

// UninstallTool 删掉某个工具的**所有**已装版本（`pk uninstall go`）。
//
// 语义与单个卸载一致：不带 force 时，被项目锁 / 其他工具依赖引用的版本会被拒绝；
// 但「全局默认」不算障碍 —— 整个工具都要没了，默认版本自然一起清掉。
func (i *Installer) UninstallTool(tool string, force bool) ([]string, error) {
	versions := make([]string, 0, 4)
	for _, in := range i.ledger.Installs {
		if in.Tool == tool {
			versions = append(versions, in.Version)
		}
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("%s 没有已装版本（`pk list` 看已装工具）", tool)
	}
	sort.Strings(versions)

	removed := make([]string, 0, len(versions))
	var blocked []string
	for _, v := range versions {
		in := i.ledger.Find(tool, v)
		if in == nil {
			continue
		}
		if refs := nonGlobalRefs(in); len(refs) > 0 && !force {
			blocked = append(blocked, fmt.Sprintf("%s@%s（%s）", tool, v, strings.Join(refs, ", ")))
			continue
		}
		if err := removeDirWithRetry(in.Path, 3); err != nil {
			return removed, fmt.Errorf("删除 %q 失败（可能仍有进程占用，关掉后重试或用 --force 留残）: %w", in.Path, err)
		}
		pruneEmptyDirs(filepath.Dir(in.Path), i.opt.Paths.Root)
		if err := i.removeShimsFor(in.Path); err != nil {
			return removed, err
		}
		if i.ActiveVersion(tool) == v {
			i.clearActive(tool)
		}
		i.ledger.Remove(tool, v)
		removed = append(removed, v)
		i.emit(jsonout.Event{Ev: "uninstall", Tool: tool, Version: v})
	}
	if len(blocked) > 0 {
		return removed, fmt.Errorf("%s 里这些版本仍被引用，未删除（确需删除加 --force）: %s",
			tool, strings.Join(blocked, "; "))
	}
	return removed, nil
}

// nonGlobalRefs 列出"不是全局默认"的引用（项目锁、被其他工具依赖）。
func nonGlobalRefs(in *store.Install) []string {
	out := make([]string, 0, len(in.References))
	for _, r := range in.References {
		if r.Kind == store.RefGlobal {
			continue
		}
		name := r.Kind
		if r.Name != "" {
			name += ":" + r.Name
		}
		out = append(out, name)
	}
	return out
}

func (i *Installer) removeShimsFor(dir string) error {
	names, err := shim.List(i.opt.Paths.Bin(), i.opt.GOOS)
	if err != nil {
		return err
	}
	for _, name := range names {
		target, isShim, err := shim.Resolve(i.opt.Paths.Bin(), name, i.opt.GOOS)
		if err != nil || !isShim {
			continue
		}
		if target == dir || strings.HasPrefix(target, dir+string(filepath.Separator)) {
			path := filepath.Join(i.opt.Paths.Bin(), name)
			if i.opt.GOOS == "windows" {
				path = shim.CmdPath(i.opt.Paths.Bin(), name)
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("删除 shim %q 失败: %w", name, err)
			}
		}
	}
	return nil
}

// checkRequires 校验工具依赖（pnpm 需要 node，方案 §18.1）：不自动装，只给建议。
func (i *Installer) checkRequires(p *provider.Provider) error {
	for _, need := range p.Requires {
		if i.ledger.Active(need) != "" || len(i.installedVersions(need)) > 0 {
			continue
		}
		return fmt.Errorf("%s 需要 %s：先 `pk install %s` 再装 %s（我们不隐式安装运行时）", p.ID, need, need, p.ID)
	}
	return nil
}

func (i *Installer) installedVersions(tool string) []string {
	var out []string
	for _, in := range i.ledger.Installs {
		if in.Tool == tool {
			out = append(out, in.Version)
		}
	}
	sort.Strings(out)
	return out
}

// resolveAutoProfile：档位是 auto 时先判定环境（国外 → 官方源，国内 → 镜像），
// 判定结果同时写进探测缓存，后续命令 6h 内不再探。
func (i *Installer) resolveAutoProfile(ctx context.Context, p *provider.Provider) {
	if i.opt.Profile != mirror.AutoProfile && i.opt.Profile != "" {
		return
	}
	official, domestic := p.Index.URLs["default"], p.Index.URLs["cn"]
	prober := &mirror.Prober{Client: i.opt.Client, Path: i.opt.Paths.Probe(), TTL: mirror.DefaultTTL}
	profile, res, err := mirror.Detect(ctx, prober, official, domestic)
	if err != nil || profile == "" {
		i.opt.Profile = "cn" // 探测不出结论时按国内档（国内是主要受众）
		return
	}
	i.opt.Profile = profile
	i.emit(jsonout.Event{Ev: "mirror", Tool: p.ID, Source: profile,
		Msg: fmt.Sprintf("%s (%dms)", profile, res.LatencyMS)})
}

func (i *Installer) resolveVersion(ctx context.Context, p *provider.Provider, specStr string) (provider.Version, *provider.Index, error) {
	if specStr == "" {
		if pinned, ok := i.opt.Project[p.ID]; ok && pinned != "" {
			specStr = pinned
		} else if pinned, ok := i.opt.Cfg.Tools[p.ID]; ok && pinned != "" {
			specStr = pinned
		} else {
			specStr = "latest"
		}
	}
	sp, err := spec.Parse(specStr)
	if err != nil {
		return provider.Version{}, nil, fmt.Errorf("版本说明符 %q 非法: %w", specStr, err)
	}
	if sp.Kind() == spec.Exact && !sp.IsAlias() {
		version := strings.TrimPrefix(specStr, "v")
		// registry 型（npm/pnpm）的校验和来自元数据、index-inline 型的校验和也在索引里，
		// 所以即便钉了精确版本也要读索引；读不到不算失败（会走 --insecure 或报缺校验和）。
		if p.Index.Kind == provider.IndexRegistry || p.Checksum.Format == provider.ChecksumIndexInline ||
			artifactNeedsFilename(p) {
			if idx, err := i.loadIndex(ctx, p); err == nil {
				if v, ok := idx.Find(version); ok {
					return v, idx, nil
				}
				return provider.Version{Version: version}, idx, nil
			}
		}
		return provider.Version{Version: version}, nil, nil
	}
	idx, err := i.loadIndex(ctx, p)
	if err != nil {
		return provider.Version{}, nil, err
	}
	v, err := idx.Resolve(sp)
	if err != nil {
		return provider.Version{}, idx, err
	}
	return v, idx, nil
}

// artifactNeedsFilename 判断产物 URL 模板里是否用了 {filename}（go/python 需要索引元数据）。
func artifactNeedsFilename(p *provider.Provider) bool {
	for _, tmpl := range p.Artifact.URLs {
		if strings.Contains(tmpl, "{filename}") {
			return true
		}
	}
	return false
}

func (i *Installer) loadIndex(ctx context.Context, p *provider.Provider) (*provider.Index, error) {
	loader := &provider.IndexLoader{
		Client:   i.opt.Client,
		CacheDir: i.opt.Paths.CacheMeta(),
		TTL:      i.opt.TTL,
		Custom:   toolchain.LoadPBSIndex,
	}
	idx, err := loader.Load(ctx, p, i.opt.Profile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	return idx, nil
}

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
	cachePath := filepath.Join(i.opt.Paths.CacheArchives(),
		fmt.Sprintf("%s-%s-%s.%s", p.ID, version, platform, ext))

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

func (i *Installer) verifyArtifact(ctx context.Context, p *provider.Provider, resolved provider.Version, platform, artifactURL, archivePath, sha256 string, idx *provider.Index) (verify.Result, error) {
	version := resolved.Version
	sumsURLs, err := p.ChecksumURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return verify.Result{}, err
	}
	algo, digest := registryDigest(idx, version, archivePath, sha256)
	inline := ""
	if p.Checksum.Format == provider.ChecksumIndexInline {
		// go 的 dl JSON 自带每个产物的 sha256：省掉一次校验和请求。
		algo, digest = verify.AlgoSHA256, sha256
		if f, ok := resolved.FileFor(i.opt.GOOS, i.opt.GOARCH); ok {
			inline = f.SHA256
		}
	} else if p.Checksum.Format != "registry-integrity" {
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
	v := verify.New(i.known)
	v.Fetch = func(ctx context.Context, url string) ([]byte, error) {
		return i.opt.Client.Get(ctx, url)
	}
	v.Now = i.opt.Now
	req := verify.Request{
		Tool:         p.ID,
		Version:      version,
		Platform:     platform,
		Artifact:     filepath.Base(artifactURL),
		Digest:       digest,
		Algo:         algo,
		SumsURLs:     sumsURLs,
		Format:       p.Checksum.Format,
		InlineShasum: inline,
		Insecure:     i.opt.Insecure,
		Rehash:       i.opt.Rehash,
	}
	return v.Verify(ctx, req)
}

// applyCompanion 处理 node→npm 伴生（方案 §7）。返回（生效 npm 版本, 自带 npm 版本）。
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
	if err == nil {
		if idx, err := i.loadIndex(ctx, npmP); err == nil {
			indexAvailable = true
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
	if npmP != nil {
		if idx, err := i.loadIndex(ctx, npmP); err == nil {
			if v, ok := idx.Find(npmVersion); ok {
				shasum = v.Shasum
			}
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
	urls, err := npmP.ArtifactURLs(version, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
	if err != nil {
		return err
	}
	ext := extOrDefault(npmP, i.opt.GOOS)
	platform := i.opt.GOOS + "-" + i.opt.GOARCH
	cachePath := filepath.Join(i.opt.Paths.CacheArchives(), fmt.Sprintf("npm-%s-%s.%s", version, platform, ext))
	if st, err := os.Stat(cachePath); err != nil || st.Size() == 0 {
		if _, _, err := i.downloadFromChain(ctx, "npm", version, urls, cachePath); err != nil {
			return err
		}
	}
	sha, err := verify.SHA256File(cachePath)
	if err != nil {
		return err
	}
	v := verify.New(i.known)
	v.Fetch = func(ctx context.Context, url string) ([]byte, error) { return i.opt.Client.Get(ctx, url) }
	v.Now = i.opt.Now
	algo, digest := verify.AlgoSHA256, sha
	if shasum != "" {
		if d, err := verify.DigestFile(cachePath, verify.AlgoSHA1); err == nil {
			algo, digest = verify.AlgoSHA1, d
		}
	}
	if _, err := v.Verify(ctx, verify.Request{
		Tool: "npm", Version: version, Platform: platform,
		Digest: digest, Algo: algo, Format: "registry-integrity", InlineShasum: shasum,
		Insecure: i.opt.Insecure, Rehash: i.opt.Rehash,
	}); err != nil {
		_ = os.Remove(cachePath)
		return err
	}

	tmp := npmDir + ".tmp." + randomSuffix()
	format, err := archive.FormatFor(ext)
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

func (i *Installer) installShims(p *provider.Provider, toolDir string) error {
	for _, name := range p.Layout.Bins {
		binPath, err := archive.FindBin(toolDir, name, i.opt.GOOS, p.Layout.BinTargets)
		if err != nil {
			// node 的 npm/npx 在 Windows zip 里是 .cmd，FindBin 已覆盖；真找不到就跳过（不阻断安装）。
			continue
		}
		if err := shim.Ensure(i.opt.Paths.Bin(), name, binPath, i.opt.GOOS); err != nil {
			return err
		}
	}
	return nil
}

// companionTarget 处理 Windows 与 Unix 的目录差异：
// Unix 是 lib/node_modules/npm，Windows 官方 zip 是 node_modules/npm。
func companionTarget(target, goos string) string {
	if goos == "windows" {
		return strings.TrimPrefix(target, "lib/")
	}
	return target
}

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

func randomSuffix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func removeDirWithRetry(dir string, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// LoadProject 读取项目 pk.toml 的 [tools]（项目锁的另一半在 M4）。
func LoadProject(dir string) (map[string]string, error) {
	path := filepath.Join(dir, "pk.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %q 失败: %w", path, err)
	}
	var doc struct {
		Tools map[string]string `toml:"tools"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 %q 失败: %w", path, err)
	}
	return doc.Tools, nil
}

func boolPtr(b bool) *bool { return &b }

// Root 暴露 PK_ROOT（CLI 打印"从哪里卸载"用）。
func (i *Installer) Root() string { return i.opt.Paths.Root }
