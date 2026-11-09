package main

import (
	"errors"
	"flag"
	"fmt"
	"runtime"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/store"
)

// cmdUse 切换全局默认版本（写 config.toml + 台账引用 + 重指 shim）。
func (a *app) cmdUse(args []string) int {
	fs := flag.NewFlagSet("use", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 {
		return a.failUsage(fmt.Errorf("用法: pk use <工具>@<版本>"))
	}
	tool, version, err := splitToolVersion(fs.Arg(0))
	if err != nil {
		return a.failUsage(err)
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			fmt.Fprintln(a.errOut, "pk: 另一个 pk 正在运行（退出码 5）")
			return jsonout.ExitLocked
		}
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

// cmdGC 回收零引用版本与超限缓存。
func (a *app) cmdGC(args []string) int {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	dryRun := fs.Bool("dry-run", false, "只报告不删除")
	cacheMax := fs.String("cache-max", "", "覆盖产物缓存上限（如 20G）")
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
		if errors.Is(err, store.ErrLocked) {
			fmt.Fprintln(a.errOut, "pk: 另一个 pk 正在运行（退出码 5）")
			return jsonout.ExitLocked
		}
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths: a.paths, Client: a.client, Cfg: a.cfg, Profile: a.cfg.Mirror,
		Offline: a.offline, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
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
		}{Schema: jsonout.SchemaVersion, GCResult: res, CacheMax: limit})
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
	if len(res.KeptRef) > 0 {
		fmt.Fprintf(a.out, "保留（仍被引用）：%d 项\n", len(res.KeptRef))
		for _, name := range res.KeptRef {
			fmt.Fprintf(a.out, "  %s\n", name)
		}
	}
	return jsonout.ExitOK
}
