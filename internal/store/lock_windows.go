//go:build windows

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// ErrLocked 表示等不到全局锁（退出码 5）。
var ErrLocked = errors.New("另一个 pk 正在运行（state/lock 被占用）")

// LockFileEx/UnlockFileEx 直接从 kernel32 取（std 的 syscall 没有绑定，引 x/sys 会多一个第三方依赖）。
var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = kernel32.NewProc("LockFileEx")
	procUnlockFilex = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
	// errLockViolation / errIOPending：std 的 syscall 未导出这两个常量（见 internal/syscall/windows）。
	errLockViolation syscall.Errno = 33
	errIOPending     syscall.Errno = 997
)

// Lock 取全局锁：Windows 上用 LockFileEx 做真内核锁（独占 + 拿不到立刻失败）。
//
// 早先用 O_CREATE|O_EXCL + 1 小时残留清理，两个进程仍可能同时拿到（窗口 <1 次 CreateFile），
// 而且超过 1 小时的下载会被后来者误判成残留、删掉活锁。锁文件本身留在磁盘上无害，无需清理。
func Lock(path string) (func(), error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建锁目录失败: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开锁文件失败: %w", err)
	}
	ol := new(syscall.Overlapped) // 必须活到解锁：地址会被内核记住
	r, _, errno := procLockFileEx.Call(f.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(ol)))
	if r == 0 {
		f.Close()
		if errno == errLockViolation || errno == errIOPending {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("取全局锁失败: %w", errno)
	}
	return func() {
		_, _, _ = procUnlockFilex.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(ol)))
		_ = f.Close()
	}, nil
}
