// Package shim 负责 bin/ 里的入口生成与解析（方案 §8、§13.1）。
//
// 设计要点：
//   - Unix 用符号链接（切换版本 = 原子换链接，`pk which` 直接读出真实路径）；
//   - Windows 用 .cmd 包装（node 的 tar 里 bin/npm 是符号链接，建链接要管理员权限，
//     所以 Windows 一律取 zip 产物，并在 shim 里写成文本包装）；
//   - `pk` 与 `packnest` 是同一个目标的两个入口，argv[0] 不改变行为。
package shim

import (
	"github.com/packnest/packnest/internal/coded"
	"os"
	"path/filepath"
	"strings"
)

// 命令名。
const (
	SelfName  = "pk"
	AliasName = "packnest"
)

// CmdExt 是 Windows shim 的扩展名。
const CmdExt = ".cmd"

// Ensure 在 binDir 里为 name 建立指向 target 的入口。
func Ensure(binDir, name, target, goos string) error {
	if name == "" || target == "" {
		return coded.New(CodeEmptyArgs)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return coded.Wrap(err, CodeMkdir)
	}
	if goos == "windows" {
		return writeCmd(binDir, name, target)
	}
	return writeSymlink(binDir, name, target)
}

// Self 建立 pk + packnest 两个入口，指向同一个可执行文件。
func Self(binDir, selfPath, goos string) error {
	for _, name := range []string{SelfName, AliasName} {
		if err := Ensure(binDir, name, selfPath, goos); err != nil {
			return err
		}
	}
	return nil
}

// Resolve 解析 binDir/name 指向的真实路径。
// 返回 isShim=false 表示该名字是个普通文件（不是我们建的入口）。
func Resolve(binDir, name, goos string) (string, bool, error) {
	if goos == "windows" {
		path := filepath.Join(binDir, name+CmdExt)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, err
		}
		target, ok := parseCmdTarget(string(data))
		if !ok {
			return "", false, coded.New(CodeUnparsable, path)
		}
		return target, true, nil
	}

	path := filepath.Join(binDir, name)
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", false, coded.Wrap(err, CodeReadFailed, path)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(binDir, target)
		}
		return target, true, nil
	}
	return path, false, nil
}

// List 返回 binDir 下的全部 shim 名字（排序不做，调用方按需排）。
func List(binDir, goos string) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if goos == "windows" {
			if !strings.HasSuffix(name, CmdExt) {
				continue
			}
			name = strings.TrimSuffix(name, CmdExt)
		} else if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// CmdPath 返回 Windows shim 的文件名。
func CmdPath(binDir, name string) string { return filepath.Join(binDir, name+CmdExt) }

func writeSymlink(binDir, name, target string) error {
	link := filepath.Join(binDir, name)
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return coded.Wrap(err, CodeSymlinkFailed, link)
	}
	// rename 覆盖旧链接是原子的：切换版本不会出现半状态。
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return coded.Wrap(err, CodeReplaceFailed, link)
	}
	return nil
}

func writeCmd(binDir, name, target string) error {
	path := CmdPath(binDir, name)
	// cmd.exe 在双引号里也会展开 `%VAR%`：路径里有 `%`（合法目录名）就会变成另一条路径。
	// `%` → `%%` 是 .cmd 里的转义写法；`&` / `^` 在引号内不会被当成分隔符，不用管。
	target = strings.ReplaceAll(target, "%", "%%")
	content := "@ECHO off\r\nREM managed by packnest\r\n\"" + target + "\" %*\r\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o755); err != nil {
		return coded.Wrap(err, CodeWriteFailed, path)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return coded.Wrap(err, CodeReplaceFailed, path)
	}
	return nil
}

// parseCmdTarget 从 .cmd 内容里取出第一个带引号的目标路径。
func parseCmdTarget(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "REM") ||
			strings.HasPrefix(strings.ToUpper(line), "@ECHO") {
			continue
		}
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		if end := strings.Index(line[1:], `"`); end >= 0 {
			// .cmd 里 `%` 写成 `%%`（否则 cmd 会当变量展开）：对外的路径要还原成单个 `%`。
			return strings.ReplaceAll(line[1:1+end], "%%", "%"), true
		}
	}
	return "", false
}
