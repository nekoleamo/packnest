package main

// 0.14.0 发版前整体 review 抓到的三条"只有真窗口/真浏览器才错"的回归点。
//
// 这三条在 ui.Tester（win == nil）里都复现不了：mygo.Window.Update 的异步投递、
// copyText 的自递归、CSRF，都要求真实窗口或真实 HTTP 请求。所以除了能测的逻辑
// （纯函数）之外，再用两条**源码断言**把写法钉死，避免下次又被写回去。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// sourceOf 读一份本包源码（源码断言用；路径相对测试文件所在目录）。
func sourceOf(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	return string(b)
}

// funcBody 粗暴切出一个函数的函数体（够用了：只用来查"里面有没有某个写法"）。
func funcBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	j := strings.Index(rest, "\n}\n")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

// TestP0_CopyTextWritesRealClipboard 守 0.13.0 那个必崩的写法：
// taskWriteTotal 的 copyText 在 win != nil 分支里调用了自己 → 有限递归 → 栈溢出。
func TestP0_CopyTextWritesRealClipboard(t *testing.T) {
	body := funcBody(sourceOf(t, "page_settings.go"), "func (g *gui) copyText(s string) {")
	if body == "" {
		t.Fatal("没找到 copyText（改名了？这条源码断言也要跟着改）")
	}
	if strings.Contains(body, "g.copyText(") {
		t.Fatal("copyText 自己调自己：真窗口里点「复制」会栈溢出（要写 mygo.Clipboard.WriteText）")
	}
	if !strings.Contains(body, "mygo.Clipboard.WriteText") {
		t.Fatal("copyText 没有真的写系统剪贴板")
	}
}

// TestP0_QueueUsesMainSync 守队列：Window.Update 是异步投递，
// "投递完立刻读 item" 会让队列一条都不装（0.13.0 真窗口里的表现）。
func TestP0_QueueUsesMainSync(t *testing.T) {
	body := funcBody(sourceOf(t, "page_queue.go"), "func (g *gui) runQueue() {")
	if !strings.Contains(body, "g.onMainSync(") {
		t.Fatal("runQueue 没用 onMainSync：异步投递后读 item 会恒为 nil，队列不会装任何东西")
	}
}

// TestNextWaitingItem 把队列"取下一条"的规则钉死（这条是纯函数，可真测）。
func TestNextWaitingItem(t *testing.T) {
	a := &queueItem{State: "done"}
	b := &queueItem{State: "wait"}
	c := &queueItem{State: "wait"}
	q := []*queueItem{a, b, c}
	if got := nextWaitingItem(q); got != b || b.State != "run" {
		t.Fatalf("应取下一条 wait 并标 run，实际 %+v", got)
	}
	if got := nextWaitingItem(q); got != c || c.State != "run" {
		t.Fatalf("第二条应接上，实际 %+v", got)
	}
	if got := nextWaitingItem(q); got != nil {
		t.Fatalf("全跑完了应返回 nil，实际 %+v", got)
	}
}

// TestSameOriginGuard 守 CSRF：桌面壳默认没 token，任意网页都能 fetch 本机接口。
func TestSameOriginGuard(t *testing.T) {
	h := sameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		name    string
		host    string
		origin  string
		fetch   string
		allowed bool
	}{
		{name: "命令行（无 Origin）", host: "127.0.0.1:7799", allowed: true},
		{name: "同源页面", host: "127.0.0.1:7799", origin: "http://127.0.0.1:7799", allowed: true},
		{name: "别的站点", host: "127.0.0.1:7799", origin: "https://evil.example"},
		{name: "同主机不同端口", host: "127.0.0.1:7799", origin: "http://127.0.0.1:8080"},
		{name: "浏览器标注跨站", host: "127.0.0.1:7799", fetch: "cross-site"},
		{name: "浏览器标注同站", host: "127.0.0.1:7799", fetch: "same-origin", allowed: true},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "http://"+c.host+"/api/uninstall", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if c.fetch != "" {
			req.Header.Set("Sec-Fetch-Site", c.fetch)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Code == http.StatusNoContent
		if got != c.allowed {
			t.Fatalf("%s: allowed=%v 期望 %v（code=%d）", c.name, got, c.allowed, rec.Code)
		}
	}
	// 非 http(s) 的 Origin（例如 file://）一律拒
	if originMatchesHost("file://localhost", "127.0.0.1:7799") {
		t.Fatal("file:// 不该被当成同源")
	}
	if originMatchesHost("http://127.0.0.1:7799.evil.example", "127.0.0.1:7799") {
		t.Fatal("后缀匹配不算同源")
	}
}

// TestQuitGraceAfterApply 守"正在替换自己时不能硬退出"：
// 替换是"旧的挪开 → 拷新的 → 删旧的"，os.Exit 落在中间会留下半个应用。
func TestQuitGraceAfterApply(t *testing.T) {
	if got := quitGraceAfterApply(3*time.Second, false); got != 3*time.Second {
		t.Fatalf("平时就是原来那 3 秒，实际 %v", got)
	}
	if got := quitGraceAfterApply(3*time.Second, true); got < time.Minute {
		t.Fatalf("替换中要给足时间，实际 %v", got)
	}
	if got := quitGraceAfterApply(0, false); got != 3*time.Second {
		t.Fatalf("没设宽限期时默认 3 秒，实际 %v", got)
	}
}

// TestBrowserURL 守"在浏览器里打开"跟着 --addr 走（硬编码 7799 换个端口就是死链）。
func TestBrowserURL(t *testing.T) {
	g := &gui{}
	if got := g.browserURL(); got != "http://127.0.0.1:7799/" {
		t.Fatalf("默认地址 = %q", got)
	}
	g.addr = "127.0.0.1:8899"
	if got := g.browserURL(); got != "http://127.0.0.1:8899/" {
		t.Fatalf("--addr 改端口后 = %q", got)
	}
	g.addr = "0.0.0.0:7799"
	if got := g.browserURL(); got != "http://127.0.0.1:7799/" {
		t.Fatalf("绑 0.0.0.0 时要给回环地址，实际 %q", got)
	}
}
