package state

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// 这里是 GUI 自更新需要的两个传输原语（取小文件 / 下载大文件）。
// 放在 state 里而不是 main：--serve 与原生界面走的是同一条升级路径。

// httpClient 是所有 GUI 自更新请求共用的客户端。
//
// ⚠️ 刻意**不设** Client.Timeout：那个超时覆盖"整个请求含读完 body"，
// 15s 会把 25–40MB 的 GUI 产物下载在慢网上直接掐断（表现为"下载失败: context deadline exceeded"）。
// 上限分成两层：小文件（latest/checksums）在 fetchBytes 里用 ctx 限 15s；
// 大文件由调用方的 ctx 管（UpdateApply 给 300s），进度条也会一直动。
var httpClient = &http.Client{}

// fetchBytesTimeout 是"取一个小文件"的单次上限。
const fetchBytesTimeout = 15 * time.Second

// exe 是"我自己"的路径（GUI 自更新要替换它）。
func exe() (string, error) { return os.Executable() }

// fetchBytes 取一个小文件（上限 1MB：latest / checksums 这类）。
func fetchBytes() func(context.Context, string) ([]byte, error) {
	return func(ctx context.Context, url string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, fetchBytesTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	}
}

// shortName 取 URL 最后一段（进度文案里显示文件名就够了）。
func shortName(u string) string {
	if i := lastIndexByte(u, '/'); i >= 0 {
		return u[i+1:]
	}
	return u
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// fetchToFileWithProgress 下载大文件并上报进度（GUI 的升级进度条吃这个）。
func fetchToFileWithProgress(s *Service) func(context.Context, string, string) error {
	return func(ctx context.Context, url, dst string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		total := resp.ContentLength
		var done int64
		buf := make([]byte, 128<<10)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					f.Close()
					return werr
				}
				done += int64(n)
				s.progress("下载 "+shortName(url), done, total)
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				return rerr
			}
		}
		return f.Close()
	}
}

// fetchToFile 下载大文件到磁盘。
func fetchToFile() func(context.Context, string, string) error {
	return func(ctx context.Context, url, dst string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
}
