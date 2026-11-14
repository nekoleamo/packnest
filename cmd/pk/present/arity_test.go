package present

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 参数个数门禁（E1 的衍生坑）：`coded.New/Wrap(…, CodeX, a, b, …)` 的实参个数必须与码表模板的
// 占位符个数一致（Wrap 的 cause 不算；少一个时 Text 会自动补 Cause）。
//
// 为什么做成测试而不是 `make codes`：这里能直接读码表本身，不必再写一遍解析。
// 少了这道门禁，用户就会看到 `%!s(MISSING)`（1.8 实测踩到过）。
func TestCodedArityMatchesTemplates(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	consts := map[string]string{}
	fset := token.NewFileSet()

	collect := func(path string) *ast.File {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		return f
	}
	walkGo := func(dir string) []string {
		var out []string
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s: %v", dir, err)
		}
		return out
	}

	files := walkGo(filepath.Join(root, "internal"))
	for _, path := range files { // 第一遍：Code* 常量名 → 值
		ast.Inspect(collect(path), func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, id := range vs.Names {
				if !strings.HasPrefix(id.Name, "Code") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						consts[id.Name] = v
					}
				}
			}
			return true
		})
	}

	bad := 0
	for _, path := range files { // 第二遍：调用点
		ast.Inspect(collect(path), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "coded" || (sel.Sel.Name != "New" && sel.Sel.Name != "Wrap") {
				return true
			}
			idx, code := -1, ""
			for i, a := range call.Args {
				if id, ok := a.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Code") {
					idx, code = i, consts[id.Name]
					break
				}
			}
			if idx < 0 {
				return true
			}
			entry, known := codeTable[code]
			if !known {
				return true // "定义了码但没有文案" 由 make codes 报
			}
			want := verbCount(entry.zh)
			got := len(call.Args) - idx - 1
			// 允许少一个：缺的那个由 Text 用 Cause 补
			if got+1 < want || got > want {
				pos := fset.Position(call.Pos())
				t.Errorf("%s:%d: %s 模板要 %d 个参数，调用点给了 %d 个",
					strings.TrimPrefix(pos.Filename, root+string(filepath.Separator)), pos.Line, code, want, got)
				bad++
			}
			return true
		})
	}
	if bad > 0 {
		t.Fatalf("%d 处参数个数与模板不符", bad)
	}
}
