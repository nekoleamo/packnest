package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveProjectTool 把 "工具 = 版本" 写进项目 pk.toml 的 [tools] 段。
//
// 用**文本级合并**而不是解析再序列化：这样用户手写的注释、分段、顺序都不会被冲掉。
// 只加/改一行，其余原样保留。
func SaveProjectTool(dir, tool, version string) error {
	path := filepath.Join(dir, "pk.toml")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取 %q 失败: %w", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	entry := fmt.Sprintf("%s = %q", tool, version)
	inTools := false
	replaced := false
	out := make([]string, 0, len(lines)+3)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if inTools && !replaced {
				out = append(out, entry)
				replaced = true
			}
			inTools = trimmed == "[tools]"
			out = append(out, line)
			continue
		}
		if inTools && !replaced {
			key := trimmed
			if idx := strings.Index(key, "="); idx > 0 {
				key = strings.TrimSpace(key[:idx])
			}
			if key == tool {
				out = append(out, entry)
				replaced = true
				continue
			}
		}
		out = append(out, line)
	}
	if inTools && !replaced {
		out = append(out, entry)
		replaced = true
	}
	if !replaced {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "[tools]", entry)
	}

	body := strings.Join(out, "\n") + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入 %q 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %q 失败: %w", path, err)
	}
	return nil
}
