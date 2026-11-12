//go:build windows

package pkcli

import (
	"os/exec"
	"syscall"
)

// setHideWindow 让子进程不弹控制台窗口（GUI 里最刺眼的问题就是每次调用闪一个黑框）。
func setHideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
