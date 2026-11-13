// Package install 是安装编排层：解析 → 索引 → 下载 → 校验 → 解压 → 伴生 → shim → 台账。
//
// 之所以单独成包：`cmd/pk` 只负责参数与输出（预算 1500 行），编排逻辑要能被 GUI
// 复用，也要能被用例直接调用（不经子进程）。预算从 cmd/pk 里划过来，总量不变。
package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
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
	// Corepack 为 true 时，装完 node（且该 node 自带 corepack）后执行 `corepack enable`
	// 到 $PK_ROOT/global/corepack/bin（方案 B2-2；默认关，`pk install node --corepack` 才开）。
	Corepack bool
	// RunCommand 执行外部命令（默认 os/exec）；测试注入以免真跑 corepack。
	RunCommand func(ctx context.Context, name string, args ...string) error
	// CorepackBin 是 corepack 的 shim 目录；默认真实的 GlobalCorepackBin()（测试注入临时目录）。
	CorepackBin string
	// KeepVersions > 0 时，gc 对每个工具保留最新的 N 个版本（`pk gc --keep N`）。
	KeepVersions int
}

// Result 是一次安装的结果。
type Result struct {
	Tool        string
	Version     string
	Path        string
	Verified    string
	SHA256      string
	NPMVersion  string
	NPMReplaced string
	// Corepack 表示本次装完启用了 corepack（--corepack 且该 node 自带）。
	Corepack         bool
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
	// pf 是"正在后台加载的索引"（键为 provider ID）。多工具并行安装时多个 worker 共享它，
	// 同一份索引不重复拉；互斥在 indexCache 内部。
	pf *indexCache
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
	return &Installer{opt: opt, ledger: ledger, known: known, pf: newIndexCache()}, nil
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
//
// 拆成 prepare（可并行：校验/解压/伴生包，只写自己的 tmp）与 commit（单写者：落盘/台账/shim）。
// 单工具路径两者紧挨着跑，行为与拆分前完全一致。
func (i *Installer) Install(ctx context.Context, tool, specStr string) (Result, error) {
	prep, err := i.prepare(ctx, tool, specStr, true)
	if err != nil {
		return Result{}, err
	}
	return i.commit(ctx, prep)
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

func (i *Installer) installedVersions(tool string) []string { return i.ledger.Versions(tool) }

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
	cached := ""
	if res.Cached {
		cached = ", 6h 内探过"
	}
	i.emit(jsonout.Event{Ev: "mirror", Tool: p.ID, Source: profile,
		Msg: fmt.Sprintf("%s (%dms%s)", profile, res.LatencyMS, cached)})
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
			if idx, err := i.loadOrTakeIndex(ctx, p); err == nil {
				if v, ok := idx.Find(version); ok {
					return v, idx, nil
				}
				return provider.Version{Version: version}, idx, nil
			}
		}
		return provider.Version{Version: version}, nil, nil
	}
	idx, err := i.loadOrTakeIndex(ctx, p)
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
	}
	idx, err := loader.Load(ctx, p, i.opt.Profile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	return idx, nil
}

func transportOf(p *provider.Provider) string {
	if p.InsecureTransport {
		return "http"
	}
	return ""
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

func boolPtr(b bool) *bool { return &b }

// Root 暴露 PK_ROOT（CLI 打印"从哪里卸载"用）。
func (i *Installer) Root() string { return i.opt.Paths.Root }
