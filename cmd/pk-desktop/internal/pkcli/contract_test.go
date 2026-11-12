package pkcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestContractAgainstRealCLI：用仓库构建出来的真 pk 跑一遍 docs/json-schema.md 里的只读命令，
// 断言 GUI 侧的结构体还能解析（契约漂移会在这里先红）。
//
// 没构建 CLI 时跳过（CI 里先 `make build`）；用临时 PK_ROOT，不碰用户真实状态。
func TestContractAgainstRealCLI(t *testing.T) {
	bin := os.Getenv("PK_BIN")
	if bin == "" {
		// 从 cmd/pk-desktop/internal/pkcli 往上找到仓库根的 bin/pk
		cand, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "bin", "pk"))
		if err == nil {
			bin = cand
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("没找到 pk 二进制（先 make build，或设 PK_BIN）")
	}
	root := t.TempDir()
	cl := &Client{Path: bin, Env: append(os.Environ(), "PK_ROOT="+root, "PK_SKIP_VERSION_CHECK=1")}
	ctx := context.Background()

	info, err := cl.Version(ctx)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if info.Schema != MinSchema || info.Version == "" {
		t.Fatalf("version 契约不对: %+v", info)
	}
	if _, err := cl.List(ctx); err != nil { // 空台账也要能解析（数组契约）
		t.Fatalf("list: %v", err)
	}
	if _, err := cl.Doctor(ctx, false, false, false); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if _, err := cl.Mirrors(ctx); err != nil {
		t.Fatalf("mirror list: %v", err)
	}
	gc, err := cl.GC(ctx, true, 2) // 预演：不删东西（顺带覆盖 --keep）
	if err != nil {
		t.Fatalf("gc --dry-run --keep 2: %v", err)
	}
	if !gc.DryRun {
		t.Fatalf("gc --dry-run 应回报 DryRun=true: %+v", gc)
	}
	if gc.Keep != 2 {
		t.Fatalf("gc --keep 2 应回报 keep=2: %+v", gc)
	}
}

// CompilePK 只是确保测试里引用了 exec（未来要构建临时二进制时用得上）。
var _ = exec.Command

// TestProbeContract：`pk mirror probe --json` 的结构（GUI 的"镜像体检"表）。
func TestProbeContract(t *testing.T) {
	bin := os.Getenv("PK_BIN")
	if bin == "" {
		cand, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "bin", "pk"))
		if err == nil {
			bin = cand
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("没找到 pk 二进制（先 make build，或设 PK_BIN）")
	}
	cl := &Client{Path: bin, Env: append(os.Environ(), "PK_ROOT="+t.TempDir())}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rows, err := cl.Probe(ctx, "node")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("probe 应该至少返回一条源")
	}
	for _, r := range rows {
		if r.URL == "" {
			t.Fatalf("probe 结果缺 url: %+v", r)
		}
	}
}
