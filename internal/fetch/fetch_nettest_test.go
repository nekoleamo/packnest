//go:build nettest

// 真网测量：多连接下载到底快多少（不进门禁，`go test -tags nettest -run TestParallelDownloadRealWorld`）。
package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParallelDownloadRealWorld 在同一份 48MB 产物上比较 --jobs 1 / 4 / 8（npmmirror 的 node 包）。
func TestParallelDownloadRealWorld(t *testing.T) {
	const url = "https://registry.npmmirror.com/-/binary/node/v22.23.5/node-v22.23.5-darwin-arm64.tar.gz"
	dir := t.TempDir()
	for _, jobs := range []int{1, 2, 4, 8} {
		c := New()
		c.Jobs = jobs
		dst := filepath.Join(dir, "art")
		start := time.Now()
		n, err := c.Download(context.Background(), url, dst)
		if err != nil {
			t.Fatalf("jobs=%d: %v", jobs, err)
		}
		el := time.Since(start)
		t.Logf("jobs=%d  %6.2fs  %5.1f MB  %5.1f MB/s", jobs, el.Seconds(),
			float64(n)/(1<<20), float64(n)/(1<<20)/el.Seconds())
		if err := os.Remove(dst); err != nil {
			t.Fatal(err)
		}
	}
}
