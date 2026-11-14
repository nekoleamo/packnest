//go:build ignore

// langasserts 统计"测试断言里断中文子串"的规模与形态（完善路线 §E1 试点用）。
//
//	go run tools/langasserts.go [-v] [-pkg internal/env] [-by pkg|shape|file]
//
// 为什么要统计：L2（把内部错误文案改成"错误码 + 参数"，由 CLI 渲染）要不要全量推进，
// 取决于两件事 —— ① 有多少断言在断中文子串（全量推进会同时动这些断言）；
// ② 其中能**机器替换**的比例（人肉改 1000 处不现实，能机器改 200 处就值得）。
//
// 判定口径（只看 *_test.go）：
//   - 形状：Contains / HasPrefix / HasSuffix / EqualError / ErrorContains / 其它（==、正则、数组遍历）
//   - 只看含汉字的字面量（英文断言不受 L2 影响）
//
// 机器可替换的启发式（保守）：断言对象是"字符串常量且不含格式占位符"、且所在调用是
// strings.Contains / HasPrefix / HasSuffix / errors.Is / EqualError —— 这类可以机械地
// 换成语义更稳的断言（错误码或哨兵错误），不需要理解业务。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type hit struct {
	file  string
	line  int
	pkg   string
	shape string // Contains / HasPrefix / HasSuffix / EqualError / Other
	text  string // 断言里的中文文案（截断展示）
	pkgOK bool   // 机器可替换的启发式判定
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func main() {
	verbose := flag.Bool("v", false, "逐条打印")
	by := flag.String("by", "pkg", "汇总维度：pkg|shape|file")
	only := flag.String("pkg", "", "只看某个包（如 internal/env）")
	flag.Parse()

	fset := token.NewFileSet()
	var hits []hit
	roots := []string{"internal", "cmd", "acceptance"}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == "testdata" || strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			pkg := strings.TrimSuffix(strings.TrimPrefix(filepath.Dir(path), "./"), "")
			if *only != "" && pkg != *only {
				return nil
			}
			collect(fset, path, pkg, f, &hits)
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "langasserts:", err)
			os.Exit(2)
		}
	}

	if len(hits) == 0 {
		fmt.Println("没有断中文的断言")
		return
	}

	machine := 0
	byShape := map[string]int{}
	byPkg := map[string]int{}
	byPkgMachine := map[string]int{}
	for _, h := range hits {
		byShape[h.shape]++
		byPkg[h.pkg]++
		if h.pkgOK {
			machine++
			byPkgMachine[h.pkg]++
		}
		if *verbose {
			fmt.Printf("  %-40s %-11s %-12s %s\n", fmt.Sprintf("%s:%d", h.file, h.line), h.shape, h.pkg, h.text)
		}
	}

	fmt.Printf("断中文的断言共 %d 条，其中机器可替换（启发式）%d 条 = %.0f%%\n\n", len(hits), machine, pct(machine, len(hits)))
	switch *by {
	case "shape":
		printMap("形态", byShape)
	case "file":
		files := map[string]int{}
		for _, h := range hits {
			files[h.file]++
		}
		printMap("文件", files)
	default:
		printMap("包", byPkg)
		fmt.Println("\n包（其中机器可替换）：")
		keys := sortedKeys(byPkg)
		for _, k := range keys {
			fmt.Printf("  %-28s %4d 条（%d 条可机器替换）\n", k, byPkg[k], byPkgMachine[k])
		}
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func printMap(title string, m map[string]int) {
	fmt.Printf("%s分布：\n", title)
	for _, k := range sortedKeys(m) {
		fmt.Printf("  %-28s %4d\n", k, m[k])
	}
}

// collect 找断言调用里含汉字的字面量。
func collect(fset *token.FileSet, path, pkg string, f *ast.File, out *[]hit) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		shape := shapeOf(call)
		if shape == "" {
			return true
		}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				lit, ok := m.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !hasHan(s) {
					return true
				}
				pos := fset.Position(lit.Pos())
				*out = append(*out, hit{
					file: pos.Filename, line: pos.Line, pkg: pkg,
					shape: shape, text: truncate(s, 40),
					pkgOK: machineOK(shape, s),
				})
				return true
			})
		}
		return true
	})
}

// shapeOf 把断言调用归类；不是断言就返回空串。
func shapeOf(call *ast.CallExpr) string {
	name := ""
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	case *ast.Ident:
		name = fun.Name
	}
	switch name {
	case "Contains":
		return "Contains"
	case "HasPrefix":
		return "HasPrefix"
	case "HasSuffix":
		return "HasSuffix"
	case "EqualError":
		return "EqualError"
	case "ErrorContains":
		return "ErrorContains"
	case "Error", "Fatalf", "Fatal", "Errorf", "Logf", "Log":
		// t.Fatalf("... 中文") 不是"断中文"，只是消息里带中文；不统计
		return ""
	}
	return ""
}

// machineOK 是保守的"能机器替换"判定：文案不含格式占位符、断言形状是子串匹配类。
func machineOK(shape, s string) bool {
	if strings.Contains(s, "%") { // 带格式占位符的要人读一遍上下文
		return false
	}
	switch shape {
	case "Contains", "HasPrefix", "HasSuffix", "EqualError", "ErrorContains":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
