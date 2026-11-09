package install

import (
	"fmt"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
)

// BuildProjectLock 用台账 + 全局默认生成本项目的 pk.lock 内容。
//
// 规则（方案 §8.3）：node 记录会展开成 node + npm 两条，npm 带 companion_of；
// 每个工具取"当前生效版本"，若没有生效版本则取满足 pk.toml 说明符的最新已装版本。
func BuildProjectLock(project map[string]string, ledger *store.Ledger, active map[string]string) (*Lock, error) {
	lock := &Lock{}
	names := make([]string, 0, len(project))
	for name := range project {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if name == "npm" {
			continue // 由 node 的伴生展开
		}
		in := pickInstallForTool(name, project[name], ledger, active)
		if in == nil {
			return nil, fmt.Errorf("%s 还没装，无法生成锁（先 `pk install %s`）", name, name)
		}
		lock.Tool = append(lock.Tool, LockTool{
			Name: in.Tool, Version: in.Version, SHA256: in.SHA256, Source: in.Source,
		})
		if in.NpmVersion != "" {
			lock.Tool = append(lock.Tool, LockTool{
				Name: "npm", Version: in.NpmVersion, SHA256: "", Source: in.Source,
				CompanionOf: "node@" + in.Version,
			})
		}
	}
	return lock, nil
}

// pickInstallForTool 选"该工具的当前生效版本"，没有就选满足说明符的最新已装版本。
func pickInstallForTool(tool, want string, ledger *store.Ledger, active map[string]string) *store.Install {
	if v := active[tool]; v != "" {
		if in := ledger.Find(tool, v); in != nil {
			return in
		}
	}
	var candidates []string
	for _, in := range ledger.Installs {
		if in.Tool == tool {
			candidates = append(candidates, in.Version)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if sp, err := spec.Parse(want); err == nil && !sp.IsAlias() {
		if best, ok := spec.Best(candidates, sp); ok {
			return ledger.Find(tool, best)
		}
	}
	for _, v := range spec.SortDesc(candidates) {
		if in := ledger.Find(tool, v); in != nil {
			return in
		}
	}
	return nil
}

// LockTools 返回锁里声明的工具名（用于日志/诊断）。
func (l *Lock) LockTools() string {
	if l == nil {
		return ""
	}
	names := make([]string, 0, len(l.Tool))
	for _, t := range l.Tool {
		names = append(names, t.Name+"@"+t.Version)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
