//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// Linux：XDG autostart 目录里的一个 .desktop 文件。
func enabled(st Status) bool {
	_, err := os.Stat(st.Path)
	return err == nil
}

func add(st Status) error {
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return fmt.Errorf("创建 autostart 目录失败: %w", err)
	}
	body := "[Desktop Entry]\nType=Application\nName=packnest\nComment=packnest 桌面端（GUI 只是 CLI 的可视化补充）\n" +
		"Exec=\"" + st.Self + "\"\nX-GNOME-Autostart-enabled=true\nTerminal=false\n"
	return os.WriteFile(st.Path, []byte(body), 0o644)
}

func remove(st Status) error {
	if err := os.Remove(st.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %w", st.Path, err)
	}
	return nil
}
