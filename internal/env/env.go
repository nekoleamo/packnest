// Package env 生成 PATH 与 registry 环境（方案 §6.6 / §6.7）。
//
// 铁律：默认只**输出**变量片段给 shell eval，不落盘、不改用户 rc
// （`pk env --write` 才写，且带 `# managed by pk` 注释块）。
package env

import (
	"fmt"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
		// corepack 首次拉 pnpm/yarn 时按这个走，否则会绕开镜像档（方案 B.2）
		{"COREPACK_NPM_REGISTRY", base},
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
	return globalVars(filepath.Join(p.Root, "global", "node-"+nodeVersion), p, npmCache)
}

// SharedVars 返回共享全局包前缀相关变量（方案 §6.7 总表）。
//
// npmCache 为已有的 ~/.npm（存在时沿用，绝不迁移不复制）。
func SharedVars(p paths.Paths, npmCache string) []KV {
	return globalVars(p.Global(), p, npmCache)
}

// globalVars 是 shared 与 per-version 两种模式共用的变量表（区别只有前缀一个参数）。
func globalVars(prefix string, p paths.Paths, npmCache string) []KV {
	return []KV{
		{"npm_config_prefix", prefix},
		{"npm_config_cache", npmCache},
		{"npm_config_globalconfig", filepath.Join(prefix, "etc", "npmrc")},
		{"npm_config_prefer_offline", "true"},
		{"PNPM_HOME", filepath.Join(prefix, "pnpm")},
		{"BUN_INSTALL", filepath.Join(prefix, "bun")},
		// corepack 的缓存也进我们的缓存根（--purge 能一起清）
		{"COREPACK_HOME", p.CacheDir("corepack")},
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

// ExportScript 生成可 eval 的片段：先 prepend PATH 四段，再 export 变量。
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
			// 仅在真 Windows 上读本机环境：CI（Linux）也装着 pwsh，PSModulePath 会让"预测 windows 路径"的调用
			// 误判成 PowerShell 7（门禁在 ubuntu 上就这样红过一次）。
			getenv := os.Getenv
			if runtime.GOOS != "windows" {
				getenv = func(string) string { return "" }
			}
			return WindowsProfilePaths(home, getenv)[0]
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

// Path 列表的解析/比较（Windows 注册表 Path 用 ";" 分隔）。
// 放在不带 build tag 的文件里，是为了能在非 Windows 上单测这两个纯函数。

// splitPathList 按 ";" 切分并去掉空项与多余空白。
func splitPathList(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// samePath 比较两个 Windows 路径（大小写不敏感、分隔符不敏感、忽略尾部反斜杠）。
func samePath(a, b string) bool {
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, "/", `\`), `\`))
	}
	return norm(a) == norm(b)
}

func NpmCacheDir(p paths.Paths) string {
	home, err := os.UserHomeDir()
	if err == nil {
		existing := filepath.Join(home, ".npm")
		if st, err := os.Stat(existing); err == nil && st.IsDir() {
			return existing
		}
	}
	return p.CacheDir("npm")
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
// OnSyncDir 判断路径是否落在常见的云同步目录里（OneDrive / Dropbox / iCloud / Google Drive / 坚果云）。
//
// 为什么值得单独报警：同步客户端会在后台改写/回滚文件，`flock` 在同步盘上常常形同虚设、
// 原子 rename 也可能被同步进程抢成半成品 —— 把 PK_ROOT 放进去会偶发"锁不住""台账回滚成旧版本"。
func OnSyncDir(path string) bool {
	// 按路径段比对，避免 "onedrive-backup-local" 这种自家目录被误判
	segs := strings.Split(strings.ToLower(strings.ReplaceAll(path, `\`, "/")), "/")
	for _, seg := range segs {
		for _, name := range []string{"onedrive", "dropbox", "icloud", "mobile documents",
			"google drive", "googledrive", "nutstore", "坚果云", "百度网盘"} {
			if seg == name || strings.HasPrefix(seg, name+" ") { // 真实目录常带后缀，如 "OneDrive - Contoso"
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 云同步目录判定（原 syncdir.go）：doctor 用，PK_ROOT 落在 OneDrive/iCloud 里时锁与台账不可靠。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// go 的 GOTOOLCHAIN（原 env/toolchain.go，更早是 internal/toolchain 包）。
// ---------------------------------------------------------------------------

// GoToolchainEnv / CheckGoToolchain：装完必须让 GOTOOLCHAIN=local 生效（方案 §6.6），
// 否则 go 会按 go.mod 的 toolchain 声明自己再下一次工具链（多一个下载面）。
// GoToolchainEnv 是 go 必须写进环境的那一条。
const GoToolchainEnv = "GOTOOLCHAIN=local"

// CheckGoToolchain 检查已生效的环境里是否有 GOTOOLCHAIN=local（doctor 用）。
// 返回空字符串表示没问题；否则返回给人看的告警文本。
func CheckGoToolchain(lookup func(string) string) string {
	if lookup == nil {
		return ""
	}
	if v := strings.TrimSpace(lookup("GOTOOLCHAIN")); v != "local" {
		return "GOTOOLCHAIN 不是 local：go 会按 go.mod 的 toolchain 声明自己再下一次工具链（多一个下载面）。" +
			"用 `eval \"$(pk env --shell bash)\"` 让 GOTOOLCHAIN=local 生效"
	}
	return ""
}
