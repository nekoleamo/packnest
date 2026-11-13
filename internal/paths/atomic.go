// 原子落盘：写临时文件 → rename（同目录，保证同文件系统）。
//
// 只此一份实现：台账、TOFU 表、探测缓存、索引元数据、`pk.toml` 合并都走这里。
// 以前每个包各写一遍（fetch / mirror / verify / store / install），
// 差别只在错误文案上 —— 那是五处需要同步维护的原子语义。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic 原子写文件（自动建父目录）。data 已完整在内存里，失败不留半个文件。
func WriteAtomic(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %q 失败: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写 %q 失败: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换 %q 失败: %w", path, err)
	}
	return nil
}
