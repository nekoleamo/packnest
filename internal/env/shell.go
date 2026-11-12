package env

import (
	"os"
	"path/filepath"
	"runtime"
)

func NpmCacheDir(root string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		existing := filepath.Join(home, ".npm")
		if st, err := os.Stat(existing); err == nil && st.IsDir() {
			return existing
		}
	}
	return filepath.Join(root, "cache", "npm")
}

// MergeVars 按键去重（后者覆盖前者，保留首次出现的位置）。
func MergeVars(vars []KV) []KV {
	index := map[string]int{}
	out := make([]KV, 0, len(vars))
	for _, kv := range vars {
		if i, ok := index[kv.Key]; ok {
			out[i] = kv
			continue
		}
		index[kv.Key] = len(out)
		out = append(out, kv)
	}
	return out
}

// WriteTargets 决定 `pk env --write` 往哪几个文件写：
// --rc 显式指定 > Windows 的 PowerShell profile（5.1 与 7 可能各一份）> 按 $SHELL 猜。
//
// 返回多条时要全写：Windows 上 5.1 与 7 互不读对方 profile，猜错一个等于没写。

func WriteTargets(rcFlag string, sh Shell, goosName, home string, getenv func(string) string) []string {
	if rcFlag != "" {
		return []string{rcFlag}
	}
	if sh == ShellPowerShell && goosName == "windows" {
		return WindowsProfilePaths(home, getenv)
	}
	return []string{RCFileFor(sh, goosName, home, getenv("SHELL"))}
}

// DetectShell 从 $SHELL 猜方言（PowerShell 由 --shell 显式给）。
func DetectShell() string {
	return DetectShellFor(runtime.GOOS, os.Getenv)
}

// DetectShellFor 是 DetectShell 的可测版本（goos / 环境变量注入）。
//
// Windows 上没有 $SHELL：PowerShell 不会设它。原来一律回落到 "bash"，于是
// `pk env --write` 在 Windows 上把 POSIX 片段的托管块写进了 ~/.bashrc ——
// 一个 Windows 上没人会读的文件，用户自然"写了还是不生效"。

func DetectShellFor(goos string, getenv func(string) string) string {
	switch base := filepath.Base(getenv("SHELL")); base {
	case "zsh", "bash", "sh", "dash", "ksh": // fish 不在支持列表里，仍回落 bash
		return base
	case "pwsh", "powershell":
		return "powershell"
	}
	if goos == "windows" {
		// PowerShell 会设 PSModulePath；cmd.exe 里也可能有（安装器写进了系统环境变量），
		// 但那时写 PowerShell profile 仍是最有用的默认（Windows 11 的默认 shell 就是它）。
		return "powershell"
	}
	return "bash"
}

// projectBins 读取项目 node_modules/.bin 的命令名 → 所属包（L3 命令遮蔽检测）。
// globalPrefix 是一个包管理器的"全局包前缀"（都套 lib/node_modules 或 node_modules 布局）。
