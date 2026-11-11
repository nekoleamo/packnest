package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcceptance_B5_PnpmBunGlobals：doctor --globals 要覆盖 pnpm 与 bun 的全局目录，
// 复用 GLOBAL_* 三个码，并用 detail.manager 标明来源（方案 B.5）。
func TestAcceptance_B5_PnpmBunGlobals(t *testing.T) {
	root := t.TempDir()
	// node 已装且是默认版本
	writeFakeLedger(t, root, "node", "22.22.2")
	cfg := filepath.Join(root, "config.toml")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[tools]\nnode = \"22.22.2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// pnpm：<PNPM_HOME>/global/<N>/node_modules/<pkg>
	pnpmPkg := filepath.Join(root, "global", "pnpm", "global", "5", "node_modules", "needsnew")
	plantPkg(t, pnpmPkg, `{"name":"needsnew","version":"1.0.0","engines":{"node":">=99"}}`)
	// bun：<BUN_INSTALL>/install/global/node_modules/<pkg>
	bunPkg := filepath.Join(root, "global", "bun", "install", "global", "node_modules", "nativeonly")
	plantPkg(t, bunPkg, `{"name":"nativeonly","version":"1.0.0","gypfile":true}`)

	code, out, _ := execCLI(t, "--root", root, "--json", "doctor", "--globals")
	if code != 0 {
		t.Fatalf("只有 warn 时 doctor 应退出 0: %d\n%s", code, out)
	}
	for _, want := range []string{"GLOBAL_ENGINES_UNMET", `"manager": "pnpm"`, "GLOBAL_NATIVE_ABI", `"manager": "bun"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor --globals 缺少 %s:\n%s", want, out)
		}
	}
}

func plantPkg(t *testing.T, dir, pkgJSON string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}
