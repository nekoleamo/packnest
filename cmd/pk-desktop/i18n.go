package main

// 界面文案：中文即源串、英文内联（不用 key 表，改文案即改这里）。

// t 是界面文案的唯一入口：中文即源串，英文内联（不用 key 表，改文案即改这里）。
func (g *gui) t(zh, en string) string {
	if g.lang == "en" {
		return en
	}
	return zh
}

// langOf 把 Segmented 的下标换成语言代码。
func langOf(idx int) string {
	if idx == 1 {
		return "en"
	}
	return "zh"
}

// installCmd 给没装 CLI 的用户一条"对应平台"的命令。
func installCmd(platform string) string {
	switch platform {
	case "windows":
		return `powershell -c "irm https://nekoleamo.github.io/packnest/install.ps1 | iex"`
	default:
		return "curl -fsSL https://nekoleamo.github.io/packnest/install.sh | sh"
	}
}
