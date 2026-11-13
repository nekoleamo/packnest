//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// macOS：~/Library/LaunchAgents 里一个 RunAtLoad 的 plist。
// 用 LaunchAgent 而不是 System Events 登录项：不需要授权弹窗，也不依赖 Automation 权限。
func enabled(st Status) bool {
	_, err := os.Stat(st.Path)
	return err == nil
}

func add(st Status) error {
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string></array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`, Label, st.Self)
	return os.WriteFile(st.Path, []byte(plist), 0o644)
}

func remove(st Status) error {
	if err := os.Remove(st.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %w", st.Path, err)
	}
	return nil
}
