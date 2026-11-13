//go:build ignore

// langcheck 扫描 cmd/ 下没被 tr(zh, en) 包起来的含汉字字符串字面量。
//
//	go run tools/langcheck.go ./cmd/pk
//
// 退出码：0 干净；1 有遗漏（逐条打印 文件:行: 文案）。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func main() {
	dir := "./cmd/pk"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "langcheck: %v\n", err)
		os.Exit(2)
	}
	fset := token.NewFileSet()
	var bad []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "lang.go" {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "langcheck: 解析 %s: %v\n", path, err)
			os.Exit(2)
		}
		// 第一遍：标出 tr(...) 实参里的字面量位置。
		inside := map[token.Pos]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "tr" {
				return true
			}
			for _, arg := range call.Args {
				ast.Inspect(arg, func(m ast.Node) bool {
					if lit, ok := m.(*ast.BasicLit); ok {
						inside[lit.Pos()] = true
					}
					return true
				})
			}
			return true
		})
		// 第二遍：剩下的含汉字字面量就是漏翻。
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || inside[lit.Pos()] {
				return true
			}
			if hasHan(lit.Value) {
				line := fset.Position(lit.Pos()).Line
				bad = append(bad, fmt.Sprintf("%s:%d: %s", path, line, lit.Value))
			}
			return true
		})
	}
	sort.Strings(bad)
	for _, b := range bad {
		fmt.Println(b)
	}
	if len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "langcheck: %d 处未翻译（用 tr(zh, en) 包起来）\n", len(bad))
		os.Exit(1)
	}
	fmt.Println("langcheck: ok")
}
