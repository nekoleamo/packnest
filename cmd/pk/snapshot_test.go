package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
)

// 人读输出快照（完善路线 §E6 的前置）：10 个命令的**终端输出**逐字节入库。
//
// 为什么需要它：`--json` 有 golden 挡着，人读输出（表格 / 进度 / 文案）一条都没测。
// E6 要把呈现层搬到 cmd/pk/present，搬迁前后必须"逐字节一样"——没有快照就没有判据。
//
// 重建：go test ./cmd/pk -run TestHumanSnapshots -update-snap
// 快照里不能出现随机 tmp 路径 / 主机名 / Go 版本，统一归一化（见 normalizeSnap）。
var updateSnap = flag.Bool("update-snap", false, "重建 testdata/human 快照")

const snapDir = "testdata/human"

type snapCase struct {
	name string
	args []string
	seed func(t *testing.T, p paths.Paths)
	// windowsUnsafe 的用例含"按平台变"的内容（归档扩展名 / 命令名后缀），
	// Windows 上直接跳过 —— 门禁只在 macOS 与 Linux 跑，别让 Windows 开发者跑出红灯
	windowsUnsafe bool
}

func TestHumanSnapshots(t *testing.T) {
	for _, c := range snapCases() {
		t.Run(c.name, func(t *testing.T) {
			if c.windowsUnsafe && runtime.GOOS == "windows" {
				t.Skip("快照含平台相关内容，Windows 上跳过")
			}
			root, home := snapEnv(t)
			p, err := paths.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if c.seed != nil {
				c.seed(t, p)
			}
			args := append([]string{"--root", root}, c.args...)
			code, out, errOut := execCLI(t, args...)

			var sb strings.Builder
			sb.WriteString("$ pk " + strings.Join(c.args, " ") + "\n")
			if code != 0 {
				sb.WriteString("[exit " + itoa(code) + "]\n")
			}
			sb.WriteString(out)
			if errOut != "" {
				sb.WriteString("[stderr]\n" + errOut)
			}
			got := normalizeSnap(sb.String(), root, home)

			path := filepath.Join(snapDir, c.name+".txt")
			if *updateSnap {
				if err := os.MkdirAll(snapDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("缺少快照 %s（先跑 -update-snap）: %v", path, err)
			}
			if string(want) != got {
				t.Fatalf("人读输出变了（%s）：\n--- 快照 ---\n%s\n--- 实际 ---\n%s", path, want, got)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// snapEnv 把一切环境差异钉死：家目录、语言、终端宽度、PATH、Go 工具链变量。
// 不钉这些，快照在任何别的机器上都会红。
func snapEnv(t *testing.T) (root, home string) {
	t.Helper()
	root = t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PK_ROOT", "")
	t.Setenv("PK_CACHE", "")
	t.Setenv("PK_LANG", "zh")
	t.Setenv("SHELL", "/bin/zsh") // rc 文件是按 $SHELL 猜的（env --unset / --write），不钉死就会跟着机器变
	t.Setenv("PK_JOBS", "")
	t.Setenv("COLUMNS", "100")
	t.Setenv("GOTOOLCHAIN", "")
	t.Setenv("GOBIN", filepath.Join(home, "go", "bin"))
	t.Setenv("GOPATH", filepath.Join(home, "go"))
	t.Setenv("PK_PROVIDERS_DIR", "")
	t.Setenv("PK_ALLOW_HTTP", "")
	// PATH 里只放本根目录的 bin + 系统目录：doctor 的 PATH_* 码因此确定
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+"/usr/bin:/bin")
	return root, home
}

var (
	snapGoVersion = regexp.MustCompile(`go\d+\.\d+(\.\d+)?`)
	// 平台出现在三种写法里：`pk version` 的 `darwin/arm64`、产物名 `packnest_…_darwin_arm64`、
	// npmmirror 的 `node-v26.13.0-darwin-arm64`（Linux 上是 `linux-x64`）
	snapPlatform = regexp.MustCompile(`\b(darwin|linux|windows|freebsd)([/_-])(amd64|arm64|armv7l|x64|x86|386)\b`)
)

// normalizeSnap 抹掉每台机器都不一样、但与人读呈现无关的部分。
func normalizeSnap(s, root, home string) string {
	s = strings.ReplaceAll(s, root, "<ROOT>")
	s = strings.ReplaceAll(s, home, "<HOME>")
	s = snapGoVersion.ReplaceAllString(s, "go<VER>")
	s = snapPlatform.ReplaceAllString(s, "<OS>$2<ARCH>")
	return s
}

// seedLedger 造一份"装过 node（带 npm）+ pnpm"的台账，模拟真实用户的 list/which 输出。
func seedLedger(t *testing.T, p paths.Paths) {
	t.Helper()
	nodeDir := p.ToolVersion("node", "26.13.0")
	pnpmDir := p.ToolVersion("pnpm", "10.4.1")
	for _, d := range []string{nodeDir, pnpmDir} {
		if err := os.MkdirAll(filepath.Join(d, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ledger := &store.Ledger{}
	ledger.Upsert(store.Install{Tool: "node", Version: "26.13.0", Path: nodeDir,
		NpmVersion: "12.2.2", NpmBundled: "11.20.0", References: []store.Ref{{Kind: store.RefGlobal}}})
	ledger.Upsert(store.Install{Tool: "pnpm", Version: "10.4.1", Path: pnpmDir})
	if err := ledger.Save(p.Ledger()); err != nil {
		t.Fatal(err)
	}
	cfg := store.DefaultConfig()
	cfg.Tools = map[string]string{"node": "26.13.0"}
	if err := cfg.Save(p.Config()); err != nil {
		t.Fatal(err)
	}
}

// seedSelf 造 pk/packnest 两个入口（shim），which 与 doctor 的 SHIM_* 码都靠它。
func seedSelf(t *testing.T, p paths.Paths) {
	t.Helper()
	self := filepath.Join(p.Root, "pk-self")
	if err := os.WriteFile(self, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := shim.Self(p.Bin(), self, goos()); err != nil {
		t.Fatal(err)
	}
}

func seedLedgerAndSelf(t *testing.T, p paths.Paths) {
	t.Helper()
	seedLedger(t, p)
	seedSelf(t, p)
}

func snapCases() []snapCase {
	return []snapCase{
		// 全局与帮助
		{name: "version", args: []string{"version"}},
		{name: "usage", args: []string{"help"}},
		{name: "usage-install", args: []string{"help", "install"}},
		// list / which（表格与对齐的搬运重点）
		{name: "list-empty", args: []string{"list"}},
		{name: "list-seeded", args: []string{"list"}, seed: seedLedger},
		{name: "list-seeded-major", args: []string{"list", "--major", "26", "--limit", "3"}, seed: seedLedger},
		{name: "which-self", args: []string{"which", "pk"}, seed: seedSelf},
		{name: "which-missing", args: []string{"which", "zzz"}},
		// mirror
		{name: "mirror-usage", args: []string{"mirror"}},
		{name: "mirror-list", args: []string{"mirror", "list"}},
		// doctor（问题码渲染）
		{name: "doctor-empty-root", args: []string{"doctor"}},
		{name: "doctor-seeded", args: []string{"doctor", "--globals"}, seed: seedLedgerAndSelf},
		// env
		{name: "env-bash", args: []string{"env", "--shell", "bash"}},
		{name: "env-unset", args: []string{"env", "--unset"}},
		// gc / use / uninstall / install / upgrade
		{name: "gc-nothing", args: []string{"gc"}},
		{name: "use-switch", args: []string{"use", "node@26.13.0"}, seed: seedLedgerAndSelf},
		{name: "uninstall-missing", args: []string{"uninstall", "node"}},
		{name: "install-dry-run", args: []string{"install", "--offline", "--dry-run", "node@26.13.0"}, windowsUnsafe: true},
		{name: "install-help", args: []string{"install", "--help"}},
		{name: "upgrade-help", args: []string{"upgrade", "--help"}},
	}
}
