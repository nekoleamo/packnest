package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// engines.node 是"进现有前端项目就能装"的入口：pk.toml 没声明 node 时用它。
func TestProjectSpecsFromEngines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":{"node":"^20.17 || >=22.9"}}`)
	specs, notes, err := ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("ProjectSpecs: %v", err)
	}
	if specs["node"] != "^20.17 || >=22.9" {
		t.Fatalf("node 说明符 = %q", specs["node"])
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "engines.node") {
		t.Fatalf("应给一条来源提示: %v", notes)
	}
}

// pk.toml 是权威：两边都有时 pk.toml 赢。
func TestProjectSpecsTomlWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pk.toml", "[tools]\nnode = \"22\"\n")
	writeFile(t, dir, "package.json", `{"engines":{"node":"^18"}}`)
	specs, _, err := ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("ProjectSpecs: %v", err)
	}
	if specs["node"] != "22" {
		t.Fatalf("pk.toml 应优先，得到 %q", specs["node"])
	}
}

// pk.toml 只声明了别的工具时，node 仍然由 engines 补上（不少项目只 pin pnpm）。
func TestProjectSpecsFillsMissingNode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pk.toml", "[tools]\npnpm = \"11\"\n")
	writeFile(t, dir, "package.json", `{"engines":{"node":">=22.9"}}`)
	specs, _, err := ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("ProjectSpecs: %v", err)
	}
	if specs["node"] != ">=22.9" || specs["pnpm"] != "11" {
		t.Fatalf("应两者都在: %+v", specs)
	}
}

// 坏掉的 package.json 只是提示，不拦安装（模板/半成品很常见）。
func TestProjectSpecsBrokenPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":`)
	specs, notes, err := ProjectSpecs(dir)
	if err != nil {
		t.Fatalf("坏 package.json 不该报错: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("不该补出工具: %+v", specs)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "解析失败") {
		t.Fatalf("应有一条提示: %v", notes)
	}
}

// engines.node 无法解析要在"读项目"这一层就报错并给建议，而不是留到后面解析失败。
func TestProjectSpecsBadRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":{"node":"not-a-range"}}`)
	_, _, err := ProjectSpecs(dir)
	if err == nil {
		t.Fatal("非法范围应报错")
	}
	if !strings.Contains(err.Error(), "pk install node@") {
		t.Fatalf("错误里应给可执行建议: %v", err)
	}
}

func TestProjectSpecsEmptyDir(t *testing.T) {
	specs, notes, err := ProjectSpecs(t.TempDir())
	if err != nil || len(specs) != 0 || len(notes) != 0 {
		t.Fatalf("空目录应无事发生: %+v %v %v", specs, notes, err)
	}
}

// 只有 pk.toml 存在时才写 pk.lock —— package.json 驱动的项目不该凭空多出一个 pk 文件。
func TestHasProjectFile(t *testing.T) {
	dir := t.TempDir()
	if HasProjectFile(dir) {
		t.Fatal("空目录不该认为有 pk.toml")
	}
	writeFile(t, dir, "package.json", `{}`)
	if HasProjectFile(dir) {
		t.Fatal("package.json 不算项目清单")
	}
	writeFile(t, dir, "pk.toml", "[tools]\n")
	if !HasProjectFile(dir) {
		t.Fatal("有 pk.toml 时应为真")
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if HasProjectFile(filepath.Join(dir, "sub")) {
		t.Fatal("同名目录不算")
	}
}
