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
	// 第二类：shell 脚本里 `$var` 直接贴上非 ASCII 字符。
	// macOS 的 /bin/sh（bash 3.2 POSIX）会把多字节字体的首字节算进变量名，
	// `set -u` 下直接 "unbound variable" 退出 —— 2026-11-14 实测：
	// install.sh 的「缺 .sig 提示后继续」分支就这样坏过（真机里只能看到 exit 1）。
	bad = append(bad, scanShellVarGlue("scripts")...)
	sort.Strings(bad)
	for _, b := range bad {
		fmt.Println(b)
	}
	if len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "langcheck: %d 处问题（未翻译的汉字 / shell 变量紧贴中文）\n", len(bad))
		os.Exit(1)
	}
	fmt.Println("langcheck: ok")
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// scanShellVarGlue 扫目录下所有 .sh：`$var` 后面紧跟非 ASCII 字节就报（应写 `${var}`）。
// 只看非注释行；`${var}` 与 `$1` 这类不会命中。
func scanShellVarGlue(dir string) []string {
	paths, err := filepath.Glob(filepath.Join(dir, "*.sh"))
	if err != nil {
		return nil
	}
	var bad []string
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "langcheck: 读 %s: %v\n", path, err)
			continue
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for j := 0; j < len(line); j++ {
				if line[j] != '$' {
					continue
				}
				k := j + 1
				for k < len(line) && (line[k] == '_' || line[k] >= '0' && line[k] <= '9' ||
					line[k] >= 'a' && line[k] <= 'z' || line[k] >= 'A' && line[k] <= 'Z') {
					k++
				}
				if k == j+1 || k >= len(line) || line[k] < 0x80 {
					continue
				}
				// $1..$9 是位置参数，单个数字后面跟中文没事（变量名不能以数字开头）
				if name := line[j+1 : k]; isAllDigits(name) {
					continue
				}
				bad = append(bad, fmt.Sprintf("%s:%d: $%s 后面直接跟非 ASCII 字符（shell 会把它算进变量名，改成 ${%s}）",
					path, i+1, line[j+1:k], line[j+1:k]))
			}
		}
	}
	return bad
}
