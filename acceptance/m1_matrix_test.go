// 故障注入矩阵（方案 §9.3）：3 种源类型 × 10 种故障 × 3 个动作 = 90 格。
//
// 每一格都断言"要么装上、要么干净失败"，绝不允许半成品目录留在 tools/ 下。
package acceptance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// matrixSource 是一种"源类型"：决定哪一路 URL 指向可用镜像。
type matrixSource struct {
	name string
	// useDefault / useCN 决定假镜像挂在哪个档位；internal 为 true 时走 PK_MIRROR_* 覆盖。
	useDefault bool
	useCN      bool
	internal   bool
}

var matrixSources = []matrixSource{
	{name: "官方源", useDefault: true},
	{name: "国内镜像", useCN: true},
	{name: "自定义内网", internal: true},
}

// matrixBehavior 是一种故障注入。
type matrixBehavior struct {
	name string
	opts []testkit.Option
	// wantInstall 为 true 表示这一格应该装成功。
	wantInstall bool
	// wantErrContains 在失败时要求错误信息包含的关键词。
	wantErrContains string
}

const matrixVersion = "22.99.99" // 上游不存在的版本号，避免真实镜像干扰

func matrixBehaviors() []matrixBehavior {
	return []matrixBehavior{
		{name: "Status500", opts: []testkit.Option{testkit.Status(500)}, wantErrContains: "网络"},
		{name: "TruncateAt", opts: []testkit.Option{testkit.TruncateAt(64)}, wantErrContains: ""},
		{name: "DropAfter", opts: []testkit.Option{testkit.DropAfter(64)}, wantErrContains: ""},
		{name: "Slow", opts: []testkit.Option{testkit.Slow(5 * time.Millisecond)}, wantInstall: true},
		{name: "NoRange", opts: []testkit.Option{testkit.NoRange()}, wantInstall: true},
		{name: "WrongETag", opts: []testkit.Option{testkit.WrongETag()}, wantInstall: true},
		{name: "StaleIndex", opts: []testkit.Option{testkit.StaleIndex([]byte(`[{"version":"v0.0.1","lts":false}]`))}, wantErrContains: "没有满足"},
		{name: "CorruptArchive", opts: []testkit.Option{testkit.Corrupt()}, wantErrContains: "校验"},
		{name: "MissingChecksum", opts: []testkit.Option{testkit.MissingChecksum()}, wantErrContains: "校验"},
		{name: "ChecksumMismatch", opts: []testkit.Option{testkit.ChecksumMismatch()}, wantErrContains: "校验"},
	}
}

// matrixProviderTOML 按源类型生成 node 清单（默认档/cn 档分别指向活/死镜像）。
func matrixProviderTOML(t *testing.T, aliveURL, deadURL string, src matrixSource) *provider.Provider {
	t.Helper()
	defBase, cnBase := deadURL, deadURL
	if src.useDefault {
		defBase = aliveURL
	}
	if src.useCN {
		cnBase = aliveURL
	}
	toml := fmt.Sprintf(`id = "node"
display = "Node.js"
kind = "binary"
[index]
kind = "json"
[index.urls]
default = "%[1]s/node/index.json"
cn = "%[2]s/node/index.json"
[artifact]
[artifact.urls]
default = "%[1]s/node/v{version}/node-v{version}-{platform}.{ext}"
cn = "%[2]s/node/v{version}/node-v{version}-{platform}.{ext}"
[artifact.ext]
windows = "zip"
darwin = "tar.gz"
linux = "tar.gz"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-x64"
windows-amd64 = "win-x64"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[1]s/node/v{version}/SHASUMS256.txt"
cn = "%[2]s/node/v{version}/SHASUMS256.txt"
[layout]
strip = 1
bins = ["node", "npm", "npx"]
[companions.npm]
mode = "latest-compatible"
registry = "npm"
target = "lib/node_modules/npm"
`, defBase, cnBase)
	p, err := provider.Parse([]byte(toml))
	if err != nil {
		t.Fatalf("矩阵 provider 清单非法: %v", err)
	}
	return p
}

// TestAcceptance_M1_FailureMatrix 逐格验证故障矩阵（90 格）。
func TestAcceptance_M1_FailureMatrix(t *testing.T) {
	for _, src := range matrixSources {
		for _, beh := range matrixBehaviors() {
			for _, action := range []string{"install", "state", "offline"} {
				name := fmt.Sprintf("%s/%s/%s", src.name, beh.name, action)
				t.Run(name, func(t *testing.T) {
					runMatrixCell(t, src, beh, action)
				})
			}
		}
	}
}

func runMatrixCell(t *testing.T, src matrixSource, beh matrixBehavior, action string) {
	t.Helper()

	// 可用镜像（带故障注入）+ 死镜像（永远 500）。
	alive := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions:  []testkit.NodeSpec{{Version: matrixVersion, NPMVersion: "11.20.0"}},
		NPM:       map[string]string{"11.20.0": "^20.17.0 || >=22.9.0"},
		Behaviors: beh.opts,
	})
	dead := testkit.Mirror(t, map[string][]byte{}, testkit.Status(500))

	nodeP := matrixProviderTOML(t, alive.Server.URL, dead.URL, src)
	if src.internal {
		// 内网场景：清单里的档位全挂，只靠 PK_MIRROR_* 覆盖（方案 §6.5）。
		t.Setenv("PK_MIRROR_NODE_INDEX", alive.Server.URL+"/node/index.json")
		t.Setenv("PK_MIRROR_NODE_ARTIFACT", alive.Server.URL+"/node/v{version}/node-v{version}-{platform}.{ext}")
		t.Setenv("PK_MIRROR_NODE_CHECKSUM", alive.Server.URL+"/node/v{version}/SHASUMS256.txt")
	}

	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(id string) (*provider.Provider, error) {
		switch id {
		case "node":
			return nodeP, nil
		case "npm":
			return alive.NPMProvider, nil
		}
		return nil, fmt.Errorf("未知工具 %s", id)
	}
	newInstaller := func(client *fetch.Client, offline bool) *install.Installer {
		inst, err := install.New(install.Options{
			Paths: p, Client: client, Cfg: store.DefaultConfig(), Profile: "cn",
			Offline: offline, GOOS: "darwin", GOARCH: "arm64", LookupProvider: lookup,
		})
		if err != nil {
			t.Fatalf("install.New: %v", err)
		}
		return inst
	}

	// 第一格：真正安装一次（StaleIndex 需要用范围说明符才走索引）。
	specStr := matrixVersion
	if beh.name == "StaleIndex" {
		specStr = "22"
	}
	inst := newInstaller(fetch.New(), false)
	res, installErr := inst.Install(context.Background(), "node", specStr)
	if err := inst.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// 每一格都先断言"装成功/失败"的期望，再按动作追加断言。
	switch beh.wantInstall {
	case true:
		if installErr != nil {
			t.Fatalf("应装成功，实际失败: %v", installErr)
		}
	default:
		if installErr == nil {
			t.Fatalf("应失败，实际装成功了: %+v", res)
		}
		if beh.wantErrContains != "" && !strings.Contains(installErr.Error(), beh.wantErrContains) {
			t.Fatalf("错误信息应含 %q: %v", beh.wantErrContains, installErr)
		}
	}

	switch action {
	case "install":
		// 成功/失败的判定已在上面完成。
	case "state":
		assertNoPartialState(t, p, installErr == nil, beh.wantInstall)
	case "offline":
		offlineClient := fetch.New()
		offlineClient.Offline = true
		inst2 := newInstaller(offlineClient, true)
		res2, err2 := inst2.Install(context.Background(), "node", specStr)
		if installErr == nil && beh.wantInstall {
			// 已装 → 幂等（不发请求）。
			if err2 != nil {
				t.Fatalf("已装时应幂等: %v", err2)
			}
			if !res2.AlreadyInstalled {
				t.Fatalf("已装时应报告 AlreadyInstalled: %+v", res2)
			}
		} else {
			// 没装成 → 离线也必须干净失败，不能 panic 或留残。
			if err2 == nil {
				t.Fatalf("未装成时离线不该成功: %+v", res2)
			}
			assertNoPartialState(t, p, false, false)
		}
	}
}

// assertNoPartialState 断言磁盘上只有"完整安装"或"什么都没留"两种状态。
func assertNoPartialState(t *testing.T, p paths.Paths, installed, wantInstall bool) {
	t.Helper()
	entries, err := os.ReadDir(p.Tool("node"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("读取 tools/node 失败: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".tmp.") || strings.HasSuffix(name, ".old") || strings.HasSuffix(name, ".part") {
			t.Fatalf("不该留下半成品: %s", name)
		}
	}
	if installed && wantInstall {
		if len(entries) != 1 {
			t.Fatalf("装成功后应只有一个版本目录，实际 %d 个: %v", len(entries), entries)
		}
		dir := filepath.Join(p.Tool("node"), entries[0].Name())
		if _, err := os.Stat(filepath.Join(dir, "bin", "node")); err != nil {
			t.Fatalf("装成功后应有 bin/node: %v", err)
		}
		ledger, err := store.LoadLedger(p.Ledger())
		if err != nil || len(ledger.Installs) != 1 {
			t.Fatalf("装成功后台账应有 1 条: %+v %v", ledger, err)
		}
	}
	if !wantInstall {
		if len(entries) != 0 {
			t.Fatalf("失败后不该留下目录: %v", entries)
		}
		ledger, err := store.LoadLedger(p.Ledger())
		if err != nil {
			t.Fatalf("读取台账失败: %v", err)
		}
		if len(ledger.Installs) != 0 {
			t.Fatalf("失败后台账应为空: %+v", ledger.Installs)
		}
	}
}
