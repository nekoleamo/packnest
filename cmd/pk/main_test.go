package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
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
	if code, out, _ := execCLI(t, "--root", root, "mirror", "write", "--file", filepath.Join(t.TempDir(), "npmrc")); code != 0 || !strings.Contains(out, "registry=") {
		t.Fatalf("mirror write 应可用: %d %q", code, out)
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
	if code, out, _ := execCLI(t, "--root", root, "--json", "doctor", "--globals"); code != 0 || !strings.Contains(out, "GLOBALS_NONE") {
		t.Fatalf("doctor --globals 应报告全局包目录为空: %d %q", code, out)
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
	// env --write：写 rc 的托管块（幂等），--unset 删掉
	rcPath := filepath.Join(t.TempDir(), "rc-file")
	code, out, _ = execCLI(t, "--root", root, "env", "--write", "--rc", rcPath)
	if code != 0 || !strings.Contains(out, "托管块") {
		t.Fatalf("env --write 应可用: %d %q", code, out)
	}
	rc := rcPath
	data, err := os.ReadFile(rc)
	if err != nil || !strings.Contains(string(data), ">>> packnest >>>") {
		t.Fatalf("rc 应写入托管块: %v %q", err, data)
	}
	// 幂等：再写一次不重复
	if code, _, _ = execCLI(t, "--root", root, "env", "--write", "--rc", rcPath); code != 0 {
		t.Fatal("重复写应成功")
	}
	data2, _ := os.ReadFile(rc)
	if strings.Count(string(data2), ">>> packnest >>>") != 1 {
		t.Fatalf("托管块应只有一段:\n%s", data2)
	}
	if code, out, _ = execCLI(t, "--root", root, "env", "--unset", "--rc", rcPath); code != 0 || !strings.Contains(out, "已删掉") {
		t.Fatalf("env --unset 应可用: %d %q", code, out)
	}
	data3, _ := os.ReadFile(rc)
	if strings.Contains(string(data3), ">>> packnest >>>") {
		t.Fatalf("托管块应被删掉:\n%s", data3)
	}
}

func TestStubs(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"use", "node@22"}, "还没装"},
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
	if code, out, _ := execCLI(t, "--root", root, "--json", "gc", "--dry-run"); code != 0 || !strings.Contains(out, "schema") {
		t.Errorf("gc --dry-run 应可用: %d %q", code, out)
	}
	if code, _, errOut := execCLI(t, "--root", root, "uninstall", "node@22.22.2"); code != 1 || !strings.Contains(errOut, "不在台账里") {
		t.Errorf("卸载不在台账里的版本应退出 1: %d %q", code, errOut)
	}
	if code, _, errOut := execCLI(t, "--root", root, "install"); code != 2 || !strings.Contains(errOut, "没有 pk.toml") {
		t.Errorf("无参数且无 pk.toml 应退出 2: %d %q", code, errOut)
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

// setupFakeMirror 用 PK_MIRROR_* 把 CLI 指向假镜像（方案 §6.5 的内网覆盖机制）。
func setupFakeMirror(t *testing.T) *testkit.FakeNode {
	t.Helper()
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.99.99", NPMVersion: "11.20.0"}},
		NPM: map[string]string{
			"11.20.0": "^20.17.0 || >=22.9.0",
			"12.2.2":  "^22.22.2 || ^24.15.0 || >=26.0.0",
		},
	})
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")
	t.Setenv("PK_MIRROR_NODE_ARTIFACT", fm.Server.URL+"/node/v{version}/node-v{version}-{platform}.{ext}")
	t.Setenv("PK_MIRROR_NODE_CHECKSUM", fm.Server.URL+"/node/v{version}/SHASUMS256.txt")
	t.Setenv("PK_MIRROR_NPM_INDEX", fm.Server.URL+"/npm")
	t.Setenv("PK_MIRROR_NPM_ARTIFACT", fm.Server.URL+"/npm/-/npm-{version}.tgz")
	return fm
}

func TestCLIInstallAndUninstall(t *testing.T) {
	fm := setupFakeMirror(t)
	root := t.TempDir()

	code, out, errOut := execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 0 {
		t.Fatalf("install 退出码 %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(out, "装好 node@22.99.99") || !strings.Contains(out, "npm 12.2.2") {
		t.Fatalf("install 输出异常:\n%s", out)
	}

	code, out, _ = execCLI(t, "--root", root, "--json", "list")
	if code != 0 || !strings.Contains(out, `"version": "22.99.99"`) || !strings.Contains(out, `"replaced": "11.20.0"`) {
		t.Fatalf("list 应显示伴生 npm: %d\n%s", code, out)
	}

	// 再装一次：幂等
	code, out, _ = execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 0 || !strings.Contains(out, "已是最新") {
		t.Fatalf("重复安装应幂等: %d %q", code, out)
	}

	// which 能解析到真实二进制
	code, out, _ = execCLI(t, "--root", root, "which", "node")
	if code != 0 || !strings.Contains(out, "tools/node/22.99.99") {
		t.Fatalf("which node 异常: %d %q", code, out)
	}
	// 双名 shim 也被补齐
	code, out, _ = execCLI(t, "--root", root, "which", "pk")
	if code != 0 || !strings.Contains(strings.TrimSpace(out), "pk") {
		t.Fatalf("install 后应补齐 pk shim: %d %q", code, out)
	}

	// 首次安装即全局默认；不带 --force 应被引用检查挡住
	if code, _, errOut := execCLI(t, "--root", root, "uninstall", "node@22.99.99"); code != 1 || !strings.Contains(errOut, "--force") {
		t.Fatalf("默认版本卸载应被拒并提示 --force: %d %q", code, errOut)
	}
	code, out, _ = execCLI(t, "--root", root, "uninstall", "node@22.99.99", "--force")
	if code != 0 || !strings.Contains(out, "已卸载") {
		t.Fatalf("uninstall: %d %q", code, out)
	}
	if code, _, _ := execCLI(t, "--root", root, "which", "node"); code != 1 {
		t.Fatalf("卸载后 which 应失败，得到 %d", code)
	}
	_ = fm
}

func TestCLIInstallByProjectFile(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "pk.toml"), []byte("[tools]\nnode = \"22\"\nnpm = \"auto\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PK_CWD", cwd)
	code, out, errOut := execCLI(t, "--root", root, "install")
	if code != 0 {
		t.Fatalf("按 pk.toml 安装失败: %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "node@22.99.99") {
		t.Fatalf("应按 pk.toml 装 node 22.x:\n%s", out)
	}
}

func TestCLIInstallJSONEvents(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "--json", "install", "node@22.99.99")
	if code != 0 {
		t.Fatalf("--json install 退出码 %d", code)
	}
	wantEvents := []string{`"ev":"resolve"`, `"ev":"download"`, `"ev":"verify"`, `"ev":"companion"`, `"ev":"done"`}
	for _, w := range wantEvents {
		if !strings.Contains(out, w) {
			t.Errorf("NDJSON 缺少 %s:\n%s", w, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("NDJSON 行不是合法 JSON: %v (%q)", err, line)
		}
		if ev["ev"] == nil {
			t.Fatalf("事件缺少 ev 字段: %q", line)
		}
	}
}

func TestCLIInstallNetworkFailure(t *testing.T) {
	// 校验和/产物都不可达 → 退出码 3（网络类）。
	t.Setenv("PK_MIRROR_NODE_INDEX", "http://127.0.0.1:1/node/index.json")
	root := t.TempDir()
	code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 3 {
		t.Fatalf("网络失败应退出 3，得到 %d（%s）", code, errOut)
	}
}

func TestCLIInstallChecksumFailure(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: "22.99.99"}},
		NPM:       map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
		Behaviors: []testkit.Option{testkit.Corrupt()},
	})
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")
	t.Setenv("PK_MIRROR_NODE_ARTIFACT", fm.Server.URL+"/node/v{version}/node-v{version}-{platform}.{ext}")
	t.Setenv("PK_MIRROR_NODE_CHECKSUM", fm.Server.URL+"/node/v{version}/SHASUMS256.txt")
	root := t.TempDir()
	code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 4 {
		t.Fatalf("校验失败应退出 4，得到 %d（%s）", code, errOut)
	}
}

func TestCLIInstallBadSpec(t *testing.T) {
	root := t.TempDir()
	if code, _, _ := execCLI(t, "--root", root, "install", "node@bogus"); code != 1 {
		t.Fatalf("非法版本说明符应退出 1，得到 %d", code)
	}
	if code, _, _ := execCLI(t, "--root", root, "install", "rust@1"); code != 1 {
		t.Fatalf("未知工具应退出 1，得到 %d", code)
	}
	if code, _, _ := execCLI(t, "--root", root, "uninstall", "node"); code != 2 {
		t.Fatalf("uninstall 需要精确版本，应退出 2，得到 %d", code)
	}
}

func TestCLILockBusy(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	unlock, err := store.Lock(p.Lock())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 5 || !strings.Contains(errOut, "另一个 pk 正在运行") {
		t.Fatalf("锁被占用应退出 5: %d %q", code, errOut)
	}
	if code, _, _ := execCLI(t, "--root", root, "uninstall", "node@22.99.99"); code != 5 {
		t.Fatalf("uninstall 也应受全局锁保护，得到 %d", code)
	}
}

func TestSplitToolVersion(t *testing.T) {
	tool, ver, err := splitToolVersion("node@22.22.2")
	if err != nil || tool != "node" || ver != "22.22.2" {
		t.Fatalf("splitToolVersion = %q/%q/%v", tool, ver, err)
	}
	for _, bad := range []string{"node", "@22", "node@"} {
		if _, _, err := splitToolVersion(bad); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
}

func TestInstallTargets(t *testing.T) {
	targets, err := installTargets([]string{"node@22", "bun"}, nil)
	if err != nil || len(targets) != 2 || targets[0].spec != "22" || targets[1].tool != "bun" {
		t.Fatalf("targets = %+v/%v", targets, err)
	}
	if _, err := installTargets([]string{""}, nil); err == nil {
		t.Fatal("空目标应报错")
	}
	targets, err = installTargets(nil, map[string]string{"node": "22", "npm": "auto", "pnpm": "10"})
	if err != nil || len(targets) != 2 {
		t.Fatalf("pk.toml 目标应跳过 npm: %+v/%v", targets, err)
	}
	if _, err := installTargets(nil, map[string]string{"npm": "auto"}); err == nil {
		t.Fatal("只有 npm 时应报错")
	}
}

func TestDoctorGoToolchainWarning(t *testing.T) {
	// 装了 go 但 GOTOOLCHAIN 不是 local → 必须告警（方案 §6.6：会多下一次工具链）
	root := t.TempDir()
	p, _ := paths.New(root)
	dir := p.ToolVersion("go", "1.27.3")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := &store.Ledger{}
	ledger.Upsert(store.Install{Tool: "go", Version: "1.27.3", Path: dir})
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOTOOLCHAIN", "")
	code, out, _ := execCLI(t, "--root", root, "--json", "doctor")
	if code != 0 {
		t.Fatalf("doctor 退出码 %d", code)
	}
	if !strings.Contains(out, "GO_TOOLCHAIN_NOT_LOCAL") {
		t.Fatalf("应告警 GOTOOLCHAIN 未设: %s", out)
	}
	if !strings.Contains(out, "pk env") {
		t.Fatalf("告警应给处置: %s", out)
	}

	t.Setenv("GOTOOLCHAIN", "local")
	_, out, _ = execCLI(t, "--root", root, "--json", "doctor")
	if strings.Contains(out, "GO_TOOLCHAIN_NOT_LOCAL") {
		t.Fatalf("local 时不该告警: %s", out)
	}
}

func TestCLIUseSwitchesVersion(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("安装失败: %d %s", code, errOut)
	}
	// 切到已装版本
	code, out, _ := execCLI(t, "--root", root, "use", "node@22.99.99")
	if code != 0 || !strings.Contains(out, "已切到 node@22.99.99") {
		t.Fatalf("use 输出异常: %d %q", code, out)
	}
	code, out, _ = execCLI(t, "--root", root, "--json", "list")
	if code != 0 || !strings.Contains(out, `"active": true`) {
		t.Fatalf("list 应显示 active: %d %q", code, out)
	}
	// 切到没装的版本 → 报错并给建议
	code, _, errOut := execCLI(t, "--root", root, "use", "node@1.0.0")
	if code != 1 || !strings.Contains(errOut, "还没装") {
		t.Fatalf("未装版本应报错: %d %q", code, errOut)
	}
	if code, _, _ := execCLI(t, "--root", root, "use", "node"); code != 2 {
		t.Fatalf("use 缺版本应退出 2，得到 %d", code)
	}
}

func TestCLIGCDryRunAndReal(t *testing.T) {
	fm := setupFakeMirror(t)
	// 第二个版本用来当"零引用"对象
	fm2 := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "24.99.99", NPMVersion: "11.20.0"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	_ = fm2
	root := t.TempDir()
	// 装两个版本：第一个成为默认，第二个零引用
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("装第一个: %d %s", code, errOut)
	}
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")

	code, out, errOut := execCLI(t, "--root", root, "--json", "gc", "--dry-run")
	if code != 0 {
		t.Fatalf("gc --dry-run: %d %s", code, errOut)
	}
	var doc struct {
		Schema       int      `json:"schema"`
		Removed      []string `json:"Removed"`
		CacheRemoved []string `json:"CacheRemoved"`
		KeptRef      []string `json:"KeptRef"`
		DryRun       bool     `json:"DryRun"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("gc --json 非法: %v\n%s", err, out)
	}
	if !doc.DryRun {
		t.Fatalf("应标记 dry-run: %s", out)
	}
	if len(doc.KeptRef) == 0 {
		t.Fatalf("默认版本应报告保留原因: %s", out)
	}
	// 真删：缓存上限设 1 字节，产物应被淘汰
	code, out, _ = execCLI(t, "--root", root, "gc", "--cache-max", "1")
	if code != 0 || !strings.Contains(out, "已回收") {
		t.Fatalf("gc 输出异常: %d %q", code, out)
	}
}

func TestCLIProjectLockAndFrozen(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	cwd := t.TempDir()
	writeProject := func(content string) {
		if err := os.WriteFile(filepath.Join(cwd, "pk.toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PK_CWD", cwd)
	writeProject("[tools]\nnode = \"22\"\nnpm = \"auto\"\n")

	code, out, errOut := execCLI(t, "--root", root, "install")
	if code != 0 {
		t.Fatalf("按 pk.toml 安装失败: %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "已更新 pk.lock") {
		t.Fatalf("应写出 pk.lock:\n%s", out)
	}
	lockData, err := os.ReadFile(filepath.Join(cwd, "pk.lock"))
	if err != nil {
		t.Fatalf("读 pk.lock: %v", err)
	}
	if !strings.Contains(string(lockData), "version = \"22.99.99\"") {
		t.Fatalf("锁里应有解析后的精确版本:\n%s", lockData)
	}

	// 清单改成不兼容的大版本 → --frozen 必须非 0
	writeProject("[tools]\nnode = \"24\"\n")
	code, _, errOut = execCLI(t, "--root", root, "install", "--frozen")
	if code != 1 || !strings.Contains(errOut, "不一致") {
		t.Fatalf("清单与锁不一致应退出 1: %d %q", code, errOut)
	}
	// 清单改回一致 → --frozen 通过（直接用锁里的版本，不做解析）
	writeProject("[tools]\nnode = \"22\"\nnpm = \"auto\"\n")
	code, out, errOut = execCLI(t, "--root", root, "install", "--frozen")
	if code != 0 {
		t.Fatalf("一致时 --frozen 应成功: %d\n%s\n%s", code, out, errOut)
	}
}

func TestCLIStartupSweep(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	p, _ := paths.New(root)
	stale := filepath.Join(p.Tool("node"), "9.9.9.tmp.dead")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, ts, ts); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := execCLI(t, "--root", root, "install", "node@22.99.99")
	if code != 0 {
		t.Fatalf("安装失败: %d %s", code, errOut)
	}
	if !strings.Contains(out, "清扫残留") {
		t.Fatalf("应报告清扫结果:\n%s", out)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("残留应被清掉")
	}
}

func TestCLIUninstallRepoints(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	if code, _, _ := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("安装失败")
	}
	if code, _, _ := execCLI(t, "--root", root, "install", "node@22.99.99", "--force"); code != 0 {
		t.Fatalf("强制重装失败")
	}
	// 卸载默认版本（--force）后，shim 与配置都应清理干净
	if code, _, errOut := execCLI(t, "--root", root, "uninstall", "node@22.99.99", "--force"); code != 0 {
		t.Fatalf("卸载失败: %d %s", code, errOut)
	}
	cfg, err := store.LoadConfig(filepath.Join(root, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools["node"] != "" {
		t.Fatalf("卸载默认版本后应清掉配置: %+v", cfg.Tools)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:            "0 B",
		512:          "512 B",
		1536:         "1.5 KB",
		47 * 1 << 20: "47.0 MB",
		5 << 30:      "5.0 GB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q，期望 %q", in, got, want)
		}
	}
}

func TestWriteCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "powershell"} {
		var buf bytes.Buffer
		if err := writeCompletion(&buf, shell); err != nil {
			t.Fatalf("%s 补全脚本生成失败: %v", shell, err)
		}
		if !strings.Contains(buf.String(), "install") {
			t.Fatalf("%s 补全里应包含命令名:\n%s", shell, buf.String())
		}
	}
	if err := writeCompletion(&bytes.Buffer{}, "fish"); err == nil {
		t.Fatal("不支持的 shell 应报错")
	}

	var out bytes.Buffer
	code := run([]string{"env", "--completion", "bash"}, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "complete -F _packnest pk") {
		t.Fatalf("pk env --completion bash = %d\n%s", code, out.String())
	}
}

func TestListAvailable(t *testing.T) {
	fm := setupFakeMirror(t)
	root := t.TempDir()
	// list --available 走索引（这里用 PK_MIRROR_NODE_INDEX 指向假镜像）
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")
	t.Setenv("PK_MIRROR_NPM_INDEX", fm.Server.URL+"/npm")
	t.Setenv("PK_MIRROR_PNPM_INDEX", fm.Server.URL+"/npm")
	code, out, errOut := execCLI(t, "--root", root, "--json", "list", "--available")
	if code != 0 {
		t.Fatalf("list --available 退出码 %d: %s", code, errOut)
	}
	if !strings.Contains(out, `"name": "node"`) || !strings.Contains(out, `"latest": "22.99.99"`) {
		t.Fatalf("应给出 node 的最新可用版本:\n%s", out)
	}
	// 非 JSON：人类可读列表
	code, out, _ = execCLI(t, "--root", root, "list", "--available")
	if code != 0 || !strings.Contains(out, "可装版本") {
		t.Fatalf("文本输出异常: %d %q", code, out)
	}
}

func TestInstallQuiet(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	code, out, errOut := execCLI(t, "--root", root, "install", "node@22.99.99", "--quiet")
	if code != 0 {
		t.Fatalf("--quiet 安装失败: %d %s", code, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("--quiet 时不该有标准输出: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "node", "22.99.99")); err != nil {
		t.Fatal("安静模式也要真装上")
	}
}

func TestDoctorForeign(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// 造一个假 proto 的 node 版本目录
	if err := os.MkdirAll(filepath.Join(home, ".proto", "tools", "node", "26.13.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	code, out, _ := execCLI(t, "--root", root, "--json", "doctor", "--foreign")
	if code != 0 {
		t.Fatalf("doctor --foreign 退出码 %d: %s", code, out)
	}
	if !strings.Contains(out, "FOREIGN_MANAGER") || !strings.Contains(out, "proto") {
		t.Fatalf("应列出 proto: %s", out)
	}
	if !strings.Contains(out, "26.13.0") {
		t.Fatalf("应列出版本号: %s", out)
	}
	// PATH 里 pk 之前有别人的 shim → 告警
	t.Setenv("PATH", filepath.Join(home, ".proto", "shims")+string(os.PathListSeparator)+filepath.Join(root, "bin"))
	_, out, _ = execCLI(t, "--root", root, "--json", "doctor", "--foreign")
	if !strings.Contains(out, "FOREIGN_SHIM_PRIORITY") {
		t.Fatalf("应告警 shim 优先级: %s", out)
	}
}

func TestScanForeignManagersEmpty(t *testing.T) {
	if got := scanForeignManagers(t.TempDir()); len(got) != 0 {
		t.Fatalf("空 home 应返回空: %+v", got)
	}
	if got := foreignPathConflicts("", "/x/bin"); len(got) != 0 {
		t.Fatalf("空 PATH 应返回空: %+v", got)
	}
}

func TestInPath(t *testing.T) {
	t.Setenv("PATH", "/a"+string(os.PathListSeparator)+"/b")
	if !inPath("/a") || inPath("/c") {
		t.Fatalf("inPath 判断错误")
	}
	t.Setenv("PATH", "")
	if inPath("/a") {
		t.Fatal("空 PATH 应返回 false")
	}
}

func TestEnvProject(t *testing.T) {
	fm := setupFakeMirror(t)
	root := t.TempDir()
	proj := t.TempDir()
	// 项目里锁定 node 22
	if err := os.WriteFile(filepath.Join(proj, "pk.toml"), []byte("[tools]\nnode = \"22.99.99\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PK_CWD", proj)
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("安装失败: %d %s", code, errOut)
	}
	_ = fm

	code, out, _ := execCLI(t, "--root", root, "env", "--project")
	if code != 0 {
		t.Fatalf("env --project 退出码 %d", code)
	}
	if !strings.Contains(out, "tools/node/22.99.99/bin") {
		t.Fatalf("项目 bin 应前置到 PATH:\n%s", out)
	}
	// 项目 bin 必须在最前
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "tools/node/22.99.99/bin") {
		t.Fatalf("项目路径应在 PATH 最前，实际第一行: %q", first)
	}
	// 没有 pk.toml 的目录：给提示且不报错
	t.Setenv("PK_CWD", t.TempDir())
	code, _, errOut := execCLI(t, "--root", root, "env", "--project")
	if code != 0 || !strings.Contains(errOut, "没有 pk.toml") {
		t.Fatalf("无项目时应提示: %d %q", code, errOut)
	}
}

func TestUseProjectPin(t *testing.T) {
	setupFakeMirror(t)
	root := t.TempDir()
	proj := t.TempDir()
	t.Setenv("PK_CWD", proj)
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("安装失败: %d %s", code, errOut)
	}
	// 固定到项目
	code, out, errOut := execCLI(t, "--root", root, "use", "node@22.99.99", "--project")
	if code != 0 {
		t.Fatalf("use --project 失败: %d %s", code, errOut)
	}
	if !strings.Contains(out, "已固定到项目") {
		t.Fatalf("输出异常: %q", out)
	}
	toml, err := os.ReadFile(filepath.Join(proj, "pk.toml"))
	if err != nil || !strings.Contains(string(toml), `node = "22.99.99"`) {
		t.Fatalf("pk.toml 未写入: %v %q", err, toml)
	}
	lock, err := os.ReadFile(filepath.Join(proj, "pk.lock"))
	if err != nil || !strings.Contains(string(lock), `version = "22.99.99"`) {
		t.Fatalf("pk.lock 未生成: %v %q", err, lock)
	}
	// 幂等：再固定一次，pk.toml 里只有一行 node
	if code, _, _ = execCLI(t, "--root", root, "use", "node@22.99.99", "--project"); code != 0 {
		t.Fatal("重复固定应成功")
	}
	toml, _ = os.ReadFile(filepath.Join(proj, "pk.toml"))
	if strings.Count(string(toml), "node = ") != 1 {
		t.Fatalf("pk.toml 出现重复键:\n%s", toml)
	}
	// --lock 只刷新锁
	if code, out, _ := execCLI(t, "--root", root, "use", "--lock"); code != 0 || !strings.Contains(out, "已更新 pk.lock") {
		t.Fatalf("use --lock 异常: %d %q", code, out)
	}
	// 没装过的版本不能固定
	if code, _, errOut := execCLI(t, "--root", root, "use", "node@1.0.0", "--project"); code != 1 || !strings.Contains(errOut, "还没装") {
		t.Fatalf("未装版本应拒绝: %d %q", code, errOut)
	}
}

// --force-rehash 是校验失败信息里给出的处置，必须真的存在（否则建议是空头支票）。
func TestCLIInstallForceRehashFlag(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.99.99"}},
		NPM:      map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
	})
	t.Setenv("PK_MIRROR_NODE_INDEX", fm.Server.URL+"/node/index.json")
	t.Setenv("PK_MIRROR_NODE_ARTIFACT", fm.Server.URL+"/node/v{version}/node-v{version}-{platform}.{ext}")
	t.Setenv("PK_MIRROR_NODE_CHECKSUM", fm.Server.URL+"/node/v{version}/SHASUMS256.txt")
	root := t.TempDir()

	// 先正常装一次，拿到真实 TOFU 记录，再把它改脏（模拟上游重新发布）。
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99"); code != 0 {
		t.Fatalf("首次安装应成功，得到 %d（%s）", code, errOut)
	}
	knownPath := filepath.Join(root, "state", "known.json")
	data, err := os.ReadFile(knownPath)
	if err != nil {
		t.Fatal(err)
	}
	var known map[string]any
	if err := json.Unmarshal(data, &known); err != nil {
		t.Fatal(err)
	}
	entries, _ := known["entries"].(map[string]any)
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		entry["digest"] = strings.Repeat("a", 64)
	}
	dirty, _ := json.Marshal(known)
	if err := os.WriteFile(knownPath, dirty, 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99", "--force")
	if code != 4 || !strings.Contains(errOut, "--force-rehash") {
		t.Fatalf("脏 TOFU 应退 4 并提示 --force-rehash，得到 %d（%s）", code, errOut)
	}
	if code, _, errOut := execCLI(t, "--root", root, "install", "node@22.99.99", "--force", "--force-rehash"); code != 0 {
		t.Fatalf("--force-rehash 应放行，得到 %d（%s）", code, errOut)
	}
}
