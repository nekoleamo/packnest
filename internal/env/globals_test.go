package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/packnest/packnest/internal/paths"
)

func writePkg(t *testing.T, dir, name, version, engines string, extra map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"` + name + `","version":"` + version + `"`
	if engines != "" {
		body += `,"engines":{"node":"` + engines + `"}`
	}
	for k, v := range extra {
		body += `,"` + k + `":` + v
	}
	body += `}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanGlobals(t *testing.T) {
	prefix := t.TempDir()
	writePkg(t, filepath.Join(prefix, "lib", "node_modules", "vite"), "vite", "7.0.0", ">=24", nil)
	writePkg(t, filepath.Join(prefix, "lib", "node_modules", "@scope", "cli"), "@scope/cli", "1.0.0", "", map[string]string{
		"bin": `{"scoped-cli":"bin/cli.js"}`,
	})
	// 原生模块（无 N-API 标记）
	nativeDir := filepath.Join(prefix, "lib", "node_modules", "sharp")
	writePkg(t, nativeDir, "sharp", "0.33.0", "", map[string]string{"gypfile": "true"})
	if err := os.WriteFile(filepath.Join(nativeDir, "binding.node"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// N-API 预编译（跨版本安全）
	napiDir := filepath.Join(prefix, "lib", "node_modules", "napi-native")
	writePkg(t, napiDir, "napi-native", "1.0.0", "", nil)
	if err := os.MkdirAll(filepath.Join(napiDir, "prebuilds", "darwin-arm64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(napiDir, "prebuilds", "darwin-arm64", "node.napi.node"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := ScanGlobals(prefix, "darwin")
	if err != nil {
		t.Fatalf("ScanGlobals: %v", err)
	}
	if len(pkgs) != 4 {
		t.Fatalf("应扫到 4 个包（含 scope），得到 %d: %+v", len(pkgs), pkgs)
	}
	byName := map[string]GlobalPackage{}
	for _, p := range pkgs {
		byName[p.Name] = p
	}
	if byName["vite"].EnginesNode != ">=24" {
		t.Fatalf("engines 未读到: %+v", byName["vite"])
	}
	if !byName["sharp"].Native || byName["sharp"].NAPI {
		t.Fatalf("sharp 应判定为原生且非 N-API: %+v", byName["sharp"])
	}
	if !byName["napi-native"].NAPI {
		t.Fatalf("prebuilds/*.napi.node 应判定 N-API: %+v", byName["napi-native"])
	}
	if len(byName["@scope/cli"].Bins) != 1 || byName["@scope/cli"].Bins[0] != "scoped-cli" {
		t.Fatalf("bin 名未读到: %+v", byName["@scope/cli"])
	}

	// 目录不存在 → 空结果、不报错
	if pkgs, err := ScanGlobals(filepath.Join(prefix, "nope"), "darwin"); err != nil || len(pkgs) != 0 {
		t.Fatalf("缺失目录应返回空: %+v %v", pkgs, err)
	}
}

func TestCheckGlobals(t *testing.T) {
	pkgs := []GlobalPackage{
		{Name: "vite", EnginesNode: ">=24"},
		{Name: "sharp", Native: true},
		{Name: "napi-native", Native: true, NAPI: true},
	}
	problems := CheckGlobals(pkgs, "22.22.2")
	codes := map[string]bool{}
	for _, p := range problems {
		codes[p.Code] = true
	}
	if !codes["GLOBAL_ENGINES_UNMET"] || !codes["GLOBAL_NATIVE_ABI"] {
		t.Fatalf("应报 engines 与 ABI 两类问题: %+v", problems)
	}
	if len(problems) != 2 {
		t.Fatalf("N-API 包不该告警: %+v", problems)
	}
	// engines 满足时不报
	if problems := CheckGlobals([]GlobalPackage{{Name: "vite", EnginesNode: ">=20"}}, "22.22.2"); len(problems) != 0 {
		t.Fatalf("engines 满足时不该告警: %+v", problems)
	}
	// 没有生效 node 版本时跳过 engines 判断
	if problems := CheckGlobals([]GlobalPackage{{Name: "vite", EnginesNode: ">=24"}}, ""); len(problems) != 0 {
		t.Fatalf("没有 node 版本时不该判 engines: %+v", problems)
	}
}

func TestCheckShadowing(t *testing.T) {
	pkgs := []GlobalPackage{{Name: "typescript", Bins: []string{"tsc", "tsserver"}}}
	project := map[string]string{"tsc": "node_modules/.bin"}
	problems := CheckShadowing(pkgs, project)
	if len(problems) != 1 || problems[0].Code != "GLOBAL_SHADOWS_PROJECT" {
		t.Fatalf("应报命令遮蔽: %+v", problems)
	}
	if problems := CheckShadowing(pkgs, nil); len(problems) != 0 {
		t.Fatalf("没有项目时不该报: %+v", problems)
	}
}

func TestSharedVarsForPerVersion(t *testing.T) {
	p, _ := paths.New("/root")
	shared := kvs(SharedVarsFor("shared", p, "/home/u/.npm", "22.22.2"))
	if shared["npm_config_prefix"] != "/root/global" {
		t.Fatalf("shared 前缀应固定: %q", shared["npm_config_prefix"])
	}
	perVersion := kvs(SharedVarsFor("per-version", p, "/home/u/.npm", "22.22.2"))
	if perVersion["npm_config_prefix"] != "/root/global/node-22.22.2" {
		t.Fatalf("per-version 前缀应带版本: %q", perVersion["npm_config_prefix"])
	}
	if perVersion["npm_config_cache"] != "/home/u/.npm" {
		t.Fatal("缓存仍应共享")
	}
	// 没有版本时退回 shared
	fallback := kvs(SharedVarsFor("per-version", p, "/home/u/.npm", ""))
	if fallback["npm_config_prefix"] != "/root/global" {
		t.Fatalf("缺版本应退回 shared: %q", fallback["npm_config_prefix"])
	}
}
