package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// gcLedger 造一份"三个零引用 node 版本"的台账（--keep 的判定对象）。
func gcLedger(t *testing.T, versions ...string) (string, paths.Paths) {
	t.Helper()
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &store.Ledger{}
	for _, v := range versions {
		dir := p.ToolVersion("node", v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "node"), []byte("x"), 0o644)
		ledger.Upsert(store.Install{Tool: "node", Version: v, Path: dir})
	}
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}
	return root, p
}

// TestAcceptance_M6_GCKeep：`gc --keep N` 每个工具保留最新 N 个，更旧的零引用版本被回收。
func TestAcceptance_M6_GCKeep(t *testing.T) {
	root, _ := gcLedger(t, "20.9.0", "20.10.0", "20.11.0")
	code, out, errOut := execCLI(t, "--root", root, "--json", "gc", "--keep", "2")
	if code != 0 {
		t.Fatalf("gc --keep 退出码 %d\n%s%s", code, out, errOut)
	}
	var res struct {
		Removed    []string `json:"Removed"`
		KeptRecent []string `json:"KeptRecent"`
		Keep       int      `json:"keep"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("非法 JSON: %v\n%s", err, out)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "node@20.9.0" {
		t.Fatalf("应只回收最旧的 20.9.0（按语义化版本比较，不是字符串）：%v", res.Removed)
	}
	if len(res.KeptRecent) != 2 || res.Keep != 2 {
		t.Fatalf("应保留最近 2 个：%v keep=%d", res.KeptRecent, res.Keep)
	}
}

// TestAcceptance_M6_ListOutdated：已装版本落后于索引最新时，`pk list` 给出可升级提示。
// 索引走缓存（不联网），所以先装一次把缓存写出来。
func TestAcceptance_M6_ListOutdated(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{
			{Version: "22.98.0", NPMVersion: "11.20.0"},
			{Version: "22.99.99", NPMVersion: "11.20.0"},
		},
		NPM: map[string]string{"11.20.0": ">=20.0.0"},
	})
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")
	t.Setenv("PK_MIRROR_NODE_ARTIFACT", fm.Server.URL+"/node/v{version}/node-v{version}-{platform}.{ext}")
	t.Setenv("PK_MIRROR_NODE_CHECKSUM", fm.Server.URL+"/node/v{version}/SHASUMS256.txt")
	t.Setenv("PK_MIRROR_NPM_INDEX", fm.Server.URL+"/npm")
	t.Setenv("PK_MIRROR_NPM_ARTIFACT", fm.Server.URL+"/npm/-/npm-{version}.tgz")

	root := t.TempDir()
	if code, out, errOut := execCLI(t, "--root", root, "install", "node@22.98.0"); code != 0 {
		t.Fatalf("install 退出码 %d\n%s%s", code, out, errOut)
	}
	// 索引缓存只能由联网的读索引动作写出（精确版本安装不需要索引）；`list node` 只碰 node。
	if code, out, errOut := execCLI(t, "--root", root, "--json", "list", "node"); code != 0 {
		t.Fatalf("list node 退出码 %d\n%s%s", code, out, errOut)
	}
	code, out, _ := execCLI(t, "--root", root, "--json", "list")
	if code != 0 {
		t.Fatalf("list 退出码 %d", code)
	}
	if !strings.Contains(out, `"latest": "22.99.99"`) || !strings.Contains(out, `"installed": "22.98.0"`) {
		t.Fatalf("应提示 22.98.0 → 22.99.99：\n%s", out)
	}
	if code, out, _ := execCLI(t, "--root", root, "list"); code != 0 || !strings.Contains(out, "pk install node@latest") {
		t.Fatalf("文本模式应给可执行建议：%d\n%s", code, out)
	}
}

// TestAcceptance_M6_DoctorFix：`doctor --fix` 只修 PATH 托管块与 pk/packnest 入口，其余不碰。
func TestAcceptance_M6_DoctorFix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 PATH 走注册表用户变量，测试不碰真实注册表")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	root := t.TempDir()

	code, out, _ := execCLI(t, "--root", root, "--json", "doctor", "--fix")
	if code != 0 {
		t.Fatalf("doctor --fix 退出码 %d（PATH/SHIM 都是 warn 级，应退出 0）", code)
	}
	var doc struct {
		Fixed  []string `json:"fixed"`
		Issues []struct {
			Code string `json:"code"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("非法 JSON: %v\n%s", err, out)
	}
	if len(doc.Fixed) == 0 {
		t.Fatalf("应至少修了 PATH 托管块或入口：%s", out)
	}
	rc := filepath.Join(home, ".zshrc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("应把托管块写进 %s: %v", rc, err)
	}
	if !strings.Contains(string(data), "# >>> packnest >>>") {
		t.Fatalf("rc 里应有托管块：\n%s", data)
	}
	// 幂等：再跑一次不应重复追加
	execCLI(t, "--root", root, "doctor", "--fix")
	again, _ := os.ReadFile(rc)
	if strings.Count(string(again), "# >>> packnest >>>") != 1 {
		t.Fatalf("托管块必须幂等（只一段）：\n%s", again)
	}
}
