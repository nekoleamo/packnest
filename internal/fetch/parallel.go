package fetch

// 多连接下载（`--jobs N`）：把产物按 byte range 切成 N 段并行拉，再按序拼成一个 `.part`。
//
// 只在"冷启动 + 服务器支持 Range + 文件够大"时启用；任何异常（某段失败、Content-Range 不符、
// 服务器不支持 Range）都**整体退回单流**，交给 Download 的老路径 —— 多连接只是加速，不是唯一路径。
//
// 实测（2026-11-12，npmmirror 48MB node 包）：单流 1.14–1.23s，2/4/8 段 0.72–1.03s，约 1.3×。
import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// parallelMinBytes：小于 8MB 不分片（每段握手/建连的开销比省下的还多）。
const parallelMinBytes = 8 << 20

// shardMinBytes：每段至少 1MB，避免 --jobs 100 这种把请求切碎。
const shardMinBytes = 1 << 20

// parallelDownload 尝试多连接下载；ok=false 表示"这条路走不通，请用单流"。
func (c *Client) parallelDownload(ctx context.Context, url, dst string) (int64, bool, error) {
	hc := c.shardHTTP(c.Jobs)
	size, ok := c.remoteSize(ctx, hc, url)
	if !ok || size < parallelMinBytes {
		return 0, false, nil
	}
	n := c.Jobs
	if maxN := int(size / shardMinBytes); n > maxN {
		n = maxN
	}
	if n < 2 {
		return 0, false, nil
	}

	partPath := dst + ".part"
	var done int64
	var mu sync.Mutex
	var firstErr error

	shards := make([]string, 0, n)
	defer func() { // 失败时把分片清干净（成功时已经拼走了）
		for _, p := range shards {
			_ = os.Remove(p)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		start := int64(i) * size / int64(n)
		end := int64(i+1)*size/int64(n) - 1
		if i == n-1 {
			end = size - 1
		}
		p := fmt.Sprintf("%s.%d", partPath, i)
		shards = append(shards, p)
		wg.Add(1)
		go func(p string, start, end int64) {
			defer wg.Done()
			got, err := c.fetchShard(ctx, hc, url, p, start, end, &done, size)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if got != end-start+1 {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("第 %d-%d 字节只拿到 %d 字节", start, end, got)
				}
				mu.Unlock()
			}
		}(p, start, end)
	}
	wg.Wait()
	if firstErr != nil {
		return 0, false, firstErr
	}

	// 按序拼接（分片名里带序号，文件名排序即字节序，但这里按下标拼更稳）
	if err := concatFiles(partPath, shards); err != nil {
		return 0, false, err
	}
	if err := os.Rename(partPath, dst); err != nil {
		return 0, false, fmt.Errorf("落盘 %q 失败: %w", dst, err)
	}
	return size, true, nil
}

// shardHTTP 给分片单独一个 HTTP/1.1 客户端。
// 为什么要关 HTTP/2：h2 下所有分片共用一条 TCP 连接、受同一套流控（Go 默认 initial window 64KB），
// 分片越多越互相挤 —— 实测（npmmirror）8 段并行用 h2 反而比单流还慢，换成独立 HTTP/1.1 连接后才有 1.5× 收益。
func (c *Client) shardHTTP(n int) *http.Client {
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok || tr == nil {
		return c.HTTP
	}
	nt := tr.Clone()
	nt.ForceAttemptHTTP2 = false
	nt.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{} // 显式禁用 h2 升级
	if nt.TLSClientConfig == nil {
		nt.TLSClientConfig = &tls.Config{}
	} else {
		nt.TLSClientConfig = nt.TLSClientConfig.Clone()
	}
	nt.TLSClientConfig.NextProtos = []string{"http/1.1"} // 只谈判 HTTP/1.1：否则服务端仍回 h2 帧，transport 会判"malformed response"
	nt.MaxIdleConnsPerHost = n
	return &http.Client{Transport: nt, CheckRedirect: c.HTTP.CheckRedirect}
}

// remoteSize 用 `Range: bytes=0-0` 探总大小；服务器不支持 Range 时返回 ok=false。
func (c *Client) remoteSize(ctx context.Context, hc *http.Client, url string) (int64, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	c.setHeaders(req)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusPartialContent {
		return 0, false // 200 = 忽略 Range；其它错误也让单流去报
	}
	total, ok := parseContentRangeTotal(resp.Header.Get("Content-Range"))
	return total, ok && total > 0
}

// fetchShard 拉一段（含一次重试），并把进度累加进共享计数。
func (c *Client) fetchShard(ctx context.Context, hc *http.Client, url, path string, start, end int64, done *int64, total int64) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		c.setHeaders(req)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return 0, &StatusError{URL: url, Code: resp.StatusCode, Reason: http.StatusText(resp.StatusCode)}
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			resp.Body.Close()
			return 0, err
		}
		var r io.Reader = resp.Body
		if c.Progress != nil { // 进度按全局累计算，别让每个分片各自报百分比
			r = &shardReader{r: resp.Body, done: done, total: total, fn: c.Progress}
		}
		n, copyErr := io.Copy(f, r)
		closeErr := f.Close()
		resp.Body.Close()
		if copyErr == nil && closeErr == nil {
			return n, nil
		}
		lastErr = errors.Join(copyErr, closeErr)
		_ = os.Remove(path)
		atomic.AddInt64(done, -n)
	}
	return 0, lastErr
}

// shardReader 把分片的字节累加进共享计数，再按"全局已完成/总大小"回报进度。
type shardReader struct {
	r     io.Reader
	done  *int64
	total int64
	fn    func(done, total int64)
}

func (s *shardReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.fn(atomic.AddInt64(s.done, int64(n)), s.total)
	}
	return n, err
}

// concatFiles 把若干分片按给定顺序拼进 dst。
func concatFiles(dst string, parts []string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	for _, p := range parts {
		in, err := os.Open(p)
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			out.Close()
			return err
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	for _, p := range parts {
		_ = os.Remove(p)
	}
	return nil
}

// JobsFromEnv 读 PK_JOBS 作为默认并发段数（`--jobs` 会覆盖它）。
func JobsFromEnv(getenv func(string) string) int {
	if v := strings.TrimSpace(getenv("PK_JOBS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 1 {
			return n
		}
	}
	return 1
}
