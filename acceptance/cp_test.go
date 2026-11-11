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
func cpEnv(t *testing.T, version string, withCorepack bool) (*testkit.FakeNode, *install.Installer, paths.Paths, *[]string) {
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
	inst, err := install.New(install.Options{
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
	})
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
