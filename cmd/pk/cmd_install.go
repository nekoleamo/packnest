package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/packnest/packnest/cmd/pk/present"
	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/verify"
)

// exitCodeFor 把错误映射成固定退出码（方案 §10）：0/1/2/3/4/5。
func exitCodeFor(err error) int {
	var statusErr *fetch.StatusError
	switch {
	case err == nil:
		return jsonout.ExitOK
	case errors.Is(err, store.ErrLocked):
		return jsonout.ExitLocked
	case errors.Is(err, verify.ErrChecksum):
		return jsonout.ExitChecksum
	case errors.Is(err, fetch.ErrOffline), errors.Is(err, install.ErrNetwork), errors.As(err, &statusErr):
		return jsonout.ExitNetwork
	}
	return jsonout.ExitFail
}

// cmdInstall 安装工具（M1：node + 伴生 npm）。
func (a *app) cmdInstall(args []string) int {
	fs := a.newFlagSet("install")
	frozen := fs.Bool("frozen", false, tr("严格按 pk.lock，不做解析；清单与锁不一致即失败（CI 用）", "follow pk.lock exactly, no resolution; fail when manifest and lock disagree (CI)"))
	insecure := fs.Bool("insecure", false, tr("允许无校验安装（标记 unverified；校验源互相矛盾时也靠它放行）", "allow installing without verification (marked unverified; also passes when checksum sources contradict each other)"))
	jobs := fs.Int("jobs", 0, tr("多连接下载的并发段数（0 = 按产物大小自适应 2~8 段；1 = 强制单流。PK_JOBS 可设默认。产物 ≥8MB 且源支持 Range 才生效）", "parallel download segments (0 = pick 2-8 by artifact size; 1 = force single stream. PK_JOBS sets the default. Only for artifacts >=8MB from Range-capable sources)"))
	force := fs.Bool("force", false, tr("重装已存在的版本", "reinstall a version that already exists"))
	rehash := fs.Bool("force-rehash", false, tr("忽略本地校验表，重新向校验源求值并覆盖记录", "ignore the local TOFU table and re-evaluate against checksum sources"))
	quiet := fs.Bool("quiet", false, tr("只报错，不打印进度", "only print errors, no progress"))
	dryRun := fs.Bool("dry-run", false, tr("只解析并打印计划（版本/URL/校验方式），不下载", "resolve and print the plan (version/URL/checksum mode) without downloading"))
	corepack := fs.Bool("corepack", false, tr("装 node 后启用 corepack（只有 node ≤24 自带；写进 config.toml）", "enable corepack after installing node (bundled up to node 24; persisted in config.toml)"))
	noCorepack := fs.Bool("no-corepack", false, tr("关闭 corepack（写进 config.toml）", "disable corepack (persisted in config.toml)"))
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	if *jobs > 0 {
		a.client.Jobs = *jobs // 多连接下载（只在冷启动 + 支持 Range + ≥8MB 时生效；1 = 单流）
	}

	// corepack 是全局开关（方案 B2-2）：翻一下就落盘，之后装/切带 corepack 的 node 自动重放。
	// --dry-run 不落盘（预览不该改 config.toml）。
	if *corepack != *noCorepack && !*dryRun {
		a.cfg.Corepack = *corepack
		if err := a.cfg.Save(a.paths.Config()); err != nil {
			return a.fail(err)
		}
	}

	project, notes, err := install.ProjectSpecs(a.cwd())
	if err != nil {
		return a.fail(err)
	}
	for _, n := range notes {
		if !a.json {
			fmt.Fprintln(a.out, n)
		}
	}
	targets, err := installTargets(fs.Args(), project)
	if err != nil {
		return a.failUsage(err)
	}

	var lockFile *install.Lock
	if *frozen {
		lockFile, err = install.LoadLock(a.cwd())
		if err != nil {
			return a.fail(err)
		}
		if err := install.CheckFrozen(project, lockFile); err != nil {
			fmt.Fprintf(a.errOut, "pk: %v\n", err)
			return jsonout.ExitFail
		}
	}

	// 进度：每工具一行（E4）。TTY 且支持 ANSI 时整块重绘，否则逐行；
	// --quiet 与 --json 下整个 group 为 nil（For 返回 nil，install 侧据此不挂回调）。
	var group *present.ProgressGroup
	if !*quiet && !a.json {
		labels := make([]string, len(targets))
		for i, t := range targets {
			labels[i] = t.tool
		}
		group = present.NewGroup(a.out, labels)
		// 单工具走 Install()（不复制 client），回调直接挂在共享 client 上，
		// 行为与 1.6 一致；多工具由 install 的 ProgressFor 在 worker 副本上挂。
		if len(targets) == 1 {
			a.client.Progress = group.For(targets[0].tool)
		}
		defer group.Done()
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths:          a.paths,
		Client:         a.client,
		Cfg:            a.cfg,
		Profile:        a.cfg.Mirror,
		Offline:        a.offline,
		Insecure:       *insecure,
		Force:          *force,
		Rehash:         *rehash,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		Project:        project,
		Frozen:         *frozen,
		DryRun:         *dryRun,
		Corepack:       a.cfg.Corepack,
		Lock:           lockFile,
		Events:         a.eventWriterFor(*quiet),
		ProgressFor:    group.For,
		LookupProvider: nil,
	})
	if err != nil {
		return a.failWith(err)
	}
	// 写操作前先清扫崩溃残留（断电/强杀留下的 .tmp/.old/.part）；--dry-run 不动盘。
	if !*dryRun {
		if swept, err := inst.SweepStale(); err == nil && len(swept.Removed) > 0 && !a.json {
			fmt.Fprintf(a.out, tr("清扫残留 %d 项（约 %.1f MB）\n", "swept %d leftover item(s) (~%.1f MB)\n"), len(swept.Removed), float64(swept.Bytes)/(1<<20))
		}
	}

	// 多工具时先把索引一起拉回来（网络并行；并行安装时各 worker 共享这份预取结果）。
	tools := make([]string, len(targets))
	for i, t := range targets {
		tools[i] = t.tool
	}
	inst.PrefetchIndexes(a.context(), tools)

	pre := make([]install.Target, len(targets))
	for i, t := range targets {
		pre[i] = install.Target{Tool: t.tool, Spec: t.spec}
	}
	if len(targets) > 1 && !*quiet && !a.json {
		fmt.Fprintf(a.out, tr("并行安装 %d 个工具（下载进度按工具分行）\n", "installing %d tools in parallel (one download line per tool)\n"), len(targets))
	}
	// 跨工具并行（校验/解压/伴生包），落盘与台账仍单线程。
	outcomes := inst.InstallMany(a.context(), pre)

	var failed bool
	code := 0
	for _, oc := range outcomes {
		if oc.Err != nil {
			if errors.Is(oc.Err, context.Canceled) || errors.Is(oc.Err, context.DeadlineExceeded) {
				fmt.Fprintln(a.errOut, tr("pk: 已取消（缓存已保留，下次会续传/复用）", "pk: cancelled (cache kept; the next run resumes or reuses it)"))
				_ = inst.Finalize()
				return jsonout.ExitFail
			}
			fmt.Fprintf(a.errOut, tr("pk: %s@%s 安装失败: %v\n", "pk: %s@%s failed to install: %v\n"), oc.Tool, oc.Spec, oc.Err)
			if !a.json {
				fmt.Fprintln(a.errOut, tr("下一步：pk mirror probe（看哪个源通）· pk doctor（看环境）· pk install <工具> --insecure（确认产物可信后）", "next: pk mirror probe (which source works) · pk doctor (environment) · pk install <tool> --insecure (only after verifying the artifact)"))
			}
			failed = true
			if c := exitCodeFor(oc.Err); c != jsonout.ExitFail && code == 0 {
				code = c
			}
			continue
		}
		if !a.json && !*quiet {
			a.printInstallResult(oc.Res)
		}
	}
	if err := inst.Finalize(); err != nil {
		return a.fail(err)
	}
	if code != 0 {
		return code
	}
	if err := a.ensureSelfShims(); err != nil {
		return a.fail(err)
	}
	// 装完第一件要告诉用户的事：bin 目录在不在 PATH（否则 node/npm 这些命令还用不了）
	if !a.json && !*quiet && !inPath(a.paths.Bin()) {
		fmt.Fprintf(a.out, tr("\n提示：%s 不在 PATH，node/npm 等命令还不能直接用。\n", "\nNote: %s is not on PATH, so node/npm are not usable yet.\n"), a.paths.Bin())
		fmt.Fprintln(a.out, tr("      运行一次 `pk env --write` 即可（写进 rc 的托管块，块外内容不动），或临时：", "      run `pk env --write` once (managed block in your rc; nothing outside it changes), or for this shell:"))
		fmt.Fprintf(a.out, "      eval \"$(pk env --shell %s)\"\n", env.DetectShell())
	}
	if failed {
		return jsonout.ExitFail
	}
	// 项目里安装成功就刷新 pk.lock。--frozen 不动锁；--dry-run 什么都不落盘
	// （它只打印计划，写锁既无意义也可能把一个tr("还没装", "not installed yet")的项目卡成错误）。
	if *dryRun || *frozen || len(project) == 0 {
		return jsonout.ExitOK
	}
	// 只有 pk.toml 存在时才写：engines 来自 package.json 的项目不该凭空多出一个 pk 文件。
	if !install.HasProjectFile(a.cwd()) {
		if !a.json {
			fmt.Fprintln(a.out, tr("没写 ", "not written: ")+install.LockFileName+tr("（这个项目没有 pk.toml；想固化版本就加一份）", "(this project has no pk.toml; add one to pin versions)"))
		}
		return jsonout.ExitOK
	}
	lock, err := a.writeProjectLock(project, inst)
	if err != nil {
		return a.fail(err)
	}
	if !a.json {
		fmt.Fprintf(a.out, tr("已更新 %s（%s）\n", "updated %s (%s)\n"), install.LockFileName, lock.LockTools())
	}
	return jsonout.ExitOK
}

// cmdUninstall 台账驱动卸载。
func (a *app) cmdUninstall(args []string) int {
	fs := a.newFlagSet("uninstall")
	force := fs.Bool("force", false, tr("仍被引用也删除", "delete even if referenced"))
	all := fs.Bool("all", false, tr("清掉 pk 装过的一切（所有运行时 + shim + PATH/rc 托管块）", "remove everything pk installed (all runtimes + shims + managed PATH/rc blocks)"))
	purge := fs.Bool("purge", false, tr("配合 --all：连 PK_ROOT 一起删（缓存、台账、配置）", "with --all: also delete PK_ROOT (cache, ledger, config)"))
	keepCache := fs.Bool("keep-cache", false, tr("配合 --purge：保留 cache/（离线可重装）", "with --purge: keep cache/ (offline reinstall possible)"))
	self := fs.Bool("self", false, tr("配合 --all：连 pk 二进制自己一起删（Windows 会给出命令）", "with --all: remove the pk binary too (Windows prints the command)"))
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if !*all && fs.NArg() != 1 {
		return a.failUsage(errors.New(tr("用法: pk uninstall <工具>[@<版本>] | pk uninstall --all [--purge] [--yes]", "usage: pk uninstall <tool>[@<version>] | pk uninstall --all [--purge] [--yes]")))
	}
	if *all && fs.NArg() != 0 {
		return a.failUsage(errors.New(tr("--all 不接受工具参数；只想删一个用 pk uninstall <工具>@<版本>", "--all takes no tool argument; to remove one version use pk uninstall <tool>@<version>")))
	}
	tool, version := "", ""
	if !*all {
		arg := fs.Arg(0)
		if idx := strings.LastIndex(arg, "@"); idx > 0 && idx == len(arg)-1 {
			return a.failUsage(errT(tr("版本为空: %q（写 pk uninstall %s 可删该工具的所有版本）", "empty version: %q (use pk uninstall %s to remove every version of that tool)"), arg, arg[:idx]))
		}
		if strings.Contains(arg, "@") {
			var err error
			tool, version, err = splitToolVersion(arg)
			if err != nil {
				return a.failUsage(err)
			}
		} else {
			tool = arg // 不带版本：删这个工具的全部版本
		}
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		return a.fail(err)
	}
	defer unlock()

	// --all 一次性输出汇总（或 NDJSON 汇总），逐条 uninstall 事件会把 JSON 文档冲乱
	events := a.eventWriter()
	if *all {
		events = func(jsonout.Event) {}
	}
	inst, err := install.New(install.Options{
		Paths:   a.paths,
		Client:  a.client,
		Cfg:     a.cfg,
		Profile: a.cfg.Mirror,
		Offline: a.offline,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		Events:  events,
	})
	if err != nil {
		return a.failWith(err)
	}
	if *all {
		return a.uninstallAll(inst, *purge, *keepCache, *self)
	}
	if *keepCache || *purge || *self {
		return a.failUsage(errors.New(tr("--purge/--keep-cache/--self 只能配合 --all 用", "--purge/--keep-cache/--self only work with --all")))
	}
	if version == "" {
		return a.uninstallTool(inst, tool, *force)
	}
	// 与 pk use / pk install 一致：node@22 这类说明符先在已装版本里解析，否则会误报tr("不在台账里", "not in the ledger")
	resolved := inst.ResolveInstalled(tool, version)
	if err := inst.Uninstall(tool, resolved, *force); err != nil {
		return a.fail(err)
	}
	if err := inst.Finalize(); err != nil {
		return a.fail(err)
	}
	if !a.json {
		fmt.Fprintf(a.out, tr("已卸载 %s@%s（缓存保留，离线可重装）\n", "uninstalled %s@%s (cache kept; offline reinstall possible)\n"), tool, resolved)
	}
	return jsonout.ExitOK
}

type installTarget struct{ tool, spec string }

// installTargets 决定装什么：显式参数 > pk.toml。
func installTargets(args []string, project map[string]string) ([]installTarget, error) {
	if len(args) > 0 {
		out := make([]installTarget, 0, len(args))
		for _, arg := range args {
			tool, specStr := arg, ""
			if idx := strings.LastIndex(arg, "@"); idx > 0 {
				tool, specStr = arg[:idx], arg[idx+1:]
			}
			if tool == "" {
				return nil, errT(tr("安装目标 %q 非法（应为 工具 或 工具@版本）", "invalid install target %q (expected tool or tool@version)"), arg)
			}
			out = append(out, installTarget{tool: tool, spec: specStr})
		}
		return out, nil
	}
	if len(project) == 0 {
		return nil, errors.New(tr("没有 pk.toml / package.json 的 engines.node，也没指定工具；用 `pk install node@22` 或先写 pk.toml", "no pk.toml, no engines.node in package.json, and no tool given; run `pk install node@22` or write a pk.toml"))
	}
	keys := make([]string, 0, len(project))
	for k := range project {
		if k == "npm" {
			continue // npm 是 node 的伴生包，不单独装
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]installTarget, 0, len(keys))
	for _, k := range keys {
		out = append(out, installTarget{tool: k, spec: project[k]})
	}
	if len(out) == 0 {
		return nil, errors.New(tr("pk.toml 里没有可安装的工具", "pk.toml declares no installable tool"))
	}
	return out, nil
}

func splitToolVersion(arg string) (string, string, error) {
	idx := strings.LastIndex(arg, "@")
	if idx <= 0 || idx == len(arg)-1 {
		return "", "", errT(tr("需要精确版本: pk uninstall <工具>@<版本>（得到 %q）", "exact version required: pk uninstall <tool>@<version> (got %q)"), arg)
	}
	return arg[:idx], arg[idx+1:], nil
}

// inPath 判断目录是否已在 PATH 里。
func inPath(dir string) bool {
	for _, seg := range splitPath(os.Getenv("PATH")) {
		if seg == dir {
			return true
		}
	}
	return false
}

// cwd 返回当前目录（调试/测试可用 PK_CWD 覆盖）。
func (a *app) cwd() string {
	if v := os.Getenv("PK_CWD"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

// eventWriter 返回事件输出函数（默认人类可读）。
func (a *app) eventWriter() func(jsonout.Event) { return a.eventWriterFor(false) }

// eventWriterFor 返回事件输出函数：--json 时写 NDJSON，--quiet 时静默，否则写人类可读进度。
func (a *app) eventWriterFor(quiet bool) func(jsonout.Event) {
	if quiet && !a.json {
		return func(jsonout.Event) {}
	}
	if a.json {
		return func(ev jsonout.Event) { _ = jsonout.WriteEvent(a.out, ev) }
	}
	return func(ev jsonout.Event) {
		switch ev.Ev {
		case "resolve":
			fmt.Fprintf(a.out, tr("解析 %s@%s\n", "resolved %s@%s\n"), ev.Tool, ev.Version)
		case "download":
			fmt.Fprintf(a.out, tr("下载 %s@%s：%.1f MB\n", "downloading %s@%s: %.1f MB\n"), ev.Tool, ev.Version, float64(ev.Bytes)/(1<<20))
		case "cache":
			fmt.Fprintf(a.out, tr("缓存命中 %s@%s（%.1f MB，0 网络）\n", "cache hit %s@%s (%.1f MB, no network)\n"), ev.Tool, ev.Version, float64(ev.Bytes)/(1<<20))
		case "verify":
			fmt.Fprintf(a.out, tr("校验通过 %s@%s（%s）\n", "verified %s@%s (%s)\n"), ev.Tool, ev.Version, ev.Mode)
		case "companion":
			fmt.Fprintf(a.out, tr("伴生包 %s@%s（%s）\n", "companion %s@%s (%s)\n"), ev.Tool, ev.Version, ev.Msg)
		case "mirror":
			fmt.Fprintf(a.out, tr("镜像判定 %s: %s\n", "mirror decided %s: %s\n"), ev.Tool, ev.Msg)
		case "plan":
			fmt.Fprintf(a.out, tr("  产物 %s\n  校验 %s\n", "  artifact %s\n  checksum %s\n"), ev.Source, ev.Mode)
		}
	}
}

func (a *app) printInstallResult(res install.Result) {
	if res.Planned {
		fmt.Fprintf(a.out, tr("计划：%s@%s（%s）\n", "plan: %s@%s (%s)\n"), res.Tool, res.Version, res.PlanFormat)
		return
	}
	if res.AlreadyInstalled {
		fmt.Fprintf(a.out, tr("已是最新：%s@%s\n", "already up to date: %s@%s\n"), res.Tool, res.Version)
		return
	}
	line := fmt.Sprintf(tr("装好 %s@%s → %s", "installed %s@%s -> %s"), res.Tool, res.Version, res.Path)
	if res.BecameDefault {
		line += tr("  （已设为全局默认）", "  (now the global default)")
	}
	if res.NPMVersion != "" {
		line += fmt.Sprintf(" (npm %s", res.NPMVersion)
		if res.NPMReplaced != "" && res.NPMReplaced != res.NPMVersion {
			line += fmt.Sprintf(tr(" ← 自带 %s", " <- bundled %s"), res.NPMReplaced)
		}
		line += ")"
	}
	if res.Verified == verify.VerifiedUnverified {
		line += tr(" [未校验]", " [unverified]")
	} else if res.Verified == verify.VerifiedWeak {
		line += tr(" [校验: weak]", " [verified: weak]")
	}
	fmt.Fprintln(a.out, line)
}

// ensureSelfShims 保证 bin/ 里有 pk 与 packnest 两个入口（同一文件）。
func (a *app) ensureSelfShims() error {
	self, err := os.Executable()
	if err != nil {
		return nil // 拿不到自身路径不算致命
	}
	if resolved, err := os.Readlink(self); err == nil {
		self = resolved
	}
	return shim.Self(a.paths.Bin(), self, runtime.GOOS)
}

// failWith 打印错误并按类型返回退出码。
func (a *app) failWith(err error) int {
	fmt.Fprintf(a.errOut, "pk: %v\n", err)
	return exitCodeFor(err)
}

var _ io.Writer
