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
