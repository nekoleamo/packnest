//go:build windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked 表示等不到全局锁（退出码 5）。
var ErrLocked = errors.New("另一个 pk 正在运行（state/lock 被占用）")

// staleAfter 是锁文件被视为"断电残留"的阈值。
const staleAfter = time.Hour

// Lock 取全局锁：Windows 上没有 flock（syscall.LockFileEx 需要 x/sys），
// 用 O_CREATE|O_EXCL 独占创建 + 1 小时残留清理实现。
//
// shortcut: 独占创建不是内核锁，极端并发下两个进程可能都拿到锁（窗口 <1 次 CreateFile）；
// 天花板是单用户 CLI（安装低频），升级路径 = 引入 x/sys 调 LockFileEx（会加一个依赖，暂不做）。
func Lock(path string) (func(), error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if st, statErr := os.Stat(path); statErr == nil && time.Since(st.ModTime()) > staleAfter {
			_ = os.Remove(path)
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		}
	}
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrLocked
		}
		return nil, err
	}
	_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339) + "\n")
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}
