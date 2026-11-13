// Package autostart 管理"开机/登录后自动启动 GUI"。
//
// 三条硬规矩：
//   - **默认关**，只有用户在界面里显式打开才写入；
//   - 只动**用户级**位置（LaunchAgent / HKCU Run / ~/.config/autostart），永不写系统级；
//   - 关闭 = 删掉自己写的那一条，不碰别人的同类条目。
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Status 是当前自启状态（给 /api/autostart 用）。
type Status struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	// Path 是那条自启条目的位置（关闭时为探针路径，便于用户自己核对）。
	Path string `json:"path"`
	// Self 是当前可执行文件（自启条目指向它）。
	Self string `json:"self,omitempty"`
	// Note 是给界面看的说明（不支持时写原因）。
	Note string `json:"note,omitempty"`
}

// Label 是自启条目的名字（三平台共用）。
const Label = "packnest-desktop"

// Get 返回当前状态。
func Get() Status {
	self, err := os.Executable()
	if err != nil {
		self = ""
	}
	st := Status{Supported: true, Self: self, Path: entryPath()}
	if !st.Supported {
		return st
	}
	st.Enabled = enabled(st)
	return st
}

// Set 打开/关闭自启。失败时返回错误（界面直接展示）。
func Set(enable bool) (Status, error) {
	st := Get()
	if !st.Supported {
		return st, fmt.Errorf("%s 不支持自启", runtime.GOOS)
	}
	if enable && st.Self == "" {
		return st, fmt.Errorf("拿不到自己的可执行文件路径")
	}
	var err error
	if enable {
		err = add(st)
	} else {
		err = remove(st)
	}
	if err != nil {
		return st, err
	}
	return Get(), nil
}

func entryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
	case "windows":
		return `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` + `\` + Label
	default:
		return filepath.Join(home, ".config", "autostart", Label+".desktop")
	}
}
