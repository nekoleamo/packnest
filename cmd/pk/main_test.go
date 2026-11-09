package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
)

// exec 跑一次 CLI，返回退出码、stdout、stderr。
func execCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := execCLI(t, "--version")
	if code != 0 || !strings.Contains(out, "packnest v") {
		t.Fatalf("--version = %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--json", "version")
	if code != 0 {
		t.Fatalf("version --json 退出码 %d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("非法 JSON: %v\n%s", err, out)
	}
	if doc["schema"].(float64) != 1 || doc["name"] != "packnest" {
		t.Fatalf("版本 JSON 异常: %v", doc)
	}

	code, out, _ = execCLI(t)
	if code != 0 || !strings.Contains(out, "packnest ·") {
		t.Fatalf("裸执行应打印用法: %d %q", code, out)
	}
	if _, out, _ = execCLI(t, "help"); !strings.Contains(out, "命令:") {
		t.Fatalf("help 输出异常: %q", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := execCLI(t, "frobnicate")
	if code != 2 || !strings.Contains(errOut, "未知命令") {
		t.Fatalf("未知命令应退出 2: %d %q", code, errOut)
	}
	if code, _, errOut := execCLI(t, "--root"); code != 2 || !strings.Contains(errOut, "--root") {
		t.Fatalf("--root 缺参数应退出 2: %d %q", code, errOut)
	}
}

func TestListEmptyAndJSON(t *testing.T) {
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "list")
	if code != 0 || !strings.Contains(out, "还没装任何工具") {
		t.Fatalf("空台账输出异常: %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json 退出码 %d", code)
	}
	var doc struct {
		Schema int `json:"schema"`
		Tools  []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if doc.Schema != 1 || doc.Tools == nil || len(doc.Tools) != 0 {
		t.Fatalf("空列表应为 tools: []: %s", out)
	}
}

func TestListWithLedger(t *testing.T) {
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	toolsDir := p.ToolVersion("node", "26.13.0")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := &store.Ledger{}
	ledger.Upsert(store.Install{Tool: "node", Version: "26.13.0", Path: toolsDir,
		NpmVersion: "12.2.2", NpmBundled: "11.20.0", References: []store.Ref{{Kind: store.RefGlobal}}})
	ledger.Upsert(store.Install{Tool: "pnpm", Version: "10.4.1", Path: toolsDir})
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}
	cfg := store.DefaultConfig()
	cfg.Tools = map[string]string{"node": "26.13.0"}
	if err := cfg.Save(p.Config()); err != nil {
		t.Fatal(err)
	}

	code, out, _ := execCLI(t, "--root", root, "list")
	if code != 0 {
		t.Fatalf("list 退出码 %d", code)
	}
	for _, want := range []string{"* node", "26.13.0", "npm 12.2.2", "自带 11.20.0", "pnpm"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}

	code, out, _ = execCLI(t, "--root", root, "--json", "list", "--available")
	if code != 0 {
		t.Fatalf("list --available 退出码 %d", code)
	}
	var doc struct {
		Tools []struct {
			Name   string `json:"name"`
			Active bool   `json:"active"`
			NPM    *struct {
				Version  string `json:"version"`
				Replaced string `json:"replaced"`
			} `json:"npm"`
			Requires []string `json:"requires"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("非法 JSON: %v", err)
	}
	if len(doc.Tools) != 2 {
		t.Fatalf("应有 2 条记录: %s", out)
	}
	node := doc.Tools[0]
	if node.Name != "node" || !node.Active || node.NPM == nil || node.NPM.Replaced != "11.20.0" {
		t.Fatalf("node 记录异常: %+v", node)
	}
	if len(doc.Tools[1].Requires) != 1 || doc.Tools[1].Requires[0] != "node" {
		t.Fatalf("pnpm 必须声明 requires node（方案 §18.1）: %+v", doc.Tools[1])
	}
}

func TestWhich(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	self := filepath.Join(root, "pk-self")
	if err := os.WriteFile(self, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := shim.Self(p.Bin(), self, goos()); err != nil {
		t.Fatal(err)
	}

	code, out, _ := execCLI(t, "--root", root, "which", "pk")
	if code != 0 || strings.TrimSpace(out) != self {
		t.Fatalf("which pk = %d %q，期望 %q", code, out, self)
	}
	code, out, _ = execCLI(t, "--root", root, "which", "packnest")
	if code != 0 || strings.TrimSpace(out) != self {
		t.Fatalf("which packnest = %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "--json", "which", "pk")
	if code != 0 || !strings.Contains(out, `"shim": true`) {
		t.Fatalf("which --json = %d %q", code, out)
	}

	code, _, errOut := execCLI(t, "--root", root, "which", "nope")
	if code != 1 || !strings.Contains(errOut, "nope") {
		t.Fatalf("缺失命令应退出 1: %d %q", code, errOut)
	}
	if code, _, _ := execCLI(t, "--root", root, "which"); code != 2 {
		t.Fatalf("参数缺失应退出 2，得到 %d", code)
	}
	if code, _, _ := execCLI(t, "--root", root, "which", "a", "b"); code != 2 {
		t.Fatalf("多余参数应退出 2，得到 %d", code)
	}
}

func TestMirrorListAndUse(t *testing.T) {
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "mirror", "list")
	if code != 0 || !strings.Contains(out, "cn") || !strings.Contains(out, "auto") {
		t.Fatalf("mirror list = %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "--json", "mirror", "list")
	if code != 0 || !strings.Contains(out, `"current": "auto"`) {
		t.Fatalf("mirror list --json = %d %q", code, out)
	}

	code, out, _ = execCLI(t, "--root", root, "mirror", "use", "cn")
	if code != 0 || !strings.Contains(out, "cn") {
		t.Fatalf("mirror use = %d %q", code, out)
	}
	p, _ := paths.New(root)
	cfg, err := store.LoadConfig(p.Config())
	if err != nil || cfg.Mirror != "cn" {
		t.Fatalf("配置未写入: %+v %v", cfg, err)
	}

	if code, _, errOut := execCLI(t, "--root", root, "mirror", "use", "nope"); code != 2 || !strings.Contains(errOut, "未知镜像档") {
		t.Fatalf("非法档位应退出 2: %d %q", code, errOut)
	}
	if code, _, _ := execCLI(t, "--root", root, "mirror", "use"); code != 2 {
		t.Fatalf("缺参数应退出 2，得到 %d", code)
	}
	if code, _, errOut := execCLI(t, "--root", root, "mirror", "write"); code != 1 || !strings.Contains(errOut, "M5") {
		t.Fatalf("mirror write 应报 M5: %d %q", code, errOut)
	}
	if code, _, _ := execCLI(t, "--root", root, "mirror"); code != 2 {
		t.Fatalf("缺子命令应退出 2，得到 %d", code)
	}
	if code, _, _ := execCLI(t, "--root", root, "mirror", "bogus"); code != 2 {
		t.Fatalf("未知子命令应退出 2，得到 %d", code)
	}
}

func TestMirrorProbeOffline(t *testing.T) {
	root := t.TempDir()
	code, _, _ := execCLI(t, "--root", root, "--offline", "mirror", "probe")
	if code != 3 {
		t.Fatalf("离线探测应退出 3（网络类），得到 %d", code)
	}
	code, _, errOut := execCLI(t, "--root", root, "--offline", "mirror", "probe", "--tool", "nope")
	if code != 1 || !strings.Contains(errOut, "没有可探测的源") {
		t.Fatalf("未知工具应退出 1: %d %q", code, errOut)
	}
}

func TestDoctorText(t *testing.T) {
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "doctor")
	if code != 0 {
		t.Fatalf("空白环境 doctor 应退出 0（只有 warn/info），得到 %d\n%s", code, out)
	}
	if !strings.Contains(out, "PATH") {
		t.Fatalf("应提示 PATH 问题:\n%s", out)
	}
	if code, out, _ := execCLI(t, "--root", root, "doctor", "--json"); code != 0 || !strings.Contains(out, `"schema": 1`) {
		t.Fatalf("doctor --json = %d %q", code, out)
	}
	if code, _, errOut := execCLI(t, "--root", root, "doctor", "--globals"); code != 1 || !strings.Contains(errOut, "M4") {
		t.Fatalf("doctor --globals 应报 M4: %d %q", code, errOut)
	}
}

func TestDoctorBrokenLedger(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	if err := os.MkdirAll(p.State(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Ledger(), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := execCLI(t, "--root", root, "--json", "doctor")
	if code != 1 || !strings.Contains(out, "LEDGER_BROKEN") {
		t.Fatalf("台账损坏应退出 1 并报 LEDGER_BROKEN: %d\n%s", code, out)
	}
}

func TestBrokenConfig(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	if err := os.WriteFile(p.Config(), []byte("mirror = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := execCLI(t, "--root", root, "list"); code != 1 || !strings.Contains(errOut, "配置不可用") {
		t.Fatalf("坏配置应让普通命令失败: %d %q", code, errOut)
	}
	code, out, _ := execCLI(t, "--root", root, "--json", "doctor")
	if code != 1 || !strings.Contains(out, "CONFIG_BROKEN") {
		t.Fatalf("doctor 应报 CONFIG_BROKEN: %d\n%s", code, out)
	}
	if code, _, _ := execCLI(t, "--root", root, "--version"); code != 0 {
		t.Fatalf("坏配置不该影响 --version，得到 %d", code)
	}
}

func TestEnv(t *testing.T) {
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "env")
	if code != 0 || !strings.Contains(out, "export PATH=") || !strings.Contains(out, "npm_config_registry") {
		t.Fatalf("env = %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "env", "--shell", "powershell")
	if code != 0 || !strings.Contains(out, "$env:PATH") {
		t.Fatalf("env --shell powershell = %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "--json", "env")
	if code != 0 || !strings.Contains(out, `"script_hint"`) {
		t.Fatalf("env --json = %d %q", code, out)
	}
	if code, _, _ := execCLI(t, "--root", root, "env", "--shell", "fish"); code != 2 {
		t.Fatalf("未知 shell 应退出 2，得到 %d", code)
	}
	if code, _, errOut := execCLI(t, "--root", root, "env", "--write"); code != 1 || !strings.Contains(errOut, "M5") {
		t.Fatalf("env --write 应报 M5: %d %q", code, errOut)
	}
}

func TestStubs(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"install"}, "M1"},
		{[]string{"install", "node@22"}, "M1"},
		{[]string{"uninstall", "node@22.22.2"}, "M4"},
		{[]string{"use", "node@22"}, "M4"},
		{[]string{"gc", "--dry-run"}, "M4"},
	}
	for _, c := range cases {
		code, _, errOut := execCLI(t, append([]string{"--root", root}, c.args...)...)
		if code != 1 || !strings.Contains(errOut, c.want) {
			t.Errorf("%v 应退出 1 并提示 %s，得到 %d %q", c.args, c.want, code, errOut)
		}
	}
	if code, _, _ := execCLI(t, "--root", root, "uninstall"); code != 2 {
		t.Errorf("uninstall 缺参数应退出 2，得到 %d", code)
	}
	if code, _, _ := execCLI(t, "--root", root, "use"); code != 2 {
		t.Errorf("use 缺参数应退出 2，得到 %d", code)
	}
}

func TestGlobalFlagPositions(t *testing.T) {
	root := t.TempDir()
	// 全局开关在命令前后都应生效。
	if code, out, _ := execCLI(t, "list", "--json", "--root", root); code != 0 || !strings.Contains(out, `"schema": 1`) {
		t.Fatalf("命令后的全局开关应生效: %d %q", code, out)
	}
	// `--` 之后的参数原样传递。
	if code, _, _ := execCLI(t, "--root", root, "which", "--", "-weird"); code != 1 {
		t.Fatalf("`--` 后参数应原样传给子命令，得到 %d", code)
	}
}
