//go:build !windows

package pkcli

import "os/exec"

// setHideWindow 在非 Windows 上是空操作（没有"控制台窗口"这回事）。
func setHideWindow(*exec.Cmd) {}
