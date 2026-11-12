package fetch

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// rangeServer 造一个支持 Range 的假源（payload 按 9MB 造，刚过 8MB 阈值）。
func rangeServer(t *testing.T, payload []byte, supportRange bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if !supportRange || rangeHdr == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
			return
		}
		// 支持 "bytes=N-M" 与续传用的开区间 "bytes=N-"
		spec := strings.TrimPrefix(rangeHdr, "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(payload) - 1
		if len(parts) == 2 && parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}
		if start < 0 || end >= len(payload) || start > end {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
}

func bigPayload(t *testing.T) []byte {
	t.Helper()
	return bytes.Repeat([]byte("packnest!"), (9<<20)/9) // ≈9MB，>parallelMinBytes
}

// TestParallelDownloadSplits：支持 Range 时走分片，拼出来的字节必须与源一致。
func TestParallelDownloadSplits(t *testing.T) {
	payload := bigPayload(t)
	srv := rangeServer(t, payload, true)
	defer srv.Close()

	c := New()
	c.Jobs = 4
	dst := filepath.Join(t.TempDir(), "art.tar.gz")
	n, err := c.Download(context.Background(), srv.URL, dst)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || !bytes.Equal(got, payload) {
		t.Fatalf("分片拼出来的字节不对：n=%d len=%d equal=%v", n, len(got), bytes.Equal(got, payload))
	}
	// 分片临时文件必须清干净（只留最终的 dst）
	entries, _ := os.ReadDir(filepath.Dir(dst))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".part") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

// TestParallelFallsBackWithoutRange：源不支持 Range（永远 200）时必须回落到单流，且结果正确。
func TestParallelFallsBackWithoutRange(t *testing.T) {
	payload := bigPayload(t)
	srv := rangeServer(t, payload, false)
	defer srv.Close()

	c := New()
	c.Jobs = 4
	dst := filepath.Join(t.TempDir(), "art2")
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("不支持 Range 时应回落到单流: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Fatal("回落单流后的字节不对")
	}
}

// TestParallelSkipsWhenResuming：已有 .part 续传现场时不做分片（不然会把续传进度冲掉）。
func TestParallelSkipsWhenResuming(t *testing.T) {
	payload := bigPayload(t)
	srv := rangeServer(t, payload, true)
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "art3")
	// 造一个"下了前 1MB"的现场
	if err := os.WriteFile(dst+".part", payload[:1<<20], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(dst+".part.meta", partMeta{URL: srv.URL, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	c := New()
	c.Jobs = 4
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, payload) {
		t.Fatalf("续传后的字节不对：got=%d want=%d", len(got), len(payload))
	}
}

// TestJobsFromEnv：PK_JOBS 只认 >1 的整数（其它值一律当 1，别让拼错的变量把下载切成奇怪段数）。
func TestJobsFromEnv(t *testing.T) {
	cases := map[string]int{"": 1, "4": 4, "1": 1, "0": 1, "-3": 1, "abc": 1, " 8 ": 8}
	for val, want := range cases {
		got := JobsFromEnv(func(string) string { return val })
		if got != want {
			t.Errorf("JobsFromEnv(%q) = %d，期望 %d", val, got, want)
		}
	}
}

// TestShardHTTPFallsBack：注入的 HTTP 客户端不是 *http.Transport 时直接用它（测试替身不该被改造）。
func TestShardHTTPFallsBack(t *testing.T) {
	custom := &http.Client{}
	c := &Client{HTTP: custom}
	if got := c.shardHTTP(4); got != custom {
		t.Fatalf("非标准 transport 应原样返回: %p", got)
	}
	c2 := New()
	if got := c2.shardHTTP(4); got == c2.HTTP {
		t.Fatal("标准 client 应派生一个独立的 HTTP/1.1 客户端")
	}
}

// TestParallelRetryAndPartialFailure：某段第一次 500 要重试成功；某段始终失败则整体退回单流。
func TestParallelRetryAndPartialFailure(t *testing.T) {
	payload := bigPayload(t)
	var failOnce atomic.Bool
	var alwaysFail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" { // 单流兜底：整包给
			_, _ = w.Write(payload)
			return
		}
		if alwaysFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if failOnce.CompareAndSwap(false, true) { // 第一段的第一枪故意失败
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		spec := strings.TrimPrefix(rangeHdr, "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(payload) - 1
		if len(parts) == 2 && parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	c := New()
	c.Jobs = 4
	dst := filepath.Join(t.TempDir(), "retry")
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("单段失败应重试成功: %v", err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, payload) {
		t.Fatal("重试后的字节不对")
	}

	// 所有分段永久 500：多连接整体放弃，回落单流 —— 单流不带 Range，服务端照样给整包，所以最终要成功
	alwaysFail.Store(true)
	dst2 := filepath.Join(t.TempDir(), "fallback")
	if _, err := c.Download(context.Background(), srv.URL, dst2); err != nil {
		t.Fatalf("分段全失败时应回落单流并成功: %v", err)
	}
	if got, _ := os.ReadFile(dst2); !bytes.Equal(got, payload) {
		t.Fatal("回落单流后的字节不对")
	}
}

// TestParallelSkipsSmallFile：小于 8MB 不分片（走单流，分片文件都不该出现）。
func TestParallelSkipsSmallFile(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 512<<10) // 512KB
	srv := rangeServer(t, payload, true)
	defer srv.Close()
	c := New()
	c.Jobs = 8
	dst := filepath.Join(t.TempDir(), "small")
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, payload) {
		t.Fatal("小文件应走单流且字节正确")
	}
}

// TestParallelProgress：分片下载也要把进度汇总成"全局已完成/总大小"报出去（GUI 与 CLI 都靠它画进度条）。
func TestParallelProgress(t *testing.T) {
	payload := bigPayload(t)
	srv := rangeServer(t, payload, true)
	defer srv.Close()
	c := New()
	c.Jobs = 4
	var lastDone, lastTotal int64
	var calls int
	c.Progress = func(done, total int64) { lastDone, lastTotal, calls = done, total, calls+1 }
	dst := filepath.Join(t.TempDir(), "prog")
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if calls == 0 {
		t.Fatal("分片下载应回报进度")
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("最后一次进度不对: done=%d total=%d want=%d", lastDone, lastTotal, len(payload))
	}
}

// TestConcatFilesErrors：拼接的两种失败都要报出来（目录不存在 / 分片缺失）。
func TestConcatFilesErrors(t *testing.T) {
	dir := t.TempDir()
	if err := concatFiles(filepath.Join(dir, "no-such-dir", "out"), []string{filepath.Join(dir, "x")}); err == nil {
		t.Fatal("目标目录不存在应报错")
	}
	if err := concatFiles(filepath.Join(dir, "out"), []string{filepath.Join(dir, "missing-part")}); err == nil {
		t.Fatal("分片缺失应报错")
	}
}

// TestFetchShardErrors：分片自身的两种失败（写不进去 / 两次都失败）都要返回错误，别静默当成功。
func TestFetchShardErrors(t *testing.T) {
	payload := bigPayload(t)
	srv := rangeServer(t, payload, true)
	defer srv.Close()
	c := New()
	hc := c.shardHTTP(2)
	var done int64

	// ① 目标目录不存在 → 打开文件失败
	if _, err := c.fetchShard(context.Background(), hc, srv.URL, filepath.Join(t.TempDir(), "no-such", "s0"), 0, 1023, &done, int64(len(payload))); err == nil {
		t.Fatal("目录不存在时应报错")
	}

	// ② 服务端永远 500 → 两次都失败
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	if _, err := c.fetchShard(context.Background(), hc, bad.URL, filepath.Join(t.TempDir(), "s1"), 0, 1023, &done, 1024); err == nil {
		t.Fatal("两次都 500 时应返回错误")
	}
}

// TestParallelShardLengthMismatch：服务端给的分片比要求的短 → 判失败并整体退回单流。
func TestParallelShardLengthMismatch(t *testing.T) {
	payload := bigPayload(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" { // 单流兜底：整包
			_, _ = w.Write(payload)
			return
		}
		// 声称给一整段，实际只给前 1KB
		spec := strings.TrimPrefix(rangeHdr, "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end, _ := strconv.Atoi(parts[1])
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start:min(start+1024, end+1)])
	}))
	defer srv.Close()

	c := New()
	c.Jobs = 4
	dst := filepath.Join(t.TempDir(), "short")
	n, err := c.Download(context.Background(), srv.URL, dst)
	if err != nil {
		t.Fatalf("分片长度不符应退回单流成功: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("回落单流后大小不对: %d", n)
	}
}
