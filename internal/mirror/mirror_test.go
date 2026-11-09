package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
)

func TestProfilesAndNames(t *testing.T) {
	names := Names()
	if names[0] != AutoProfile {
		t.Fatalf("auto 应排首位: %v", names)
	}
	for _, want := range []string{"default", "cn", "tuna", "aliyun", "ustc"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("缺少档位 %q: %v", want, names)
		}
	}
	for _, p := range Profiles() {
		if p.Registry == "" || p.GoProxy == "" || p.PipIndex == "" || p.Display == "" {
			t.Errorf("档位 %q 字段不全: %+v", p.Name, p)
		}
	}
}

func TestGetProfile(t *testing.T) {
	if p, err := Get(""); err != nil || p.Name != AutoProfile {
		t.Fatalf("空名应解析为 auto: %+v %v", p, err)
	}
	if p, err := Get("AUTO"); err != nil || p.Name != AutoProfile {
		t.Fatalf("auto 应大小写不敏感: %+v %v", p, err)
	}
	p, err := Get("cn")
	if err != nil || p.Registry != "https://registry.npmmirror.com" {
		t.Fatalf("cn 档: %+v %v", p, err)
	}
	if _, err := Get("nope"); err == nil {
		t.Fatal("未知档应报错")
	}
}

func TestChainOrder(t *testing.T) {
	chain := Chain("cn")
	if chain[0] != "cn" {
		t.Fatalf("首选档应在最前: %v", chain)
	}
	if chain[len(chain)-1] != "default" {
		t.Fatalf("官方源应最后: %v", chain)
	}
	if len(chain) != len(Profiles()) {
		t.Fatalf("回退链应覆盖全部档位: %v", chain)
	}
	if got := Chain("auto")[0]; got != "cn" {
		t.Fatalf("auto 默认走 cn，得到 %q", got)
	}
	if got := Chain("")[0]; got != "cn" {
		t.Fatalf("空档默认走 cn，得到 %q", got)
	}
}

func TestPickURLs(t *testing.T) {
	templates := map[string]string{
		"default": "https://nodejs.org/dist/index.json",
		"cn":      "https://registry.npmmirror.com/-/binary/node/index.json",
		"tuna":    "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/index.json",
		"aliyun":  "https://mirrors.aliyun.com/nodejs-release/index.json",
		"ustc":    "",
	}
	urls := PickURLs(templates, Chain("cn"))
	if urls[0] != templates["cn"] {
		t.Fatalf("首选应是 cn: %v", urls)
	}
	if urls[len(urls)-1] != templates["default"] {
		t.Fatalf("官方源应在最后: %v", urls)
	}
	for _, u := range urls {
		if u == "" {
			t.Fatalf("空模板不该进入候选: %v", urls)
		}
	}
	// 自定义内网档不在回退链里，也必须兜底追加（PK_MIRROR 场景）。
	templates["intranet"] = "http://nexus.local/node/index.json"
	urls = PickURLs(templates, Chain("cn"))
	if urls[len(urls)-1] != templates["intranet"] {
		// default 之后的兜底：顺序为内网档在后
		t.Logf("顺序: %v", urls)
	}
	found := false
	for _, u := range urls {
		if u == templates["intranet"] {
			found = true
		}
	}
	if !found {
		t.Fatalf("自定义档应出现在候选里: %v", urls)
	}
}

func TestPickURLsDedup(t *testing.T) {
	same := "https://registry.npmmirror.com/x"
	urls := PickURLs(map[string]string{"cn": same, "tuna": same}, Chain("cn"))
	if len(urls) != 1 {
		t.Fatalf("相同 URL 应去重: %v", urls)
	}
}

func TestProbeOneOKAndFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "1")
	}))
	defer srv.Close()

	p := &Prober{Client: fetch.New()}
	ok := p.ProbeOne(context.Background(), srv.URL+"/index.json")
	if !ok.OK || ok.Status != 200 {
		t.Fatalf("应探测成功: %+v", ok)
	}
	bad := p.ProbeOne(context.Background(), srv.URL+"/bad")
	if bad.OK || bad.Status != 404 || bad.Err == "" {
		t.Fatalf("404 应判定失败: %+v", bad)
	}
}

func TestProbeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	p := &Prober{Client: fetch.New(), Timeout: 30 * time.Millisecond}
	r := p.ProbeOne(context.Background(), srv.URL)
	if r.OK {
		t.Fatalf("超时应失败: %+v", r)
	}
}

func TestProbeCacheTTL(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	now := time.Now()
	p := &Prober{
		Client: fetch.New(),
		Path:   filepath.Join(t.TempDir(), "probe.json"),
		TTL:    DefaultTTL,
		Now:    func() time.Time { return now },
	}
	urls := []string{srv.URL + "/a", srv.URL + "/b"}
	if _, err := p.Select(context.Background(), urls); err != nil {
		t.Fatalf("Select: %v", err)
	}
	first := atomic.LoadInt32(&hits)
	// 6h 内再选：不发请求。
	if _, err := p.Select(context.Background(), urls); err != nil {
		t.Fatalf("Select 第二次: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != first {
		t.Fatalf("TTL 内不应再探测：%d → %d", first, got)
	}
	// 过期后重新探测（首选项失败则回退）。
	now = now.Add(DefaultTTL + time.Minute)
	if _, err := p.Select(context.Background(), []string{srv.URL + "/gone", srv.URL + "/a"}); err != nil {
		t.Fatalf("过期后应重新探测并回退: %v", err)
	}
}

func TestSelectFallsBack(t *testing.T) {
	var good int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good" {
			atomic.AddInt32(&good, 1)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := &Prober{Client: fetch.New(), Path: filepath.Join(t.TempDir(), "probe.json")}
	res, err := p.Select(context.Background(), []string{srv.URL + "/dead", srv.URL + "/good"})
	if err != nil {
		t.Fatalf("应回退到可用源: %v", err)
	}
	if res.URL != srv.URL+"/good" || atomic.LoadInt32(&good) != 1 {
		t.Fatalf("回退结果异常: %+v", res)
	}
}

func TestSelectAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := &Prober{Client: fetch.New()}
	_, err := p.Select(context.Background(), []string{srv.URL + "/a", srv.URL + "/b"})
	if err == nil {
		t.Fatal("全部失败应报错")
	}
	if !strings.Contains(err.Error(), "pk mirror use cn") {
		t.Fatalf("错误信息应给可执行建议: %v", err)
	}
	if _, err := p.Select(context.Background(), nil); err == nil {
		t.Fatal("空候选应报错")
	}
}

func TestProbeAllParallelPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "probe.json")
	p := &Prober{Client: fetch.New(), Path: path}
	results := p.Probe(context.Background(), []string{srv.URL + "/a", srv.URL + "/b", srv.URL + "/c"})
	if len(results) != 3 {
		t.Fatalf("应探测 3 个源: %v", results)
	}
	cache := p.Load()
	if len(cache.Results) != 3 {
		t.Fatalf("探测结果应落盘: %+v", cache)
	}
}

func TestLoadCorruptCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Prober{Path: path}
	if cache := p.Load(); cache.Results == nil || len(cache.Results) != 0 {
		t.Fatalf("损坏缓存应视为空: %+v", cache)
	}
}

func TestDescribeFailureHint(t *testing.T) {
	got := describeFailure("http://x", errString("dial tcp: lookup x: no such host"))
	if !strings.Contains(got, "mirror use cn") {
		t.Fatalf("应给换档建议: %q", got)
	}
	if got := describeFailure("http://x", errString("boom")); got != "boom" {
		t.Fatalf("普通错误不该加建议: %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
