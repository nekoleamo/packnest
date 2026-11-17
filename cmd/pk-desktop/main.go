// pk-desktop：packnest 的**补充式**图形界面（必须先装 CLI）。
//
// 设计（方案 §11 定案 2026-11-11，2026-11-15 换壳）：
//   - 不 import 主模块任何 internal/*，全部能力都通过 `pk <cmd> --json` 子进程获得；
//   - 界面是**原生 UI**（github.com/egoist/mygo 的 ui 包，纯 Go 无 cgo、无 webview）：
//     同一份数据既能画成桌面窗口，也能用 `--serve` 在浏览器里看（浏览器形态用冻结的旧页面）。
//   - 所有删除/覆盖操作都要界面显式确认，后端只负责把 `--yes` 传给 CLI。
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/state"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/update"
)

// guiVersion 是 GUI 自己的版本（与 CLI 解耦；CLI 版本靠 pkcli 的启动门禁检查）。
// 发布用的 tag 与产物文件名都跟它走：`gui-v<guiVersion>`。
const guiVersion = "0.14.0"

// 图标：packaging/genicon 生成（纯标准库画的"巢"标记）。
//
//go:embed assets/icon.png
var appIcon []byte

//go:embed assets/tray-template.png
var trayTemplate []byte

// 浏览器形态的静态页：2026-11-15 起**冻结**，不再随界面改动更新，
// 只作"窗口起不来/远程看"的兜底（原生界面与它共用同一套 /api）。
//
//go:embed frontend/dist
var frontendFS embed.FS

func main() {
	showVersion := flag.Bool("version", false, "打印 GUI 版本并退出")
	serve := flag.Bool("serve", false, "只起 HTTP（用浏览器打开，不建桌面窗口）")
	addr := flag.String("addr", "127.0.0.1:7799", "--serve 时监听的地址")
	token := flag.String("token", "", "非回环地址时必须给：访问 /api/* 要带 ?token= 或 Authorization: Bearer")
	flag.Parse()
	if *showVersion {
		fmt.Printf("pk-desktop %s（原生 UI / mygo，需要 pk CLI）\n", guiVersion)
		return
	}

	svc := &state.Service{Locate: state.Locate, GUIVersion: guiVersion}
	a := &api{svc: svc}
	mux := a.routes(*token)
	web := withAPI(mux, staticHandler())

	// 单实例：先占用一个固定回环端口当互斥锁（零依赖）；占不到说明已经开着一个 GUI。
	single := &singleInstance{}
	if err := single.acquire(*addr); err != nil {
		log.Fatal(singleInstanceMsg(*addr, err))
	}
	defer single.release()

	// 上一次自升级留下的 `<exe>.old.*` / `packnest.app.old.*`：本进程已经跑起来了，
	// 说明替换是成功的，这时才清（更早清会把唯一那份回滚备份删掉）。
	// 不清的话 Windows 每次自升级都永久多一个几十 MB 的旧 exe。
	if exe, err := os.Executable(); err == nil {
		update.CleanupOld(exe, runtime.GOOS)
	}

	if *serve {
		if !loopback(*addr) && *token == "" {
			log.Fatal("拒绝启动：--serve 绑到非回环地址却没有 --token（这等于把装/卸软件的接口暴露到网络）")
		}
		log.Printf("pk-desktop --serve：http://%s/（需要本机已安装 pk；这是冻结的旧页面）", *addr)
		log.Fatal(http.Serve(single.ln, web))
	}
	// 桌面壳也顺带把同一个地址服务起来：单实例锁 + "浏览器里也能用"一举两得
	go func() {
		if loopback(*addr) || *token != "" {
			log.Printf("同一个界面也在 http://%s/ 提供（可选用浏览器打开）", *addr)
			_ = http.Serve(single.ln, web)
		}
	}()

	gui := newGUI(svc)
	gui.addr = *addr // 设置页“在浏览器里打开”要拼对端口（--addr 能改）
	// 本地/发布前冒烟：PK_GUI_SMOKE=quit 让界面启动后自己走一遍退出流程，
	// 用来验证"点了退出进程真的没了"（各平台通用；不进 make check，需要真 GUI 会话）。
	if os.Getenv("PK_GUI_SMOKE") == "upgrade" {
		// 自升级的冒烟：启动后自动走一遍"升级界面"，用来核对进度/结果 UI（本地验证用）
		gui.quitGrace = 120 * time.Second
		go func() {
			time.Sleep(2 * time.Second)
			gui.onMain(func() { gui.page = "update" })
			gui.onMain(func() { gui.applyUpdate("gui") })
		}()
	}
	if os.Getenv("PK_GUI_SMOKE") == "quit" {
		gui.quitGrace = 5 * time.Second
		go func() {
			time.Sleep(1500 * time.Millisecond)
			gui.quit()
		}()
	}
	mygo.App.SetName("packnest")
	mygo.App.SetVersion(guiVersion)
	mygo.App.WhenReady(gui.start)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// staticHandler 服务冻结的浏览器页面。
func staticHandler() http.Handler {
	sub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	return http.FileServer(http.FS(sub))
}

// withAPI 让 /api/* 走我们自己的处理器，其余交给静态资源。
// /api/* 全部先过 sameOrigin（挡浏览器里的跨源页面）。
func withAPI(apiHandler, assetsHandler http.Handler) http.Handler {
	guarded := sameOrigin(apiHandler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			guarded.ServeHTTP(w, r)
			return
		}
		assetsHandler.ServeHTTP(w, r)
	})
}

// start 建窗口与托盘。关窗口只是收进托盘，退出走托盘菜单（macOS 的 ⌘Q 仍然退）。
func (g *gui) start() {
	g.win = mygo.NewWindow(mygo.WindowOptions{
		Title:   "packnest",
		Width:   980,
		Height:  680,
		Content: ui.View(g.view),
	})
	// 窗口/任务栏图标：macOS 走 .app bundle 的 icon.icns；Windows 的 exe 里没有嵌 ico
	// （go build 不生成资源段），所以在运行时补一张，任务栏与 Alt-Tab 才不会显示白图标。
	if runtime.GOOS != "darwin" {
		_ = g.win.SetIcon(appIcon)
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 packnest", Click: func(*mygo.MenuItem, *mygo.Window) { g.show() }},
		{Type: mygo.MenuItemSeparator},
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { g.quit() }},
	})
	_, trayErr := mygo.NewTray(mygo.TrayOptions{
		ID:             "packnest",
		Icon:           iconForTray(),
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        "packnest",
		Menu:           menu,
	})
	if trayErr != nil {
		// Linux 缺 libayatana-appindicator3 时会失败：降级为"无托盘、关窗即退"
		log.Printf("托盘不可用（%v）：关窗口将直接退出", trayErr)
	}

	// 关窗口：有托盘就收进托盘，没有托盘就真的退出（只注册一次，避免两个 hook 打架）。
	//
	// ⚠️ "退出"（托盘菜单 / ⌘Q / Dock 右键退出）也会走这里：那时必须放行，
	// 否则 PreventDefault 会把退出取消掉 —— 表现就是"点了退出但进程还在后台"。
	g.win.OnClose(func(e *mygo.CloseEvent) {
		switch closeBehavior(g.quitting, trayErr == nil) {
		case closeAllow:
			return // 正在退出：让窗口正常关闭，进程随之结束
		case closeHide:
			e.PreventDefault()
			g.win.Hide()
		default: // closeQuit：没托盘（Linux 缺 appindicator），关窗就是退出
			mygo.App.Quit()
		}
	})

	// 任何"退出"入口（⌘Q、Dock 菜单、系统注销）先打标记，再让 mygo 走正常退出流程
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		g.markQuitting()
	})
}

// quit 是托盘菜单里的"退出"：先打标记，再请求退出；
// 若平台没走 OnBeforeQuit（例如托盘直接关窗口），标记已经就位，OnClose 会放行。
func (g *gui) quit() {
	g.markQuitting()
	mygo.App.Quit()
}

// markQuitting 是"退出"的唯一开关：打标记 + 落盘偏好 + 起一个硬退出兜底。
func (g *gui) markQuitting() {
	if g.quitting {
		return
	}
	g.quitting = true
	g.prefsSave() // 窗口关掉之后再写就晚了

	// 兜底：各平台的退出流程都可能被"托盘"/"窗口关闭被拦"拖住（macOS 实测过一次：
	// 托盘点退出 → 窗口关闭被 OnClose 拦住 → 进程留在后台）。
	// 正常流程 3 秒内会自己走完，走不完就强制结束 —— "退出"必须真的退出。
	grace := g.quitGrace
	if grace <= 0 {
		grace = 3 * time.Second
	}
	// 正在替换自己时不能半夜硬结束：替换是"旧的挪开 → 拷新的 → 删旧的"多步，
	// os.Exit 落在中间会留下半个应用（macOS 上甚至一个能跑的 app 都没有）。
	grace = quitGraceAfterApply(grace, g.applying.Load())
	exit := g.exitFn
	if exit == nil {
		exit = os.Exit
	}
	go func() {
		time.Sleep(grace)
		log.Println("退出流程未在预期时间内完成，强制结束进程")
		exit(0)
	}()
}

// show 从托盘/菜单回到窗口。
func (g *gui) show() {
	if g.win == nil {
		return
	}
	g.win.Show()
	g.win.Focus()
}

// iconForTray 在 macOS 上用模板图（系统自动反色），其余平台用彩色图标。
func iconForTray() []byte {
	if runtime.GOOS == "darwin" {
		return trayTemplate
	}
	return appIcon
}

// appDir 是"界面自己的状态目录"（窗口大小、主题、语言这类偏好）。
// PK_GUI_CONFIG_DIR 让测试用临时目录，不碰真实偏好。
func appDir() string {
	if dir := os.Getenv("PK_GUI_CONFIG_DIR"); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "packnest")
}

// singleInstance 用"监听一个回环端口"实现单实例：跨平台、零依赖，也不需要额外的锁文件。
type singleInstance struct{ ln net.Listener }

func (s *singleInstance) acquire(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	s.ln = ln
	return nil
}

func (s *singleInstance) release() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

// singleInstanceMsg 是"已经有另一个界面在跑"时用户看到的话（带上下一步，不让人去猜）。
// 独立成函数是为了让 `TestAcceptance_GUI2_TrayAndSingleInstance` 能钉住这句话。
func singleInstanceMsg(addr string, err error) string {
	// 两种原因共用一个回环端口当锁：①真的已经开着一个界面；②端口被别的程序占了。
	// 以前只说①，端口冲突的用户会一直在找那个不存在的窗口。
	return fmt.Sprintf("已经有一个 packnest 界面在运行（%s），或者这个端口被别的程序占着：%v。"+
		"先关掉那个窗口（托盘菜单里点退出）再打开；如果确认没开着，换一个端口：pk-desktop --addr 127.0.0.1:7899", addr, err)
}

// closeAction 是"点了窗口关闭"该干什么。
type closeAction int

const (
	closeAllow closeAction = iota // 正在退出：放行，让进程真的结束
	closeHide                     // 有托盘：关窗 = 收进托盘
	closeQuit                     // 没托盘：关窗直接退出（不是崩，也不是什么都不做）
)

// closeBehavior 把 0.12.1 那个坑写成纯函数：退出入口也会走 OnClose，
// 所以"正在退出"必须优先于"收进托盘"；Linux 缺 appindicator 时降级为关窗即退。
func closeBehavior(quitting, hasTray bool) closeAction {
	if quitting {
		return closeAllow
	}
	if hasTray {
		return closeHide
	}
	return closeQuit
}

// quitGraceAfterApply 是退出兜底的等待时长：正在替换自己时等替换收尾（最多 2 分钟），
// 其余情况就是原来那 3 秒。抽成纯函数是为了能断言（真跑一个 os.Exit 测不了）。
func quitGraceAfterApply(base time.Duration, applying bool) time.Duration {
	if applying {
		return 2 * time.Minute
	}
	if base <= 0 {
		return 3 * time.Second
	}
	return base
}
