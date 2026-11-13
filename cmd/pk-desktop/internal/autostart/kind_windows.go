//go:build windows

package autostart

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Windows：HKCU\...\Run 里一条值（用户级，不需要管理员）。
func enabled(st Status) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(Label)
	return err == nil && v != ""
}

func add(st Status) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Run 键失败: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(Label, `"`+st.Self+`"`); err != nil {
		return fmt.Errorf("写自启项失败: %w", err)
	}
	return nil
}

func remove(st Status) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil // 键都不在，等于已经关了
	}
	defer k.Close()
	if err := k.DeleteValue(Label); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除自启项失败: %w", err)
	}
	return nil
}
