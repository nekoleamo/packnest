package main

import (
	"fmt"
	"runtime"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/store"
)

// cmdUse 切换全局默认版本（写 config.toml + 台账引用 + 重指 shim）。
func (a *app) cmdUse(args []string) int {
	fs := a.newFlagSet("use")
	project := fs.Bool("project", false, "固定到当前项目的 pk.toml + pk.lock（默认改全局默认）")
	lockOnly := fs.Bool("lock", false, "只按现有 pk.toml 重新生成 pk.lock（不切版本）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 && !*lockOnly {
		return a.failUsage(fmt.Errorf("用法: pk use <工具>@<版本> [--project] 或 pk use --lock"))
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
		// 项目级固定：核对已装 → 写 pk.toml → 重新生成 pk.lock（不改全局默认）
		version = inst.ResolveInstalled(tool, version)
		if in := inst.Ledger().Find(tool, version); in == nil {
			return a.fail(fmt.Errorf("%s@%s 还没装（先 `pk install %s@%s`）", tool, version, tool, version))
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
		fmt.Fprintf(a.out, "已固定到项目：%s = %q（写入 pk.toml，并更新 %s）\n", tool, version, install.LockFileName)
		fmt.Fprintln(a.out, "项目内生效：echo 'eval \"$(pk env --project)\"' > .envrc && direnv allow")
		return jsonout.ExitOK
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
	fmt.Fprintf(a.out, "已切到 %s@%s（全局默认写进 %s，shim 已重指）\n", tool, version, a.paths.Config())
	return jsonout.ExitOK
}

// refreshProjectLock 按当前 pk.toml 重新生成 pk.lock（不装东西、不改默认）。
func (a *app) refreshProjectLock(inst *install.Installer) int {
	project, err := install.LoadProject(a.cwd())
	if err != nil {
		return a.fail(err)
	}
	if len(project) == 0 {
		fmt.Fprintln(a.errOut, "pk: 当前目录没有 pk.toml")
		return jsonout.ExitFail
	}
	lock, err := a.writeProjectLock(project, inst)
	if err != nil {
		return a.fail(err)
	}
	if !a.json {
		fmt.Fprintf(a.out, "已更新 %s（%s）\n", install.LockFileName, lock.LockTools())
	}
	return jsonout.ExitOK
}

// cmdGC 回收零引用版本与超限缓存。
func (a *app) cmdGC(args []string) int {
	fs := a.newFlagSet("gc")
	dryRun := fs.Bool("dry-run", false, "只报告不删除")
	cacheMax := fs.String("cache-max", "", "覆盖产物缓存上限（如 20G）")
	keepN := fs.Int("keep", 0, "每个工具保留最新的 N 个版本（默认不保留，只清零引用）")
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
			return a.failUsage(fmt.Errorf("config.toml 的 cache_max 非法: %w", err))
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
	mode := "已回收"
	if *dryRun {
		mode = "将回收（dry-run）"
	}
	fmt.Fprintf(a.out, "%s %d 个零引用版本、%d 个缓存文件，释放约 %.1f MB\n",
		mode, len(res.Removed), len(res.CacheRemoved), float64(res.FreedBytes)/(1<<20))
	for _, name := range res.Removed {
		fmt.Fprintf(a.out, "  版本 %s\n", name)
	}
	for _, path := range res.CacheRemoved {
		fmt.Fprintf(a.out, "  缓存 %s\n", path)
	}
	if len(res.KeptRecent) > 0 {
		fmt.Fprintf(a.out, "保留（--keep %d 的新版本）：%d 个\n", *keepN, len(res.KeptRecent))
	}
	if len(res.KeptRef) > 0 {
		fmt.Fprintf(a.out, "保留（仍被引用）：%d 项\n", len(res.KeptRef))
		for _, name := range res.KeptRef {
			fmt.Fprintf(a.out, "  %s\n", name)
		}
	}
	return jsonout.ExitOK
}

// ---------------------------------------------------------------------------
// `pk upgrade`（原 cmd_upgrade.go）：与 `pk use` 同属"切版本"这一组命令，合在一个文件里。
// ---------------------------------------------------------------------------

// cmdUpgrade 把已装工具升到索引最新（旧版本保留，想清理用 `pk gc --keep N`）。
func (a *app) cmdUpgrade(args []string) int {
	fs := a.newFlagSet("upgrade")
	dryRun := fs.Bool("dry-run", false, "只列出将从哪个版本升到哪个，不下载")
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
		fmt.Fprintln(a.out, "还没装任何工具：先 `pk install node@22`。")
		return jsonout.ExitOK
	}
	for _, it := range items {
		switch {
		case it.Err != "":
			fmt.Fprintf(a.out, "  ✘ %-6s %s 升级失败：%s\n", it.Tool, it.From, it.Err)
		case it.Skipped != "":
			fmt.Fprintf(a.out, "  ·  %-6s %s  %s\n", it.Tool, it.From, it.Skipped)
		case *dryRun:
			fmt.Fprintf(a.out, "  → %-6s %s → %s（dry-run，未下载）\n", it.Tool, it.From, it.To)
		default:
			fmt.Fprintf(a.out, "  ✔ %-6s %s → %s\n", it.Tool, it.From, it.To)
		}
	}
	if *dryRun {
		fmt.Fprintln(a.out, "dry-run：没有下载任何东西；去掉 --dry-run 才真升。")
		return jsonout.ExitOK
	}
	if changed > 0 {
		fmt.Fprintf(a.out, "已升级 %d 个工具（旧版本保留，`pk gc --keep 2` 可清理）\n", changed)
	}
	if failed > 0 {
		fmt.Fprintf(a.errOut, "pk: %d 个工具升级失败（见上），其余已完成\n", failed)
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}
