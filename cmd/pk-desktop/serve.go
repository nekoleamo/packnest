package main

// 浏览器形态（--serve）与桌面壳共用的 HTTP 接口。
//
// 2026-11-15 起这一层只剩"读参数 → 调 internal/state → 写 JSON"：
// 原生界面走的是同一个 state.Service，所以不存在"浏览器对、桌面错"。

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/state"
)

// api 把所有请求转发给装配层。
type api struct{ svc *state.Service }

func (a *api) routes(token string) *http.ServeMux {
	mux := http.NewServeMux()
	ro := func(path string, h http.HandlerFunc) { mux.HandleFunc(path, h) }
	rw := func(path string, h http.HandlerFunc) { mux.Handle(path, guard(token, h)) } // 会改用户环境的接口要 token

	ro("/api/status", a.status)
	ro("/api/tools", a.tools)
	ro("/api/doctor", a.doctor)
	ro("/api/update", a.update)
	ro("/api/available", a.available)
	ro("/api/versions", a.versions)
	ro("/api/which", a.which)
	ro("/api/env", a.env)

	rw("/api/mirrors", a.mirrors)
	rw("/api/probe", a.probe)
	rw("/api/gc", a.gc)
	rw("/api/install", a.install) // NDJSON 流：一行一个 CLI 事件
	rw("/api/use", a.use)
	rw("/api/uninstall", a.uninstall)
	rw("/api/mirror-write", a.mirrorWrite)
	rw("/api/autostart", a.autostart) // 有 POST 写分支（SetAutostart），跟其它写接口同档
	return mux
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

// sameOrigin 挡住"浏览器里的任意网页驱动本机接口"（CSRF）。
//
// 桌面壳默认不给 token，而 http://127.0.0.1:7799/api/uninstall 是 GET/POST 就能打的：
// 用户上一次恶意页面，页面就能让 GUI 卸载工具 / 跑回收 / 替换自身。
// 浏览器对跨源请求一定会带 `Origin`（或至少 `Sec-Fetch-Site`），命令行工具两者都不带：
// 所以"带了但不等于自己"与 cross-site 一律拒，其余放行（curl / install 脚本 / 本页不受影响）。
func sameOrigin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
			http.Error(w, "拒绝跨源请求", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "拒绝跨源请求", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// originMatchesHost 判断 Origin（如 `http://127.0.0.1:7799`）是否就是这个 Host。
// 只认 http/https、主机名与端口必须一致（不搞后缀匹配）。
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Host == host
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

// httpErr 把装配层错误映射成状态码：锁被占用 → 409（"另一个 pk 正在运行"），
// 找不到 CLI → 412（先装 CLI），其余 → 502。
func httpErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pkcli.ErrBusy):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, pkcli.ErrNotFound):
		http.Error(w, err.Error(), http.StatusPreconditionFailed)
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// bad 是参数缺失/方法不对的统一出口（400/405）。
func bad(w http.ResponseWriter, msg string, code int) { http.Error(w, msg, code) }

func (a *api) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.svc.Status(r.Context()))
}

func (a *api) tools(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.Tools(r.Context())
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) doctor(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := a.svc.Doctor(r.Context(), q.Get("globals") == "1", q.Get("probe") == "1", q.Get("fix") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

// update: GET 查版本，POST {target} 升级（gui 替换自身 / cli 交给 `pk upgrade --self`）。
func (a *api) update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		res, err := a.svc.UpdateCheck(r.Context())
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, res)
		return
	}
	var body struct{ Target string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	res, err := a.svc.UpdateApply(r.Context(), body.Target)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) mirrors(w http.ResponseWriter, r *http.Request) {
	use := ""
	if r.Method == http.MethodPost {
		var body struct {
			Use string `json:"use"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Use == "" {
			bad(w, "需要 {\"use\":\"<档位>\"}", http.StatusBadRequest)
			return
		}
		use = body.Use
	}
	res, err := a.svc.Mirrors(r.Context(), use)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) probe(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.Probe(r.Context(), r.URL.Query().Get("tool"))
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) gc(w http.ResponseWriter, r *http.Request) {
	keep := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("keep")); err == nil {
		keep = v
	}
	res, err := a.svc.GC(r.Context(), r.Method == http.MethodPost, keep)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) autostart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, a.svc.Autostart())
		return
	}
	var body struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		bad(w, "请求体不是合法 JSON", http.StatusBadRequest)
		return
	}
	st, err := a.svc.SetAutostart(body.Enable)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, st)
}

// install 是 NDJSON 流：一行一个事件，done/error 收尾。
func (a *api) install(w http.ResponseWriter, r *http.Request) {
	spec := r.URL.Query().Get("spec")
	if spec == "" {
		bad(w, "需要 ?spec=node@22", http.StatusBadRequest)
		return
	}
	w.Header().Set("content-type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("cache-control", "no-store")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	err := a.svc.Install(r.Context(), spec, func(ev pkcli.Event) {
		_ = enc.Encode(ev) // 前端按 progress 画进度条、按 done 收尾
		if flusher != nil {
			flusher.Flush()
		}
	})
	if err != nil {
		_ = enc.Encode(map[string]any{"ev": "error", "msg": err.Error()})
	}
}

func (a *api) uninstall(w http.ResponseWriter, r *http.Request) {
	var body struct{ Tool string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" {
		bad(w, "需要 {\"tool\":\"node\"}", http.StatusBadRequest)
		return
	}
	if err := a.svc.Uninstall(r.Context(), body.Tool); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *api) available(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.Available(r.Context())
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) versions(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		bad(w, "需要 ?tool=node", http.StatusBadRequest)
		return
	}
	res, err := a.svc.Versions(r.Context(), tool, r.URL.Query().Get("all") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) which(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("cmd")
	if name == "" {
		bad(w, "需要 ?cmd=<命令名>", http.StatusBadRequest)
		return
	}
	res, err := a.svc.Which(r.Context(), name)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) env(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.Env(r.Context(), r.URL.Query().Get("project") == "1")
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) mirrorWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "用 POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		File string `json:"file"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	res, err := a.svc.MirrorWrite(r.Context(), body.File)
	if err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, res)
}

func (a *api) use(w http.ResponseWriter, r *http.Request) {
	var body struct{ Tool, Version string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" || body.Version == "" {
		bad(w, "需要 {\"tool\":\"node\",\"version\":\"22.23.5\"}", http.StatusBadRequest)
		return
	}
	if err := a.svc.Use(r.Context(), body.Tool, body.Version); err != nil {
		httpErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
