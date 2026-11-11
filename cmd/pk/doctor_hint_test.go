package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/store"
)

// TestAcceptance_GO_InstallHint：装了 go 时 doctor 只提示 `go install` 的产物不在 PATH，
// 绝不替用户设 GOBIN（方案 B.4：默认不动环境）。
func TestAcceptance_GO_InstallHint(t *testing.T) {
	root := t.TempDir()
	writeFakeLedger(t, root, "go", "1.27.3")
	// PATH 里没有 ~/go/bin，且 GOBIN 保持未设置
	t.Setenv("GOBIN", "")
	t.Setenv("PATH", filepath.Join(root, "bin"))

	code, out, _ := execCLI(t, "--root", root, "--json", "doctor")
	if code != 0 {
		t.Fatalf("GO_INSTALL_NOT_IN_PATH 是 info，不该让 doctor 非 0: %d\n%s", code, out)
	}
	if !strings.Contains(out, "GO_INSTALL_NOT_IN_PATH") {
		t.Fatalf("应报 GO_INSTALL_NOT_IN_PATH:\n%s", out)
	}
	if got := os.Getenv("GOBIN"); got != "" {
		t.Fatalf("doctor 不许改 GOBIN，实际 %q", got)
	}

	// GOBIN 已经在 PATH 里时不再提示
	t.Setenv("GOBIN", filepath.Join(root, "bin"))
	if _, out, _ := execCLI(t, "--root", root, "--json", "doctor"); strings.Contains(out, "GO_INSTALL_NOT_IN_PATH") {
		t.Fatalf("GOBIN 在 PATH 里时不该提示:\n%s", out)
	}
}

// TestAcceptance_PIP_UserSiteHint：装了 python 时诚实说明 pip 用户级安装不在治理范围。
func TestAcceptance_PIP_UserSiteHint(t *testing.T) {
	root := t.TempDir()
	writeFakeLedger(t, root, "python", "3.13.18")
	if _, out, _ := execCLI(t, "--root", root, "--json", "doctor"); !strings.Contains(out, "PIP_USER_SITE_UNMANAGED") {
		t.Fatalf("应报 PIP_USER_SITE_UNMANAGED:\n%s", out)
	}
}

// writeFakeLedger 造一份"该工具已装"的台账（目录真实建出来，避免台账自洽检查报错）。
func writeFakeLedger(t *testing.T, root, tool, version string) {
	t.Helper()
	dir := filepath.Join(root, "tools", tool, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := store.Ledger{Schema: store.LedgerSchema, Installs: []store.Install{{
		Tool: tool, Version: version, Path: dir, Verified: "strong", Channel: "manual",
	}}}
	data, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state", "installs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
