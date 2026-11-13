// upgrade：把已装工具升到索引最新（M7 / 1.4）。命令面第 10 个命令的编排层。
package install

import (
	"context"
	"sort"

	"github.com/packnest/packnest/internal/spec"
)

// UpgradeItem 是一个工具的升级结果。
type UpgradeItem struct {
	Tool    string `json:"tool"`
	From    string `json:"from"`
	To      string `json:"to"`
	Changed bool   `json:"changed"`
	// Skipped 说明为什么没升（已是最新 / 不参与升级 / 索引里没有稳定版）。
	Skipped string `json:"skipped,omitempty"`
	// Err 是这一个工具失败的原因（其他工具继续）。
	Err string `json:"error,omitempty"`
}

// Upgrade 把 tools 里的工具升到索引最新版；tools 为空时用台账里所有已装工具。
//
// 语义（刻意与 `install` 不同）：
//   - **不动旧版本**：升级 = 装新版本 + 把默认切过去，旧版本留着，想清就 `pk gc --keep N`；
//   - **npm 不参与**：npm 是 node 的伴生包，按 node 精确版本配对（方案 §18.9），
//     跟着 node 一起换；单独升 npm 会配错 engines，所以这里跳过并给提示；
//   - **dry-run 只算不装**：返回每个工具将要从哪升到哪。
func (i *Installer) Upgrade(ctx context.Context, tools []string, dryRun bool) ([]UpgradeItem, error) {
	targets, skipped := i.upgradeTargets(tools)
	// 先把待升工具的索引一起拉回来（网络并行），后面逐个升时才不会排队等索引。
	i.PrefetchIndexes(ctx, targets)

	items := make([]UpgradeItem, 0, len(targets)+len(skipped))
	items = append(items, skipped...)

	for _, tool := range targets {
		from := spec.SortDesc(i.installedVersions(tool))[0]
		item := UpgradeItem{Tool: tool, From: from}
		p, err := i.opt.LookupProvider(tool)
		if err != nil {
			item.Err = err.Error()
			items = append(items, item)
			continue
		}
		idx, err := i.loadIndex(ctx, p)
		if err != nil {
			item.Err = err.Error()
			items = append(items, item)
			continue
		}
		latest, ok := idx.Latest()
		if !ok {
			item.Skipped = "索引里没有稳定版本"
			items = append(items, item)
			continue
		}
		item.To = latest.Version
		if spec.SortDesc([]string{from, latest.Version})[0] != latest.Version || from == latest.Version {
			item.Skipped = "已是最新"
			items = append(items, item)
			continue
		}
		if dryRun {
			item.Changed = true
			items = append(items, item)
			continue
		}
		if _, err := i.Install(ctx, tool, latest.Version); err != nil {
			item.Err = err.Error()
			items = append(items, item)
			continue
		}
		// install 不抢已有全局默认（方案 §8.2），升级必须显式切过去，否则"升了但没生效"。
		if err := i.Activate(tool, latest.Version); err != nil {
			item.Err = err.Error()
			items = append(items, item)
			continue
		}
		item.Changed = true
		items = append(items, item)
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].Tool < items[b].Tool })
	return items, nil
}

// upgradeTargets 决定升哪些工具：显式参数优先，否则台账里所有工具（去重、排序、npm 例外）。
func (i *Installer) upgradeTargets(tools []string) (targets []string, skipped []UpgradeItem) {
	seen := map[string]bool{}
	add := func(tool string) {
		if seen[tool] {
			return
		}
		seen[tool] = true
		if tool == "npm" {
			from := ""
			if vs := spec.SortDesc(i.installedVersions("npm")); len(vs) > 0 {
				from = vs[0]
			}
			skipped = append(skipped, UpgradeItem{Tool: "npm", From: from,
				Skipped: "npm 是 node 的伴生包，跟着 node 一起换（单独升会配错 engines）"})
			return
		}
		if len(i.installedVersions(tool)) == 0 {
			skipped = append(skipped, UpgradeItem{Tool: tool, Skipped: "还没装（先 pk install " + tool + "@latest）"})
			return
		}
		targets = append(targets, tool)
	}
	if len(tools) > 0 {
		for _, t := range tools {
			add(t)
		}
		return targets, skipped
	}
	names := make([]string, 0, len(i.ledger.Installs))
	for _, in := range i.ledger.Installs {
		names = append(names, in.Tool)
	}
	sort.Strings(names)
	for _, t := range names {
		add(t)
	}
	return targets, skipped
}

// UpgradeSummary 给文案用的一行摘要。
func UpgradeSummary(items []UpgradeItem) (changed, failed int) {
	for _, it := range items {
		switch {
		case it.Err != "":
			failed++
		case it.Changed:
			changed++
		}
	}
	return changed, failed
}
