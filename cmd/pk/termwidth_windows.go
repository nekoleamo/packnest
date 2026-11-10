//go:build windows

package main

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// termWidth 返回控制台窗口的列数；拿不到时返回 0。
func termWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getInfo := kernel32.NewProc("GetConsoleScreenBufferInfo")
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0
	}
	type coord struct{ x, y int16 }
	type smallRect struct{ left, top, right, bottom int16 }
	type consoleScreenBufferInfo struct {
		size              coord
		cursorPosition    coord
		attributes        uint16
		window            smallRect
		maximumWindowSize coord
	}
	var info consoleScreenBufferInfo
	r, _, _ := getInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	w := int(info.window.right-info.window.left) + 1
	if w <= 0 {
		return 0
	}
	return w
}
