//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// removeSelf 在 unix 上直接 unlink 正在运行的二进制（合法），并删掉同目录的 packnest 别名。
func removeSelf(exe, goos string) (bool, string) {
	if exe == "" {
		return false, tr("拿不到自身路径", "cannot determine own path")
	}
	dir := filepath.Dir(exe)
	removed := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, name := range []string{filepath.Base(exe), "pk", "packnest"} {
		p := filepath.Join(dir, name)
		if seen[p] {
			continue
		}
		seen[p] = true
		if st, err := os.Lstat(p); err != nil || st.IsDir() {
			continue
		}
		if err := os.Remove(p); err != nil {
			return false, err.Error()
		}
		removed = append(removed, p)
	}
	if len(removed) == 0 {
		return false, tr("同目录下没找到 pk/packnest 可执行文件", "no pk/packnest executable next to this binary")
	}
	// 目录空了就一并收掉（只删空目录，绝不递归）
	_ = os.Remove(dir)
	return true, strings.Join(removed, ", ")
}
