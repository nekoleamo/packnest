package state

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// 这里是 GUI 自更新需要的两个传输原语（取小文件 / 下载大文件）。
// 放在 state 里而不是 main：--serve 与原生界面走的是同一条升级路径。

// exe 是"我自己"的路径（GUI 自更新要替换它）。
func exe() (string, error) { return os.Executable() }

// fetchBytes 取一个小文件（上限 1MB：latest / checksums 这类）。
func fetchBytes() func(context.Context, string) ([]byte, error) {
	return func(ctx context.Context, url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
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

// fetchToFile 下载大文件到磁盘。
func fetchToFile() func(context.Context, string, string) error {
	return func(ctx context.Context, url, dst string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
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
