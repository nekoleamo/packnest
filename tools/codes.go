//go:build ignore

// codes：doctor 问题码契约校验（完善路线 §E8a）。
//
// 从代码里扫出**全部**问题码，和 docs/json-schema.md 的「doctor 问题码」表比对：
// 排序后的集合必须完全一致（忽略大小写），否则红。脚本与 GUI 依赖这些码，改码不 Lark 文档 = 契约静默破裂。
//
// 三个来源（新增发码方式时同步这里的扫描规则）：
//  1. add(jsonout.SeverityX, "CODE", ...) —— cmd/pk 的 doctor 直接发码；
//  2. Code: "CODE"                        —— 结构化 problem（internal/env 等）。
//  3. const Issue<X> = "CODE"             —— 抽出来的问题码常量（internal/provider）。
//
// 只比**码集合**，不比级别（severity）：同一码在不同分支可能是 warn/info（如 SHIM_*），
// 文档表里写成 `warn/info`，机器判定谁是"正确"的那一半是伪精确。级别由文档表人工保证。
//
// 用法：go run tools/codes.go [-v]
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	docPath = "docs/json-schema.md"
	docSec  = "## doctor 问题码"
	// 扫描根：只要 CLI 与 internal（GUI 是独立 go.mod 的契约消费者，不定义码）
	scanRoots = "cmd,internal"
	// 不进契约扫描的目录（GUI 用子进程 + --json，不定义码）
	skipDirs = "cmd/pk-desktop"
)

// codeLiteral 判断一个字符串字面量像不像问题码（全大写 + 下划线）。
var codeLiteral = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

type found struct {
	code string
	pos  string
}

func main() {
	verbose := flag.Bool("v", false, "打印每个码的来源位置")
	flag.Parse()

	fset := token.NewFileSet()
	byCode := map[string][]string{}
	for _, root := range strings.Split(scanRoots, ",") {
		scanDir(fset, root, byCode)
	}

	docCodes, err := parseDoc(docPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✘ 读文档失败:", err)
		os.Exit(1)
	}

	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	if *verbose {
		for _, c := range codes {
			fmt.Printf("  %-28s %s\n", c, byCode[c][0])
		}
	}

	missingInDoc, missingInCode := diff(codes, docCodes)
	if len(missingInDoc) == 0 && len(missingInCode) == 0 {
		fmt.Printf("问题码契约一致：%d 个（代码 %d 处，文档 %d 处）\n", len(codes), countPos(byCode), len(docCodes))
		return
	}
	if len(missingInDoc) > 0 {
		fmt.Fprintf(os.Stderr, "✘ 代码里有、文档表里没有（%d）：\n", len(missingInDoc))
		for _, c := range missingInDoc {
			fmt.Fprintf(os.Stderr, "    %-28s %s\n", c, strings.Join(byCode[c], " "))
		}
	}
	if len(missingInCode) > 0 {
		fmt.Fprintf(os.Stderr, "✘ 文档表里有、代码里扫不到（%d，码改了没同步或已废弃）：\n", len(missingInCode))
		for _, c := range missingInCode {
			fmt.Fprintf(os.Stderr, "    %s\n", c)
		}
	}
	fmt.Fprintf(os.Stderr, "改码请同时改 %s 的「doctor 问题码」表（脚本与 GUI 依赖它）\n", docPath)
	os.Exit(1)
}

func countPos(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func scanDir(fset *token.FileSet, dir string, byCode map[string][]string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "testdata" || base == "node_modules" || base == "dist" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			if strings.HasPrefix(filepath.ToSlash(path), skipDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		scanFile(fset, f, byCode)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "✘ 扫描失败:", err)
		os.Exit(1)
	}
}

func scanFile(fset *token.FileSet, f *ast.File, byCode map[string][]string) {
	addCode := func(lit *ast.BasicLit) {
		if lit == nil || lit.Kind != token.STRING {
			return
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !codeLiteral.MatchString(s) || len(s) < 4 {
			return
		}
		pos := fset.Position(lit.Pos())
		byCode[s] = append(byCode[s], fmt.Sprintf("%s:%d", pos.Filename, pos.Line))
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			// 1. add(sev, "CODE", msg, detail)
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "add" && len(x.Args) >= 2 {
				if lit, ok := x.Args[1].(*ast.BasicLit); ok {
					addCode(lit)
				}
			}
		case *ast.CompositeLit:
			// 2. Issue{Code: "CODE", ...}
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Code" {
					if lit, ok := kv.Value.(*ast.BasicLit); ok {
						addCode(lit)
					}
				}
			}
		case *ast.GenDecl:
			// 3. const IssueXxx = "CODE"（只认名字以 Issue 开头、值是码形状的常量）
			if x.Tok != token.CONST {
				return true
			}
			for _, spec := range x.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Issue") || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						addCode(lit)
					}
				}
			}
		}
		return true
	})
}

// parseDoc 解析 docs/json-schema.md 里「doctor 问题码」表的第一列。
// 一行可能挂多个码（`A` / `B`），按 `/` 拆开；表头与分隔行跳过。
func parseDoc(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	inSection := false
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, docSec) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "## ") { // 下一节：结束
			break
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 2 || strings.HasPrefix(strings.TrimSpace(cells[0]), "code") ||
			strings.HasPrefix(strings.TrimSpace(cells[0]), "---") {
			continue
		}
		for _, part := range strings.Split(cells[0], "/") {
			code := strings.Trim(strings.TrimSpace(part), "`")
			if code == "" {
				continue
			}
			if !seen[code] {
				seen[code] = true
				out = append(out, code)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("在 %s 里没找到「%s」表（标题改了？）", path, strings.TrimPrefix(docSec, "## "))
	}
	sort.Strings(out)
	return out, nil
}

// diff 两边都按排序切片比，返回 代码独有 / 文档独有。
func diff(codeSide, docSide []string) (onlyCode, onlyDoc []string) {
	cs := map[string]string{}
	for _, c := range codeSide {
		cs[strings.ToUpper(c)] = c
	}
	ds := map[string]string{}
	for _, c := range docSide {
		ds[strings.ToUpper(c)] = c
	}
	for k, v := range cs {
		if _, ok := ds[k]; !ok {
			onlyCode = append(onlyCode, v)
		}
	}
	for k, v := range ds {
		if _, ok := cs[k]; !ok {
			onlyDoc = append(onlyDoc, v)
		}
	}
	sort.Strings(onlyCode)
	sort.Strings(onlyDoc)
	return onlyCode, onlyDoc
}
