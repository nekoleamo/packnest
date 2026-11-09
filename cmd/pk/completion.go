package main

import (
	"fmt"
	"io"
	"strings"
)

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
    install) COMPREPLY=( $(compgen -W "--frozen --insecure --force --quiet --json" -- "$cur") ); return ;;
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
    install) _values 'flag' --frozen --insecure --force --quiet --json ;;
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
