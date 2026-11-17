//go:build !windows

package autostart

// 失败与边界：拿不到 HOME、目标位置被占用、要删的不是普通文件。
// 这些路径在真机上不常走，但一旦走了就会静默失败（用户以为自启开了其实没开）。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEntryPathWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := entryPath(); got != "" {
		t.Fatalf("没有 HOME 时应返回空路径，实际 %q", got)
	}
	// Get 仍要给出结构化状态（不是 panic，也不是空状态）
	if st := Get(); st.Supported && st.Path != "" {
		t.Fatalf("没有 HOME 时不该给出条目路径: %+v", st)
	}
}

func TestSetReportsAddFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// 把 LaunchAgents（或 ~/.config/autostart 的父目录）先占成一个普通文件
	path := entryPath()
	if path == "" {
		t.Skip("当前平台没有条目路径")
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("我不是目录"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Set(true)
	if err == nil {
		t.Fatalf("目录被占用时应报错，实际成功: %+v", st)
	}
}

func TestSetReportsRemoveFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := entryPath()
	if path == "" {
		t.Skip("当前平台没有条目路径")
	}
	// 条目位置是个非空目录 → os.Remove 删不掉（只删自己那一条，不 RM -rf）
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "别人的东西"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Set(false)
	if err == nil {
		t.Fatalf("删不掉时应如实报错，而不是假装关闭: %+v", st)
	}
	if _, serr := os.Stat(filepath.Join(path, "别人的东西")); serr != nil {
		t.Fatal("不该递归删除条目位置里的东西")
	}
}
