// pk-desktop：packnest 的**补充式**图形界面（必须先装 CLI）。
//
// 设计（方案 §11 定案 2026-11-11）：
//   - 不 import 主模块任何 internal/*，全部能力都通过 `pk <cmd> --json` 子进程获得；
//   - 前端是普通静态页 + 一个只读/只写的 HTTP 接口，因此同一份代码既能跑桌面壳，
//     也能用 `--serve` 在浏览器里用（GUI 只是 CLI 的可视化补充，不做第二套状态机）；
//   - 所有删除/覆盖操作都要前端显式确认，后端只负责把 `--yes` 传给 CLI。
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// 前端产物：Vue3 + TS（frontend/）构建到 frontend/dist，由这里嵌进二进制。
//
//go:embed frontend/dist
var frontendFS embed.FS

// 图标：packaging/genicon 生成（纯标准库画的"巢"标记）。
//
//go:embed assets/icon.png
var appIcon []byte

//go:embed assets/tray-template.png
var trayTemplate []byte

// guiVersion 是 GUI 自己的版本（与 CLI 解耦；CLI 版本靠 pkcli 的启动门禁检查）。
// 发布用的 tag 与产物文件名都跟它走：`gui-v<guiVersion>`。
const guiVersion = "0.10.3"

// api 是前端唯一的数据来源（不用 Wails 绑定，浏览器形态也能用）。
type api struct {
	find func(extra ...string) (*pkcli.Client, pkcli.Info, error)
}

func main() {
	showVersion := flag.Bool("version", false, "打印 GUI 版本并退出")
	serve := flag.Bool("serve", false, "只起 HTTP（用浏览器打开，不建桌面窗口）")
	addr := flag.String("addr", "127.0.0.1:7799", "--serve 时监听的地址")
	token := flag.String("token", "", "非回环地址时必须给：访问 /api/* 要带 ?token= 或 Authorization: Bearer")
	flag.Parse()
	if *showVersion {
		fmt.Printf("pk-desktop %s（Wails v3.1.0，需要 pk CLI）\n", guiVersion)
		return
	}

	a := &api{find: pkcli.Locate}
	mux := a.routes(*token)

	sub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	static := application.BundledAssetFileServer(sub)
	// 单实例：桌面壳先占用一个固定回环端口当互斥锁（零依赖）；占不到说明已经开着一个 GUI。
	// --serve 模式下这个检查也会生效（同一个端口不能同时被两个实例监听）。
	single := &singleInstance{}
	if err := single.acquire(*addr); err != nil {
		log.Fatalf("已经有一个 packnest 界面在运行（%s）：%v", *addr, err)
	}
	defer single.release()

	if *serve {
		if !loopback(*addr) && *token == "" {
			log.Fatal("拒绝启动：--serve 绑到非回环地址却没有 --token（这等于把装/卸软件的接口暴露到网络）")
		}
		// 浏览器形态和桌面壳共用同一套接口 + 同一份静态页
		log.Printf("pk-desktop --serve：http://%s/（需要本机已安装 pk）", *addr)
		log.Fatal(http.Serve(single.ln, withAPI(mux, static)))
	}
	// 桌面壳也顺带把同一个地址服务起来：单实例锁 + "浏览器里也能用"一举两得
	go func() {
		if loopback(*addr) || *token != "" {
			log.Printf("同一个界面也在 http://%s/ 提供（可选用浏览器打开）", *addr)
			_ = http.Serve(single.ln, withAPI(mux, static))
		}
	}()

	app := application.New(application.Options{
		Name:        "packnest",
		Description: "packnest 的可视化补充界面（需要本机已安装 pk）",
		Icon:        appIcon,
		Assets:      application.AssetOptions{Handler: withAPI(mux, static)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	winOpts := application.WebviewWindowOptions{Title: "packnest", URL: "/"}
	if runtime.GOOS == "linux" { // Linux 的窗口图标在 LinuxWindow 里；macOS 走 .app bundle，Windows 用 exe 资源
		winOpts.Linux = application.LinuxWindow{Icon: appIcon}
	}
	win := app.Window.NewWithOptions(winOpts)

	// 托盘：关窗口不退出（退到托盘），菜单里能重新打开窗口或退出
	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplate) // 模板图标由系统反色，深浅菜单栏都清楚
	} else {
		tray.SetIcon(appIcon)
	}
	menu := app.NewMenu()
	menu.Add("打开 packnest").OnClick(func(*application.Context) { win.Show(); win.Focus() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	// 点窗口关闭 → 隐藏，交给托盘收起（macOS 的 ⌘Q 仍然退）
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		win.Hide()
		e.Cancel()
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// withAPI 让 /api/* 走我们自己的处理器，其余交给 Wails 的静态资源服务。
func withAPI(apiHandler, assetsHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		assetsHandler.ServeHTTP(w, r)
	})
}

// guard 给 /api/* 加一层令牌校验：`--serve` 一旦绑到非回环地址，等于把"装/卸软件"的接口
// 暴露到网络里，必须显式给 token（桌面壳形态不走 HTTP 端口，不受影响）。
func guard(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := r.URL.Query().Get("token") == token
		if !ok && r.Header.Get("Authorization") == "Bearer "+token {
			ok = true
		}
		if !ok {
			http.Error(w, "需要 token", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// loopback 判断监听地址是否只在本机（默认安全形态）。
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *api) routes(token string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/tools", a.handleTools)
	mux.HandleFunc("/api/doctor", a.handleDoctor)
	mux.Handle("/api/mirrors", guard(token, http.HandlerFunc(a.handleMirrors)))
	mux.Handle("/api/probe", guard(token, http.HandlerFunc(a.handleProbe)))
	mux.Handle("/api/gc", guard(token, http.HandlerFunc(a.handleGC)))
	mux.Handle("/api/install", guard(token, http.HandlerFunc(a.handleInstall))) // NDJSON 流：一行一个 CLI 事件
	mux.Handle("/api/use", guard(token, http.HandlerFunc(a.handleUse)))
	mux.HandleFunc("/api/available", a.handleAvailable)
	mux.Handle("/api/uninstall", guard(token, http.HandlerFunc(a.handleUninstall)))
	mux.HandleFunc("/api/versions", a.handleVersions)
	mux.HandleFunc("/api/which", a.handleWhich)                                          // 只读：命令名 → 真实二进制
	mux.HandleFunc("/api/env", a.handleEnv)                                              // 只读：当前 PATH / 环境变量
	mux.Handle("/api/mirror-write", guard(token, http.HandlerFunc(a.handleMirrorWrite))) // 写 npmrc
	return mux
}

// httpErr 把 pkcli 的错误映射成状态码：锁被占用 → 409（前端提示"另一个 pk 正在运行"），其余 → 502。
func httpErr(w http.ResponseWriter, err error) {
	if errors.Is(err, pkcli.ErrBusy) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

// client 取"已定位的 pk"；没装 CLI 时直接 412，前端据此引导安装。
func (a *api) client(w http.ResponseWriter) (*pkcli.Client, bool) {
	cl, _, err := a.find()
	if err != nil {
		status := http.StatusPreconditionFailed // 412：先装 CLI
		if !errors.Is(err, pkcli.ErrNotFound) {
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
		return nil, false
	}
	return cl, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *api) handleStatus(w http.ResponseWriter, _ *http.Request) {
	cl, info, err := a.find()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "reason": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "cli": info, "path": cl.Path})
}

func (a *api) handleTools(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 60*time.Second)
	defer cancel()
	tools, err := cl.List(ctx)
	if err != nil {
		httpErr(w, err)
		return
	}
	// outdated 来自 `pk list --json`（1.3：只读 24h 索引缓存，没缓存就是空）
	writeJSON(w, map[string]any{"tools": tools.Tools, "outdated": tools.Outdated})
}

func (a *api) handleDoctor(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 90*time.Second)
	defer cancel()
	q := r.URL.Query()
	res, err := cl.Doctor(ctx, q.Get("globals") == "1", q.Get("probe") == "1", q.Get("fix") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"issues": res.Issues, "fixed": res.Fixed})
}

func (a *api) handleMirrors(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 30*time.Second)
	defer cancel()
	if r.Method == http.MethodPost {
		var body struct {
			Use string `json:"use"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Use == "" {
			http.Error(w, "需要 {\"use\":\"<档位>\"}", http.StatusBadRequest)
			return
		}
		if err := cl.UseMirror(ctx, body.Use); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	m, err := cl.Mirrors(ctx)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, m)
}

// handleProbe：并行探测镜像源（会发 HEAD 请求，慢，单独一个接口）。
func (a *api) handleProbe(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 120*time.Second)
	defer cancel()
	rows, err := cl.Probe(ctx, r.URL.Query().Get("tool"))
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"results": rows})
}

func (a *api) handleGC(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 120*time.Second)
	defer cancel()
	dry := r.Method != http.MethodPost // POST 才真删
	keep := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("keep")); err == nil {
		keep = v
	}
	res, err := cl.GC(ctx, dry, keep)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) handleInstall(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	spec := r.URL.Query().Get("spec")
	if spec == "" {
		http.Error(w, "需要 ?spec=node@22", http.StatusBadRequest)
		return
	}
	w.Header().Set("content-type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("cache-control", "no-store")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	err := cl.Install(r.Context(), spec, func(ev pkcli.Event) {
		_ = enc.Encode(ev) // 一行一个事件：前端按 progress 画进度条、按 done 收尾
		if flusher != nil {
			flusher.Flush()
		}
	})
	if err != nil {
		_ = enc.Encode(map[string]any{"ev": "error", "msg": err.Error()})
	}
}

// handleUninstall：卸载某工具的全部版本（前端已确认过，这里直接传 --yes）。
func (a *api) handleUninstall(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	var body struct{ Tool string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" {
		http.Error(w, "需要 {\"tool\":\"node\"}", http.StatusBadRequest)
		return
	}
	ctx, cancel := timeout(r, 300*time.Second)
	defer cancel()
	if err := cl.UninstallTool(ctx, body.Tool); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAvailable：每个工具的最新可装版本（索引 24h 缓存，不刷新）。
func (a *api) handleAvailable(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 120*time.Second)
	defer cancel()
	rows, err := cl.Available(ctx)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"available": rows})
}

// handleVersions：某个工具的可装 / 已装版本（GUI 的版本选择器）。
func (a *api) handleVersions(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		http.Error(w, "需要 ?tool=node", http.StatusBadRequest)
		return
	}
	ctx, cancel := timeout(r, 120*time.Second)
	defer cancel()
	v, err := cl.ToolVersions(ctx, tool, 40, r.URL.Query().Get("all") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, v)
}

// handleWhich：把命令名解析回真实二进制（`pk which <cmd>`）。
func (a *api) handleWhich(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("cmd")
	if name == "" {
		http.Error(w, "需要 ?cmd=<命令名>", http.StatusBadRequest)
		return
	}
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 30*time.Second)
	defer cancel()
	res, err := cl.Which(ctx, name)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

// handleEnv：只读地看当前生效的 PATH 与环境变量（写 rc 是 `pk env --write`，GUI 不做）。
func (a *api) handleEnv(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 30*time.Second)
	defer cancel()
	doc, err := cl.Environment(ctx, r.URL.Query().Get("project") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, doc)
}

// handleMirrorWrite：把当前档位的 registry 写进 npmrc（只增不改、写前备份）。
// 它会改用户的 ~/.npmrc，所以前端必须先弹确认再打这个接口。
func (a *api) handleMirrorWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "用 POST", http.StatusMethodNotAllowed)
		return
	}
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 60*time.Second)
	defer cancel()
	var body struct {
		File string `json:"file"` // 空 = 默认 ~/.npmrc
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	res, err := cl.MirrorWrite(ctx, body.File)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) handleUse(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	var body struct{ Tool, Version string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" || body.Version == "" {
		http.Error(w, "需要 {\"tool\":\"node\",\"version\":\"22.23.5\"}", http.StatusBadRequest)
		return
	}
	ctx, cancel := timeout(r, 60*time.Second)
	defer cancel()
	if err := cl.Use(ctx, body.Tool, body.Version); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// timeout 把 HTTP 请求的取消信号与超时合并（GUI 里用户点了"取消"要能真的杀掉 pk 子进程）。
func timeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
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
