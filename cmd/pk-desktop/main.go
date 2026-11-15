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

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/state"
)

// guiVersion 是 GUI 自己的版本（与 CLI 解耦；CLI 版本靠 pkcli 的启动门禁检查）。
// 发布用的 tag 与产物文件名都跟它走：`gui-v<guiVersion>`。
const guiVersion = "0.12.0"

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
		log.Fatalf("已经有一个 packnest 界面在运行（%s）：%v", *addr, err)
	}
	defer single.release()

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
func withAPI(apiHandler, assetsHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		assetsHandler.ServeHTTP(w, r)
	})
}

// start 建窗口与托盘。关窗口只是收进托盘，退出走托盘菜单（macOS 的 ⌘Q 仍然退）。
func (g *gui) start() {
	g.win = mygo.NewWindow(mygo.WindowOptions{
		Title:   "packnest",
		Width:   1040,
		Height:  780,
		Content: ui.View(g.view),
	})
	if runtime.GOOS == "linux" { // Windows 用 exe 资源，macOS 走 .app bundle
		_ = g.win.SetIcon(appIcon)
	}
	g.win.OnClose(func(e *mygo.CloseEvent) {
		e.PreventDefault()
		g.win.Hide()
	})

	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 packnest", Click: func(*mygo.MenuItem, *mygo.Window) { g.show() }},
		{Type: mygo.MenuItemSeparator},
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "packnest",
		Icon:           iconForTray(),
		IconIsTemplate: runtime.GOOS == "darwin",
		Title:          "",
		ToolTip:        "packnest",
		Menu:           menu,
	})
	if err != nil {
		// Linux 缺 libayatana-appindicator3 时会失败：降级为"无托盘、关窗即退"
		log.Printf("托盘不可用（%v）：关窗口将直接退出", err)
		g.win.OnClose(func(e *mygo.CloseEvent) { g.win.Destroy() })
		return
	}
	_ = tray
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
