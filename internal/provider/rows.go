// `pk list --available` 的数据层：并发拉多工具索引 → 每个工具一行。
//
// 放在 provider 而不是 cmd/pk 的理由：这是"索引 → 一行结论"的纯装配（最新版、LTS、近几个、
// 是否来自过期缓存），没有任何打印；`cmd/pk` 只负责排版，GUI 侧的同类视图也不用再写一遍。
package provider

import (
	"context"

	"github.com/packnest/packnest/internal/spec"
)

// AvailableRow 是 `pk list --available` 的一行（也是 `--json` 的 available[] 契约）。
type AvailableRow struct {
	Name     string   `json:"name"`
	Latest   string   `json:"latest,omitempty"`
	LTS      string   `json:"lts,omitempty"`
	Recent   []string `json:"recent,omitempty"`
	Requires []string `json:"requires"`
	Stale    bool     `json:"stale,omitempty"`
	Source   string   `json:"source,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// AvailableRows 汇总各工具的最新可装版本（失败只记 Err，不影响其他工具）。
// all 为 nil 时按内嵌 + 外部清单现取；顺序与 all 一致。
func AvailableRows(ctx context.Context, loader *IndexLoader, all []*Provider, profile string) []AvailableRow {
	if all == nil {
		var err error
		if all, err = All(); err != nil {
			return []AvailableRow{{Name: "-", Err: err.Error()}}
		}
	}
	res := loader.LoadAll(ctx, all, profile)
	rows := make([]AvailableRow, 0, len(res))
	for _, r := range res {
		p, idx, err := r.Provider, r.Index, r.Err
		row := AvailableRow{Name: p.ID, Requires: Requires(p), Source: p.Source}
		if err != nil {
			row.Err = err.Error()
			rows = append(rows, row)
			continue
		}
		row.Stale = idx.Stale
		if latest, ok := idx.Latest(); ok {
			row.Latest = latest.Version
		}
		if lts, err := idx.Resolve(spec.MustParse("lts")); err == nil {
			row.LTS = lts.Version
		}
		row.Recent = spec.SortDesc(idx.VersionList())
		if len(row.Recent) > 5 {
			row.Recent = row.Recent[:5]
		}
		rows = append(rows, row)
	}
	return rows
}

// Requires / SourceOf 是 nil 安全的清单取值（取不到清单时有稳定输出：[] 与 ""）。
func Requires(p *Provider) []string {
	if p == nil || len(p.Requires) == 0 {
		return []string{}
	}
	return append([]string{}, p.Requires...)
}

func SourceOf(p *Provider) string {
	if p == nil {
		return ""
	}
	return p.Source
}
