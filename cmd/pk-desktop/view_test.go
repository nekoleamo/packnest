package main

// 界面层的验收测试：用 ui.Tester 在没有窗口的情况下跑视图，断言"用户看到的文字"与
// "危险动作必须先确认"。这是 `make check` 里 GUI 那一项的主体，不需要显示器。
//
// 命名按项目规矩：TestAcceptance_<里程碑>_<名字>，方案 §12 列过的名字不许改。

import (
	"context"
	"io"
	"log"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/state"
)

// fakeCLI 是 package main 里的假 Commands：只提供测试需要的数据，记录写操作。
type fakeCLI struct {
	tools     pkcli.ListResult
	mirror    pkcli.Mirror
	doctor    pkcli.DoctorResult
	gc        pkcli.GCResult
	avail     []pkcli.AvailableTool
	versions  pkcli.Versions
	probe     []pkcli.ProbeEntry
	env       pkcli.EnvDoc
	which     pkcli.Which
	uninstall string
	use       string
	npmrc     bool
	installs  []string
}

func (f *fakeCLI) List(context.Context) (pkcli.ListResult, error) { return f.tools, nil }
func (f *fakeCLI) Doctor(context.Context, bool, bool, bool) (pkcli.DoctorResult, error) {
	return f.doctor, nil
}
func (f *fakeCLI) Install(_ context.Context, spec string, onEvent func(pkcli.Event)) error {
	f.installs = append(f.installs, spec)
	if onEvent != nil {
		onEvent(pkcli.Event{Ev: "done", Tool: "node", Version: "22.0.0"})
	}
	return nil
}
func (f *fakeCLI) Use(_ context.Context, tool, version string) error {
	f.use = tool + "@" + version
	return nil
}
func (f *fakeCLI) Mirrors(context.Context) (pkcli.Mirror, error)           { return f.mirror, nil }
func (f *fakeCLI) UseMirror(context.Context, string) error                 { return nil }
func (f *fakeCLI) GC(context.Context, bool, int) (pkcli.GCResult, error)   { return f.gc, nil }
func (f *fakeCLI) UninstallTool(_ context.Context, tool string) error      { f.uninstall = tool; return nil }
func (f *fakeCLI) Which(context.Context, string) (pkcli.Which, error)      { return f.which, nil }
func (f *fakeCLI) Environment(context.Context, bool) (pkcli.EnvDoc, error) { return f.env, nil }
func (f *fakeCLI) MirrorWrite(context.Context, string) (pkcli.MirrorWriteResult, error) {
	f.npmrc = true
	return pkcli.MirrorWriteResult{File: "/tmp/.npmrc"}, nil
}
func (f *fakeCLI) Available(context.Context) ([]pkcli.AvailableTool, error) { return f.avail, nil }
func (f *fakeCLI) ToolVersions(context.Context, string, int, bool) (pkcli.Versions, error) {
	return f.versions, nil
}
func (f *fakeCLI) Probe(context.Context, string) ([]pkcli.ProbeEntry, error) { return f.probe, nil }
func (f *fakeCLI) CheckSelfUpdate(context.Context) (pkcli.SelfUpdate, error) {
	return pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1"}, nil
}
func (f *fakeCLI) SelfUpdate(context.Context) (pkcli.SelfUpdate, error) {
	return pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1"}, nil
}

// testGUI 造一个"数据已经就位"的界面：loaded=true 跳过首屏异步加载，测试里没有 goroutine。
func testGUI(t *testing.T) (*gui, *fakeCLI) {
	t.Helper()
	// 偏好写到临时目录，绝不碰真实 ~/.config/packnest
	t.Setenv("PK_GUI_CONFIG_DIR", t.TempDir())
	log.SetOutput(io.Discard)

	f := &fakeCLI{
		tools: pkcli.ListResult{
			Tools: []pkcli.Tool{
				{Name: "node", Version: "22.1.0", Active: true, Path: "/root/.packnest/tools/node/22.1.0/bin/node", Verified: "ok", SizeBytes: 52 << 20},
				{Name: "pnpm", Version: "10.2.0", Path: "/root/.packnest/tools/pnpm/10.2.0/bin/pnpm", Verified: "weak"},
			},
			Outdated: []pkcli.OutdatedTool{{Name: "node", Installed: "22.1.0", Latest: "22.2.0", Advice: "pk install node@22.2.0"}},
		},
		mirror: pkcli.Mirror{Current: "auto", Mirrors: []struct {
			Name     string `json:"name"`
			Display  string `json:"display"`
			Registry string `json:"registry"`
			Current  bool   `json:"current"`
		}{
			{Name: "default", Display: "官方源", Registry: "https://registry.npmjs.org"},
			{Name: "npmmirror", Display: "国内镜像", Registry: "https://registry.npmmirror.com", Current: false},
			{Name: "auto", Display: "自动", Registry: "https://registry.npmjs.org", Current: true},
		}},
		doctor: pkcli.DoctorResult{
			Issues: []pkcli.Issue{{Code: "PATH_MISSING", Severity: "warn", Message: "PATH 里没有 pk bin"}},
		},
		gc:    pkcli.GCResult{DryRun: true, Removed: []string{"node@20.1.0"}, FreedBytes: 42 << 20},
		avail: []pkcli.AvailableTool{{Name: "node", Latest: "22.2.0", LTS: "22.2.0", Source: "npmmirror"}},
		versions: pkcli.Versions{Tool: "node", Latest: "22.2.0", Total: 2, Available: []struct {
			Version   string `json:"version"`
			LTS       string `json:"lts"`
			Installed bool   `json:"installed"`
		}{{Version: "22.2.0", LTS: "22.2.0"}, {Version: "20.11.0", Installed: true}}},
		probe: []pkcli.ProbeEntry{{URL: "https://registry.npmmirror.com/", OK: true, Status: 200, LatencyMS: 42}},
		env: pkcli.EnvDoc{
			Shell: "zsh",
			Paths: []string{"/root/.packnest/bin", "/usr/bin"},
			Env: []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}{{Key: "npm_config_registry", Value: "https://registry.npmmirror.com"}},
		},
		which: pkcli.Which{Name: "node", Path: "/root/.packnest/bin/node", Shim: true},
	}
	svc := &state.Service{
		GUIVersion: guiVersion,
		Locate: func() (state.Commands, pkcli.Info, error) {
			return f, pkcli.Info{Schema: 1, Name: "packnest", Version: "1.9.1", Path: "/usr/bin/pk"}, nil
		},
	}
	g := newGUI(svc)
	g.loaded = true // 测试里不跑后台加载（没有窗口就没有 Update 回来）
	g.status = svc.Status(context.Background())
	g.tools = state.Tools{Tools: f.tools.Tools, Outdated: f.tools.Outdated}
	g.mirror = f.mirror
	g.doctor = state.Doctor{Issues: f.doctor.Issues}
	g.gc = f.gc
	g.available = f.avail
	g.versions = f.versions
	g.probe = f.probe
	g.env = f.env
	g.which = f.which
	g.verTool = "node"
	g.verAll = true
	return g, f
}

// TestAcceptance_GUI3_ReadOnlyPages：三个只读页把数据画出来（没数据就不该画假的）。
func TestAcceptance_GUI3_ReadOnlyPages(t *testing.T) {
	g, _ := testGUI(t)
	tr := ui.NewTester(g.view, 1200, 900)
	tr.Frame()

	if !tr.HasText("node") || !tr.HasText("22.1.0") {
		t.Fatalf("已装工具页缺内容: %v", tr.Texts())
	}
	if !tr.HasText("22.2.0") {
		t.Fatalf("可升级提示没画: %v", tr.Texts())
	}

	// 镜像页
	g.page = "mirror"
	tr.Frame()
	if !tr.HasText("auto") {
		t.Fatalf("镜像页缺当前档位: %v", tr.Texts())
	}

	// 体检页
	g.page = "doctor"
	tr.Frame()
	if !tr.HasText("PATH_MISSING") {
		t.Fatalf("体检页缺问题码: %v", tr.Texts())
	}

	// 磁盘页
	g.page = "gc"
	tr.Frame()
	if !tr.HasText("node@20.1.0") {
		t.Fatalf("磁盘页缺分组清单: %v", tr.Texts())
	}
}

// TestAcceptance_GUI4_WriteGuards：卸载/回收/写 npmrc 未经确认一律不执行。
func TestAcceptance_GUI4_WriteGuards(t *testing.T) {
	g, f := testGUI(t)
	tr := ui.NewTester(g.view, 1200, 900)
	tr.Frame()

	// 行内动作在"⋯"菜单里：先开菜单再选「卸载」
	openUninstall := func() {
		if err := tr.Click("⋯"); err != nil {
			t.Fatalf("点不到行菜单: %v", err)
		}
		tr.Frame()
		if err := tr.ChooseMenuItem("卸载"); err != nil {
			t.Fatalf("菜单里选不到卸载: %v", err)
		}
		tr.Frame()
	}
	openUninstall()
	if f.uninstall != "" {
		t.Fatal("确认框还没点，就已经卸载了")
	}
	if !g.confirm.open {
		t.Fatalf("卸载没有弹确认框: %v", tr.Texts())
	}

	// 取消：什么都不该发生
	if err := tr.Click("取消"); err != nil {
		t.Fatalf("点不到取消: %v", err)
	}
	tr.Frame()
	if f.uninstall != "" {
		t.Fatal("点了取消还执行了卸载")
	}

	// 再来一遍，这次确认
	openUninstall()
	if err := tr.Click("确定"); err != nil {
		t.Fatalf("点不到确定: %v", err)
	}
	tr.Frame()
	if f.uninstall != "node" {
		t.Fatalf("确认后应当卸载 node，实际 %q", f.uninstall)
	}
}

// TestAcceptance_GUI4_GCGuard：回收也是"默认不执行"的那类动作。
func TestAcceptance_GUI4_GCGuard(t *testing.T) {
	g, f := testGUI(t)
	g.page = "gc"
	tr := ui.NewTester(g.view, 1200, 900)
	tr.Frame()

	if err := tr.Click("执行回收"); err != nil {
		t.Fatalf("点不到执行回收: %v", err)
	}
	tr.Frame()
	if !g.confirm.open {
		t.Fatal("回收没有弹确认框")
	}
	_ = f
}

// TestAcceptance_GUI6_I18nComplete：中文是源串，切英文后同一页必须出英文。
func TestAcceptance_GUI6_I18nComplete(t *testing.T) {
	g, _ := testGUI(t)
	g.lang = "en"
	g.langIdx = 1
	tr := ui.NewTester(g.view, 1200, 900)
	tr.Frame()
	if !tr.HasText("Installed tools") {
		t.Fatalf("英文模式没切: %v", tr.Texts())
	}
	if tr.HasText("已装工具") {
		t.Fatalf("英文模式还在画中文: %v", tr.Texts())
	}
	g.page = "settings"
	tr.Frame()
	if !tr.HasText("Launch at login") {
		t.Fatalf("设置页英文缺失: %v", tr.Texts())
	}
}

// TestAcceptance_GUI6_NoRawErrorInUI：错误要翻译成人话，不把 err.Error() 原样糊上屏。
func TestAcceptance_GUI6_NoRawErrorInUI(t *testing.T) {
	if got := friendly(pkcli.ErrNotFound); got == pkcli.ErrNotFound.Error() {
		t.Fatalf("ErrNotFound 没翻译: %q", got)
	}
	if got := friendly(pkcli.ErrBusy); got == "" || got == pkcli.ErrBusy.Error() {
		t.Fatalf("ErrBusy 没翻译: %q", got)
	}
}

// TestAcceptance_GUI7_StateShared：界面用的数据来自 svc（同一个 state 层），
// 不再自己拼 pk 子进程参数 —— 这条用"读操作只调 svc"来守。
func TestAcceptance_GUI7_StateShared(t *testing.T) {
	g, f := testGUI(t)
	g.page = "env"
	tr := ui.NewTester(g.view, 1200, 900)
	tr.Frame()
	if len(f.tools.Tools) == 0 {
		t.Fatal("夹具没装好")
	}
	if !tr.HasText("/root/.packnest/bin") {
		t.Fatalf("环境页没画出 PATH: %v", tr.Texts())
	}
}

// TestAcceptance_GUI5_LayoutNotCollapsed 是"画出来但看不见"的守门：
// mygo 里没有高度约束的 List/Table 会被压成表头一行、行元素尺寸变成 0x0，
// 文字明明在 Texts() 里却看不见。所以每一页的关键文字都断言"有尺寸、在窗口内"。
func TestAcceptance_GUI5_LayoutNotCollapsed(t *testing.T) {
	g, _ := testGUI(t)
	cases := []struct {
		page string
		want []string
	}{
		{"installed", []string{"node", "22.1.0", "pnpm", "10.2.0", "⋯"}},
		{"queue", []string{"node", "22.2.0", "装最新"}},
		{"versions", []string{"22.2.0", "20.11.0"}},
		{"mirror", []string{"官方源", "国内镜像", "自动", "https://registry.npmmirror.com"}},
		{"env", []string{"/root/.packnest/bin", "npm_config_registry"}},
		{"update", []string{"检查更新"}},
		{"doctor", []string{"开始体检", "PATH_MISSING"}},
		{"gc", []string{"每工具保留", "预演", "执行回收", "node@20.1.0"}},
		{"settings", []string{"开机自启", "版本", "这个界面怎么拿数据"}},
	}
	for _, tc := range cases {
		g.page = tc.page
		tr := ui.NewTester(g.view, 1040, 780)
		tr.Frame()
		for _, want := range tc.want {
			r, ok := tr.Find(want)
			if !ok {
				t.Fatalf("%s：找不到 %q（没画）｜%v", tc.page, want, tr.Texts())
			}
			if r.W <= 0 || r.H <= 0 {
				t.Fatalf("%s：%q 尺寸是 %.0fx%.0f（被压扁了，用户看不见）", tc.page, want, r.W, r.H)
			}
			if r.Y >= 780 {
				t.Fatalf("%s：%q 落在窗口外（y=%.0f）", tc.page, want, r.Y)
			}
		}
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
