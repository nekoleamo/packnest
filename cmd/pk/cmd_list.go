package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/toolchain"
)

// cmdList 列出已装工具（--available 时附带内嵌 provider 目录）。
func (a *app) cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "list")
	available := fs.Bool("available", false, "列出各工具的最新可用版本（需索引，可离线读缓存）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	ledger, recovered, err := store.LoadLedgerWithRecovery(a.paths.Ledger())
	if err != nil {
		return a.fail(err)
	}
	if recovered && !a.json {
		fmt.Fprintf(a.errOut, "pk: 台账损坏，已从 %s.bak 恢复（`pk doctor` 查看详情）\n", a.paths.Ledger())
	}

	entries := make([]jsonout.ToolEntry, 0, len(ledger.Installs))
	active := a.activeVersions(ledger)
	for _, in := range ledger.Installs {
		p, _ := provider.Get(in.Tool)
		entry := jsonout.ToolEntry{
			Name:     in.Tool,
			Version:  in.Version,
			Active:   active[in.Tool] == in.Version,
			Path:     in.Path,
			Requires: requiresOf(p),
			Verified: in.Verified,
		}
		if in.Tool == "node" {
			entry.NPM = &jsonout.ToolNPM{Version: in.NpmVersion, Replaced: in.NpmBundled}
		}
		if in.Path != "" {
			entry.SizeBytes = store.DirSize(in.Path)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})

	if a.json {
		if *available {
			rows := a.availableRows()
			return a.emit(struct {
				Schema    int                 `json:"schema"`
				Mirror    string              `json:"mirror"`
				Tools     []jsonout.ToolEntry `json:"tools"`
				Available []availableEntry    `json:"available"`
			}{jsonout.SchemaVersion, a.cfg.Mirror, entries, rows})
		}
		return a.emit(jsonout.NewList(entries))
	}

	if len(entries) == 0 {
		fmt.Fprintln(a.out, "还没装任何工具。`pk install node@22` 开始。")
	} else {
		fmt.Fprintln(a.out, "已装:")
		for _, e := range entries {
			mark := " "
			if e.Active {
				mark = "*"
			}
			line := fmt.Sprintf("  %s %-6s %-10s %8s", mark, e.Name, e.Version, humanBytes(e.SizeBytes))
			if e.NPM != nil && e.NPM.Version != "" {
				line += fmt.Sprintf(" (npm %s", e.NPM.Version)
				if e.NPM.Replaced != "" && e.NPM.Replaced != e.NPM.Version {
					line += fmt.Sprintf(" ← 自带 %s", e.NPM.Replaced)
				}
				line += ")"
			}
			if e.Verified == "unverified" {
				line += " [未校验]"
			}
			fmt.Fprintln(a.out, line)
		}
	}

	if *available {
		return a.printAvailable()
	}
	return jsonout.ExitOK
}

// availableEntry 是 `list --available` 的一项。
type availableEntry struct {
	Name     string   `json:"name"`
	Latest   string   `json:"latest,omitempty"`
	LTS      string   `json:"lts,omitempty"`
	Recent   []string `json:"recent,omitempty"`
	Requires []string `json:"requires"`
	Stale    bool     `json:"stale,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// printAvailable 打印各工具的最新可装版本（索引走缓存；离线时读 24h 内缓存）。
func (a *app) printAvailable() int {
	rows := a.availableRows()
	if a.json {
		return a.emit(struct {
			Schema int              `json:"schema"`
			Mirror string           `json:"mirror"`
			Tools  []availableEntry `json:"tools"`
		}{jsonout.SchemaVersion, a.cfg.Mirror, rows})
	}
	fmt.Fprintf(a.out, "可装版本（档位 %s；缓存 24h，离线读缓存）:\n", a.cfg.Mirror)
	for _, row := range rows {
		if row.Err != "" {
			fmt.Fprintf(a.out, "  %-7s 取不到索引: %s\n", row.Name, firstLine(row.Err))
			continue
		}
		extra := ""
		if row.LTS != "" {
			extra = " (lts " + row.LTS + ")"
		}
		if row.Stale {
			extra += " [索引来自过期缓存]"
		}
		fmt.Fprintf(a.out, "  %-7s %-10s%s  近几个: %s\n",
			row.Name, row.Latest, extra, strings.Join(row.Recent, ", "))
	}
	return jsonout.ExitOK
}

// availableRows 拉取各工具的可用版本（失败只记录，不影响其他工具）。
func (a *app) availableRows() []availableEntry {
	all, err := provider.All()
	if err != nil {
		return []availableEntry{{Name: "-", Err: err.Error()}}
	}
	loader := &provider.IndexLoader{
		Client:   a.client,
		CacheDir: a.paths.CacheMeta(),
		TTL:      provider.DefaultIndexTTL,
		Custom:   toolchain.LoadPBSIndex,
	}
	rows := make([]availableEntry, 0, len(all))
	for _, p := range all {
		row := availableEntry{Name: p.ID, Requires: requiresOf(p)}
		idx, err := loader.Load(a.context(), p, a.cfg.Mirror)
		if err != nil {
			row.Err = err.Error()
			rows = append(rows, row)
			continue
		}
		row.Stale = idx.Stale
		if latest, ok := idx.Latest(); ok {
			row.Latest = latest.Version
		}
		if lts, ok := idx.Resolve(mustAlias("lts")); ok == nil {
			row.LTS = lts.Version
		}
		sorted := spec.SortDesc(idx.VersionList())
		if len(sorted) > 5 {
			sorted = sorted[:5]
		}
		row.Recent = sorted
		rows = append(rows, row)
	}
	return rows
}

// mustAlias 把 "lts" 包成 spec（别名由索引层解析）。
func mustAlias(name string) spec.Spec { return spec.MustParse(name) }

// firstLine 取多行错误的第一行（列表里不刷屏）。
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}

// activeVersions 汇总"当前生效版本"：config 的全局默认优先，台账引用兜底。
func (a *app) activeVersions(ledger *store.Ledger) map[string]string {
	active := make(map[string]string, len(ledger.Installs))
	for tool, version := range a.cfg.Tools {
		active[tool] = version
	}
	for _, in := range ledger.Installs {
		if _, ok := active[in.Tool]; ok {
			continue
		}
		for _, ref := range in.References {
			if ref.Kind == store.RefGlobal {
				active[in.Tool] = in.Version
				break
			}
		}
	}
	return active
}

func requiresOf(p *provider.Provider) []string {
	if p == nil || len(p.Requires) == 0 {
		return []string{}
	}
	return append([]string{}, p.Requires...)
}

// cmdWhich 解析 shim 到真实二进制路径（调试 shim / 双名一致性的唯一手段）。
func (a *app) cmdWhich(args []string) int {
	fs := flag.NewFlagSet("which", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "which")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 {
		return a.failUsage(fmt.Errorf("用法: pk which <命令>"))
	}
	name := fs.Arg(0)
	path, isShim, err := shim.Resolve(a.paths.Bin(), name, goos())
	if err != nil {
		return a.fail(err)
	}
	if path == "" {
		return a.fail(fmt.Errorf("命令 %q 不在 %s（`pk list` 看已装工具，`pk env` 检查 PATH）", name, a.paths.Bin()))
	}
	if a.json {
		return a.emit(struct {
			Schema int    `json:"schema"`
			Name   string `json:"name"`
			Path   string `json:"path"`
			Shim   bool   `json:"shim"`
		}{jsonout.SchemaVersion, name, path, isShim})
	}
	fmt.Fprintln(a.out, path)
	return jsonout.ExitOK
}
