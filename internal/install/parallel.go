package install

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/verify"
)

// prepared 是一次安装的"前半段"结果：校验、解压、伴生包都落在自己的 tmp 树里，
// 不碰台账、TOFU 主表、配置与 shim。并行只跑这一段，落盘统一由 commit 在主协程做。
type prepared struct {
	res    Result
	tmpDir string
	// skip 表示无需落盘（已装 / --dry-run）；plan 表示这是预览结果。
	skip bool
	plan bool
	p    *provider.Provider
	// events 是 prepare 阶段产生的事件（并行时不直接 emit，由 commit 按序补发）。
	events []jsonout.Event
	// known 是本次校验用的 TOFU 副本（verify.Verify 会写 map，共享主表会并发写崩）。
	known    *verify.Known
	knownKey string
}

// worker 复制一份"只带自己 TOFU 副本"的视图：台账只读共享（层内没人写），
// prefetch 表互斥共享（同一个索引别拉 N 次），client 共享（fetch.Client 无可变状态）。
func (i *Installer) worker(events *[]jsonout.Event) *Installer {
	sub := *i
	sub.known = cloneKnown(i.known)
	// client 的字段除了 Progress 都是只读的，值拷贝一份就能安全地挂自己的进度回调
	// （downloadFromChain 会临时改 Client.Progress，共享指针会互相覆盖）。
	if i.opt.Client != nil {
		cl := *i.opt.Client
		sub.opt.Client = &cl
	}
	if events != nil {
		sub.opt.Events = func(ev jsonout.Event) { *events = append(*events, ev) }
	}
	return &sub
}

func cloneKnown(k *verify.Known) *verify.Known {
	out := &verify.Known{Schema: k.Schema, Entries: make(map[string]verify.Entry, len(k.Entries))}
	for key, e := range k.Entries {
		out.Entries[key] = e
	}
	return out
}

// prepare 执行"能并行"的全部步骤。
//
// emitNow 为 true 时事件即时发出（单工具路径，行为与旧版完全一致）；
// 为 false 时收集到 prepared.events，由 commit 补发（并行路径）。
func (i *Installer) prepare(ctx context.Context, tool, specStr string, emitNow bool) (*prepared, error) {
	p, err := i.opt.LookupProvider(tool)
	if err != nil {
		return nil, err
	}
	if err := i.checkRequires(p); err != nil {
		return nil, err
	}

	var evs []jsonout.Event
	sub := i
	if !emitNow {
		sub = i.worker(&evs)
	}

	var lockedSHA string
	var resolved provider.Version
	var idx *provider.Index
	if i.opt.Frozen {
		version, sha, err := i.lockedVersion(i.opt.Lock, tool)
		if err != nil {
			return nil, err
		}
		lockedSHA = sha
		resolved = provider.Version{Version: version}
	} else {
		sub.resolveAutoProfile(ctx, p)
		resolved, idx, err = sub.resolveVersion(ctx, p, specStr)
		if err != nil {
			return nil, err
		}
	}
	version := resolved.Version
	if err := checkMinVersion(p, version); err != nil {
		return nil, err
	}
	sub.emit(jsonout.Event{Ev: "resolve", Tool: tool, Version: version})

	target := i.opt.Paths.ToolVersion(tool, version)
	prep := &prepared{p: p, events: evs, known: sub.known}
	prep.res = Result{Tool: tool, Version: version, Path: target}
	if existing := i.ledger.Find(tool, version); existing != nil && !i.opt.Force {
		if _, statErr := os.Stat(target); statErr == nil {
			prep.res.Verified = existing.Verified
			prep.res.AlreadyInstalled = true
			prep.res.NPMVersion = existing.NpmVersion
			prep.res.NPMReplaced = existing.NpmBundled
			prep.skip = true
			return prep, nil
		}
	}

	if i.opt.DryRun {
		urls, err := p.ArtifactURLsFor(resolved, i.opt.GOOS, i.opt.GOARCH, i.opt.Profile)
		if err != nil {
			return nil, err
		}
		prep.res.Planned, prep.res.PlanURL, prep.res.PlanFormat = true, urls[0], extOrDefault(p, i.opt.GOOS)
		prep.res.PlanChecksum = fmt.Sprintf("%s(%s)", p.Checksum.Format, p.Checksum.Mode)
		sub.emit(jsonout.Event{Ev: "plan", Tool: tool, Version: version, Source: prep.res.PlanURL, Mode: prep.res.PlanChecksum})
		prep.skip, prep.plan = true, true
		return prep, nil
	}

	archivePath, _, sha, vr, err := sub.fetchArtifactVerified(ctx, p, resolved, idx)
	if err != nil {
		return nil, err
	}
	prep.res.SHA256, prep.res.Verified = sha, vr.Verified
	sub.emit(jsonout.Event{Ev: "verify", Tool: tool, Version: version, Mode: vr.Mode, OK: boolPtr(true), Msg: vr.Note})

	if i.opt.Frozen && lockedSHA != "" {
		if err := verifyLockedSHA(archivePath, lockedSHA); err != nil {
			return nil, err
		}
	}

	tmpDir := target + ".tmp." + randomSuffix()
	layout := archive.Layout{Strip: p.Layout.Strip, Single: p.Layout.Single, Bins: p.Layout.Bins, BinTargets: p.Layout.BinTargets}
	format, err := archive.FormatFor(extOrDefault(p, i.opt.GOOS))
	if err != nil {
		return nil, err
	}
	// 解压要 1s 上下（纯 CPU），这段时间把伴生包的索引先拉回来。
	sub.prefetchCompanionIndex(ctx, p)

	placed := false
	defer func() {
		if !placed {
			_ = os.RemoveAll(tmpDir)
		}
	}()
	if err := archive.Extract(archivePath, tmpDir, format, layout, i.opt.GOOS); err != nil {
		_ = os.Remove(archivePath) // 归档损坏：缓存也删掉，下次重下
		return nil, fmt.Errorf("解压失败（产物可能被截断或损坏）: %w", err)
	}

	npmVersion, npmBundled, err := sub.applyCompanion(ctx, p, version, tmpDir, idx)
	if err != nil {
		return nil, err
	}
	prep.res.NPMVersion, prep.res.NPMReplaced = npmVersion, npmBundled

	if err := sub.applyGenericCompanions(ctx, p, version, tmpDir); err != nil {
		return nil, err
	}

	prep.tmpDir = tmpDir
	prep.knownKey = verify.Key(p.ID, version, i.opt.GOOS+"-"+i.opt.GOARCH)
	placed = true // 交给 commit 决定去留
	return prep, nil
}

// commit 是单写者阶段：改名落盘、写台账/TOFU、切默认、装 shim、corepack。
func (i *Installer) commit(ctx context.Context, prep *prepared) (Result, error) {
	res := prep.res
	// 并行路径的事件在 prepare 阶段被收集，这里按发生顺序补发（单工具路径 events 为空，已直接发出）。
	for _, ev := range prep.events {
		i.emit(ev)
	}
	if prep.plan {
		return res, nil
	}
	if prep.skip {
		if prep.p.ID == "node" && i.opt.Corepack && !i.opt.DryRun {
			enabled, err := i.enableCorepack(ctx, res.Path)
			if err != nil {
				return res, err
			}
			res.Corepack = enabled
		}
		return res, nil
	}
	placed := false
	defer func() {
		if !placed {
			_ = os.RemoveAll(prep.tmpDir)
		}
	}()
	if err := archive.Replace(res.Path, prep.tmpDir); err != nil {
		return res, err
	}
	placed = true

	if prep.known != nil && prep.knownKey != "" {
		if e, ok := prep.known.Get(prep.knownKey); ok {
			i.known.Entries[prep.knownKey] = e
		}
	}
	i.ledger.Upsert(store.Install{
		Tool: res.Tool, Version: res.Version, Path: res.Path,
		Source: i.opt.Profile, SHA256: res.SHA256, Verified: res.Verified, Transport: transportOf(prep.p),
		NpmBundled: res.NPMReplaced, NpmVersion: res.NPMVersion,
		Channel: "manual", SizeBytes: store.DirSize(res.Path),
	})
	if i.ActiveVersion(res.Tool) == "" {
		if err := i.Activate(res.Tool, res.Version); err != nil {
			return res, err
		}
		res.BecameDefault = true
	} else if i.ActiveVersion(res.Tool) == res.Version {
		if err := i.installShims(prep.p, res.Path); err != nil {
			return res, err
		}
	}
	if prep.p.ID == "node" && i.opt.Corepack {
		enabled, err := i.enableCorepack(ctx, res.Path)
		if err != nil {
			return res, err
		}
		res.Corepack = enabled
	}
	i.emit(jsonout.Event{Ev: "done", Tool: res.Tool, Version: res.Version, Path: res.Path})
	return res, nil
}

// Outcome 是一个目标的安装结果（与输入同序）。
type Outcome struct {
	Tool string
	Spec string
	Res  Result
	Err  error
}

// InstallMany 按"跨工具并行、同工具串行"安装一批目标：层内并发 prepare，层间按序 commit。
// 单目标时退化为串行 Install（行为与旧版一致，含进度条与事件时序）。
func (i *Installer) InstallMany(ctx context.Context, targets []Target) []Outcome {
	if len(targets) == 1 {
		res, err := i.Install(ctx, targets[0].Tool, targets[0].Spec)
		return []Outcome{{Tool: targets[0].Tool, Spec: targets[0].Spec, Res: res, Err: err}}
	}
	out := make([]Outcome, len(targets))
	for k, t := range targets {
		out[k] = Outcome{Tool: t.Tool, Spec: t.Spec}
	}
	// auto 档位必须在并发前定下来（resolveAutoProfile 会写 opt.Profile）。
	for _, t := range targets {
		if p, err := i.opt.LookupProvider(t.Tool); err == nil {
			i.resolveAutoProfile(ctx, p)
		}
	}
	levels := i.planLevels(targets)
	sem := make(chan struct{}, 3)
	for _, lvl := range levels {
		if err := ctx.Err(); err != nil {
			for _, k := range lvl {
				out[k].Err = err
			}
			continue
		}
		preps := make([]*prepared, len(lvl))
		errs := make([]error, len(lvl))
		var wg sync.WaitGroup
		for s, k := range lvl {
			wg.Add(1)
			go func(s, k int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				preps[s], errs[s] = i.prepare(ctx, targets[k].Tool, targets[k].Spec, false)
			}(s, k)
		}
		wg.Wait()
		for s, k := range lvl {
			if errs[s] != nil {
				out[k].Err = errs[s]
				continue
			}
			out[k].Res, out[k].Err = i.commit(ctx, preps[s])
		}
	}
	return out
}

// planLevels 把目标分层：层内可并行，层间串行。
//
// 三条约束（都来自现有语义，不是新发明）：
//   - requires：pnpm/bun 需要 node，依赖方必须在下一层；
//   - 同一工具多 spec：Activate/shim 互相覆盖，必须按命令行顺序串行；
//   - shim 同名：node 的 bins 含 npm，npx/npm 与独立 npm 冲突，必须错层。
func (i *Installer) planLevels(targets []Target) [][]int {
	type meta struct {
		tool string
		rank int
		bins map[string]bool
	}
	metas := make([]meta, len(targets))
	rank := map[string]int{}
	var rankOf func(tool string, depth int) int
	rankOf = func(tool string, depth int) int {
		if depth > 8 { // 清单里不可能有环，防御一下
			return 0
		}
		if r, ok := rank[tool]; ok {
			return r
		}
		p, err := i.opt.LookupProvider(tool)
		if err != nil {
			return 0
		}
		r := 0
		for _, need := range p.Requires {
			if nr := rankOf(need, depth+1) + 1; nr > r {
				r = nr
			}
		}
		rank[tool] = r
		return r
	}
	for k, t := range targets {
		m := meta{tool: t.Tool, bins: map[string]bool{}}
		if p, err := i.opt.LookupProvider(t.Tool); err == nil {
			for _, b := range p.Layout.Bins {
				m.bins[b] = true
			}
		}
		m.rank = rankOf(t.Tool, 0)
		metas[k] = m
	}

	levels := make([][]int, 0, 4)
	lastOf := map[string]int{} // 同工具最后出现的层
	conflict := func(level int, m meta) bool {
		for _, other := range levels[level] {
			om := metas[other]
			if om.tool == m.tool {
				return true
			}
			for b := range m.bins {
				if om.bins[b] {
					return true
				}
			}
		}
		return false
	}
	for k, m := range metas {
		want := m.rank
		if lv, ok := lastOf[m.tool]; ok && lv+1 > want {
			want = lv + 1
		}
		for len(levels) <= want {
			levels = append(levels, nil)
		}
		for conflict(want, m) {
			want++
			for len(levels) <= want {
				levels = append(levels, nil)
			}
		}
		levels[want] = append(levels[want], k)
		lastOf[m.tool] = want
	}
	out := make([][]int, 0, len(levels))
	for _, l := range levels {
		if len(l) > 0 {
			out = append(out, l)
		}
	}
	return out
}
