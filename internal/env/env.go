// Package env 生成 PATH 与 registry 环境（方案 §6.6 / §6.7）。
//
// 铁律：默认只**输出**变量片段给 shell eval，不落盘、不改用户 rc
// （`pk env --write` 才写，且带 `# managed by pk` 注释块）。
package env

import (
	"fmt"
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
