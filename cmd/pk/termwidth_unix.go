//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// termWidth 返回终端的列数；拿不到（不是 TTY）时返回 0，由调用方用默认值。
func termWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	type winsize struct{ row, col, xpixel, ypixel uint16 }
	ws := &winsize{}
	// TIOCGWINSZ on stdout；不是终端时会失败，返回 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws))); errno != 0 {
		return 0
	}
	return int(ws.col)
}

// stdinIsTerminal 判断标准输入是不是终端。
// 注意 /dev/null 也是字符设备，所以不能只看 os.ModeCharDevice —— 用 ioctl 才准。
func stdinIsTerminal() bool {
	type winsize struct{ row, col, xpixel, ypixel uint16 }
	ws := &winsize{}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws)))
	return errno == 0
}
