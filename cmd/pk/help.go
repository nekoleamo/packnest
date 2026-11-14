package main

import (
	"embed"
	"fmt"
	"github.com/packnest/packnest/cmd/pk/present"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// commandHelp 是各命令的 `--help` 文本（方案 §12.2 第 10 条：命令要能自解释）。
// 文本放 cmd/pk/help/<命令>.txt 并 //go:embed 进来：它们是"数据"，不该占代码行数预算
// （与 providers/*.toml 同一思路）。
//
//go:embed help/*.txt
var helpFS embed.FS

// 英文帮助也是数据：放 help/en/*.txt，零代码行数（--lang=en 时优先用它）。
//
//go:embed help/en/*.txt
var helpENFS embed.FS

// 补全脚本同理：是数据，不是代码（`pk env --completion bash|zsh|powershell`）。
//
//go:embed completion/*.txt
var completionFS embed.FS

var commandHelp = loadHelp(helpFS, "help")
var commandHelpEN = loadHelp(helpENFS, "help/en")

// loadHelp 把一个 embed 目录读成 命令名 → 文本。
func loadHelp(fsys embed.FS, dir string) map[string]string {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		data, err := fsys.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(e.Name(), ".txt")] = strings.TrimRight(string(data), "\n")
	}
	return out
}

// printCommandHelp 打印某个命令的详细帮助。
func printCommandHelp(w io.Writer, cmd string) bool {
	text, ok := commandHelp[cmd]
	if !ok {
		return false
	}
	if present.LangIsEN() {
		if en, has := commandHelpEN[cmd]; has {
			text = en
		}
	}
	fmt.Fprintln(w, text)
	fmt.Fprintf(w, tr("\n开关与默认值：pk %s --help\n", "\nFlags and defaults: pk %s --help\n"), cmd)
	return true
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
	fmt.Fprintln(w, tr("详细用法: pk help <命令>，可用命令: ", "details: pk help <command>; available commands: ")+strings.Join(commandNames(), " / "))
}

// shell 补全脚本。放在 env 子命令里（`pk env --completion bash`），
// 这样不用为了补全多一个命令（命令面固定 9 个）。
//
// 内容保持朴素：只补命令名、子命令与常用开关，不引入任何运行时依赖。

var completionCommands = []string{
	"install", "uninstall", "use", "list", "which", "mirror", "doctor", "env", "gc", "upgrade", "version", "help",
}

func writeCompletion(w io.Writer, shell string) error {
	data, err := completionFS.ReadFile("completion/" + shell + ".txt")
	if err != nil {
		return errT(tr("不支持的 shell %q（bash/zsh/powershell）", "unsupported shell %q (bash/zsh/powershell)"), shell)
	}
	arg := strings.Join(completionCommands, " ")
	if shell == "powershell" { // PowerShell 的数组要带引号
		arg = quoteList(completionCommands)
	}
	_, err = fmt.Fprintf(w, string(data), arg)
	return err
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
	slices.SortFunc(out, func(a, b foreignManager) int { return strings.Compare(a.Name, b.Name) })
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
