package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// commandHelp 是 9 个命令的 `--help` 文本（方案 §12.2 第 10 条：命令要能自解释）。
var commandHelp = map[string]string{
	"install": `pk install [工具[@版本]]... [--frozen] [--insecure] [--force] [--force-rehash] [--quiet] [--corepack|--no-corepack]

  装工具。不带参数时按当前目录的 pk.toml 全装。
  作用域：没有 --global/--project 开关，由当前目录决定：
    当前目录有 pk.toml → 按项目声明装，并生成/更新 pk.lock（项目级）
    没有 pk.toml        → 装进共享目录；若该工具还没有全局默认，则自动设为默认
  磁盘上永远只有一份（~/.packnest/tools/<tool>/<ver>），项目级只体现"锁定 + PATH 顺序"，
  不复制副本；项目内生效用 pk env --project（direnv 里跑）。
  语义（方案 §18.9）：
    pk install                 按 pk.toml 全装（没有 pk.toml 则报错）
    pk install node            装 pk.toml 里 pin 的 node；没有则最新稳定版
    pk install node@22         显式 spec，不受 pk.toml 影响
  退出码：0 成功 / 1 通用 / 2 用法 / 3 网络 / 4 校验 / 5 锁被占用`,

	"uninstall": `pk uninstall <工具>[@<版本>] [--force]
pk uninstall --all [--purge] [--keep-cache] [--yes]

  单个：台账驱动卸载，只删台账里记录过的目录（不在台账里的只提示不删）；
        被引用（全局默认 / 项目锁 / 其他工具依赖）的版本需要 --force。
  全部：--all 清掉 pk 装过的一切 —— 所有运行时 + shim + shell rc / Windows 用户 Path 里的托管块，
        最后按"自己是怎么装的"给出删除 pk 二进制的命令（brew / npm / scoop / winget / 脚本）。
        不带 --yes 只打印计划，不动任何东西；交互终端会列出版本再问一次 y/N。`,

	"use": `pk use <工具>@<版本> [--project]
pk use --lock

  切换全局默认版本：写 config.toml、更新台账引用、把 shim 重指到该版本。
  只会切到**已安装**的版本（先 pk install）。
`,

	"list": `pk list [工具] [--major N] [--available] [--refresh] [--all|--limit N] [--json]

  不带参数：列出已装工具（* 是当前全局默认），带版本占用空间。这个不带网络。
  pk list node：只看 node —— 已装版本 + 可装版本，旧 → 新、最新在最下面；
                版本 ≤40 个时一版一行（带 latest / lts: <代号> / 已装 标签），
                更多时自动换成「按大版本分组 + 多列网格」（列数按终端宽度算），
                标记：* 最新 · l LTS · + 已装 · @ 当前默认。
`,

	"which": `pk which <命令> [--json]

  解析 shim 到真实二进制路径（调试 shim / 双名一致性检查）。
  例：pk which node；pk which pk 与 pk which packnest 必须指向同一个文件。`,

	"mirror": `pk mirror list|probe|use <档位>|write

  list    列出内嵌档位（default/cn/tuna/aliyun/ustc/auto）与当前档位
  probe   并行探测各工具索引源（结果缓存 6h；--tool 只探一个）
  use     切换档位（写 config.toml）
  write   把当前档位的 registry 写进 ~/.npmrc（先备份、只增不改）`,

	"doctor": `pk doctor [--json] [--globals] [--probe]

  环境体检，问题码固定（GUI 与脚本依赖）：
    PATH_ORDER / PATH_MISSING         bin 目录不在 PATH 最前
    LEDGER_BROKEN / LEDGER_RECOVERED  台账损坏 / 已从 .bak 恢复
    CONFIG_BROKEN / MIRROR_PROFILE_INVALID
    MIRROR_UNREACHABLE（--probe）      索引源不可达
    GO_TOOLCHAIN_NOT_LOCAL            装了 go 但 GOTOOLCHAIN 不是 local
    GLOBAL_ENGINES_UNMET / GLOBAL_NATIVE_ABI / GLOBAL_SHADOWS_PROJECT（--globals）
    FOREIGN_MANAGER                   检测到 proto/nvm/mise 等其他管理器（--foreign 列出版本）
    FOREIGN_SHIM_PRIORITY             别人的 shim 排在 pk 前面，会抢走 node
  退出码：有 error 级问题时为 1。`,

	"env": `pk env [--shell bash|zsh|sh|powershell] [--write] [--unset] [--completion SHELL] [--json]

  输出 shell 片段（PATH 五段 + registry/共享前缀/镜像档变量）。
  默认只打印不落盘；--write 才写进 rc 的托管块（幂等，块外内容不动）。
`,

	"gc": `pk gc [--dry-run] [--cache-max 5GB] [--json]

  回收零引用版本（被引用的列在 KeptRef 里）与超限的产物缓存（LRU）。
  默认上限取 config.toml 的 cache_max。`,

	"version": `pk version [--json]

  打印版本与构建信息（渠道安装时是机器可读输出）。`,
}

// printCommandHelp 打印某个命令的详细帮助。
func printCommandHelp(w io.Writer, cmd string) bool {
	if text, ok := commandHelp[cmd]; ok {
		fmt.Fprintln(w, text)
		fmt.Fprintf(w, "\n开关与默认值：pk %s --help\n", cmd)
		return true
	}
	return false
}

// commandNames 返回有详细帮助的命令名（排序）。
func commandNames() []string {
	names := make([]string, 0, len(commandHelp))
	for name := range commandHelp {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// helpSummary 是 `pk help` 的总览（与 printUsage 一致，但列出可查询的命令）。
func helpSummary(w io.Writer) {
	fmt.Fprintln(w, "详细用法: pk help <命令>，可用命令: "+strings.Join(commandNames(), " / "))
}

// shell 补全脚本。放在 env 子命令里（`pk env --completion bash`），
// 这样不用为了补全多一个命令（命令面固定 9 个）。
//
// 内容保持朴素：只补命令名、子命令与常用开关，不引入任何运行时依赖。

var completionCommands = []string{
	"install", "uninstall", "use", "list", "which", "mirror", "doctor", "env", "gc", "version", "help",
}

func writeCompletion(w io.Writer, shell string) error {
	commands := strings.Join(completionCommands, " ")
	switch shell {
	case "bash":
		fmt.Fprintf(w, `# packnest bash 补全：eval "$(pk env --completion bash)"
_packnest() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  case "$prev" in
    mirror) COMPREPLY=( $(compgen -W "list probe use write" -- "$cur") ); return ;;
    env) COMPREPLY=( $(compgen -W "--shell --write --unset --completion --json" -- "$cur") ); return ;;
    doctor) COMPREPLY=( $(compgen -W "--globals --probe --json" -- "$cur") ); return ;;
    gc) COMPREPLY=( $(compgen -W "--dry-run --cache-max --json" -- "$cur") ); return ;;
    install) COMPREPLY=( $(compgen -W "--frozen --insecure --force --force-rehash --quiet --json" -- "$cur") ); return ;;
  esac
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
  fi
}
complete -F _packnest pk
complete -F _packnest packnest
`, commands)
	case "zsh":
		fmt.Fprintf(w, `# packnest zsh 补全：eval "$(pk env --completion zsh)"
_packnest() {
  local -a cmds
  cmds=(%s)
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
    return
  fi
  case $words[2] in
    mirror) _values 'subcommand' list probe use write ;;
    install) _values 'flag' --frozen --insecure --force --force-rehash --quiet --json ;;
    gc) _values 'flag' --dry-run --cache-max --json ;;
    doctor) _values 'flag' --globals --probe --json ;;
    env) _values 'flag' --shell --write --unset --completion --json ;;
  esac
}
compdef _packnest pk
compdef _packnest packnest
`, commands)
	case "powershell":
		fmt.Fprintf(w, `# packnest PowerShell 补全：pk env --completion powershell | Out-String | Invoke-Expression
$packnestCommands = @(%s)
Register-ArgumentCompleter -Native -CommandName pk,packnest -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $packnestCommands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
  }
}
`, quoteList(completionCommands))
	default:
		return fmt.Errorf("不支持的 shell %q（bash/zsh/powershell）", shell)
	}
	return nil
}

func quoteList(items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, "'"+item+"'")
	}
	return strings.Join(out, ", ")
}

// 其他版本管理器的只读探测（方案 §18.10）。
//
// 铁律：**只读、只提示、不删不改**别人的目录。用户当前很可能同时在用 proto/nvm，
// 我们的价值是"告诉他 PATH 上谁在生效"，而不是"帮他清理"。

type foreignManager struct {
	Name     string
	Path     string
	Versions []string
}

// foreignCandidates 是各管理器放 node 版本的常见路径（相对 $HOME）。
var foreignCandidates = []struct {
	name   string
	dir    string
	prefix string // 版本目录前缀（nvm/fnm 带 v）
}{
	{"proto", ".proto/tools/node", ""},
	{"nvm", ".nvm/versions/node", "v"},
	{"volta", ".volta/tools/image/node", ""},
	{"asdf", ".asdf/installs/nodejs", ""},
	{"mise", ".mise/installs/node", ""},
	{"fnm", ".fnm/node-versions", "v"},
	{"nodenv", ".nodenv/versions", ""},
}

// scanForeignManagers 扫描其他管理器的 node 版本（只读）。
func scanForeignManagers(home string) []foreignManager {
	out := make([]foreignManager, 0, len(foreignCandidates))
	for _, c := range foreignCandidates {
		dir := filepath.Join(home, filepath.FromSlash(c.dir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var versions []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := strings.TrimPrefix(e.Name(), c.prefix)
			if name == "" || name[0] < '0' || name[0] > '9' {
				continue
			}
			versions = append(versions, name)
		}
		if len(versions) == 0 {
			continue
		}
		sort.Strings(versions)
		out = append(out, foreignManager{Name: c.name, Path: dir, Versions: versions})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// foreignPathConflicts 找出 PATH 里排在 pk 前面的"别人的 bin"（会导致 pk 的 shim 不生效）。
func foreignPathConflicts(pathEnv string, pkBin string) []string {
	segs := splitPath(pathEnv)
	var conflicts []string
	for _, seg := range segs {
		if seg == pkBin {
			break // pk 之前的才算冲突
		}
		lower := strings.ToLower(filepath.ToSlash(seg))
		for _, marker := range []string{"/.proto/shims", "/.nvm/", "/.volta/bin", "/.asdf/shims", "/.mise/shims", "/.fnm/", "/.nodenv/shims", "/.local/share/mise/shims"} {
			if strings.Contains(lower, strings.TrimSuffix(marker, "/")) {
				conflicts = append(conflicts, seg)
				break
			}
		}
	}
	return conflicts
}
