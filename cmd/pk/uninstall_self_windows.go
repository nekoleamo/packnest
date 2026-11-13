//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// removeSelf 在 Windows 上没法直接删正在运行的 exe，改为**安排一个延时删除**。
//
// 走过的弯路（0.7.8 实测仍然失败）：直接把
// `ping … & del "a b" & rd "c"` 整串交给 `exec.Command("cmd.exe", "/c", cmdLine)`。
// Go 会按 Windows 规则给带空格的参数加引号并转义内部引号（`\"…\"`），而 cmd.exe 不认识
// 反斜杠转义 —— 命令行被拼坏，cmd.exe 什么也没干（错误还被 >nul 吞了）。
//
// 现在的做法：把要执行的命令写成 %TEMP% 下的一个 .cmd 文件，只把**文件路径**交给 cmd.exe。
// 路径里没有引号可拼错，脚本内部自己引用路径；脚本还会写一行日志到
// %TEMP%\packnest-self-remove.log，方便tr("到底删没删", "whether it was deleted")有据可查。
func removeSelf(exe, goos string) (bool, string) {
	if exe == "" {
		return false, tr("拿不到自身路径", "cannot determine own path")
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(dir); err != nil {
		return false, tr("安装目录不存在：", "install dir does not exist: ") + dir
	}

	bat := filepath.Join(os.TempDir(), fmt.Sprintf("packnest-self-remove-%d.cmd", time.Now().UnixNano()))
	if err := os.WriteFile(bat, []byte(selfRemoveScript(dir, bat)), 0o600); err != nil {
		return false, tr("写临时删除脚本失败: ", "failed to write the removal script: ") + err.Error()
	}

	c := exec.Command("cmd.exe", "/d", "/c", bat)
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW：无窗口，且不随父进程退出
	}
	if err := c.Start(); err != nil {
		_ = os.Remove(bat)
		return false, tr("启动延时删除进程失败: ", "failed to start the delayed removal: ") + err.Error()
	}
	_ = c.Process.Release()
	return true, tr("已安排延时删除 ", "scheduled delayed removal ") + filepath.Join(dir, "pk.exe") +
		tr("（约 3 秒后生效；结果记在 %TEMP%\\packnest-self-remove.log）", "(effective in ~3s; result logged to %TEMP%\\packnest-self-remove.log)")
}
