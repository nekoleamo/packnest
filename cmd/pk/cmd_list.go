package main

import (
	"flag"
	"fmt"
	"sort"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
)

// cmdList 列出已装工具（--available 时附带内嵌 provider 目录）。
func (a *app) cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "list")
	available := fs.Bool("available", false, "同时列出内嵌 provider 目录")
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
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})

	if a.json {
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
			line := fmt.Sprintf("  %s %-6s %s", mark, e.Name, e.Version)
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
		all, err := provider.All()
		if err != nil {
			return a.fail(err)
		}
		fmt.Fprintln(a.out, "\n可装工具（内嵌 provider，版本清单需联网/缓存）:")
		for _, p := range all {
			fmt.Fprintf(a.out, "  %-7s %-16s requires=%v\n", p.ID, p.Display, p.Requires)
		}
	}
	return jsonout.ExitOK
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
