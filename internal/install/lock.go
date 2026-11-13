// pk.lock：项目锁（方案 §8.3）。
//
// 只写 version + sha256 + 来源名，**不写 URL** —— 换镜像不影响锁的可用性。
package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
)

// LockFileName 是项目锁的文件名。
const LockFileName = "pk.lock"

// LockTool 是锁里的一条工具记录。
type LockTool struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	SHA256  string `toml:"sha256,omitempty"`
	Source  string `toml:"source,omitempty"`
	// CompanionOf 表示这是伴生包（npm 跟着 node 走）。
	CompanionOf string `toml:"companion_of,omitempty"`
}

// Lock 是 pk.lock 的内容。
type Lock struct {
	Tool []LockTool `toml:"tool"`
}

// LoadLock 读取项目锁；文件不存在返回 nil（不是错误）。
func LoadLock(dir string) (*Lock, error) {
	data, err := os.ReadFile(filepath.Join(dir, LockFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", LockFileName, err)
	}
	var l Lock
	if err := toml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", LockFileName, err)
	}
	return &l, nil
}

// Save 原子写项目锁（按名字排序，保证 diff 稳定）。
func (l *Lock) Save(dir string) error {
	slices.SortFunc(l.Tool, func(a, b LockTool) int { return strings.Compare(a.Name, b.Name) })
	data, err := toml.Marshal(l)
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", LockFileName, err)
	}
	header := "# 由 pk 生成，请入库（不写 URL：换镜像不影响锁）\n"
	return store.WriteAtomic(filepath.Join(dir, LockFileName), append([]byte(header), data...))
}

// Find 按工具名查锁记录。
func (l *Lock) Find(name string) (LockTool, bool) {
	if l == nil {
		return LockTool{}, false
	}
	for _, t := range l.Tool {
		if t.Name == name {
			return t, true
		}
	}
	return LockTool{}, false
}

// CheckFrozen 校验"清单 ↔ 锁"一致：`--frozen` 下不一致即失败，不做解析。
func CheckFrozen(project map[string]string, lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("--frozen 需要 %s，但项目里没有（先跑一次 `pk install` 生成并入库）", LockFileName)
	}
	var problems []string
	names := make([]string, 0, len(project))
	for name := range project {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := project[name]
		entry, ok := lock.Find(name)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s 在 pk.toml 里声明了（%q），但 %s 里没有", name, want, LockFileName))
			continue
		}
		if name == "npm" && strings.EqualFold(want, "auto") {
			continue // npm = auto 跟随伴生，不做版本匹配
		}
		sp, err := spec.Parse(want)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s 的说明符 %q 非法: %v", name, want, err))
			continue
		}
		if sp.IsAlias() {
			problems = append(problems, fmt.Sprintf("%s = %q 是别名（latest/lts），--frozen 下必须显式版本或已锁定的版本", name, want))
			continue
		}
		if !sp.Match(entry.Version) {
			problems = append(problems, fmt.Sprintf("%s 锁的是 %s，不满足 pk.toml 的 %q", name, entry.Version, want))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("清单与 %s 不一致（--frozen 不做解析）：\n  %s\n建议：本地 `pk install` 更新锁并提交",
			LockFileName, strings.Join(problems, "\n  "))
	}
	return nil
}

// LockFromInstalls 用台账里"当前作用域"的记录生成锁内容。
func LockFromInstalls(installs []store.Install) *Lock {
	l := &Lock{}
	for _, in := range installs {
		l.Tool = append(l.Tool, LockTool{
			Name: in.Tool, Version: in.Version, SHA256: in.SHA256, Source: in.Source,
			CompanionOf: in.CompanionOf,
		})
	}
	return l
}

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

// SaveProjectTool 把 "工具 = 版本" 写进项目 pk.toml 的 [tools] 段。
//
// 用**文本级合并**而不是解析再序列化：这样用户手写的注释、分段、顺序都不会被冲掉。
// 只加/改一行，其余原样保留。
func SaveProjectTool(dir, tool, version string) error {
	path := filepath.Join(dir, "pk.toml")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取 %q 失败: %w", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	entry := fmt.Sprintf("%s = %q", tool, version)
	inTools := false
	replaced := false
	out := make([]string, 0, len(lines)+3)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if inTools && !replaced {
				out = append(out, entry)
				replaced = true
			}
			inTools = trimmed == "[tools]"
			out = append(out, line)
			continue
		}
		if inTools && !replaced {
			key := trimmed
			if idx := strings.Index(key, "="); idx > 0 {
				key = strings.TrimSpace(key[:idx])
			}
			if key == tool {
				out = append(out, entry)
				replaced = true
				continue
			}
		}
		out = append(out, line)
	}
	if inTools && !replaced {
		out = append(out, entry)
		replaced = true
	}
	if !replaced {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "[tools]", entry)
	}

	return paths.WriteAtomic(path, []byte(strings.Join(out, "\n")+"\n"))
}

// ---------------------------------------------------------------------------
// 项目输入（原 project.go）：pk.toml 的 [tools] 与 package.json 的 engines.node。
// 与 pk.lock 同属"项目清单"这一件事，放一个文件里读起来才连贯。
// ---------------------------------------------------------------------------

// PackageJSONFile 是 node 生态的项目清单文件名。
const PackageJSONFile = "package.json"

// ProjectFileName 是 pk 自己的项目清单名。
const ProjectFileName = "pk.toml"

// HasProjectFile 判断目录里有没有 pk.toml（决定要不要写 pk.lock：
// 只有 pk 自己的清单存在时，pk 才维护项目锁，不去别人的 repo 里新建文件）。
func HasProjectFile(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ProjectFileName))
	return err == nil && !st.IsDir()
}

// LoadProject 读取项目 pk.toml 的 [tools]（项目锁的另一半在 M4）。
func LoadProject(dir string) (map[string]string, error) {
	path := filepath.Join(dir, ProjectFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %q 失败: %w", path, err)
	}
	var doc struct {
		Tools map[string]string `toml:"tools"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 %q 失败: %w", path, err)
	}
	return doc.Tools, nil
}

// ProjectSpecs 汇总 `pk install` 不带参数时要装什么：
//
//  1. pk.toml 的 [tools] 优先（权威，和以前完全一致）；
//  2. 没声明 node 时，用 package.json 的 engines.node 补一条（不改写任何文件）。
//
// notes 是给人看的一行提示（来源说明、package.json 坏了），不是错误。
// engines.node 无法解析才报错 —— 顺手给可执行建议，不把用户留给后续的解析失败。
func ProjectSpecs(dir string) (map[string]string, []string, error) {
	project, err := LoadProject(dir)
	if err != nil {
		return nil, nil, err
	}
	engines, notes := packageJSONEngines(dir)
	if engines == "" {
		return project, notes, nil
	}
	if _, ok := project["node"]; ok {
		return project, notes, nil // pk.toml 已经说了用哪个 node
	}
	if _, err := spec.Parse(engines); err != nil {
		return nil, notes, fmt.Errorf("package.json 的 engines.node %q 无法解析: %w（改用 pk install node@<版本>，或写进 pk.toml）", engines, err)
	}
	if project == nil {
		project = map[string]string{}
	}
	project["node"] = engines
	notes = append(notes, "按 package.json 的 engines.node 装 node："+engines)
	return project, notes, nil
}

// packageJSONEngines 读 package.json 的 engines.node。
//
// 它只是"额外输入"：文件不存在返回空；JSON 坏了只给一条提示就跳过 ——
// 项目里的 package.json 常是模板/半成品，不该拦住一次显式 `pk install node@22`。
func packageJSONEngines(dir string) (string, []string) {
	path := filepath.Join(dir, PackageJSONFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", []string{fmt.Sprintf("读不到 %s（忽略 engines.node）：%v", PackageJSONFile, err)}
	}
	var doc struct {
		Engines map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", []string{fmt.Sprintf("%s 解析失败（忽略 engines.node）：%v", PackageJSONFile, err)}
	}
	return strings.TrimSpace(doc.Engines["node"]), nil
}
