//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// removeSelf 在 Windows 上没法直接删正在运行的 exe，改为**分离一个延时进程**来删。
//
// 做法：起一个 DETACHED 的 cmd.exe，先躲过当前进程退出（ping 空转约 3 秒），
// 再 rd /s /q 整个安装目录。当前进程随后立即 exit，文件句柄就释放了。
// 这是无管理员权限下最稳的办法（MoveFileEx+DELAY_UNTIL_REBOOT 需要管理员）。
func removeSelf(exe, goos string) (bool, string) {
	if exe == "" {
		return false, "拿不到自身路径"
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(dir); err != nil {
		return false, "安装目录不存在：" + dir
	}
	// 只删自己的两个 exe，然后尝试删**空**目录：
	// 用户可能用 -Dir 指到共享目录（C:\Tools\bin），绝不能 rd /s 把别人的东西带走。
	// /d 关掉 AutoRun；>nul 压掉 ping 的输出。
	cmdLine := `ping -n 4 127.0.0.1 >nul & del /f /q "` + filepath.Join(dir, "pk.exe") + `" "` +
		filepath.Join(dir, "packnest.exe") + `" >nul 2>&1 & rd "` + dir + `" >nul 2>&1`
	c := exec.Command("cmd.exe", "/d", "/c", cmdLine)
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008, // DETACHED_PROCESS：不跟着本进程退出
	}
	if err := c.Start(); err != nil {
		return false, "启动延时删除进程失败: " + err.Error()
	}
	_ = c.Process.Release()
	return true, "已安排延时删除 " + dir + "（当前窗口关掉后约 3 秒生效）"
}
