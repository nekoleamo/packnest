// Package fetch 是可注入的 HTTP 下载层（方案 §6.3）。
//
// 三条硬规则：
//  1. 客户端不是包级变量，必须经 Client 注入 —— 假镜像/故障注入的入口；
//  2. 断点续传用 .part + .part.meta（url/etag/size），Content-Range 不符即作废重下；
//  3. 不做长退避：单源最多重试 2 次（用户宁可切源，不可等 60s）。
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrOffline 表示 --offline 下试图访问网络。
var ErrOffline = errors.New("离线模式下禁止访问网络")

// 默认超时（方案 §6.3 第 3 条）。
const (
	DefaultConnectTimeout = 5 * time.Second
	DefaultHeaderTimeout  = 8 * time.Second
	DefaultMinThroughput  = 1 << 20 // 1MB/s，用于按体积估算整体超时
	DefaultRetries        = 2
	DefaultChunkTimeout   = 10 * time.Minute
)

// StatusError 表示 HTTP 状态非 2xx（可据此判定网络类失败，退出码 3）。
type StatusError struct {
	URL    string
	Code   int
	Reason string
}

func (e *StatusError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s 返回 HTTP %d (%s)", e.URL, e.Code, e.Reason)
	}
	return fmt.Sprintf("%s 返回 HTTP %d", e.URL, e.Code)
}

// Permanent 判断是否属于"重试也没用"的 4xx。
func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != http.StatusTooManyRequests &&
		e.Code != http.StatusRequestTimeout
}

// Client 是可注入的下载客户端。
type Client struct {
	HTTP          *http.Client
	Retries       int
	MinThroughput int64
	UserAgent     string
	// Offline 为 true 时任何请求立即返回 ErrOffline。
	Offline bool
	// Now 可注入，用于 TTL 判断（测试不 sleep）。
	Now func() time.Time
}

// New 返回带默认超时的客户端。
func New() *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   DefaultConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   DefaultConnectTimeout,
		ResponseHeaderTimeout: DefaultHeaderTimeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &Client{
		HTTP:          &http.Client{Transport: transport},
		Retries:       DefaultRetries,
		MinThroughput: DefaultMinThroughput,
		UserAgent:     "packnest/1 (cli)",
		Now:           time.Now,
	}
}

// HeadResult 是 HEAD 探测结果。
type HeadResult struct {
	URL           string
	Status        int
	ContentLength int64
	AcceptRanges  string
	ETag          string
	LastModified  string
	Elapsed       time.Duration
}

// Head 发 HEAD 请求（镜像探测用；不带 body，不计入下载量）。
func (c *Client) Head(ctx context.Context, url string) (HeadResult, error) {
	start := time.Now()
	if c.Offline {
		return HeadResult{}, ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return HeadResult{}, fmt.Errorf("构造请求失败: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return HeadResult{}, err
	}
	defer resp.Body.Close()
	res := HeadResult{
		URL:           url,
		Status:        resp.StatusCode,
		ContentLength: resp.ContentLength,
		AcceptRanges:  resp.Header.Get("Accept-Ranges"),
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		Elapsed:       time.Since(start),
	}
	if resp.StatusCode >= 400 {
		return res, &StatusError{URL: url, Code: resp.StatusCode, Reason: http.StatusText(resp.StatusCode)}
	}
	return res, nil
}

// Get 取整个响应体（小文件：索引、校验和）。
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	if c.Offline {
		return nil, ErrOffline
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries(); attempt++ {
		body, err := c.getOnce(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		var se *StatusError
		if errors.As(err, &se) && se.Permanent() {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) getOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, &StatusError{URL: url, Code: resp.StatusCode, Reason: http.StatusText(resp.StatusCode)}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	return data, nil
}

// IndexMeta 是索引缓存元数据。
type IndexMeta struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// IndexResult 是带缓存与 ETag 协商的索引获取结果。
type IndexResult struct {
	Body        []byte
	NotModified bool
	FromCache   bool
	Stale       bool // 网络失败但返回了过期缓存
	Meta        IndexMeta
}

// GetIndex 取版本索引：先看本地缓存 TTL，过期则发条件请求（304 时 0 字节），
// 网络失败时回退过期缓存并标记 Stale（方案 §6.2：索引永远不是阻塞项）。
func (c *Client) GetIndex(ctx context.Context, url, bodyPath, metaPath string, ttl time.Duration) (IndexResult, error) {
	meta, body, hasCache := readIndexCache(bodyPath, metaPath)
	res := IndexResult{Meta: meta, Body: body, FromCache: hasCache}
	if hasCache && ttl > 0 && c.now().Sub(meta.FetchedAt) < ttl {
		return res, nil
	}
	if c.Offline {
		if hasCache {
			res.Stale = true
			return res, nil
		}
		return res, ErrOffline
	}

	fresh, status, hdr, err := c.getConditional(ctx, url, meta, hasCache)
	if err != nil {
		if hasCache {
			res.Stale = true
			return res, nil
		}
		return res, err
	}
	if status == http.StatusNotModified && hasCache {
		meta.FetchedAt = c.now()
		_ = writeIndexMeta(metaPath, meta)
		res.Meta = meta
		res.NotModified = true
		return res, nil
	}
	meta = IndexMeta{URL: url, ETag: hdr.Get("ETag"), LastModified: hdr.Get("Last-Modified"), FetchedAt: c.now()}
	if err := os.MkdirAll(filepath.Dir(bodyPath), 0o755); err != nil {
		return res, fmt.Errorf("创建缓存目录失败: %w", err)
	}
	if err := writeFileAtomic(bodyPath, fresh); err != nil {
		return res, err
	}
	_ = writeIndexMeta(metaPath, meta)
	res.Body, res.Meta, res.FromCache = fresh, meta, false
	return res, nil
}

// getConditional 发条件请求；返回 body（304 时为 nil）、状态码与响应头。
func (c *Client) getConditional(ctx context.Context, url string, meta IndexMeta, hasCache bool) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("构造请求失败: %w", err)
	}
	c.setHeaders(req)
	if hasCache && meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if hasCache && meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries(); attempt++ {
		resp, err := c.do(req.Clone(ctx))
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("读取响应体失败: %w", readErr)
			continue
		}
		if resp.StatusCode == http.StatusNotModified {
			return nil, resp.StatusCode, resp.Header, nil
		}
		if resp.StatusCode >= 400 {
			lastErr = &StatusError{URL: url, Code: resp.StatusCode, Reason: http.StatusText(resp.StatusCode)}
			if se, ok := lastErr.(*StatusError); ok && se.Permanent() {
				return nil, resp.StatusCode, resp.Header, lastErr
			}
			continue
		}
		return body, resp.StatusCode, resp.Header, nil
	}
	return nil, 0, nil, lastErr
}

// Download 下载产物到 dst，支持断点续传与不支持 Range 时的退化。
func (c *Client) Download(ctx context.Context, url, dst string) (int64, error) {
	if c.Offline {
		return 0, ErrOffline
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, fmt.Errorf("创建下载目录失败: %w", err)
	}
	partPath := dst + ".part"
	metaPath := partPath + ".meta"

	var lastErr error
	for attempt := 0; attempt <= c.retries(); attempt++ {
		n, err := c.downloadOnce(ctx, url, partPath, metaPath)
		if err == nil {
			if err := os.Rename(partPath, dst); err != nil {
				return 0, fmt.Errorf("落盘 %q 失败: %w", dst, err)
			}
			_ = os.Remove(metaPath)
			return n, nil
		}
		lastErr = err
		var se *StatusError
		if errors.As(err, &se) && se.Permanent() {
			break
		}
	}
	_ = os.Remove(partPath)
	_ = os.Remove(metaPath)
	return 0, lastErr
}

type partMeta struct {
	URL  string `json:"url"`
	ETag string `json:"etag,omitempty"`
	Size int64  `json:"size"`
}

func (c *Client) downloadOnce(ctx context.Context, url, partPath, metaPath string) (int64, error) {
	var offset int64
	var pm partMeta
	if data, err := os.ReadFile(metaPath); err == nil && json.Unmarshal(data, &pm) == nil && pm.URL == url {
		if st, err := os.Stat(partPath); err == nil {
			offset = st.Size()
		}
	} else {
		pm = partMeta{URL: url}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("构造请求失败: %w", err)
	}
	c.setHeaders(req)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if pm.ETag != "" {
			req.Header.Set("If-Range", pm.ETag)
		}
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// 服务器不支持 Range：全量重下（丢掉的只是续传进度，不是错误）。
		offset = 0
		pm.ETag = resp.Header.Get("ETag")
		if resp.ContentLength > 0 {
			pm.Size = resp.ContentLength
		}
	case http.StatusPartialContent:
		total, ok := parseContentRangeTotal(resp.Header.Get("Content-Range"))
		if !ok || (pm.Size > 0 && total > 0 && pm.Size != total) {
			_ = os.Remove(partPath)
			return 0, fmt.Errorf("续传校验失败：Content-Range %q 与本地记录不一致，已作废重下", resp.Header.Get("Content-Range"))
		}
		if total > 0 {
			pm.Size = total
		}
	case http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(partPath)
		return 0, fmt.Errorf("续传位置无效（HTTP 416），已作废")
	default:
		if resp.StatusCode >= 400 {
			return 0, &StatusError{URL: url, Code: resp.StatusCode, Reason: http.StatusText(resp.StatusCode)}
		}
		return 0, fmt.Errorf("下载 %q 失败：HTTP %d", url, resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return 0, fmt.Errorf("打开临时文件失败: %w", err)
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err := writePartMeta(metaPath, pm); err != nil {
		return 0, err
	}
	if copyErr != nil {
		return offset + n, fmt.Errorf("下载中断（已保留 %d 字节待续传）: %w", offset+n, copyErr)
	}
	if closeErr != nil {
		return offset + n, fmt.Errorf("写入失败: %w", closeErr)
	}
	total := offset + n
	if pm.Size > 0 && total != pm.Size {
		return total, fmt.Errorf("产物大小不符：期望 %d 字节，实得 %d", pm.Size, total)
	}
	return total, nil
}

func parseContentRangeTotal(v string) (int64, bool) {
	// bytes 100-199/200
	slash := strings.LastIndex(v, "/")
	if slash < 0 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(v[slash+1:]), 10, 64)
	if err != nil || total < 0 {
		return 0, false
	}
	return total, true
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept-Encoding", "identity")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	return resp, nil
}

func (c *Client) retries() int {
	if c.Retries < 0 {
		return 0
	}
	return c.Retries
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func readIndexCache(bodyPath, metaPath string) (IndexMeta, []byte, bool) {
	var meta IndexMeta
	metaData, err := os.ReadFile(metaPath)
	if err != nil || json.Unmarshal(metaData, &meta) != nil {
		return IndexMeta{}, nil, false
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil || len(body) == 0 {
		return IndexMeta{}, nil, false
	}
	return meta, body, true
}

func writeIndexMeta(path string, meta IndexMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func writePartMeta(path string, pm partMeta) error {
	data, err := json.Marshal(pm)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %q 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("落盘 %q 失败: %w", path, err)
	}
	return nil
}
