// `pk uninstall --all` 的决策与执行层（打印、确认、`--self` 的平台动作留在 cmd）。
// 搬进来的理由：卸载是破坏性路径，"删哪些版本、要不要连根删"以前只能靠 CLI 集成测试断言。
package install

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/shim"
)

// UninstallAllOptions 是 `pk uninstall --all` 的开关。
// `--self`（连 pk 二进制一起删）不在这里：它要用当前进程的可执行路径，属 cmd 的事。
type UninstallAllOptions struct {
	Purge     bool // 连 PK_ROOT 一起删
	KeepCache bool // 配合 Purge：保留 cache/（离线可重装）
}

// UninstallPlan 描述"会删什么"，只看台账与配置、**不动盘**（打印、预览、单测都用它）。
type UninstallPlan struct {
	Tools     []string          // "node@22.23.5"，已排序
	Active    map[string]string // 工具 → 当前全局默认版本（打印 * 标记用）
	BinDir    string
	Root      string
	Purge     bool
	KeepCache bool
}

// UninstallResult 是 ApplyUninstallAll 的结果。
type UninstallResult struct {
	Removed  []string // 成功删掉的 "tool@version"
	Failures []string // 逐条失败原因（其余照常继续）
	Purged   bool     // PK_ROOT 是否真的删掉了
}

// PlanUninstallAll 汇总要删的版本与路径，不碰盘。
func (i *Installer) PlanUninstallAll(opts UninstallAllOptions) UninstallPlan {
	p := UninstallPlan{
		Active: map[string]string{}, BinDir: i.opt.Paths.Bin(), Root: i.opt.Paths.Root,
		Purge: opts.Purge, KeepCache: opts.KeepCache,
	}
	for _, in := range i.ledger.Installs {
		p.Tools = append(p.Tools, in.Tool+"@"+in.Version)
		if _, ok := p.Active[in.Tool]; !ok {
			p.Active[in.Tool] = i.ActiveVersion(in.Tool)
		}
	}
	sort.Strings(p.Tools)
	return p
}

// ApplyUninstallAll 按 plan 动手：删运行时（引用不再拦路）+ 自入口 shim + （可选）连根删 PK_ROOT。
// 单条失败只记进 Failures，不中断其余删除。
func (i *Installer) ApplyUninstallAll(p UninstallPlan) UninstallResult {
	res := UninstallResult{Removed: make([]string, 0, len(p.Tools))}
	for _, t := range p.Tools {
		idx := strings.LastIndex(t, "@")
		if idx <= 0 {
			continue
		}
		if err := i.Uninstall(t[:idx], t[idx+1:], true); err != nil {
			res.Failures = append(res.Failures, t+": "+err.Error())
			continue
		}
		res.Removed = append(res.Removed, t)
	}

	// 自入口（pk / packnest）在 bin 里，留着会指向一个马上被删的二进制。
	for _, name := range []string{shim.SelfName, shim.AliasName} {
		if i.opt.GOOS == "windows" {
			_ = os.Remove(shim.CmdPath(p.BinDir, name))
			_ = os.Remove(filepath.Join(p.BinDir, name+".exe"))
			continue
		}
		_ = os.Remove(filepath.Join(p.BinDir, name))
	}

	if !p.Purge {
		if err := i.Finalize(); err != nil {
			res.Failures = append(res.Failures, err.Error())
		}
		return res
	}
	if !p.KeepCache {
		if err := os.RemoveAll(p.Root); err != nil {
			res.Failures = append(res.Failures, "删除 "+p.Root+" 失败: "+err.Error())
			return res
		}
		res.Purged = true
		return res
	}
	// 连根删但保留 cache/（离线可重装）
	entries, _ := os.ReadDir(p.Root)
	for _, e := range entries {
		if e.Name() == "cache" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(p.Root, e.Name())); err != nil {
			res.Failures = append(res.Failures, "删除 "+e.Name()+" 失败: "+err.Error())
		}
	}
	res.Purged = true
	return res
}
