package main

// 根视图：顶部状态条 + 左侧导航 + 右侧页面 + 全局提示。
//
// 视图是"状态的函数"：每帧从 gui 的字段重新画一遍，所以状态只放在 gui 里，
// 不放任何 ui 元素（元素只能活在当前帧）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/autostart"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/state"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/update"
)

// page 是一页的登记项：id 同时是侧边栏的选项 ID 与偏好里记的值。
type page struct {
	id    string
	zh    string
	en    string
	build func(g *gui, c *ui.Context)
}

var pages = []page{
	{"installed", "已装工具", "Installed", (*gui).pageInstalled},
	{"queue", "安装", "Install", (*gui).pageQueue},
	{"mirror", "镜像", "Mirrors", (*gui).pageMirror},
	{"env", "环境", "Environment", (*gui).pageEnv},
	{"update", "更新", "Update", (*gui).pageUpdate},
	{"doctor", "体检", "Doctor", (*gui).pageDoctor},
	{"gc", "磁盘", "Storage", (*gui).pageGC},
	{"settings", "设置", "Settings", (*gui).pageSettings},
	// 只在安装页里跳转进来，不画在侧边栏
	{"versions", "可选版本", "Versions", (*gui).pageVersions},
}

// queueItem 是安装队列的一行：一个 spec 一条，状态机 wait → run → done/fail。
type queueItem struct {
	Spec    string
	State   string // wait / run / done / fail
	Msg     string
	Percent float64
	Tool    string
	Version string
}

// gui 是界面的全部状态。
type gui struct {
	svc *state.Service
	win *mygo.Window

	// 首屏
	loaded bool
	status state.Status
	tools  state.Tools

	// 当前页
	page string

	// 各页数据（每页自己刷，互不阻塞）
	available []pkcli.AvailableTool
	versions  pkcli.Versions
	verTool   string
	verAll    bool
	verFilter string
	mirror    pkcli.Mirror
	probe     []pkcli.ProbeEntry
	probeTool string
	env       pkcli.EnvDoc
	envProj   bool
	whichCmd  string
	which     pkcli.Which
	doctor    state.Doctor
	dGlobals  bool
	dProbe    bool
	gc        pkcli.GCResult
	gcKeep    int
	upd       update.Result
	auto      autostart.Status

	// 安装队列
	queue      []*queueItem
	queueInput string

	// 界面偏好与临时状态
	themeMode int    // 0 跟随系统 / 1 浅色 / 2 深色
	lang      string // zh / en
	langIdx   int    // Segmented 绑定的下标（0 中文 / 1 EN）
	busy      map[string]bool
	flash     string
	flashErr  bool
	confirm   confirmBox

	// 列表状态必须跨帧存活（滚动位置、选择、列宽）
	tables map[string]*ui.ListState
	// open 是折叠组的开关（gc 的分组清单）
	open map[string]*bool
}

// confirmBox 是"这一步会改你机器"的统一确认框：open=true 才画，yes 由各页消费。
type confirmBox struct {
	open    bool
	title   string
	message string
	action  func()
}

func newGUI(svc *state.Service) *gui {
	g := &gui{
		svc:    svc,
		page:   "installed",
		busy:   map[string]bool{},
		tables: map[string]*ui.ListState{},
		lang:   "zh",
		gcKeep: 0,
	}
	g.prefsLoad()
	return g
}

// table 返回某个列表的持久状态（每页/每个表一个 key）。
func (g *gui) table(key string) *ui.ListState {
	if g.tables[key] == nil {
		g.tables[key] = &ui.ListState{}
	}
	return g.tables[key]
}

// view 是窗口的内容函数，每帧被调用一次。
func (g *gui) view(c *ui.Context) {
	g.applyTheme(c)
	if !g.loaded {
		g.loaded = true
		g.refreshAll()
	}
	if g.flash != "" {
		msg := g.flash
		g.flash = ""
		if g.flashErr {
			c.Toast(msg)
		} else {
			c.Toast(msg)
		}
	}

	ui.Column(c).Fill().Children(func() {
		g.header(c)
		ui.Divider(c)
		ui.Row(c).Fill().Grow(1).Children(func() {
			ui.Sidebar(c, &g.page, func() {
				ui.SidebarSection(c, g.t("工具", "Tools"), nil, func() {
					ui.SidebarItem(c, "installed", nil, g.t("已装工具", "Installed")).Children(func() {
						if n := len(g.tools.Outdated); n > 0 {
							ui.Badge(c, fmt.Sprint(n))
						}
					})
					ui.SidebarItem(c, "queue", nil, g.t("安装", "Install")).Children(func() {
						if n := g.queuePending(); n > 0 {
							ui.Badge(c, fmt.Sprint(n))
						}
					})
					ui.SidebarItem(c, "mirror", nil, g.t("镜像", "Mirrors"))
					ui.SidebarItem(c, "env", nil, g.t("环境", "Environment"))
				})
				ui.SidebarSection(c, g.t("维护", "Maintenance"), nil, func() {
					ui.SidebarItem(c, "update", nil, g.t("更新", "Update"))
					ui.SidebarItem(c, "doctor", nil, g.t("体检", "Doctor")).Children(func() {
						if n := len(g.doctor.Issues); n > 0 {
							ui.Badge(c, fmt.Sprint(n))
						}
					})
					ui.SidebarItem(c, "gc", nil, g.t("磁盘", "Storage"))
					ui.SidebarItem(c, "settings", nil, g.t("设置", "Settings"))
				})
			}).Width(190)
			ui.Scroll(c).Fill().Grow(1).Children(func() {
				ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
					g.pageView(c)
				})
			})
		})
	})

	g.confirmView(c)
}

// pageView 派发到当前页。
func (g *gui) pageView(c *ui.Context) {
	for _, p := range pages {
		if p.id == g.page {
			p.build(g, c)
			return
		}
	}
	g.pageInstalled(c)
}

// header 是顶部状态条：CLI 状态 + 语言 + 主题 + 刷新。
func (g *gui) header(c *ui.Context) {
	ui.Row(c).Fill().Padding(12, 16).Gap(12).Children(func() {
		ui.Text(c, "packnest").FontSize(18).Bold()
		if g.status.OK {
			ui.Badge(c, "CLI "+g.status.CLI.Version)
			ui.Text(c, g.status.Path).TextColor(c.Theme().TextMuted).SingleLine()
		} else {
			ui.Badge(c, g.t("未装 CLI", "CLI missing"))
		}
		ui.Row(c).Grow(1).Children(func() {}) // 弹性空隙，把右侧控件推到右边
		if g.busy["refresh"] {
			ui.Spinner(c)
		}
		if ui.Button(c, g.t("刷新", "Refresh")).Clicked() {
			g.refreshAll()
		}
		lang := g.lang
		ui.Segmented(c, &g.langIdx, "中文", "EN")
		if gotLang := langOf(g.langIdx); gotLang != lang {
			g.lang = gotLang
			g.prefsSave()
		}
		ui.Segmented(c, &g.themeMode, g.t("跟随系统", "System"), g.t("浅色", "Light"), g.t("深色", "Dark"))
	}).Height(56)
	if g.themeMode != themeModeSeen {
		themeModeSeen = g.themeMode
		g.prefsSave()
	}

	// 没装 CLI 时给"对应平台"的安装命令（Windows 用户不该被塞一条 sh 命令）
	if !g.status.OK {
		ui.Row(c).Fill().Padding(0, 16, 12, 16).Gap(8).Children(func() {
			ui.Text(c, g.t("首次使用先装 CLI：", "Install the CLI first:")).TextColor(c.Theme().Warning)
			ui.Text(c, installCmd(g.status.Platform)).Font("monospace").SingleLine()
			if ui.Button(c, g.t("复制", "Copy")).Clicked() {
				mygo.Clipboard.WriteText(installCmd(g.status.Platform))
				g.flashNow(g.t("已复制", "Copied"))
			}
		})
	}
}

// themeModeSeen 用来在 Segmented 改值后落盘偏好（Segmented 直接写 &g.themeMode）。
var themeModeSeen = 0

// langOf 把 Segmented 的下标换成语言代码。
func langOf(idx int) string {
	if idx == 1 {
		return "en"
	}
	return "zh"
}

// applyTheme 把主题三态落到 ui 上：默认跟随系统，手动选浅/深时换主题。
func (g *gui) applyTheme(c *ui.Context) {
	base := c.Theme()
	switch g.themeMode {
	case 1:
		base = ui.LightTheme()
	case 2:
		base = ui.DarkTheme()
	}
	t := *base
	t.Accent = ui.Hex("#0a7d4f")
	if t.Dark {
		t.Accent = ui.Hex("#4ade80")
	}
	c.SetTheme(&t)
}

// confirmView 画统一的确认框；action 只在用户点"确定"时执行。
func (g *gui) confirmView(c *ui.Context) {
	if !g.confirm.open {
		return
	}
	switch ui.AlertDialog(c, &g.confirm.open, g.confirm.title, g.confirm.message,
		g.t("取消", "Cancel"), g.t("确定", "Continue")) {
	case 1:
		act := g.confirm.action
		g.confirm.action = nil
		if act != nil {
			act()
		}
	}
}

// ask 弹确认框（会改用户机器/文件的操作必须先过这一关）。
func (g *gui) ask(title, msg string, action func()) {
	g.confirm = confirmBox{open: true, title: title, message: msg, action: action}
}

// refreshAll 并行刷新首屏三份数据（状态 / 已装 / 可装版本）。
func (g *gui) refreshAll() {
	g.load("refresh", func() (commit, error) {
		st := g.svc.Status(context.Background())
		auto := g.svc.Autostart()
		if !st.OK {
			return func() { g.status, g.auto = st, auto }, nil
		}
		tools, err := g.svc.Tools(context.Background())
		if err != nil {
			return func() { g.status, g.auto = st, auto }, err
		}
		return func() { g.status, g.auto, g.tools = st, auto, tools }, nil
	})
}

// commit 是"后台算完、回主线程落值"的那一下：改 gui 的字段必须放在它里面。
type commit func()

// load 是"后台跑一次、回主线程收尾"的统一入口。
//
// key 相同的一次只跑一个（用户连点刷新不会起 10 个 pk 子进程）。
// work 在 goroutine 里跑、可以阻塞，但**不许碰 gui 的字段**：它只返回一个 commit，
// 由主线程执行——视图永远只在主线程读状态，这是 mygo 的线程模型（也是 -race 能过的前提）。
func (g *gui) load(key string, work func() (commit, error)) {
	if g.busy[key] {
		return
	}
	g.busy[key] = true
	go func() {
		done, err := work()
		if g.win == nil { // 测试里没有窗口：不落值，由测试自己摆数据
			return
		}
		g.onMain(func() {
			delete(g.busy, key)
			if err != nil {
				g.flashErr = true
				g.flash = friendly(err)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

// friendly 把错误翻译成用户能懂的话（退出码 5 = 另一个 pk 正在运行）。
func friendly(err error) string {
	switch {
	case err == nil:
		return ""
	case err == pkcli.ErrNotFound:
		return "找不到 pk：这个界面需要先安装 CLI"
	case err == pkcli.ErrBusy:
		return "另一个 pk 正在运行，稍后再试"
	}
	return err.Error()
}

// onMain 把一段改状态的代码丢回主线程执行（测试里没有窗口时直接跑）。
//
// mygo 的约定：视图只在主线程读状态，所以任何后台 goroutine 改字段都必须过这里。
func (g *gui) onMain(fn func()) {
	if g.win == nil {
		fn()
		return
	}
	g.win.Update(fn)
}

// flashNow 直接在主线程弹一条提示。
func (g *gui) flashNow(msg string) {
	g.flashErr = false
	g.flash = msg
}

// t 是界面文案的唯一入口：中文即源串，英文内联（不用 key 表，改文案即改这里）。
func (g *gui) t(zh, en string) string {
	if g.lang == "en" {
		return en
	}
	return zh
}

// installCmd 给没装 CLI 的用户一条"对应平台"的命令。
func installCmd(platform string) string {
	switch platform {
	case "windows":
		return `powershell -c "irm https://nekoleamo.github.io/packnest/install.ps1 | iex"`
	default:
		return "curl -fsSL https://nekoleamo.github.io/packnest/install.sh | sh"
	}
}

// 偏好文件：只存"界面自己的"偏好（主题/语言/上次的页），不碰 pk 的任何配置。
type prefs struct {
	Theme int    `json:"theme"`
	Lang  string `json:"lang"`
	Page  string `json:"page"`
}

func prefsPath() string { return filepath.Join(appDir(), "gui.json") }

func (g *gui) prefsLoad() {
	b, err := os.ReadFile(prefsPath())
	if err != nil {
		return
	}
	var p prefs
	if json.Unmarshal(b, &p) != nil {
		return
	}
	g.themeMode = p.Theme
	themeModeSeen = p.Theme
	if p.Lang == "en" || p.Lang == "zh" {
		g.lang = p.Lang
	}
	for _, pg := range pages {
		if pg.id == p.Page {
			g.page = p.Page
		}
	}
	g.langIdx = 0
	if g.lang == "en" {
		g.langIdx = 1
	}
}

func (g *gui) prefsSave() {
	if err := os.MkdirAll(appDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(prefs{Theme: g.themeMode, Lang: g.lang, Page: g.page})
	if err != nil {
		return
	}
	_ = os.WriteFile(prefsPath(), b, 0o644)
}

// queuePending 是"还没跑完的 spec 数"（侧边栏徽标）。
func (g *gui) queuePending() int {
	n := 0
	for _, q := range g.queue {
		if q.State == "wait" || q.State == "run" {
			n++
		}
	}
	return n
}

// humanBytes 把字节数写成人读的（GC 统计块用）。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// since 是"多久以前"，用于首屏的时间戳。
func since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t).Truncate(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return d.Truncate(time.Minute).String()
}

// platformHint 在设置页显示（排障时第一句要问的）。
func platformHint() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
