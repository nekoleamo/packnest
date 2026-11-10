package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s，期望 HEAD", r.Method)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Content-Length", "42")
	}))
	defer srv.Close()

	res, err := New().Head(context.Background(), srv.URL+"/node")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if res.Status != 200 || res.AcceptRanges != "bytes" || res.ETag != `"abc"` || res.ContentLength != 42 {
		t.Fatalf("Head 结果异常: %+v", res)
	}
}

func TestHead404Permanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := New().Head(context.Background(), srv.URL)
	var se *StatusError
	if !errors.As(err, &se) || !se.Permanent() || se.Code != 404 {
		t.Fatalf("期望 404 Permanent 错误，得到 %v", err)
	}
}

func TestGetRetries5xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	body, err := New().Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("请求次数 = %d，期望 3（1 次 + 2 次重试）", got)
	}
}

func TestGetNoRetryOn404(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := New().Get(context.Background(), srv.URL); err == nil {
		t.Fatal("期望失败")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("4xx 不该重试，请求次数 = %d", got)
	}
}

func TestOfflineBlocksRequests(t *testing.T) {
	c := New()
	c.Offline = true
	if _, err := c.Get(context.Background(), "http://127.0.0.1:1/x"); !errors.Is(err, ErrOffline) {
		t.Fatalf("期望 ErrOffline，得到 %v", err)
	}
	if _, err := c.Head(context.Background(), "http://127.0.0.1:1/x"); !errors.Is(err, ErrOffline) {
		t.Fatalf("期望 ErrOffline，得到 %v", err)
	}
	if _, err := c.Download(context.Background(), "http://127.0.0.1:1/x", "/tmp/nope"); !errors.Is(err, ErrOffline) {
		t.Fatalf("期望 ErrOffline，得到 %v", err)
	}
}

func TestGetIndexUsesTTL(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fmt.Fprint(w, `{"versions":["1.0.0"]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "index.json")
	metaPath := filepath.Join(dir, "index.meta.json")
	now := time.Now()
	c := New()
	c.Now = func() time.Time { return now }

	res, err := c.GetIndex(context.Background(), srv.URL, bodyPath, metaPath, time.Hour)
	if err != nil {
		t.Fatalf("GetIndex: %v", err)
	}
	if res.FromCache || string(res.Body) != `{"versions":["1.0.0"]}` {
		t.Fatalf("首次应回源: %+v", res)
	}

	// TTL 内：不发请求。
	res, err = c.GetIndex(context.Background(), srv.URL, bodyPath, metaPath, time.Hour)
	if err != nil || !res.FromCache {
		t.Fatalf("TTL 内应命中缓存: %+v %v", res, err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("TTL 内不应再请求，请求次数 = %d", got)
	}

	// TTL 过期 + 304：仍是缓存，元数据刷新。
	now = now.Add(2 * time.Hour)
	res, err = c.GetIndex(context.Background(), srv.URL, bodyPath, metaPath, time.Hour)
	if err != nil || !res.NotModified {
		t.Fatalf("应走 304: %+v %v", res, err)
	}
	if res.Meta.FetchedAt != now {
		t.Fatalf("304 后应刷新 FetchedAt，得到 %v", res.Meta.FetchedAt)
	}
}

func TestGetIndexFallsBackToStale(t *testing.T) {
	var down int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&down) == 1 {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"versions":["2.0.0"]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "index.json")
	metaPath := filepath.Join(dir, "index.meta.json")
	now := time.Now()
	c := New()
	c.Now = func() time.Time { return now }
	if _, err := c.GetIndex(context.Background(), srv.URL, bodyPath, metaPath, time.Hour); err != nil {
		t.Fatalf("首次: %v", err)
	}
	atomic.StoreInt32(&down, 1)
	now = now.Add(3 * time.Hour)
	res, err := c.GetIndex(context.Background(), srv.URL, bodyPath, metaPath, time.Hour)
	if err != nil {
		t.Fatalf("应回退过期缓存而不是报错: %v", err)
	}
	if !res.Stale || string(res.Body) != `{"versions":["2.0.0"]}` {
		t.Fatalf("期望 stale 缓存: %+v", res)
	}
}

func TestGetIndexOfflineWithoutCache(t *testing.T) {
	dir := t.TempDir()
	if _, err := New().GetIndex(context.Background(), "http://127.0.0.1:1/x",
		filepath.Join(dir, "i.json"), filepath.Join(dir, "i.meta.json"), time.Hour); err == nil {
		t.Fatal("离线且无缓存应报错")
	}
	c := New()
	c.Offline = true
	if _, err := c.GetIndex(context.Background(), "http://127.0.0.1:1/x",
		filepath.Join(dir, "i.json"), filepath.Join(dir, "i.meta.json"), time.Hour); !errors.Is(err, ErrOffline) {
		t.Fatalf("期望 ErrOffline，得到 %v", err)
	}
}

func TestDownloadResumes(t *testing.T) {
	payload := []byte("0123456789abcdefghij")
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		http.ServeContent(w, r, "node.tar.gz", time.Now(), newSeeker(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "node.tar.gz")
	// 预置半截 .part + meta，模拟上次中断。
	if err := os.WriteFile(dst+".part", payload[:8], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(dst+".part.meta", partMeta{URL: srv.URL, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}

	n, err := New().Download(context.Background(), srv.URL, dst)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("下载字节 = %d，期望 %d", n, len(payload))
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("产物内容不符: %q %v", got, err)
	}
	if len(ranges) == 0 || ranges[0] != "bytes=8-" {
		t.Fatalf("应带 Range 续传，实际 %v", ranges)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("完成后 .part 应被清理")
	}
}

func TestDownloadNoRangeFallback(t *testing.T) {
	payload := []byte("abcdefghij")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 忽略 Range，永远全量返回 200。
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.zip")
	if err := os.WriteFile(dst+".part", []byte("junkjunk"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := New().Download(context.Background(), srv.URL, dst)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("字节 = %d", n)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != string(payload) {
		t.Fatalf("不支持 Range 时应全量重下，得到 %q", got)
	}
}

func TestDownloadTruncatedFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tgz")
	if _, err := New().Download(context.Background(), srv.URL, dst); err == nil {
		t.Fatal("截断的产物应报错")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("失败后不应留下成品文件")
	}
}

func TestDownload416DiscardsPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tgz")
	_ = os.WriteFile(dst+".part", []byte("xyz"), 0o644)
	if _, err := New().Download(context.Background(), srv.URL, dst); err == nil {
		t.Fatal("416 应报错")
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("416 后 .part 应作废")
	}
}

func TestParseContentRangeTotal(t *testing.T) {
	cases := map[string]struct {
		total int64
		ok    bool
	}{
		"bytes 8-19/20": {20, true},
		"bytes 0-0/1":   {1, true},
		"":              {0, false},
		"garbage":       {0, false},
		"bytes 1-2/*":   {0, false},
	}
	for in, want := range cases {
		got, ok := parseContentRangeTotal(in)
		if ok != want.ok || got != want.total {
			t.Errorf("parseContentRangeTotal(%q) = %d/%v，期望 %d/%v", in, got, ok, want.total, want.ok)
		}
	}
}

type seeker struct {
	data []byte
	pos  int64
}

func newSeeker(b []byte) *seeker { return &seeker{data: b} }

func (s *seeker) Read(p []byte) (int, error) {
	if s.pos >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += int64(n)
	return n, nil
}

func (s *seeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		s.pos = offset
	case 1:
		s.pos += offset
	case 2:
		s.pos = int64(len(s.data)) + offset
	}
	return s.pos, nil
}

func TestStatusErrorMessage(t *testing.T) {
	e := &StatusError{URL: "https://x/y", Code: 404}
	if !strings.Contains(e.Error(), "404") || strings.Contains(e.Error(), "(") {
		t.Fatalf("无 reason 时消息异常: %q", e.Error())
	}
	e.Reason = "Not Found"
	if !strings.Contains(e.Error(), "Not Found") {
		t.Fatalf("有 reason 时应带上: %q", e.Error())
	}
	if (&StatusError{Code: 429}).Permanent() {
		t.Fatal("429 应可重试")
	}
	if (&StatusError{Code: 500}).Permanent() {
		t.Fatal("5xx 应可重试")
	}
	if !(&StatusError{Code: 400}).Permanent() {
		t.Fatal("400 不该重试")
	}
}

func TestClientDefaults(t *testing.T) {
	c := &Client{Retries: -1}
	if c.retries() != 0 {
		t.Fatal("负重试次数应为 0")
	}
	if c.now().IsZero() {
		t.Fatal("now 应有默认实现")
	}
	if DefaultRetries != 2 {
		t.Fatal("默认单源最多重试 2 次（方案 §6.3）")
	}
}

func TestWriteFileAtomicError(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "no-such-dir")
	if err := writeFileAtomic(filepath.Join(missingDir, "x.json"), []byte("{}")); err == nil {
		t.Fatal("目录不存在应报错")
	}
}

func TestDownloadContentRangeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 8-19/999")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("xyz"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tar.gz")
	if err := os.WriteFile(dst+".part", []byte("12345678"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(dst+".part.meta", partMeta{URL: srv.URL, Size: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Download(context.Background(), srv.URL, dst); err == nil {
		t.Fatal("Content-Range 不一致应作废重下并报错")
	}
}

func TestDownloadIgnoresForeignPartMeta(t *testing.T) {
	payload := bytes.Repeat([]byte("z"), 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			t.Errorf("meta 属于别的 URL，不该续传，得到 Range=%q", r.Header.Get("Range"))
		}
		http.ServeContent(w, r, "x", time.Now(), bytes.NewReader(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tar.gz")
	if err := os.WriteFile(dst+".part", bytes.Repeat([]byte("y"), 16), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(dst+".part.meta", partMeta{URL: "http://other/x", Size: 32}); err != nil {
		t.Fatal(err)
	}
	n, err := New().Download(context.Background(), srv.URL, dst)
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("应全量重下: %d/%v", n, err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Fatal("内容不符")
	}
}

func TestDownloadServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tgz")
	if _, err := New().Download(context.Background(), srv.URL, dst); err == nil {
		t.Fatal("5xx 重试耗尽后应报错")
	}
}

func TestGetIndexBodyWriteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New().GetIndex(context.Background(), srv.URL,
		filepath.Join(blocker, "i.json"), filepath.Join(blocker, "i.meta.json"), 0)
	if err == nil {
		t.Fatal("缓存目录无法创建时应报错")
	}
}

func TestDownloadProgressCallback(t *testing.T) {
	payload := []byte("0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x", time.Now(), bytes.NewReader(payload))
	}))
	defer srv.Close()

	var calls int
	var lastDone int64
	c := New()
	c.Progress = func(done, total int64) {
		calls++
		lastDone = done
	}
	dir := t.TempDir()
	if _, err := c.Download(context.Background(), srv.URL, filepath.Join(dir, "x.bin")); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if calls == 0 {
		t.Fatal("应回调进度")
	}
	if lastDone != int64(len(payload)) {
		t.Fatalf("最后一次进度应是总字节 %d，得到 %d", len(payload), lastDone)
	}
}

func TestDownloadProgressNilSafe(t *testing.T) {
	// 没设 Progress 时不该 panic（progressReader 只在设置了才启用）
	payload := []byte("abc")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()
	if _, err := New().Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "x")); err != nil {
		t.Fatalf("Download: %v", err)
	}
}

// 半成品被清掉、只剩 meta 时，旧的大小记录不能让新下载假报"大小不符"。
func TestDownloadIgnoresStalePartMeta(t *testing.T) {
	payload := []byte("0123456789")
	// 故意不给 Content-Length（chunked）：这样"未知长度"分支才是被考的那条路径。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload[:4])
		w.(http.Flusher).Flush()
		_, _ = w.Write(payload[4:])
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "x.tar.gz")
	// meta 说上次下到 500 字节（半截文件已经被清扫掉了）。
	if err := writePartMeta(dst+".part.meta", partMeta{URL: srv.URL, Size: 500}); err != nil {
		t.Fatal(err)
	}
	n, err := New().Download(context.Background(), srv.URL, dst)
	if err != nil {
		t.Fatalf("残留 meta 不该导致失败: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("字节 = %d，期望 %d", n, len(payload))
	}
	if got, _ := os.ReadFile(dst); string(got) != string(payload) {
		t.Fatalf("产物内容不符: %q", got)
	}
}

// 索引拉取必须有总超时：慢源（npm registry 元数据可能有几十 MB）不能把命令挂死。
func TestGetIndexTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("{"))
			f.Flush()
		}
		time.Sleep(2 * time.Second) // 卡住不返回
	}))
	defer srv.Close()

	c := New()
	c.IndexTimeout = 150 * time.Millisecond
	dir := t.TempDir()
	start := time.Now()
	_, err := c.GetIndex(context.Background(), srv.URL+"/idx", filepath.Join(dir, "b.json"), filepath.Join(dir, "m.json"), time.Hour)
	if err == nil {
		t.Fatal("慢源应超时报错，而不是挂住")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Fatalf("错误信息应说明超时: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("超时没有生效，耗时 %s", elapsed)
	}
}

// registry 索引要带 abbreviated Accept 头（25MB 完整元数据 → 2MB）。
func TestGetIndexAcceptHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"versions":{}}`))
	}))
	defer srv.Close()

	c := New()
	dir := t.TempDir()
	if _, err := c.GetIndexAccept(context.Background(), srv.URL+"/npm", filepath.Join(dir, "b.json"), filepath.Join(dir, "m.json"), time.Hour, "application/vnd.npm.install-v1+json"); err != nil {
		t.Fatal(err)
	}
	if got != "application/vnd.npm.install-v1+json" {
		t.Fatalf("Accept = %q", got)
	}
}
