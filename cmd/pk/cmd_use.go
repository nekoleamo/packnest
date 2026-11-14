package main

import (
	"errors"
	"fmt"
	"github.com/packnest/packnest/internal/coded"
	"runtime"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/store"
)

// cmdUse 切换全局默认版本（写 config.toml + 台账引用 + 重指 shim）。
func (a *app) cmdUse(args []string) int {
	fs := a.newFlagSet("use")
	project := fs.Bool("project", false, tr("固定到当前项目的 pk.toml + pk.lock（默认改全局默认）", "pin into the project's pk.toml + pk.lock (default: change the global default)"))
	lockOnly := fs.Bool("lock", false, tr("只按现有 pk.toml 重新生成 pk.lock（不切版本）", "regenerate pk.lock from the current pk.toml only (no version switch)"))
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 && !*lockOnly {
		return a.failUsage(errors.New(tr("用法: pk use <工具>@<版本> [--project] 或 pk use --lock", "usage: pk use <tool>@<version> [--project] or pk use --lock")))
	}
	tool, version := "", ""
	var err error
	if !*lockOnly {
		tool, version, err = splitToolVersion(fs.Arg(0))
		if err != nil {
			return a.failUsage(err)
		}
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths: a.paths, Client: a.client, Cfg: a.cfg, Profile: a.cfg.Mirror,
		Offline: a.offline, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Events: a.eventWriter(),
	})
	if err != nil {
		return a.failWith(err)
	}
	if *lockOnly {
		return a.refreshProjectLock(inst)
	}
	if *project {
		// 项目级固定：核对已装（没装就问一句）→ 写 pk.toml → 重新生成 pk.lock（不改全局默认）
		version = inst.ResolveInstalled(tool, version)
		if v, code := a.ensureInstalled(inst, tool, version); code != 0 {
			return code
		} else {
			version = v
		}
		if err := install.SaveProjectTool(a.cwd(), tool, version); err != nil {
			return a.fail(err)
		}
		if code := a.refreshProjectLock(inst); code != 0 {
			return code
		}
		if a.json {
			return a.emit(struct {
				Schema  int    `json:"schema"`
				Tool    string `json:"tool"`
				Version string `json:"version"`
				Scope   string `json:"scope"`
				Lock    string `json:"lock"`
			}{jsonout.SchemaVersion, tool, version, "project", install.LockFileName})
		}
		fmt.Fprintf(a.out, tr("已固定到项目：%s = %q（写入 pk.toml，并更新 %s）\n", "pinned for this project: %s = %q (written to pk.toml, %s updated)\n"), tool, version, install.LockFileName)
		fmt.Fprintln(a.out, tr("项目内生效：echo 'eval \"$(pk env --project)\"' > .envrc && direnv allow", "activate inside the project: echo 'eval \"$(pk env --project)\"' > .envrc && direnv allow"))
		return jsonout.ExitOK
	}
	// 全局切换同样：没装先问一句（不隐式下载），确认后再装
	version = inst.ResolveInstalled(tool, version)
	if v, code := a.ensureInstalled(inst, tool, version); code != 0 {
		return code
	} else {
		version = v
	}
	if err := inst.Activate(tool, version); err != nil {
		return a.fail(err)
	}
	if err := inst.Finalize(); err != nil {
		return a.fail(err)
	}
	if err := a.ensureSelfShims(); err != nil {
		return a.fail(err)
	}

	if a.json {
		return a.emit(struct {
			Schema  int    `json:"schema"`
			Tool    string `json:"tool"`
			Version string `json:"version"`
			Config  string `json:"config"`
		}{jsonout.SchemaVersion, tool, version, a.paths.Config()})
	}
	fmt.Fprintf(a.out, tr("已切到 %s@%s（全局默认写进 %s，shim 已重指）\n", "switched to %s@%s (global default written to %s, shims repointed)\n"), tool, version, a.paths.Config())
	return jsonout.ExitOK
}

// refreshProjectLock 按当前 pk.toml 重新生成 pk.lock（不装东西、不改默认）。
func (a *app) refreshProjectLock(inst *install.Installer) int {
	project, err := install.LoadProject(a.cwd())
	if err != nil {
		return a.fail(err)
	}
	if len(project) == 0 {
		return a.failMsg(tr("pk: 当前目录没有 pk.toml", "pk: no pk.toml in the current directory"), jsonout.ExitFail)
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

// cmdGC 回收零引用版本与超限缓存。
func (a *app) cmdGC(args []string) int {
	fs := a.newFlagSet("gc")
	dryRun := fs.Bool("dry-run", false, tr("只报告不删除", "report only, delete nothing"))
	cacheMax := fs.String("cache-max", "", tr("覆盖产物缓存上限（如 20G）", "override the artifact cache quota (e.g. 20G)"))
	keepN := fs.Int("keep", 0, tr("每个工具保留最新的 N 个版本（默认不保留，只清零引用）", "keep the latest N versions per tool (default: keep none, only drop references)"))
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	limit := int64(0)
	if *cacheMax != "" {
		v, err := store.ParseSize(*cacheMax)
		if err != nil {
			return a.failUsage(err)
		}
		limit = v
	} else {
		v, err := store.ParseSize(a.cfg.CacheMax)
		if err != nil {
			return a.failUsage(errT(tr("config.toml 的 cache_max 非法: %w", "invalid cache_max in config.toml: %w"), err))
		}
		limit = v
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths: a.paths, Client: a.client, Cfg: a.cfg, Profile: a.cfg.Mirror,
		Offline: a.offline, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, KeepVersions: *keepN,
	})
	if err != nil {
		return a.failWith(err)
	}
	res, err := inst.GC(a.context(), limit, *dryRun, false)
	if err != nil {
		return a.fail(err)
	}
	if !*dryRun {
		if err := inst.Finalize(); err != nil {
			return a.fail(err)
		}
	}

	if a.json {
		return a.emit(struct {
			Schema int `json:"schema"`
			install.GCResult
			CacheMax int64 `json:"cache_max_bytes"`
			Keep     int   `json:"keep,omitempty"`
		}{Schema: jsonout.SchemaVersion, GCResult: res, CacheMax: limit, Keep: *keepN})
	}
	mode := tr("已回收", "reclaimed")
	if *dryRun {
		mode = tr("将回收（dry-run）", "would reclaim (dry-run)")
	}
	fmt.Fprintf(a.out, tr("%s %d 个零引用版本、%d 个缓存文件，释放约 %.1f MB\n", "%s %d zero-reference version(s), %d cache file(s), about %.1f MB freed\n"),
		mode, len(res.Removed), len(res.CacheRemoved), float64(res.FreedBytes)/(1<<20))
	for _, name := range res.Removed {
		fmt.Fprintf(a.out, tr("  版本 %s\n", "  version %s\n"), name)
	}
	for _, path := range res.CacheRemoved {
		fmt.Fprintf(a.out, tr("  缓存 %s\n", "  cache %s\n"), path)
	}
	if len(res.KeptRecent) > 0 {
		fmt.Fprintf(a.out, tr("保留（--keep %d 的新版本）：%d 个\n", "kept (newest %d): %d\n"), *keepN, len(res.KeptRecent))
	}
	if len(res.KeptRef) > 0 {
		fmt.Fprintf(a.out, tr("保留（仍被引用）：%d 项\n", "kept (still referenced): %d\n"), len(res.KeptRef))
		for _, name := range res.KeptRef {
			fmt.Fprintf(a.out, "  %s\n", name)
		}
	}
	return jsonout.ExitOK
}

// ---------------------------------------------------------------------------
// `pk upgrade`（原 cmd_upgrade.go）：与 `pk use` 同属tr("切版本", "switch versions")这一组命令，合在一个文件里。
// ---------------------------------------------------------------------------

// cmdUpgrade 把已装工具升到索引最新（旧版本保留，想清理用 `pk gc --keep N`）。
func (a *app) cmdUpgrade(args []string) int {
	fs := a.newFlagSet("upgrade")
	dryRun := fs.Bool("dry-run", false, tr("只列出将从哪个版本升到哪个，不下载", "only report from -> to, download nothing"))
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths: a.paths, Client: a.client, Cfg: a.cfg, Profile: a.cfg.Mirror,
		Offline: a.offline, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Events: a.eventWriterFor(true),
	})
	if err != nil {
		return a.failWith(err)
	}
	items, err := inst.Upgrade(a.context(), fs.Args(), *dryRun)
	if err != nil {
		return a.fail(err)
	}
	if !*dryRun {
		if err := inst.Finalize(); err != nil {
			return a.fail(err)
		}
	}

	changed, failed := install.UpgradeSummary(items)
	if a.json {
		code := a.emit(struct {
			Schema  int                   `json:"schema"`
			DryRun  bool                  `json:"dry_run,omitempty"`
			Changed int                   `json:"changed"`
			Failed  int                   `json:"failed"`
			Items   []install.UpgradeItem `json:"items"`
		}{jsonout.SchemaVersion, *dryRun, changed, failed, items})
		if code != jsonout.ExitOK {
			return code
		}
		if failed > 0 {
			return jsonout.ExitFail
		}
		return jsonout.ExitOK
	}

	if len(items) == 0 {
		fmt.Fprintln(a.out, tr("还没装任何工具：先 `pk install node@22`。", "No tools installed yet: run `pk install node@22` first."))
		return jsonout.ExitOK
	}
	for _, it := range items {
		switch {
		case it.Err != "":
			fmt.Fprintf(a.out, tr("  ✘ %-6s %s 升级失败：%s\n", "  x  %-6s %s upgrade failed: %s\n"), it.Tool, it.From, it.Err)
		case it.Skipped != "":
			fmt.Fprintf(a.out, "  ·  %-6s %s  %s\n", it.Tool, it.From, it.Skipped)
		case *dryRun:
			fmt.Fprintf(a.out, tr("  → %-6s %s → %s（dry-run，未下载）\n", "  -> %-6s %s -> %s (dry-run, nothing downloaded)\n"), it.Tool, it.From, it.To)
		default:
			fmt.Fprintf(a.out, "  ✔ %-6s %s → %s\n", it.Tool, it.From, it.To)
		}
	}
	if *dryRun {
		fmt.Fprintln(a.out, tr("dry-run：没有下载任何东西；去掉 --dry-run 才真升。", "dry-run: nothing was downloaded; drop --dry-run to really upgrade."))
		return jsonout.ExitOK
	}
	if changed > 0 {
		fmt.Fprintf(a.out, tr("已升级 %d 个工具（旧版本保留，`pk gc --keep 2` 可清理）\n", "upgraded %d tool(s) (old versions kept; clean up with `pk gc --keep 2`)\n"), changed)
	}
	if failed > 0 {
		fmt.Fprintf(a.errOut, tr("pk: %d 个工具升级失败（见上），其余已完成\n", "pk: %d tool(s) failed to upgrade (see above), the rest completed\n"), failed)
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}

// ensureInstalled 保证 use/pin 的目标版本已经装好。
//
// 语义（2026-11-15 按用户要求定）：`pk use` / `pk use --project` **绝不隐式下载** ——
// 发现没装就先说清楚"要装什么、装多大不确定"，然后问一句要不要现在装；确认后才调 install，
// 装完再解析成台账里的真实版本（`node@22` → `22.23.5`）。
//
// 三种非交互情形沿用 a.confirm 的既有规矩：
//   - `--yes`：直接确认（脚本/CI 用这个）；
//   - `--json`：不提问，提示加 `--yes`（机器没法回答 [y/N]）；
//   - 非 TTY：同样不提问，提示加 `--yes`。
//
// 返回（解析后的版本, 退出码）；退出码 0 表示可以继续。
func (a *app) ensureInstalled(inst *install.Installer, tool, version string) (string, int) {
	if inst.Ledger().Find(tool, version) != nil {
		return version, 0
	}
	prompt := fmt.Sprintf(tr("%s@%s 还没装，现在安装吗？", "%s@%s is not installed; install it now?"), tool, version)
	ok := a.confirmAction(prompt,
		tr("pk: --json 下不提问；确认要装就加 --yes", "pk: no prompts under --json; add --yes to install it"),
		tr("pk: 非交互环境不自动安装；确认要装就加 --yes 重跑", "pk: non-interactive shells never auto-install; rerun with --yes to install it"))
	if !ok {
		a.emitError(coded.New(install.CodeUseNotConfirmed, tool, version), tool, version, jsonout.ExitFail)
		return version, jsonout.ExitFail
	}
	if !a.json {
		fmt.Fprintf(a.out, tr("先装 %s@%s，装完再继续……\n", "installing %s@%s first, then continuing…\n"), tool, version)
	}
	if _, err := inst.Install(a.context(), tool, version); err != nil {
		return version, a.failWith(err)
	}
	if err := inst.Finalize(); err != nil {
		return version, a.fail(err)
	}
	resolved := inst.ResolveInstalled(tool, version)
	if inst.Ledger().Find(tool, resolved) == nil {
		return version, a.fail(errT(tr("%s@%s 装完了但台账里没有记录（先看 `pk list %s`）", "%s@%s was installed but is missing from the ledger (check `pk list %s`)"), tool, version, tool))
	}
	return resolved, 0
}
