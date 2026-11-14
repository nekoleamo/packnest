//go:build !windows

package store

import (
	"errors"
	"github.com/packnest/packnest/internal/coded"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked 表示等不到全局锁（退出码 5）。
var ErrLocked = errors.New("另一个 pk 正在运行（state/lock 被占用）")

// Lock 取全局排他锁（flock）；返回解锁函数。拿不到返回 ErrLocked。
//
// 安装是低频操作，不做细粒度锁（方案 §18.4）。
func Lock(path string) (func(), error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, coded.Wrap(err, CodeLockMkdir)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, coded.Wrap(err, CodeLockOpen)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrLocked
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
