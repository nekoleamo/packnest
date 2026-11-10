// Package env 生成 PATH 与 registry 环境（方案 §6.6 / §6.7）。
//
// 铁律：默认只**输出**变量片段给 shell eval，不落盘、不改用户 rc
// （`pk env --write` 才写，且带 `# managed by pk` 注释块）。
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
)

// KV 是一条环境变量。
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RegistryVars 返回镜像档对应的 registry 环境变量。
func RegistryVars(profile string) []KV {
	base := mirror.RegistryFor(profile)
	fallback, _ := mirror.Get("cn")
	prof, err := mirror.Get(profile)
	if err != nil || prof.Registry == "" {
		prof = fallback
	}
	return []KV{
		{"npm_config_registry", base},
		{"BUN_CONFIG_REGISTRY", base},
		{"PIP_INDEX_URL", prof.PipIndex},
		{"GOPROXY", prof.GoProxy},
		// go 装完必须写：否则 go 自己会再去下一套工具链（方案 §4.0）
		{"GOTOOLCHAIN", "local"},
	}
}

// SharedVarsFor 按 global_mode 返回全局包变量：shared（默认）或 per-version（nvm 式）。
//
// shared 是整个方案的关键：固定目录 → 新装全局包无需 reshim，切 node 版本全局包不动。
func SharedVarsFor(mode string, p paths.Paths, npmCache, nodeVersion string) []KV {
	if mode != "per-version" || nodeVersion == "" {
		return SharedVars(p, npmCache)
	}
	prefix := filepath.Join(p.Root, "global", "node-"+nodeVersion)
	return []KV{
		{"npm_config_prefix", prefix},
		{"npm_config_cache", npmCache},
		{"npm_config_globalconfig", filepath.Join(prefix, "etc", "npmrc")},
		{"npm_config_prefer_offline", "true"},
		{"PNPM_HOME", filepath.Join(prefix, "pnpm")},
		{"BUN_INSTALL", filepath.Join(prefix, "bun")},
		{"DENO_INSTALL_ROOT", filepath.Join(prefix, "deno")},
		{"DENO_DIR", p.Root + "/cache/deno"},
	}
}

// SharedVars 返回共享全局包前缀相关变量（方案 §6.7 总表）。
//
// npmCache 为已有的 ~/.npm（存在时沿用，绝不迁移不复制）。
func SharedVars(p paths.Paths, npmCache string) []KV {
	return []KV{
		{"npm_config_prefix", p.Global()},
		{"npm_config_cache", npmCache},
		{"npm_config_globalconfig", p.GlobalEtc() + "/npmrc"},
		{"npm_config_prefer_offline", "true"},
		{"PNPM_HOME", p.GlobalPnpmBin()},
		{"BUN_INSTALL", p.Global() + "/bun"},
		{"DENO_INSTALL_ROOT", p.Global() + "/deno"},
		{"DENO_DIR", p.Root + "/cache/deno"},
	}
}

// ToolchainVars 把 provider 声明的额外环境变量转成 KV（排序保证输出稳定）。
func ToolchainVars(extra map[string]string) []KV {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]KV, 0, len(keys))
	for _, k := range keys {
		out = append(out, KV{k, extra[k]})
	}
	return out
}

// Shell 是受支持的 shell 方言。
type Shell string

const (
	ShellPOSIX      Shell = "posix"      // sh / bash / zsh
	ShellPowerShell Shell = "powershell" // pwsh / Windows PowerShell
)

// ParseShell 归一化 shell 名字。
func ParseShell(name string) (Shell, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "sh", "bash", "zsh", "posix":
		return ShellPOSIX, nil
	case "powershell", "pwsh", "ps":
		return ShellPowerShell, nil
	}
	return "", fmt.Errorf("不支持的 shell %q（支持 bash/zsh/sh/powershell）", name)
}

// ExportScript 生成可 eval 的片段：先 prepend PATH 三段，再 export 变量。
func ExportScript(vars []KV, pathSegs []string, shell Shell) string {
	var b strings.Builder
	switch shell {
	case ShellPowerShell:
		if len(pathSegs) > 0 {
			b.WriteString("$env:PATH = @(\n")
			for _, seg := range pathSegs {
				b.WriteString("  '" + escapeSingle(seg) + "'\n")
			}
			b.WriteString("  $env:PATH\n) -join ';'\n")
		}
		for _, kv := range vars {
			b.WriteString("$env:" + kv.Key + " = '" + strings.ReplaceAll(kv.Value, "'", "''") + "'\n")
		}
	default:
		if len(pathSegs) > 0 {
			b.WriteString("export PATH=" + shellQuote(strings.Join(pathSegs, ":")) + ":$PATH\n")
		}
		for _, kv := range vars {
			b.WriteString("export " + kv.Key + "=" + shellQuote(kv.Value) + "\n")
		}
	}
	return b.String()
}

// PathHint 返回给人看的一行 PATH 建议（doctor 用）。
func PathHint(pathSegs []string) string {
	return "PATH 需要（顺序固定）: " + strings.Join(pathSegs, " → ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"$`\\!*?[]{}()&|;<>#~") {
		return s
	}
	return "'" + escapeSingle(s) + "'"
}

func escapeSingle(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// 托管块标记（`pk env --write` 与 `pk mirror write` 共用）。
const (
	ManagedBegin = "# >>> packnest >>>"
	ManagedEnd   = "# <<< packnest <<<"
)

// RCFileFor 决定 rc 文件：powershell 走 profile，posix 跟随 $SHELL（zsh → ~/.zshrc）。
//
// `--shell` 只决定**片段语法**；写哪个文件由用户实际的 $SHELL 决定（否则 zsh 用户
// 会被人往 .bashrc 里写东西，永远不生效）。
func RCFileFor(shell Shell, goos, home, envShell string) string {
	if shell == ShellPowerShell {
		if goos == "windows" {
			// 单值版本只给首选（cmd 侧会用 WindowsProfilePaths 拿到全部候选）
			return WindowsProfilePaths(home, os.Getenv)[0]
		}
		return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	}
	if strings.Contains(filepath.Base(envShell), "zsh") {
		return filepath.Join(home, ".zshrc")
	}
	return filepath.Join(home, ".bashrc")
}

// windowsDocumentsDirs 返回 Documents 目录候选：OneDrive 重定向（Windows 默认）优先。
func windowsDocumentsDirs(home string, getenv func(string) string) []string {
	dirs := make([]string, 0, 2)
	if od := strings.TrimSpace(getenv("OneDrive")); od != "" {
		dirs = append(dirs, filepath.Join(od, "Documents"))
	}
	return append(dirs, filepath.Join(home, "Documents"))
}

// WindowsProfilePaths 返回该写的 PowerShell profile 路径。
//
// 为什么不写死一条：PowerShell 有两套互不相干的 profile
//   - Windows PowerShell 5.1（系统自带）：Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1
//   - PowerShell 7（pwsh）：             Documents\PowerShell\Microsoft.PowerShell_profile.ps1
//
// 5.1 不会读 7 的 profile（反之亦然），所以只写一条就会出现"写了托管块、新开 shell 还是找不到 node"。
// 另外 Documents 常被 OneDrive 重定向，两个候选都要看。
//
// 返回多条时调用方应全部写入：判定不出是哪个 shell 时（例如从 cmd.exe 里跑 pk），
// 两个都写比"猜错一个"划算。
func WindowsProfilePaths(home string, getenv func(string) string) []string {
	// 统一分隔符再匹配：同一台机器上 PSModulePath 可能写成 \ 也可能写成 /
	modPath := strings.ToLower(strings.ReplaceAll(getenv("PSModulePath"), `\`, "/"))
	is7 := strings.TrimSpace(getenv("POWERSHELL_DISTRIBUTION_CHANNEL")) != "" ||
		strings.Contains(modPath, "/powershell/7")
	is5 := !is7 && strings.Contains(modPath, "windowspowershell")

	folders := []string{"WindowsPowerShell", "PowerShell"} // 判不出来：两个都写
	switch {
	case is7:
		folders = []string{"PowerShell"}
	case is5:
		folders = []string{"WindowsPowerShell"}
	}

	docs := windowsDocumentsDirs(home, getenv)
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		// 优先选已经存在该版本 profile 目录的那个 Documents（OneDrive 与本地各一份时更要紧）
		chosen := ""
		for _, d := range docs {
			if st, err := os.Stat(filepath.Join(d, folder)); err == nil && st.IsDir() {
				chosen = d
				break
			}
		}
		if chosen == "" {
			chosen = docs[0]
			if _, err := os.Stat(chosen); err != nil && len(docs) > 1 {
				chosen = docs[1]
			}
		}
		out = append(out, filepath.Join(chosen, folder, "Microsoft.PowerShell_profile.ps1"))
	}
	return out
}

// WriteManagedBlock 把内容写进 rc 文件的托管块（幂等：已有块就替换）。
//
// 只碰自己那一段：块外内容一个字节不改（方案 §8.2 第 1 条）。
func WriteManagedBlock(path, content string) error {
	block := ManagedBegin + "\n" + strings.TrimRight(content, "\n") + "\n" + ManagedEnd + "\n"

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取 %q 失败: %w", path, err)
	}
	if len(existing) > 0 {
		// 先备份（只留一份，反复写不堆垃圾）
		if err := os.WriteFile(path+".pk.bak", existing, 0o644); err != nil {
			return fmt.Errorf("备份 %q 失败: %w", path, err)
		}
	}

	text := string(existing)
	if start := strings.Index(text, ManagedBegin); start >= 0 {
		if end := strings.Index(text[start:], ManagedEnd); end >= 0 {
			// 块自带结尾换行，所以吃掉后面的那一个，反复写不会垫空行。
			rest := strings.TrimPrefix(text[start+end+len(ManagedEnd):], "\n")
			text = text[:start] + block + rest
		} else {
			text = text[:start] + block // 残缺块：整段重写
		}
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += block
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %q 失败: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("写入 %q 失败: %w", path, err)
	}
	return nil
}

// ReadManagedBlock 返回托管块内容（没有则空串），用于 doctor 自检。
func ReadManagedBlock(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	start := strings.Index(text, ManagedBegin)
	if start < 0 {
		return ""
	}
	end := strings.Index(text[start:], ManagedEnd)
	if end < 0 {
		return ""
	}
	return text[start : start+end+len(ManagedEnd)]
}

// RemoveManagedBlock 删掉托管块（`pk env --write --unset` 用）。
func RemoveManagedBlock(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := string(data)
	start := strings.Index(text, ManagedBegin)
	if start < 0 {
		return nil
	}
	end := strings.Index(text[start:], ManagedEnd)
	if end < 0 {
		return nil
	}
	cleaned := text[:start] + strings.TrimPrefix(text[start+end+len(ManagedEnd):], "\n")
	return os.WriteFile(path, []byte(cleaned), 0o644)
}
