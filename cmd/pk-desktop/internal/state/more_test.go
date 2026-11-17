package state

// 装配层的补全面：GUI 1.0 口径 H3 要求的覆盖率门禁（state ≥70%）靠这层测试撑着。
// 每个方法至少两条：正常路径（数据真的透上去）+ Locate/CLI 报错时原样上行。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/update"
)

// selfUpd 在 fake 之上补两个与自更新相关的方法（其余行为照旧）。
type selfUpd struct {
	*fake
	check     pkcli.SelfUpdate
	checkErr  error
	self      pkcli.SelfUpdate
	selfErr   error
	lastUnins string
}

func (f *selfUpd) CheckSelfUpdate(context.Context) (pkcli.SelfUpdate, error) {
	return f.check, f.checkErr
}
func (f *selfUpd) SelfUpdate(context.Context) (pkcli.SelfUpdate, error) { return f.self, f.selfErr }

// svcFor 用任意 Commands 造 Service（可选替换 Exe / Progress）。
func svcFor(c Commands) *Service {
	return &Service{
		GUIVersion: "0.12.3",
		Locate: func() (Commands, pkcli.Info, error) {
			return c, pkcli.Info{Schema: 1, Version: "1.9.1", Path: "/usr/local/bin/pk"}, nil
		},
	}
}

// 每个方法在"没有 CLI"时都必须把 ErrNotFound 原样上行（HTTP 层靠它映射 412）。
func TestAllCommandsFailWithoutCLI(t *testing.T) {
	svc := &Service{Locate: func() (Commands, pkcli.Info, error) {
		return nil, pkcli.Info{}, pkcli.ErrNotFound
	}}
	ctx := context.Background()
	checks := map[string]func() error{
		"Tools":       func() error { _, err := svc.Tools(ctx); return err },
		"Doctor":      func() error { _, err := svc.Doctor(ctx, true, false, false); return err },
		"Install":     func() error { return svc.Install(ctx, "node@22", nil) },
		"Use":         func() error { return svc.Use(ctx, "node", "22.0.0") },
		"Uninstall":   func() error { return svc.Uninstall(ctx, "node") },
		"Mirrors":     func() error { _, err := svc.Mirrors(ctx, ""); return err },
		"Probe":       func() error { _, err := svc.Probe(ctx, ""); return err },
		"GC":          func() error { _, err := svc.GC(ctx, false, 0); return err },
		"Available":   func() error { _, err := svc.Available(ctx); return err },
		"Versions":    func() error { _, err := svc.Versions(ctx, "node", false); return err },
		"Which":       func() error { _, err := svc.Which(ctx, "node"); return err },
		"Env":         func() error { _, err := svc.Env(ctx, false); return err },
		"MirrorWrite": func() error { _, err := svc.MirrorWrite(ctx, ""); return err },
		"UpdateCheck": func() error { _, err := svc.UpdateCheck(ctx); return err },
		"UpdateApply": func() error { _, err := svc.UpdateApply(ctx, "cli"); return err },
	}
	for name, call := range checks {
		if err := call(); !errors.Is(err, pkcli.ErrNotFound) {
			t.Errorf("%s 没把 ErrNotFound 原样上行: %v", name, err)
		}
	}
}

func TestSimpleWrappersPassThrough(t *testing.T) {
	ctx := context.Background()
	f := &fake{
		probe: []pkcli.ProbeEntry{{URL: "https://gitee.com/x", OK: true}},
		gc:    pkcli.GCResult{FreedBytes: 1024},
		avail: []pkcli.AvailableTool{{Name: "node", Latest: "24.0.0"}},
		vers:  pkcli.Versions{Tool: "node"},
		which: pkcli.Which{Name: "node", Path: "/root/.packnest/bin/node"},
		env:   pkcli.EnvDoc{},
	}
	svc := svcFor(f)

	if err := svc.Use(ctx, "node", "22.23.5"); err != nil {
		t.Fatal(err)
	}
	if f.used != "node@22.23.5" {
		t.Fatalf("used = %q", f.used)
	}

	pr, err := svc.Probe(ctx, "node")
	if err != nil || len(pr.Results) != 1 || pr.Results[0].URL == "" {
		t.Fatalf("Probe = %+v err=%v", pr, err)
	}

	if _, err := svc.GC(ctx, true, 2); err != nil {
		t.Fatalf("GC: %v", err)
	}

	av, err := svc.Available(ctx)
	if err != nil || len(av.Available) != 1 {
		t.Fatalf("Available = %+v err=%v", av, err)
	}

	if _, err := svc.Versions(ctx, "node", true); err != nil {
		t.Fatalf("Versions: %v", err)
	}

	w, err := svc.Which(ctx, "node")
	if err != nil || w.Path == "" {
		t.Fatalf("Which = %+v err=%v", w, err)
	}
	if _, err := svc.Which(ctx, ""); err == nil {
		t.Fatal("空命令名应报错（否则会跑 pk which ''）")
	}

	if _, err := svc.Env(ctx, true); err != nil {
		t.Fatalf("Env: %v", err)
	}

	mw, err := svc.MirrorWrite(ctx, "")
	if err != nil || !f.wroteNPMRC || mw.Registry == "" {
		t.Fatalf("MirrorWrite = %+v err=%v", mw, err)
	}
}

func TestEmptyResultsAreArrays(t *testing.T) {
	// 界面直接 v-for，null 会炸。四个"列表类"结果都要兜成空数组。
	svc := svcFor(&fake{})
	ctx := context.Background()
	if pr, _ := svc.Probe(ctx, ""); pr.Results == nil {
		t.Error("Probe 空结果应是空数组")
	}
	if av, _ := svc.Available(ctx); av.Available == nil {
		t.Error("Available 空结果应是空数组")
	}
	if d, _ := svc.Doctor(ctx, false, false, false); d.Issues == nil {
		t.Error("Doctor 空结果应是空数组")
	}
}

func TestExePath(t *testing.T) {
	svc := svcFor(&fake{})
	got, err := svc.exePath()
	if err != nil || got == "" {
		t.Fatalf("默认应能拿到自己的路径: %q %v", got, err)
	}
	svc.Exe = func() (string, error) { return "/tmp/fake", nil }
	if got, _ := svc.exePath(); got != "/tmp/fake" {
		t.Fatalf("注入的 Exe 没生效: %q", got)
	}
	svc.Exe = func() (string, error) { return "", errors.New("没有可执行文件") }
	if _, err := svc.exePath(); err == nil {
		t.Fatal("Exe 报错应上行")
	}
}

func TestErrStringAndConvertSources(t *testing.T) {
	if errString(nil) != "" {
		t.Fatal("nil 错误应给空串")
	}
	if errString(errors.New("x")) != "x" {
		t.Fatal("错误原文应保留")
	}
	var in []struct {
		Name      string `json:"name"`
		Base      string `json:"base"`
		LatestURL string `json:"latest_url"`
		LatencyMS int64  `json:"latency_ms"`
	}
	in = append(in, struct {
		Name      string `json:"name"`
		Base      string `json:"base"`
		LatestURL string `json:"latest_url"`
		LatencyMS int64  `json:"latency_ms"`
	}{Name: "gitee", LatencyMS: 42})
	got := convertSources(in)
	if len(got) != 1 || got[0].Name != "gitee" || got[0].LatencyMS != 42 {
		t.Fatalf("convertSources = %+v", got)
	}
}

// ── 传输原语（自更新用）────────────────────────────────────────

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        http.Header{},
	}
}

// withTransport 换掉包级 httpClient，测试结束后还原。
func withTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := httpClient
	httpClient = &http.Client{Transport: rt}
	t.Cleanup(func() { httpClient = old })
}

func TestFetchBytes(t *testing.T) {
	ctx := context.Background()
	withTransport(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "forbidden") {
			return resp(http.StatusForbidden, nil), nil
		}
		return resp(http.StatusOK, []byte("0.13.0")), nil
	}))

	b, err := fetchBytes()(ctx, "https://ex/latest-gui")
	if err != nil || string(b) != "0.13.0" {
		t.Fatalf("fetchBytes = %q %v", b, err)
	}
	if _, err := fetchBytes()(ctx, "https://ex/forbidden"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("非 200 应报出状态码，实际 %v", err)
	}
	if _, err := fetchBytes()(ctx, "://坏地址"); err == nil {
		t.Fatal("坏地址应报错")
	}
}

// TestHTTPClientHasNoGlobalTimeout 守住 0.14.0 修掉的真问题：
// Client.Timeout 覆盖"整个请求含读完 body"，15s 会把 25–40MB 的 GUI 产物下载截断。
// 上限必须分层（小文件 fetchBytes 用 ctx 限时，大文件由 UpdateApply 的 ctx 管）。
func TestHTTPClientHasNoGlobalTimeout(t *testing.T) {
	if httpClient.Timeout != 0 {
		t.Fatalf("httpClient 不能设全局 Timeout（会截断大产物下载），实际 %v", httpClient.Timeout)
	}
	if fetchBytesTimeout <= 0 {
		t.Fatalf("fetchBytesTimeout 必须为正，实际 %v", fetchBytesTimeout)
	}
}

func TestShortName(t *testing.T) {
	if got := shortName("https://ex/a/b/pk.tar.gz"); got != "pk.tar.gz" {
		t.Fatalf("shortName = %q", got)
	}
	if got := shortName("pk.tar.gz"); got != "pk.tar.gz" {
		t.Fatalf("没有斜杠时应原样返回: %q", got)
	}
	if lastIndexByte("abc", '/') != -1 {
		t.Fatal("lastIndexByte 没找到时应返回 -1")
	}
}

func TestFetchToFileWithProgress(t *testing.T) {
	ctx := context.Background()
	// 300KB → 至少两次 128KB 的读，进度会被上报不止一次
	body := bytes.Repeat([]byte("x"), 300<<10)
	withTransport(t, rtFunc(func(*http.Request) (*http.Response, error) {
		return resp(http.StatusOK, body), nil
	}))

	var calls []string
	svc := &Service{Progress: func(stage string, done, total int64) {
		calls = append(calls, fmt.Sprintf("%s:%d/%d", stage, done, total))
	}}
	dst := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := fetchToFileWithProgress(svc)(ctx, "https://ex/a.tar.gz", dst); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if st, err := os.Stat(dst); err != nil || st.Size() != int64(len(body)) {
		t.Fatalf("落盘大小不对: %v %v", st.Size(), err)
	}
	if len(calls) < 2 {
		t.Fatalf("进度上报太少（要能画出进度条）: %v", calls)
	}

	// 没有 Progress 钩子时也不能 panic
	if err := fetchToFileWithProgress(&Service{})(ctx, "https://ex/a.tar.gz", filepath.Join(t.TempDir(), "b")); err != nil {
		t.Fatalf("无钩子时下载失败: %v", err)
	}

	// 目标目录不存在 → 报错
	if err := fetchToFileWithProgress(svc)(ctx, "https://ex/a.tar.gz", filepath.Join(t.TempDir(), "没有", "c")); err == nil {
		t.Fatal("目标不可写应报错")
	}
}

func TestFetchToFileErrors(t *testing.T) {
	ctx := context.Background()
	withTransport(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "404") {
			return resp(http.StatusNotFound, nil), nil
		}
		return resp(http.StatusOK, []byte("x")), nil
	}))
	if err := fetchToFile()(ctx, "https://ex/404", filepath.Join(t.TempDir(), "a")); err == nil {
		t.Fatal("非 200 应报错")
	}
	if err := fetchToFile()(ctx, "://坏", filepath.Join(t.TempDir(), "a")); err == nil {
		t.Fatal("坏地址应报错")
	}
	if err := fetchToFile()(ctx, "https://ex/ok", filepath.Join(t.TempDir(), "没有", "a")); err == nil {
		t.Fatal("目标不可写应报错")
	}
	out := filepath.Join(t.TempDir(), "ok")
	if err := fetchToFile()(ctx, "https://ex/ok", out); err != nil {
		t.Fatalf("正常下载失败: %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "x" {
		t.Fatalf("内容不对: %q", b)
	}
}

// ── 检查更新 / 升级（不开真窗口、不碰真二进制）─────────────────

func sourcesForCLI() pkcli.SelfUpdate {
	su := pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1", Channel: "script"}
	su.Sources = append(su.Sources, struct {
		Name      string `json:"name"`
		Base      string `json:"base"`
		LatestURL string `json:"latest_url"`
		LatencyMS int64  `json:"latency_ms"`
	}{Name: "gitee", LatencyMS: 30})
	return su
}

func TestUpdateCheckAndApplyGUI(t *testing.T) {
	// 假源：latest-gui 报 0.13.0；产物是真 tar.gz；清单里是真 sha256；签名是假的
	// → 升级必然停在"验签失败"，这正是我们要的行为（不做只比 sha256 的降级）。
	artName := "packnest-desktop_0.13.0_" + "plan9" + "_" + "mips.tar.gz"
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pk-desktop", Mode: 0o755, Size: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("NEW")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	artBytes := tarBuf.Bytes()
	sum := sha256.Sum256(artBytes)
	checksums := hex.EncodeToString(sum[:]) + "  " + artName + "\n"

	withTransport(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "latest-gui"):
			return resp(http.StatusOK, []byte("0.13.0\n")), nil
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			return resp(http.StatusOK, []byte(checksums)), nil
		case strings.HasSuffix(r.URL.Path, ".sig"):
			return resp(http.StatusOK, []byte("这不是签名")), nil
		default:
			return resp(http.StatusOK, artBytes), nil
		}
	}))

	su := &selfUpd{fake: &fake{}, check: sourcesForCLI(), self: pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1"}}
	svc := svcFor(su)
	var progress []string
	svc.Progress = func(stage string, done, total int64) { progress = append(progress, stage) }
	exe := filepath.Join(t.TempDir(), "pk-desktop")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc.Exe = func() (string, error) { return exe, nil }

	up, err := svc.UpdateCheck(context.Background())
	if err != nil {
		t.Fatalf("UpdateCheck: %v", err)
	}
	if !up.GUI.UpdateAvailable || up.GUI.Latest != "0.13.0" || up.GUI.ArtifactURL == "" {
		t.Fatalf("检查结果不对: %+v", up.GUI)
	}

	// 升级 GUI：验签失败 → OK=false 且原因说得清；旧文件不动；进度至少报过"下载 xxx"
	res, err := svc.UpdateApply(context.Background(), "gui")
	if err != nil {
		t.Fatalf("UpdateApply(gui) 不该直接报错: %v", err)
	}
	if res.OK || res.Error == "" || res.Target != "gui" {
		t.Fatalf("验签失败应报清楚: %+v", res)
	}
	if !strings.Contains(res.Error, "签名") {
		t.Fatalf("原因里要提到签名，实际 %q", res.Error)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatalf("验签失败却改了可执行文件: %q", b)
	}
	if len(progress) == 0 {
		t.Fatal("没有任何进度上报（界面会一直转圈）")
	}

	// 升级 CLI：全交给 pk upgrade --self
	res, err = svc.UpdateApply(context.Background(), "cli")
	if err != nil || !res.OK || res.Target != "cli" {
		t.Fatalf("UpdateApply(cli) = %+v err=%v", res, err)
	}
	su.selfErr = errors.New("pk 说不行")
	res, err = svc.UpdateApply(context.Background(), "cli")
	if err != nil || res.OK || !strings.Contains(res.Error, "pk 说不行") {
		t.Fatalf("CLI 升级失败应如实上报: %+v err=%v", res, err)
	}

	// 未知目标
	if _, err := svc.UpdateApply(context.Background(), "什么"); err == nil {
		t.Fatal("未知升级目标应报错")
	}
}

func TestUpdateApplyGUINoopWhenLatest(t *testing.T) {
	// 已经是最新 → Noop（界面要说清楚，而不是假装替换成功）
	withTransport(t, rtFunc(func(*http.Request) (*http.Response, error) {
		return resp(http.StatusOK, []byte("0.12.3")), nil
	}))
	su := &selfUpd{fake: &fake{}, check: sourcesForCLI()}
	svc := svcFor(su)
	res, err := svc.UpdateApply(context.Background(), "")
	if err != nil {
		t.Fatalf("UpdateApply: %v", err)
	}
	if !res.OK || !res.Noop {
		t.Fatalf("已是最新应给 Noop: %+v", res)
	}
}

func TestUpdateApplyGUIExeError(t *testing.T) {
	// 有更新但拿不到自己的路径 → 直接报错（不装作升级成功）
	withTransport(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "latest-gui") {
			return resp(http.StatusOK, []byte("0.13.0")), nil
		}
		return resp(http.StatusOK, nil), nil
	}))
	svc := svcFor(&selfUpd{fake: &fake{}, check: sourcesForCLI()})
	svc.Exe = func() (string, error) { return "", errors.New("没有路径") }
	if _, err := svc.UpdateApply(context.Background(), "gui"); err == nil {
		t.Fatal("拿不到自身路径应报错")
	}
}

func TestUpdateCheckTimesOutQuickly(t *testing.T) {
	// 20 秒上限：源挂住时界面要显示"超时"，不是永远转圈。
	withTransport(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	}))
	svc := svcFor(&selfUpd{fake: &fake{}, check: sourcesForCLI()})
	if _, err := svc.UpdateCheck(context.Background()); err == nil {
		t.Fatal("源不可达时应报错")
	}
}

// update 包的类型也被 state 直接引用（Result 透给界面），这里钉住字段名不变。
func TestUpdateResultShape(t *testing.T) {
	var r update.Result
	r.GUI.Current = "0.12.3"
	if r.GUI.Current != "0.12.3" {
		t.Fatal("update.Result 的结构被改动了")
	}
}
