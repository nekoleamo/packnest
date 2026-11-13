package main

import (
	"fmt"
	"runtime"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/store"
)

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
