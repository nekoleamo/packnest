package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
)

// TestEnableCorepackDefaultRunner：默认执行器真的去跑 <toolDir>/bin/node（测试用假脚本代替），
// 并且 corepack 目录不存在时给出可执行建议而不是静默跳过。
func TestEnableCorepackDefaultRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假 node 是 sh 脚本，Windows 由验收用例的注入路径覆盖")
	}
	root := t.TempDir()
	p, err := paths.New(root)
	if err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "tools", "node", "22.22.2")
	if err := os.MkdirAll(filepath.Join(pkgDir, "lib", "node_modules", "corepack", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 假 node：把收到的参数写进文件，退出 0
	marker := filepath.Join(root, "called.txt")
	script := "#!/bin/sh\necho \"$@\" > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "lib", "node_modules", "corepack", "dist", "corepack.js"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{opt: Options{Paths: p}}
	enabled, err := inst.enableCorepack(context.Background(), pkgDir)
	if err != nil {
		t.Fatalf("enableCorepack: %v", err)
	}
	if !enabled {
		t.Fatal("应报告已启用")
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("默认执行器没有真的运行 node: %v", err)
	}
	if !strings.Contains(string(got), "--install-directory") || !strings.Contains(string(got), p.GlobalCorepackBin()) {
		t.Fatalf("应把 corepack enable 指到独立段: %q", got)
	}

	// 没有 corepack 的 node（25+ 布局）→ 明确报错
	noCP := t.TempDir()
	if _, err := inst.enableCorepack(context.Background(), noCP); err == nil ||
		!strings.Contains(err.Error(), "pk install pnpm") {
		t.Fatalf("缺 corepack 应报错并给建议: %v", err)
	}
}

// TestPruneDanglingShims：gc 回收 node 版本后旧 corepack shim 会悬空，
// 悬空的软链必须被清掉（否则下一次 corepack enable 在 realpath 上中断）。
func TestPruneDanglingShims(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 shim 是 .cmd，不是软链")
	}
	dir := t.TempDir()
	live := filepath.Join(dir, "live.js")
	if err := os.WriteFile(live, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(live, filepath.Join(dir, "yarn")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone.js"), filepath.Join(dir, "pnpm")); err != nil {
		t.Fatal(err)
	}
	pruneDanglingShims(dir)
	if _, err := os.Lstat(filepath.Join(dir, "pnpm")); !os.IsNotExist(err) {
		t.Fatalf("悬空软链应被删掉: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "yarn")); err != nil {
		t.Fatalf("有效软链不该被动: %v", err)
	}
	if _, err := os.Lstat(live); err != nil {
		t.Fatalf("普通文件不该被动: %v", err)
	}
}
