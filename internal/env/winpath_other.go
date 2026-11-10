//go:build !windows

package env

// 非 Windows 平台没有注册表用户 Path，`pk env --write` 走 rc 文件那条路。
// 这些函数保持同签名，调用方不必到处写 build tag。

// ErrNoUserPath 表示当前平台没有"用户 Path 注册表"这种东西。
var ErrNoUserPath = errNoUserPath{}

type errNoUserPath struct{}

func (errNoUserPath) Error() string {
	return "当前平台没有用户 Path 环境变量（用 rc 文件）"
}

// AddUserPath 在非 Windows 上是空操作。
func AddUserPath([]string) (bool, string, string, error) { return false, "", "", ErrNoUserPath }

// RemoveUserPath 在非 Windows 上是空操作。
func RemoveUserPath([]string) (bool, string, string, error) { return false, "", "", ErrNoUserPath }

// PowerShellScriptsBlocked 在非 Windows 上恒为 false。
func PowerShellScriptsBlocked() bool { return false }
