// CP 验收：corepack（方案 B.2 / 决策 2：PATH 4 → 5 段）。
//
//   - node@22 夹具带 lib/node_modules/corepack → --corepack 后 global/corepack/bin 里有 shim，
//     且 `pk env` 的 PATH 是 5 段（corepack 段紧跟 bin，provider 的 pnpm 永远优先）
//   - node@26 夹具（25+ 已移除 corepack）→ 必须明确报错，并给出 `pk install pnpm` 的建议
package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/testkit"
)

// cpEnv 造一台只提供 node 的假镜像（Corepack 决定包里带不带 corepack）。
func cpEnv(t *testing.T, version string, withCorepack bool, mutate ...func(*install.Options)) (*testkit.FakeNode, *install.Installer, paths.Paths, *[]string) {
	t.Helper()
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: version, NPMVersion: "11.20.0", Corepack: withCorepack}},
		NPM:      map[string]string{"11.20.0": ">=18.17"},
	})
	p, err := paths.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	opt := install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", Corepack: true,
		// 不真跑 node：把 corepack enable 的产物（shim）按真实行为写出来
		RunCommand: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, filepath.Base(name)+" "+strings.Join(args, " "))
			dir := args[len(args)-1]
			for _, shim := range []string{"yarn", "yarnpkg", "pnpm"} {
				_ = os.WriteFile(filepath.Join(dir, shim), []byte("#!/bin/sh\n"), 0o755)
			}
			return nil
		},
		LookupProvider: func(id string) (*provider.Provider, error) {
			if id == "npm" {
				return fm.NPMProvider, nil
			}
			return fm.NodeProvider, nil
		},
	}
	for _, m := range mutate {
		m(&opt)
	}
	inst, err := install.New(opt)
	if err != nil {
		t.Fatal(err)
	}
	return fm, inst, p, &calls
}

// TestAcceptance_CP_CorepackShims：node@22 + --corepack → corepack shim 落在独立段，PATH 变 5 段。
func TestAcceptance_CP_CorepackShims(t *testing.T) {
	_, inst, p, calls := cpEnv(t, "22.22.2", true)

	res, err := inst.Install(context.Background(), "node", "22.22.2")
	if err != nil {
		t.Fatalf("装 node + corepack: %v", err)
	}
	if !res.Corepack {
		t.Fatal("结果应标记 corepack 已启用")
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "--install-directory") {
		t.Fatalf("应调用一次 corepack enable: %v", *calls)
	}
	for _, shim := range []string{"yarn", "yarnpkg", "pnpm"} {
		if _, err := os.Stat(filepath.Join(p.GlobalCorepackBin(), shim)); err != nil {
			t.Fatalf("corepack shim %s 应存在: %v", shim, err)
		}
	}
	segs := p.PathSegments()
	if len(segs) != 5 || segs[1] != p.GlobalCorepackBin() {
		t.Fatalf("PATH 必须 5 段且 corepack 段紧跟 bin: %v", segs)
	}
}

// TestAcceptance_CP_NoCorepackOnNode25：25+ 不带 corepack → 明确报错，不静默跳过。
func TestAcceptance_CP_NoCorepackOnNode25(t *testing.T) {
	_, inst, _, calls := cpEnv(t, "26.13.0", false)

	_, err := inst.Install(context.Background(), "node", "26.13.0")
	if err == nil {
		t.Fatal("node 25+ 装 --corepack 必须报错")
	}
	if !strings.Contains(err.Error(), "不自带 corepack") || !strings.Contains(err.Error(), "pk install pnpm") {
		t.Fatalf("错误信息要给可执行建议: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("不该尝试执行 corepack: %v", *calls)
	}
}

// TestAcceptance_CP_DryRunNoCorepack：`--dry-run` 只打印计划 —— 已装版本也不该重放 corepack
// （那是真往 global/corepack/bin 写 shim）。
func TestAcceptance_CP_DryRunNoCorepack(t *testing.T) {
	_, inst, p, calls := cpEnv(t, "22.22.2", true)
	ctx := context.Background()
	if _, err := inst.Install(ctx, "node", "22.22.2"); err != nil {
		t.Fatalf("先装 node: %v", err)
	}
	if err := inst.Finalize(); err != nil {
		t.Fatal(err)
	}
	// 清掉上一轮写下的 shim，再看 dry-run 会不会又写回来
	for _, shim := range []string{"yarn", "yarnpkg", "pnpm"} {
		_ = os.Remove(filepath.Join(p.GlobalCorepackBin(), shim))
	}
	*calls = nil

	// 再建一个 dry-run 安装器（与 cmd 传 DryRun 的时机一致），用同一套路径与假命令
	dry, err := install.New(install.Options{
		Paths: p, Client: fetch.New(), Cfg: store.DefaultConfig(), Profile: "cn",
		GOOS: "darwin", GOARCH: "arm64", Corepack: true, DryRun: true,
		RunCommand: func(_ context.Context, name string, args ...string) error {
			*calls = append(*calls, filepath.Base(name)+" "+strings.Join(args, " "))
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := dry.Install(ctx, "node", "22.22.2")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !res.AlreadyInstalled || res.Corepack {
		t.Fatalf("已装版本在 dry-run 下应报 already-installed 且不启用 corepack: %+v", res)
	}
	if len(*calls) != 0 {
		t.Fatalf("dry-run 不该执行 corepack enable: %v", *calls)
	}
	for _, shim := range []string{"yarn", "yarnpkg", "pnpm"} {
		if _, err := os.Stat(filepath.Join(p.GlobalCorepackBin(), shim)); !os.IsNotExist(err) {
			t.Fatalf("dry-run 不该写回 %s", shim)
		}
	}
}
