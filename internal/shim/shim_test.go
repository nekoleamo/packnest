package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAndResolveUnix(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tools", "node", "22.22.2", "bin", "node")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := Ensure(binDir, "node", target, "darwin"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, isShim, err := Resolve(binDir, "node", "darwin")
	if err != nil || !isShim {
		t.Fatalf("Resolve: %q %v %v", got, isShim, err)
	}
	if got != target {
		t.Fatalf("Resolve = %q，期望 %q", got, target)
	}
}

func TestEnsureReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	first := filepath.Join(dir, "a")
	second := filepath.Join(dir, "b")
	if err := Ensure(binDir, "node", first, "linux"); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(binDir, "node", second, "linux"); err != nil {
		t.Fatalf("重复 Ensure 应原子替换: %v", err)
	}
	got, _, err := Resolve(binDir, "node", "linux")
	if err != nil || got != second {
		t.Fatalf("Resolve = %q/%v，期望 %q", got, err, second)
	}
	if _, err := os.Lstat(filepath.Join(binDir, "node.tmp")); !os.IsNotExist(err) {
		t.Fatal("临时链接应被清理")
	}
}

func TestResolveMissingAndPlainFile(t *testing.T) {
	dir := t.TempDir()
	if _, isShim, err := Resolve(dir, "nope", "darwin"); err != nil || isShim {
		t.Fatalf("缺失 shim 应返回空: %v/%v", isShim, err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, isShim, err := Resolve(dir, "plain", "darwin")
	if err != nil || isShim || got != plain {
		t.Fatalf("普通文件应原样返回: %q %v %v", got, isShim, err)
	}
}

func TestWindowsCmdShim(t *testing.T) {
	dir := t.TempDir()
	target := `C:\packnest\tools\node\22.22.2\node.exe`
	if err := Ensure(dir, "node", target, "windows"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := os.Stat(CmdPath(dir, "node")); err != nil {
		t.Fatalf("应生成 .cmd: %v", err)
	}
	got, isShim, err := Resolve(dir, "node", "windows")
	if err != nil || !isShim {
		t.Fatalf("Resolve: %q %v %v", got, isShim, err)
	}
	if got != target {
		t.Fatalf("Resolve = %q，期望 %q", got, target)
	}
}

func TestWindowsCmdMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(CmdPath(dir, "broken"), []byte("@ECHO off\r\necho hi\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(dir, "broken", "windows"); err == nil {
		t.Fatal("无法解析的 shim 应报错")
	}
}

func TestSelfDualName(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "pk")
	if err := os.WriteFile(self, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := Self(binDir, self, "darwin"); err != nil {
		t.Fatalf("Self: %v", err)
	}
	a, _, err := Resolve(binDir, SelfName, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Resolve(binDir, AliasName, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a != self {
		t.Fatalf("pk/packnest 必须解析到同一文件: %q vs %q", a, b)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir, "node", "/x/node", "linux"); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "npm", "/x/npm", "linux"); err != nil {
		t.Fatal(err)
	}
	names, err := List(dir, "linux")
	if err != nil || len(names) != 2 {
		t.Fatalf("List = %v/%v", names, err)
	}
	if names, err := List(filepath.Join(dir, "missing"), "linux"); err != nil || names != nil {
		t.Fatalf("缺失目录应返回空: %v/%v", names, err)
	}
}

func TestEnsureRejectsEmpty(t *testing.T) {
	if err := Ensure(t.TempDir(), "", "/x", "linux"); err == nil {
		t.Fatal("空名字应报错")
	}
	if err := Ensure(t.TempDir(), "x", "", "linux"); err == nil {
		t.Fatal("空目标应报错")
	}
}

func TestParseCmdTarget(t *testing.T) {
	cases := map[string]struct {
		target string
		ok     bool
	}{
		"@ECHO off\r\nREM x\r\n\"C:\\a b\\node.exe\" %*\r\n": {"C:\\a b\\node.exe", true},
		"@ECHO off\r\nno quotes\r\n":                         {"", false},
		"":                                                   {"", false},
	}
	for in, want := range cases {
		got, ok := parseCmdTarget(in)
		if ok != want.ok || got != want.target {
			t.Errorf("parseCmdTarget(%q) = %q/%v，期望 %q/%v", in, got, ok, want.target, want.ok)
		}
	}
}

func TestEnsureMkdirError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "bin")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(blocker, "node", "/x/node", "linux"); err == nil {
		t.Fatal("bin 是文件时应报错")
	}
	if err := Ensure(blocker, "node", "/x/node", "windows"); err == nil {
		t.Fatal("bin 是文件时应报错（windows）")
	}
}

func TestListWindows(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir, "node", `C:\x\node.exe`, "windows"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-shim.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := List(dir, "windows")
	if err != nil || len(names) != 1 || names[0] != "node" {
		t.Fatalf("List = %v/%v", names, err)
	}
}

func TestEnsureRenameFailures(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(filepath.Join(binDir, "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(binDir, "node", "/x/node", "linux"); err == nil {
		t.Fatal("目标位置是目录时符号链接替换应报错")
	}
	if err := os.MkdirAll(CmdPath(binDir, "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(binDir, "node", `C:\x\node.exe`, "windows"); err == nil {
		t.Fatal("目标位置是目录时 .cmd 替换应报错")
	}
}

func TestSelfError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "bin")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Self(blocker, "/x/pk", "linux"); err == nil {
		t.Fatal("bin 目录不可用时应返回错误")
	}
}

// BenchmarkShimResolve 的门禁阈值是 < 15ms（方案 §9.5 第 6 项）。
func BenchmarkShimResolve(b *testing.B) {
	dir := b.TempDir()
	binDir := filepath.Join(dir, "bin")
	target := filepath.Join(dir, "node")
	if err := Ensure(binDir, "node", target, "linux"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Resolve(binDir, "node", "linux"); err != nil {
			b.Fatal(err)
		}
	}
}

// TestResolveRelativeSymlink：相对软链要按 binDir 解析成绝对路径（否则 which 打印错路径）。
func TestResolveRelativeSymlink(t *testing.T) {
	bin := t.TempDir()
	if err := os.Symlink("../tools/node/22.98.0/bin/node", filepath.Join(bin, "node")); err != nil {
		t.Skip("这台机器建不了软链")
	}
	got, isShim, err := Resolve(bin, "node", "darwin")
	if err != nil || !isShim {
		t.Fatalf("相对软链应解析成 shim: %q %v %v", got, isShim, err)
	}
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join("tools", "node", "22.98.0", "bin", "node")) {
		t.Fatalf("相对软链应拼成绝对路径，得到 %q", got)
	}
}

// TestResolveWindowsCmd 与 TestResolveMissingAndPlainFile 互补：Windows 上走 .cmd 分支。
func TestResolveWindowsCmd(t *testing.T) {
	bin := t.TempDir()
	target := filepath.Join(t.TempDir(), "node.exe")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "node"+CmdExt), []byte("@echo off\r\n\""+target+"\" %*\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, isShim, err := Resolve(bin, "node", "windows")
	if err != nil || !isShim || got != target {
		t.Fatalf("Windows .cmd 应解析出目标 %q，得到 %q (shim=%v err=%v)", target, got, isShim, err)
	}
	// 内容坏掉时要报错（不然 which 会给出空答案）
	if err := os.WriteFile(filepath.Join(bin, "bad"+CmdExt), []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(bin, "bad", "windows"); err == nil {
		t.Fatal("shim 内容无法解析时应报错")
	}
}

// TestListFilters：非 Windows 只列软链，Windows 只列 .cmd（且去掉后缀）。
func TestListFilters(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "plain"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "a"+CmdExt), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	win, err := List(bin, "windows")
	if err != nil || len(win) != 1 || win[0] != "a" {
		t.Fatalf("Windows 只列 .cmd 且去后缀: %v %v", win, err)
	}
	unix, err := List(bin, "darwin")
	if err != nil || len(unix) != 0 {
		t.Fatalf("非 Windows 不列普通文件: %v %v", unix, err)
	}
	if names, err := List(filepath.Join(bin, "nope"), "darwin"); err != nil || names != nil {
		t.Fatalf("目录不存在应返回空: %v %v", names, err)
	}
}
