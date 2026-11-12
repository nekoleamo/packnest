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
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

//go:embed assets
var assets embed.FS

// api 是前端唯一的数据来源（不用 Wails 绑定，浏览器形态也能用）。
type api struct {
	find func(extra ...string) (*pkcli.Client, pkcli.Info, error)
}

func main() {
	serve := flag.Bool("serve", false, "只起 HTTP（用浏览器打开，不建桌面窗口）")
	addr := flag.String("addr", "127.0.0.1:7799", "--serve 时监听的地址")
	token := flag.String("token", "", "非回环地址时必须给：访问 /api/* 要带 ?token= 或 Authorization: Bearer")
	flag.Parse()

	a := &api{find: pkcli.Locate}
	mux := a.routes(*token)

	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		log.Fatal(err)
	}
	static := application.BundledAssetFileServer(sub)
	if *serve {
		if !loopback(*addr) && *token == "" {
			log.Fatal("拒绝启动：--serve 绑到非回环地址却没有 --token（这等于把装/卸软件的接口暴露到网络）")
		}
		// 浏览器形态和桌面壳共用同一套接口 + 同一份静态页
		log.Printf("pk-desktop --serve：http://%s/（需要本机已安装 pk）", *addr)
		log.Fatal(http.ListenAndServe(*addr, withAPI(mux, static)))
	}

	app := application.New(application.Options{
		Assets: application.AssetOptions{Handler: withAPI(mux, static)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "packnest", URL: "/"})
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
	mux.Handle("/api/gc", guard(token, http.HandlerFunc(a.handleGC)))
	mux.Handle("/api/install", guard(token, http.HandlerFunc(a.handleInstall))) // NDJSON 流：一行一个 CLI 事件
	mux.Handle("/api/use", guard(token, http.HandlerFunc(a.handleUse)))
	mux.HandleFunc("/api/available", a.handleAvailable)
	mux.Handle("/api/uninstall", guard(token, http.HandlerFunc(a.handleUninstall)))
	mux.HandleFunc("/api/versions", a.handleVersions)
	return mux
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
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"tools": tools})
}

func (a *api) handleDoctor(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 90*time.Second)
	defer cancel()
	issues, err := cl.Doctor(ctx, r.URL.Query().Get("globals") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"issues": issues})
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
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, m)
}

func (a *api) handleGC(w http.ResponseWriter, r *http.Request) {
	cl, ok := a.client(w)
	if !ok {
		return
	}
	ctx, cancel := timeout(r, 120*time.Second)
	defer cancel()
	dry := r.Method != http.MethodPost // POST 才真删
	res, err := cl.GC(ctx, dry)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
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
		http.Error(w, err.Error(), http.StatusBadGateway)
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
		http.Error(w, err.Error(), http.StatusBadGateway)
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
	v, err := cl.ToolVersions(ctx, tool, 40)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
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
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// timeout 把 HTTP 请求的取消信号与超时合并（GUI 里用户点了"取消"要能真的杀掉 pk 子进程）。
func timeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
