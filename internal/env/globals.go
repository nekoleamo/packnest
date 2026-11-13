// 全局包体检（方案 §8.5，L1/L2/L3 三级检测）。
//
// 只读：绝不改用户的全局包目录，也不自动重编译（那会引入 CGO/编译器依赖，违反精简）。
package env

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/spec"
)

// GlobalPackage 是扫到的一个全局包。
type GlobalPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	EnginesNode string `json:"engines_node,omitempty"`
	// Native 表示含原生模块（.node 文件或 gypfile/binary 字段）。
	Native bool `json:"native"`
	// NAPI 表示带 N-API 预编译产物（跨 node 版本安全）。
	NAPI bool `json:"napi"`
	// Bins 是该包暴露的命令名（L3 命令遮蔽检测用）。
	Bins []string `json:"bins,omitempty"`
}

// GlobalProblem 是一条全局包告警。
type GlobalProblem struct {
	Code    string         `json:"code"`
	Pkg     string         `json:"pkg"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// ScanGlobals 扫描全局包目录（<prefix>/lib/node_modules，Windows 为 <prefix>/node_modules）。
func ScanGlobals(prefix, goos string) ([]GlobalPackage, error) {
	// 两种布局都扫：npm 是 <prefix>/lib/node_modules（Windows 是 <prefix>/node_modules），
	// pnpm/bun 的全局目录则直接是 <prefix>/node_modules（方案 B.5）。不存在的目录自然跳过。
	dirs := []string{filepath.Join(prefix, "lib", "node_modules"), filepath.Join(prefix, "node_modules")}
	var out []GlobalPackage
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录不存在是常态（还没装过全局包）
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if strings.HasPrefix(e.Name(), "@") {
				// scope 目录：再进一层
				scoped, err := os.ReadDir(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				for _, se := range scoped {
					if se.IsDir() {
						if pkg, ok := readGlobalPackage(filepath.Join(dir, e.Name(), se.Name())); ok && !seen[pkg.Name] {
							seen[pkg.Name] = true
							out = append(out, pkg)
						}
					}
				}
				continue
			}
			if pkg, ok := readGlobalPackage(filepath.Join(dir, e.Name())); ok && !seen[pkg.Name] {
				seen[pkg.Name] = true
				out = append(out, pkg)
			}
		}
	}
	slices.SortFunc(out, func(a, b GlobalPackage) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

type pkgJSON struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Engines map[string]string `json:"engines"`
	Gypfile bool              `json:"gypfile"`
	Binary  json.RawMessage   `json:"binary"`
	Bins    json.RawMessage   `json:"bin"`
}

func readGlobalPackage(dir string) (GlobalPackage, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return GlobalPackage{}, false
	}
	var meta pkgJSON
	if err := json.Unmarshal(data, &meta); err != nil {
		return GlobalPackage{}, false
	}
	pkg := GlobalPackage{
		Name:        meta.Name,
		Version:     meta.Version,
		Path:        dir,
		EnginesNode: meta.Engines["node"],
		Bins:        binNames(meta.Bins),
	}
	if pkg.Name == "" {
		pkg.Name = filepath.Base(dir)
	}
	// L2：原生代码判定。
	pkg.Native, pkg.NAPI = detectNative(dir, meta.Gypfile, len(meta.Binary) > 0 && string(meta.Binary) != "null")
	return pkg, true
}

// detectNative 返回（是否有原生代码, 是否只有 N-API 预编译产物）。
//
// 判定规则（方案 §8.5 L2）：`prebuilds/*/…napi.node` 视为跨版本安全；
// 其它 `.node`、`gypfile`、`binary` 字段一律按"可能绑 ABI"处理。
func detectNative(dir string, gypfile, binary bool) (native, napi bool) {
	hasNode := false
	allNAPI := true
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".node") {
			return nil
		}
		hasNode = true
		if !strings.Contains(info.Name(), "napi") && !strings.Contains(path, string(filepath.Separator)+"prebuilds"+string(filepath.Separator)) {
			allNAPI = false
		}
		return nil
	})
	native = hasNode || gypfile || binary
	napi = hasNode && allNAPI
	return
}

func binNames(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return nil // 单入口且未命名，无法判定命令名
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CheckGlobals 做 L1（engines）/L2（原生 ABI）检测，返回告警。
func CheckGlobals(pkgs []GlobalPackage, nodeVersion string) []GlobalProblem {
	var problems []GlobalProblem
	for _, pkg := range pkgs {
		if pkg.EnginesNode != "" && nodeVersion != "" {
			ok, err := spec.EnginesSatisfied(pkg.EnginesNode, nodeVersion)
			if err == nil && !ok {
				problems = append(problems, GlobalProblem{
					Code:    "GLOBAL_ENGINES_UNMET",
					Pkg:     pkg.Name,
					Message: fmt.Sprintf("全局包 %s 要求 node %s，当前生效的是 %s", pkg.Name, pkg.EnginesNode, nodeVersion),
					Detail:  map[string]any{"pkg": pkg.Name, "needs": pkg.EnginesNode, "have": nodeVersion},
				})
			}
		}
		if pkg.Native && !pkg.NAPI {
			problems = append(problems, GlobalProblem{
				Code:    "GLOBAL_NATIVE_ABI",
				Pkg:     pkg.Name,
				Message: fmt.Sprintf("全局包 %s 含原生模块（与 node ABI 绑定），切到其他 node 版本会失效；可 `npm rebuild %s`（离线）", pkg.Name, pkg.Name),
				Detail:  map[string]any{"pkg": pkg.Name, "path": pkg.Path},
			})
		}
	}
	return problems
}

// CheckShadowing 做 L3：全局命令名与项目 devDependencies 的 bin 名重叠 → 交互 shell 里会遮蔽。
func CheckShadowing(pkgs []GlobalPackage, projectBins map[string]string) []GlobalProblem {
	var problems []GlobalProblem
	for _, pkg := range pkgs {
		for _, bin := range pkg.Bins {
			if owner, ok := projectBins[bin]; ok {
				problems = append(problems, GlobalProblem{
					Code:    "GLOBAL_SHADOWS_PROJECT",
					Pkg:     pkg.Name,
					Message: fmt.Sprintf("全局命令 %s（来自 %s）会遮蔽项目的 %s；`npm run` 不受影响", bin, pkg.Name, owner),
					Detail:  map[string]any{"bin": bin, "global_pkg": pkg.Name, "project_pkg": owner},
				})
			}
		}
	}
	slices.SortFunc(problems, func(a, b GlobalProblem) int {
		return strings.Compare(a.Detail["bin"].(string), b.Detail["bin"].(string))
	})
	return problems
}

// ProbeNative 用 node 的 process.dlopen 试加载全局包里的 .node（只加载二进制，不跑包的 JS 初始化），
// 从而在**切 node 版本之前**就发现 ABI 不匹配（方案 §8.5 L4）。nodeBin 为空/不存在时什么都不做。
func ProbeNative(pkgs []GlobalPackage, nodeBin string, timeout time.Duration) []GlobalProblem {
	if nodeBin == "" {
		return nil
	}
	if _, err := os.Stat(nodeBin); err != nil {
		return nil
	}
	var out []GlobalProblem
	for _, pkg := range pkgs {
		if !pkg.Native || !pkg.NAPI {
			continue // 纯 JS 不用试；非 N-API 的原生模块 CheckGlobals 已经静态告警过了，别重复报
		}
		for _, file := range nativeFiles(pkg.Path) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := exec.CommandContext(ctx, nodeBin, "-e", "process.dlopen({exports:{}}, process.argv[1])", file).Run()
			cancel()
			if err == nil {
				continue
			}
			out = append(out, GlobalProblem{
				Code:    "GLOBAL_NATIVE_ABI",
				Pkg:     pkg.Name,
				Message: fmt.Sprintf("全局包 %s 的原生模块在当前 node 上加载失败（%s）", pkg.Name, filepath.Base(file)),
				Detail:  map[string]any{"pkg": pkg.Name, "file": file, "node": nodeBin, "probe": err.Error()},
			})
			break // 一个包报一次就够
		}
	}
	return out
}

// nativeFiles 找出包目录下的 .node 文件（最多 8 个：大包不必全扫）。
func nativeFiles(dir string) []string {
	var out []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".node") && len(out) < 8 {
			out = append(out, path)
		}
		return nil
	})
	return out
}
