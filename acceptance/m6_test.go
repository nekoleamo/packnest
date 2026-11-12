// M6 验收：1.3 的“日常闭环”（多连接默认自适应在 fetch 层，其余在 cmd/pk 的 m6_test.go）。
package acceptance

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

	"github.com/packnest/packnest/internal/fetch"
)

// TestAcceptance_M6_JobsAuto：Jobs 不设（0）时按产物大小自适应分段，不需要用户写 --jobs。
func TestAcceptance_M6_JobsAuto(t *testing.T) {
	payload := bytes.Repeat([]byte("packnest!"), (9<<20)/9) // ≈9MB，刚过 8MB 阈值
	var ranged int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spec := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		if spec == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
			return
		}
		atomic.AddInt32(&ranged, 1)
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(payload) - 1
		if len(parts) == 2 && parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.tar.gz")
	c := fetch.New() // Jobs 保持零值 = 自适应
	if _, err := c.Download(context.Background(), srv.URL, dst); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("拼装后的字节不对：got=%d want=%d", len(got), len(payload))
	}
	if ranged < 2 {
		t.Fatalf("9MB 产物默认应分片（Jobs=0 自适应），实际 Range 请求 %d 次", ranged)
	}

	// 显式 1 = 强制单流（给弱网/老代理留出口）
	atomic.StoreInt32(&ranged, 0)
	one := fetch.New()
	one.Jobs = 1
	if _, err := one.Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "single")); err != nil {
		t.Fatal(err)
	}
	if ranged != 0 {
		t.Fatalf("Jobs=1 应走单流，实际 Range 请求 %d 次", ranged)
	}
}
