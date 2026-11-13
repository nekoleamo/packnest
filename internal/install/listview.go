// `pk list` 的数据装配：台账 → 一行行已装项 → "可升级"提示（纯装配，不含打印）。
package install

import (
	"context"
	"sort"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
)

// ActiveVersions 汇总"当前生效版本"：config 的全局默认优先，台账里的全局引用兜底。
func ActiveVersions(cfg *store.Config, ledger *store.Ledger) map[string]string {
	active := make(map[string]string, len(ledger.Installs))
	if cfg != nil {
		for tool, version := range cfg.Tools {
			active[tool] = version
		}
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

// InstalledEntries 把台账变成列表行（按工具名 + 版本排序，输出稳定）。
func InstalledEntries(ledger *store.Ledger, active map[string]string) []jsonout.ToolEntry {
	entries := make([]jsonout.ToolEntry, 0, len(ledger.Installs))
	for _, in := range ledger.Installs {
		p, _ := provider.Get(in.Tool)
		entry := jsonout.ToolEntry{
			Name:     in.Tool,
			Version:  in.Version,
			Active:   active[in.Tool] == in.Version,
			Path:     in.Path,
			Requires: provider.Requires(p),
			Verified: in.Verified,
			Source:   provider.SourceOf(p),
		}
		if in.Tool == "node" {
			entry.NPM = &jsonout.ToolNPM{Version: in.NpmVersion, Replaced: in.NpmBundled}
		}
		// 台账里记着大小就直接用；1.5 之前的台账没有这个字段，退回 walk 一次。
		entry.SizeBytes = in.SizeBytes
		if entry.SizeBytes == 0 && in.Path != "" {
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
	return entries
}

// OutdatedHints 算"已装版本落后于索引最新"：只读索引缓存（离线客户端，零网络），没缓存就不提示。
func OutdatedHints(ctx context.Context, p paths.Paths, profile string, entries []jsonout.ToolEntry) []jsonout.OutdatedEntry {
	byTool := map[string][]string{}
	for _, e := range entries {
		byTool[e.Name] = append(byTool[e.Name], e.Version)
	}
	cacheOnly := &provider.IndexLoader{
		Client: &fetch.Client{Offline: true}, CacheDir: p.CacheMeta(), TTL: provider.DefaultIndexTTL,
	}
	var out []jsonout.OutdatedEntry
	for name, versions := range byTool {
		prov, err := provider.Get(name)
		if err != nil {
			continue
		}
		idx, err := cacheOnly.Load(ctx, prov, profile)
		if err != nil {
			continue
		}
		latest, ok := idx.Latest()
		cur := spec.SortDesc(versions)[0]
		if !ok || cur == latest.Version {
			continue
		}
		if spec.SortDesc([]string{cur, latest.Version})[0] == latest.Version {
			out = append(out, jsonout.OutdatedEntry{Name: name, Installed: cur, Latest: latest.Version, Advice: "pk upgrade " + name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
