// 项目侧的输入：pk.toml 的 [tools] 与 package.json 的 engines.node。
//
// 取舍（方案 §9 项目锁）：pk.toml 是唯一权威；package.json 只当"前端项目已有的写法"，
// 在 pk.toml 没声明 node 时补一条 node 说明符 —— 进一个现有前端项目就能 `pk install`，
// 不用先手写 pk.toml。它永远是只读输入，pk 不写 package.json。
package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/packnest/packnest/internal/spec"
)

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
