package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProbeNative：用假 node 脚本模拟 dlopen 的成败，验证 L4 检测（只加载二进制，不跑包代码）。
func TestProbeNative(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "needsnative")
	if err := os.MkdirAll(filepath.Join(pkg, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "build", "addon.node"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 只探"标了 N-API"的包：非 N-API 的静态检查已经告警，避免重复报
	napi := []GlobalPackage{{Name: "napi-ok", Path: pkg, Native: true, NAPI: true}}
	plain := []GlobalPackage{{Name: "plain-native", Path: pkg, Native: true}}

	ok := writeFake(t, dir, "node-ok", "#!/bin/sh\nexit 0\n")
	bad := writeFake(t, dir, "node-bad", "#!/bin/sh\nexit 1\n")

	if got := ProbeNative(napi, ok, 10*time.Second); len(got) != 0 {
		t.Fatalf("加载成功不该报问题: %+v", got)
	}
	got := ProbeNative(napi, bad, 10*time.Second)
	if len(got) != 1 || got[0].Code != "GLOBAL_NATIVE_ABI" || !strings.Contains(got[0].Detail["file"].(string), "addon.node") {
		t.Fatalf("应报 GLOBAL_NATIVE_ABI 并指出文件: %+v", got)
	}
	if got := ProbeNative(plain, bad, 10*time.Second); len(got) != 0 {
		t.Fatalf("非 N-API 交给静态检查，探针不该重复报: %+v", got)
	}
	if got := ProbeNative(napi, "", 10*time.Second); len(got) != 0 {
		t.Fatalf("没有 node 时应静默跳过: %+v", got)
	}
	if got := ProbeNative(napi, filepath.Join(dir, "missing-node"), 10*time.Second); len(got) != 0 {
		t.Fatalf("node 路径不存在时应静默跳过: %+v", got)
	}
}

func writeFake(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
