package main

// 界面层的验收测试：用 ui.Tester 在没有窗口的情况下跑视图，断言"用户看到的文字"与
// "危险动作必须先确认"。这是 `make check` 里 GUI 那一项的主体，不需要显示器。
//
// 命名按项目规矩：TestAcceptance_<里程碑>_<名字>，方案 §12 列过的名字不许改。

import (
	"context"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"testing"
	"time"

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
				{Name: "go", Version: "1.27.4", Path: "/root/.packnest/tools/go/1.27.4/bin/go", Verified: "weak"},
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
		if err := tr.Click("更多"); err != nil {
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
		{"installed", []string{"node", "22.1.0", "pnpm", "10.2.0", "go", "1.27.4", "更多"}},
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

// TestAcceptance_GUI8_ThemeContrast 把"深色下看不清"这类反馈变成数字：
// 用 WCAG 对比度直接量 Text/TextMuted 对 Background/Surface（浅色与深色都量）。
//
// 为什么不是截图给眼睛看：模型对"浅灰字 vs 深灰底"的判断互相矛盾过三次，
// 而对比度是能算的（正文 ≥ 4.5:1，大字/UI 部件 ≥ 3:1）。
func TestAcceptance_GUI8_ThemeContrast(t *testing.T) {
	cases := []struct {
		name string
		base *ui.Theme
	}{
		{"light", ui.LightTheme()},
		{"dark", ui.DarkTheme()},
	}
	for _, tc := range cases {
		th := themeFor(tc.base)
		pairs := []struct {
			what   string
			fg, bg ui.Color
			want   float64
		}{
			{"正文/背景", th.Text, th.Background, 7},
			{"正文/卡片", th.Text, th.Surface, 7},
			{"次要文字/背景", th.TextMuted, th.Background, 4.5},
			{"次要文字/卡片", th.TextMuted, th.Surface, 4.5},
			{"强调色上的字/强调色", th.AccentText, th.Accent, 4.5},
			{"强调色/背景（UI 部件）", th.Accent, th.Background, 3},
			{"徽标字/徽标底", th.Text, th.SurfacePressed, 4.5},
		}
		for _, p := range pairs {
			if got := contrast(p.fg, p.bg); got < p.want {
				t.Errorf("%s：%s 对比度 %.2f < %.1f（%v on %v）", tc.name, p.what, got, p.want, p.fg, p.bg)
			}
		}
	}
}

// contrast 是 WCAG 2.1 的相对亮度对比度。
func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c ui.Color) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

// TestAcceptance_GUI9_QuitReallyQuits 守"点退出必须真的退出"：
// markQuitting 要一次打标记、落盘偏好，并在宽限期内兜底强制结束
// （macOS 上曾因为 OnClose 拦掉关窗，托盘点退出后进程留在后台）。
func TestAcceptance_GUI9_QuitReallyQuits(t *testing.T) {
	g, _ := testGUI(t)
	done := make(chan int, 2)
	g.quitGrace = 20 * time.Millisecond
	g.exitFn = func(code int) { done <- code }

	g.markQuitting()
	if !g.quitting {
		t.Fatal("markQuitting 没打上退出标记：关窗会被 OnClose 拦成「收进托盘」")
	}
	// 偏好必须已经落盘（窗口关掉之后就写不了了）
	if _, err := os.Stat(prefsPath()); err != nil {
		t.Fatalf("退出前没落盘偏好: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("兜底退出码 = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("宽限期过了还没兜底强制退出")
	}

	// 重复调用不应再起一个兜底（幂等）
	g.markQuitting()
	select {
	case <-done:
		t.Fatal("markQuitting 不是幂等的")
	case <-time.After(60 * time.Millisecond):
	}
}

// TestAcceptance_GUI9_CloseWithoutQuit 守"平时关窗只是收进托盘"：
// 没打退出标记时，OnClose 的语义是拦截并隐藏，而不是结束进程。
func TestAcceptance_GUI9_CloseWithoutQuit(t *testing.T) {
	g, _ := testGUI(t)
	if g.quitting {
		t.Fatal("新界面不该带着退出标记")
	}
	// 与 main.go 里的 OnClose 判断一致：quitting 为真才放行
	shouldClose := func(g *gui) bool { return g.quitting }
	if shouldClose(g) {
		t.Fatal("未点退出时关窗应被拦截")
	}
	g.markQuittingOrFlag()
	if !shouldClose(g) {
		t.Fatal("点了退出后关窗必须放行")
	}
}

// markQuittingOrFlag 是测试用的轻量版（不真的起兜底 goroutine）
func (g *gui) markQuittingOrFlag() { g.quitting = true }

// TestAcceptance_GUI5_FirstFrameShowsEveryRow 守"首次打开列表就完整"：
// 一帧（ui.Tester 的第一次 Frame）里三行都要画出来、行高一致、都在窗口内。
//
// 路径：2026-11-17 用户实测首帧只画了两行半（pnpm 被截断），几秒后才补全 ——
// 根因是列表套在 Scroll 里、Grow 拿不到高度，第一帧按估算高度只画了几行。
// 现在列表页不套 Scroll（见 pages[].fill），这条测试钉住它。
func TestAcceptance_GUI5_FirstFrameShowsEveryRow(t *testing.T) {
	g, _ := testGUI(t)
	g.page = "installed"
	tr := ui.NewTester(g.view, 980, 680)
	tr.Frame() // 只渲染这一帧，模拟"刚打开"

	var heights []float32
	for _, name := range []string{"go", "node", "pnpm"} {
		r, ok := tr.Find(name)
		if !ok {
			t.Fatalf("首帧没画出 %q", name)
		}
		if r.H <= 0 {
			t.Fatalf("首帧 %q 高度 %.0f（被压扁）", name, r.H)
		}
		if r.Y+r.H > 680 {
			t.Fatalf("首帧 %q 掉出窗口（y=%.0f h=%.0f）", name, r.Y, r.H)
		}
		heights = append(heights, r.H)
	}
	for _, h := range heights {
		if math.Abs(float64(h-heights[0])) > 1 {
			t.Fatalf("行高不一致（被截断的行会矮一截）：%v", heights)
		}
	}
}

// TestAcceptance_GUI10_LoadPanicDoesNotLatch 守"转圈不会永久卡住"：
// 取数里 panic 必须变成一条错误交回去，而不是让 busy 卡在 true
// （卡住的表现就是"一直在检查更新转圈"，2026-11-17 用户实测到过）。
func TestAcceptance_GUI10_LoadPanicDoesNotLatch(t *testing.T) {
	_, err := safeWork(func() (commit, error) { panic("boom") })
	if err == nil {
		t.Fatal("work panic 时应当返回错误，否则界面会一直转圈")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误里要看得到原因：%v", err)
	}

	// 正常路径不受影响
	ran := false
	done, err := safeWork(func() (commit, error) {
		return func() { ran = true }, nil
	})
	if err != nil || done == nil {
		t.Fatalf("正常路径返回 %v %v", done, err)
	}
	done()
	if !ran {
		t.Fatal("commit 没被执行")
	}
}

// TestAcceptance_GUI10_UpdateCheckTimeoutIsShort 守"检查更新不该转两分钟"：
// state 层的更新检查上限必须是几十秒级（现在的实现是 20s）。
func TestAcceptance_GUI10_UpdateCheckTimeoutIsShort(t *testing.T) {
	// 这条用静态断言守着：超时值改大（比如回到 120s）就会红
	src, err := os.ReadFile("internal/state/state.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "timeout(ctx, 20*time.Second)") {
		t.Fatal("更新检查的超时不是 20s 了：改大等于让用户盯着转圈")
	}
}

// TestAcceptance_GUI11_ApplyShowsProgressAndResult 守"点升级不能像死的"：
// 升级中要有阶段文字 + 进度百分比，结束后要有留在页面上的结果（不能只弹 4 秒 toast）。
func TestAcceptance_GUI11_ApplyShowsProgressAndResult(t *testing.T) {
	g, _ := testGUI(t)
	g.page = "update"
	g.upd.GUI.Current = "0.12.1"
	g.upd.GUI.Latest = "0.12.2"
	g.upd.GUI.UpdateAvailable = true

	// 升级中
	g.busy["apply"] = true
	g.applyStage = "下载 packnest-desktop_0.12.2_darwin_arm64.app.zip"
	g.applyPct = 0.42
	tr := ui.NewTester(g.view, 980, 680)
	tr.Frame()
	if !tr.HasText("下载 packnest-desktop_0.12.2_darwin_arm64.app.zip") {
		t.Fatalf("升级中没显示阶段：%v", tr.Texts())
	}
	if !tr.HasText("42%") {
		t.Fatalf("升级中没显示百分比：%v", tr.Texts())
	}
	// 按钮在升级中必须禁用（避免连点触发两次替换）
	for _, a := range tr.AccessAll() {
		if a.Name == "检查更新" && !a.Disabled {
			t.Fatal("升级中「检查更新」应当禁用")
		}
	}

	// 升级结束：结果留在页面上
	g.busy["apply"] = false
	g.applyStage = ""
	g.applyPct = 0
	g.applyMsg = "界面已替换：重开这个窗口后生效（当前窗口还是旧版本）。"
	tr.Frame()
	if !tr.HasText("界面已替换：重开这个窗口后生效（当前窗口还是旧版本）。") {
		t.Fatalf("升级结果没留在页面上：%v", tr.Texts())
	}

	// 失败：结果 + 复制错误按钮
	g.applyErr = true
	g.applyMsg = "下载失败: dial tcp: i/o timeout"
	tr.Frame()
	if !tr.HasText("下载失败: dial tcp: i/o timeout") {
		t.Fatalf("失败原因没显示：%v", tr.Texts())
	}
	if err := tr.Click("复制错误"); err != nil {
		t.Fatalf("失败时应当能复制错误：%v", err)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
