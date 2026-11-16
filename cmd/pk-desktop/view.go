package main

// 根视图：顶部状态条 + 左侧导航 + 右侧页面 + 全局提示。
//
// 视图是"状态的函数"：每帧从 gui 的字段重新画一遍，所以状态只放在 gui 里，
// 不放任何 ui 元素（元素只能活在当前帧）。

import (
	"context"
	"errors"
	"fmt"
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
	// fill=true：整页就是一张列表（List 自己滚动），不要套外层 Scroll。
	// 套了会出"首帧只画两三行、过几秒才对"的毛病：Scroll 里没有高度约束，
	// 列表先用估算高度画了一帧，等下一次 Update 才收敛（用户实测到的那个截图）。
	fill bool
}

var pages = []page{
	{"installed", "已装工具", "Installed", (*gui).pageInstalled, true},
	{"queue", "安装", "Install", (*gui).pageQueue, true},
	{"mirror", "镜像", "Mirrors", (*gui).pageMirror, false},
	{"env", "环境", "Environment", (*gui).pageEnv, false},
	{"update", "更新", "Update", (*gui).pageUpdate, false},
	{"doctor", "体检", "Doctor", (*gui).pageDoctor, false},
	{"gc", "磁盘", "Storage", (*gui).pageGC, false},
	{"settings", "设置", "Settings", (*gui).pageSettings, false},
	// 只在安装页里跳转进来，不画在侧边栏
	{"versions", "可选版本", "Versions", (*gui).pageVersions, true},
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

	// quitting 是"正在退出"：关窗口时要放行（有托盘时平时是收进托盘）
	quitting bool
	// relayout 记"首屏数据已经到了、还要再要一帧"（首帧视口没生效时会少画几行）
	relayout bool
	// quitGrace / exitFn 是退出兜底的注入点（测试用；生产是 3s + os.Exit）
	quitGrace time.Duration
	exitFn    func(int)

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
	doctorRan bool
	dGlobals  bool
	dProbe    bool
	gc        pkcli.GCResult
	gcKeep    int
	upd       update.Result
	auto      autostart.Status

	// 安装队列
	queue      []*queueItem
	queueInput string

	// clipboard 是测试用：没有窗口时 copyText 记在这里（不碰真实剪贴板）
	clipboard string

	// 自升级的进度与结果（点「升级界面」之后页面上要有东西可看）
	applyStage string
	applyPct   float64
	applyMsg   string
	applyErr   bool

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
	// visited 记"这一页已经拉过一次数"（进页懒加载）
	visited map[string]bool
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
		c.Toast(msg)
	}
	if g.page != pageSeen {
		pageSeen = g.page
		g.prefsSave()
	}
	// 首屏数据到位的这一帧之后再要一帧：第一帧是按"视口还没生效"的高度算的，
	// 只画得出两三行，看起来像列表少了一半（2026-11-17 用户实测）。
	if g.relayout {
		g.relayout = false
		c.Invalidate()
	}

	ui.Column(c).Fill().Children(func() {
		g.header(c)
		ui.Divider(c)
		// Stretch：侧边栏要整列铺满（默认 AlignItems 是 Center，会把它挤到竖直中间）
		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			// 平铺而不是分组：mygo 的分组标题没法调样式，加一层反而看不出层级
			ui.Sidebar(c, &g.page, func() {
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
				ui.SidebarItem(c, "update", nil, g.t("更新", "Update"))
				ui.SidebarItem(c, "doctor", nil, g.t("体检", "Doctor")).Children(func() {
					if n := len(g.doctor.Issues); n > 0 {
						ui.Badge(c, fmt.Sprint(n))
					}
				})
				ui.SidebarItem(c, "gc", nil, g.t("磁盘", "Storage"))
				ui.SidebarItem(c, "settings", nil, g.t("设置", "Settings"))
			}).Width(150)
			if g.pageFills() {
				// 列表页：不套 Scroll，让 List 自己占满并滚动（首帧就画对，不会先画两三行）
				ui.Column(c).Fill().Grow(1).Padding(12, 14).Gap(6).Children(func() {
					g.pageView(c)
				})
			} else {
				ui.Scroll(c).Fill().Grow(1).Children(func() {
					// 只填宽不填高：放在 Scroll 里时 Fill() 会把高度钉成视口高度，
					// 后面的表格就只剩表头可见（2026-11-15 实测踩到）
					ui.Column(c).FillWidth().Padding(12, 14).Gap(6).Children(func() {
						g.pageView(c)
					})
				})
			}
		})
	})

	g.confirmView(c)
}

// pageFills 判断当前页是不是"整页一张列表"（决定要不要套外层 Scroll）。
func (g *gui) pageFills() bool {
	for _, p := range pages {
		if p.id == g.page {
			return p.fill
		}
	}
	return false
}

// pageView 派发到当前页；第一次进某页时顺手把它的数据拉一次（每页只拉一次）。
func (g *gui) pageView(c *ui.Context) {
	g.ensureLoaded(g.page)
	for _, p := range pages {
		if p.id == g.page {
			p.build(g, c)
			return
		}
	}
	g.pageInstalled(c)
}

// refreshPage 重跑当前页的取数（顶栏刷新与进页懒加载共用）。
func (g *gui) refreshPage() {
	switch g.page {
	case "mirror":
		g.loadMirrors("")
	case "env":
		g.loadEnv(g.envProj)
	case "gc":
		g.runGC(false)
	case "update":
		g.checkUpdate() // 进页就查：空白页比"多一次请求"难看得多
	case "queue", "versions":
		g.reloadAvailable()
	case "installed":
		g.reloadTools()
	}
}

// ensureLoaded 是"进页就取数"的懒加载：镜像档、环境、磁盘预演、可装版本、更新检查各拉一次。
// 只有"会真的联网探测"的两个（源探测、体检）留给用户自己点 —— 它们慢且没必要自动跑。
func (g *gui) ensureLoaded(id string) {
	if g.visited == nil {
		g.visited = map[string]bool{}
	}
	if g.visited[id] {
		return
	}
	g.visited[id] = true
	switch id {
	case "mirror":
		g.loadMirrors("")
	case "env":
		g.loadEnv(g.envProj)
	case "gc":
		g.runGC(false)
	case "update":
		g.checkUpdate() // 进页就查：空白页比"多一次请求"难看得多
	case "queue", "versions":
		g.reloadAvailable()
	}
}

// header 是顶部状态条：CLI 状态 + 语言 + 主题 + 刷新。
//
// 只放"全局"的东西：每一页自己的刷新/操放在页标题行上，避免同一屏两个「刷新」。
func (g *gui) header(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(6, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "packnest").FontSize(15).Bold()
		if g.status.OK && g.status.CLI != nil {
			ui.Text(c, "CLI "+g.status.CLI.Version).TextColor(c.Theme().TextMuted)
			ui.Text(c, g.status.Path).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
		} else {
			ui.Badge(c, g.t("未装 CLI", "CLI missing"))
			ui.Row(c).Grow(1).Children(func() {})
		}
		if g.busy["refresh"] {
			ui.Spinner(c)
		}
		ui.Button(c, g.t("刷新", "Refresh")).FontSize(12).OnClick(g.refreshAll)
		lang := g.lang
		ui.Segmented(c, &g.langIdx, "中文", "EN")
		if gotLang := langOf(g.langIdx); gotLang != lang {
			g.lang = gotLang
			g.prefsSave()
		}
		ui.Segmented(c, &g.themeMode, g.t("跟随", "Auto"), g.t("浅", "Light"), g.t("深", "Dark"))
	}).Height(40)
	if g.themeMode != themeModeSeen {
		themeModeSeen = g.themeMode
		g.prefsSave()
	}

	// 没装 CLI 时给"对应平台"的安装命令（Windows 用户不该被塞一条 sh 命令）
	if !g.status.OK {
		ui.Row(c).FillWidth().Padding(0, 16, 12, 16).Gap(8).Children(func() {
			ui.Text(c, g.t("首次使用先装 CLI：", "Install the CLI first:")).TextColor(c.Theme().Warning)
			ui.Text(c, installCmd(g.status.Platform)).Font("monospace").SingleLine()
			if ui.Button(c, g.t("复制", "Copy")).Clicked() {
				mygo.Clipboard.WriteText(installCmd(g.status.Platform))
				g.flashNow(g.t("已复制", "Copied"))
			}
		})
	}
}

// themeModeSeen / pageSeen 用来在控件直接改了绑定的值之后落盘偏好
// （Segmented 写 &g.themeMode、Sidebar 写 &g.page，都不经过我们的函数）。
var (
	themeModeSeen = 0
	pageSeen      = ""
)

// 主题按 (模式, 系统深浅) 缓存：视图每帧都要它，没必要每帧重新拼一遍调色板。
var themeCache = map[[2]int]*ui.Theme{}

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

// refreshAll 刷新顶栏状态 + 已装清单，并重跑"当前页"的取数。
//
// 各页不再放自己的刷新按钮：一个刷新入口，点了就把眼前这页也刷了。
func (g *gui) refreshAll() {
	g.refreshPage()
	g.load("refresh", func() (commit, error) {
		st := g.svc.Status(context.Background())
		auto := g.svc.Autostart()
		if !st.OK {
			return func() { g.status, g.auto = st, auto; g.relayout = true }, nil
		}
		tools, err := g.svc.Tools(context.Background())
		if err != nil {
			return func() { g.status, g.auto = st, auto; g.relayout = true }, err
		}
		return func() { g.status, g.auto, g.tools = st, auto, tools; g.relayout = true }, nil
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
	if g.win == nil {
		// 测试环境（没有窗口）：同步执行，别起 goroutine —— 否则 commit 会与视图并发改状态，
		// -race 立刻红（而且测试本来就该是确定性的）。
		done, err := safeWork(work)
		if err == nil && done != nil {
			done()
		}
		return
	}
	g.busy[key] = true
	go func() {
		done, err := safeWork(work)
		g.onMain(func() {
			delete(g.busy, key) // 无论成功、失败还是 panic，这里一定要解掉
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
	case errors.Is(err, context.DeadlineExceeded):
		return "超时：这次请求 20 秒没完成（网络不通或源被墙），稍后再试或换镜像档"
	}
	return err.Error()
}

// safeWork 跑一次取数并兜住 panic：work 里崩了要变成一条错误交回去，
// 而不是让 goroutine 消失、busy 永久卡在 true —— 那在界面上就是"永远在转圈"。
func safeWork(work func() (commit, error)) (done commit, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("内部错误：%v", r)
		}
	}()
	return work()
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
