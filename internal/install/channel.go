package install

import (
	"path/filepath"
	"strings"
)

// InstallChannel 说明"这个 pk 二进制是怎么装进来的" —— 不同渠道要用不同的命令才能删干净。
type InstallChannel struct {
	Name string // brew / npm / scoop / script-posix / script-windows / manual
	Hint string // 给用户照抄的命令
	Dir  string // 已知的安装目录（来自 state/install.json）
}

// 渠道识别：pk 自己是怎么装进来的（纯函数，`pk uninstall --all` 与 GUI 共用）。

// SelfRemoveHint 生成"删掉 pk 自己"的命令（按平台）。
func SelfRemoveHint(dir, goos string) string {
	if goos == "windows" {
		return `Remove-Item -Recurse -Force "` + dir + `"   （在该目录下的 pk.exe 被占用时先关掉所有窗口）`
	}
	return "rm -f " + filepath.Join(dir, "pk") + " " + filepath.Join(dir, "packnest")
}

// DetectInstallChannel 按可执行文件的路径判断渠道（纯函数，便于单测）。
//
// 判不出来不算错：自托管/手动拷贝就是这种，直接告诉用户删哪个文件。

func DetectInstallChannel(exePath, goos string) InstallChannel {
	// 手工归一化：filepath.ToSlash 在非 Windows 上不会把 "\\" 转成 "/"，
	// 而渠道判断必须是跨平台的（同一套规则要能识别 cnpm/scoop 的路径）。
	p := strings.ToLower(strings.ReplaceAll(exePath, `\`, "/"))
	switch {
	case strings.Contains(p, "/cellar/packnest/") || strings.Contains(p, "/homebrew/cellar/packnest/"):
		return InstallChannel{Name: "brew", Hint: "brew uninstall packnest && brew untap nekoleamo/packnest"}
	case strings.Contains(p, "/node_modules/packnest/") || strings.Contains(p, "/node_modules/@packnest/"):
		return InstallChannel{Name: "npm", Hint: "npm uninstall -g packnest"}
	case strings.Contains(p, "/scoop/apps/packnest/"):
		return InstallChannel{Name: "scoop", Hint: "scoop uninstall packnest && scoop bucket rm nekoleamo"}
	case goos == "windows" && strings.Contains(p, "/packnest/bin/"):
		return InstallChannel{Name: "script-windows", Hint: `删除 "%LOCALAPPDATA%\packnest\bin"（pk env --unset 会把 PATH 项一并去掉）`}
	case goos != "windows" && (strings.Contains(p, "/.local/bin/") || strings.Contains(p, "/.packnest/bin/")):
		return InstallChannel{Name: "script-posix", Hint: "rm -f ~/.local/bin/pk ~/.local/bin/packnest（若是自托管目录，删对应文件）"}
	}
	return InstallChannel{Name: "manual", Hint: "直接删除这个文件：" + exePath}
}
