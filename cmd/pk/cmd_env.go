package main

import (
	"fmt"
	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"os"
	"path/filepath"
	"strings"
)

// `pk env` 与它用到的 shell/rc 判定（从 cmd_doctor.go 拆出来，单文件别长过 600 行）。

// cmdEnv 输出 shell 环境片段（默认只打印，不落盘）。
func (a *app) cmdEnv(args []string) int {
	fs := a.newFlagSet("env")
	shellName := fs.String("shell", "", "shell 方言: bash|zsh|sh|powershell（默认看 $SHELL）")
	write := fs.Bool("write", false, "把托管块写进 shell rc（幂等，只改自己那一段）")
	unset := fs.Bool("unset", false, "删掉 rc 里的托管块")
	completion := fs.String("completion", "", "输出 shell 补全脚本: bash|zsh|powershell")
	rcFile := fs.String("rc", "", "写哪个 rc 文件（默认按 $SHELL 猜，如 ~/.zshrc）")
	projectScope := fs.Bool("project", false, "输出项目级环境：把 pk.toml/pk.lock 锁定的版本目录前置到 PATH（配合 direnv 用）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if *completion != "" {
		if err := writeCompletion(a.out, *completion); err != nil {
			return a.failUsage(err)
		}
		return jsonout.ExitOK
	}
	dialect := *shellName
	if dialect == "" {
		dialect = env.DetectShell()
	}
	sh, err := env.ParseShell(dialect)
	if err != nil {
		return a.failUsage(err)
	}

	vars := append(env.RegistryVars(a.cfg.Mirror), env.SharedVars(a.paths, env.NpmCacheDir(a.paths.Root))...)
	if p, err := provider.Get("go"); err == nil && p.Toolchain != nil {
		vars = append(vars, env.ToolchainVars(p.Toolchain.Env)...)
	}
	vars = env.MergeVars(vars)

	pathSegs := a.paths.PathSegments()
	if *projectScope {
		projectSegs, err := a.projectPathSegments()
		if err != nil {
			return a.fail(err)
		}
		if len(projectSegs) == 0 {
			fmt.Fprintln(a.errOut, "pk: 当前目录没有 pk.toml，或锁定的工具还没安装")
		}
		pathSegs = append(projectSegs, pathSegs...)
	}

	if *write || *unset {
		home, err := os.UserHomeDir()
		if err != nil {
			return a.fail(err)
		}
		targets := env.WriteTargets(*rcFile, sh, goos(), home, os.Getenv)

		// Windows：PATH 走注册表里用户环境变量（不受 ExecutionPolicy 影响，cmd/PowerShell/Git Bash 都认）。
		if goos() == "windows" && *rcFile == "" {
			changed, before, after, perr := env.RemoveUserPath(pathSegs)
			if !*unset {
				changed, before, after, perr = env.AddUserPath(pathSegs)
			}
			if perr == nil && changed {
				// 兜底：改用户 PATH 前留一份旧值，出问题能照着抄回去
				bak := filepath.Join(a.paths.State(), "user-path.bak.txt")
				_ = os.MkdirAll(filepath.Dir(bak), 0o755)
				_ = os.WriteFile(bak, []byte(before+"\n"), 0o644)
				if !a.json {
					fmt.Fprintf(a.out, "旧 Path 已备份到 %s\n", bak)
				}
			}
			_ = after
			if perr != nil {
				fmt.Fprintf(a.errOut, "pk: 更新用户 PATH 失败（可手动把 %s 加进「用户变量 Path」）: %v\n",
					strings.Join(pathSegs, ";"), perr)
			}
			if !a.json {
				if changed {
					fmt.Fprintln(a.out, "已更新用户 Path（注册表用户环境变量，新开窗口生效）")
				} else {
					fmt.Fprintln(a.out, "用户 Path 里已经有这些目录（无需改动）")
				}
			}
		}

		// PowerShell 执行策略禁止加载脚本时，profile 写了只会让每个新窗口报错 —— 不写，并清掉旧块。
		blocked := sh == env.ShellPowerShell && goos() == "windows" && env.PowerShellScriptsBlocked()
		effective := make([]string, 0, len(targets))
		for _, t := range targets {
			if blocked && strings.Contains(strings.ToLower(t), "windowspowershell") {
				if err := env.RemoveManagedBlock(t); err != nil {
					return a.fail(err)
				}
				if !a.json {
					fmt.Fprintf(a.out, "跳过 %s（执行策略禁止加载脚本；已清掉其中的 packnest 托管块）\n", t)
				}
				continue
			}
			effective = append(effective, t)
		}
		targets = effective

		if *unset {
			for _, rc := range targets {
				if err := env.RemoveManagedBlock(rc); err != nil {
					return a.fail(err)
				}
				if !a.json {
					fmt.Fprintf(a.out, "已删掉 %s 里的 packnest 托管块\n", rc)
				}
			}
			if blocked && !a.json {
				fmt.Fprintln(a.out, "（PowerShell 执行策略禁止脚本，profile 里的块本来就不会生效）")
			}
			return jsonout.ExitOK
		}
		block := env.ExportScript(vars, pathSegs, sh)
		if len(targets) == 0 && !a.json {
			fmt.Fprintln(a.out, "没有可写的 profile；PATH 已经交给系统环境变量。")
			fmt.Fprintln(a.out, "要让 registry/GOPROXY 这些变量也常驻，二选一：")
			fmt.Fprintln(a.out, "  1) Set-ExecutionPolicy -Scope CurrentUser RemoteSigned   然后重跑 pk env --write")
			fmt.Fprintln(a.out, "  2) 每次会话：pk env --shell powershell | Invoke-Expression")
			fmt.Fprintln(a.out, "（本次已生效的部分：用户 Path；改动只涉及 packnest 自己的目录）")
		}
		for _, rc := range targets {
			if err := env.WriteManagedBlock(rc, block); err != nil {
				return a.fail(err)
			}
			if !a.json {
				fmt.Fprintf(a.out, "已写入 %s 的托管块（块外内容未改动；备份在 %s.pk.bak）\n", rc, rc)
			}
		}
		if a.json {
			return a.emit(struct {
				Schema int      `json:"schema"`
				RC     string   `json:"rc"`
				RCs    []string `json:"rcs"`
				Block  string   `json:"block"`
			}{jsonout.SchemaVersion, targets[0], targets, block})
		}
		if sh == env.ShellPowerShell {
			fmt.Fprintln(a.out, "新开一个 PowerShell 窗口生效（当前窗口可先执行上面的片段，或用 `pk env --shell powershell | Invoke-Expression`）")
			fmt.Fprintf(a.out, "确认写到哪个 profile：pwsh 里执行 `$PROFILE`；Windows PowerShell 5.1 是另一个路径（两块都已写）\n")
		} else {
			fmt.Fprintln(a.out, "新开一个 shell，或 `source "+targets[0]+"` 生效")
		}
		return jsonout.ExitOK
	}

	if a.json {
		return a.emit(struct {
			Schema     int      `json:"schema"`
			Shell      string   `json:"shell"`
			Path       []string `json:"path"`
			Env        []env.KV `json:"env"`
			ScriptHint string   `json:"script_hint"`
		}{
			Schema:     jsonout.SchemaVersion,
			Shell:      string(sh),
			Path:       a.paths.PathSegments(),
			Env:        vars,
			ScriptHint: "eval \"$(pk env --shell " + string(sh) + ")\"",
		})
	}
	fmt.Fprint(a.out, env.ExportScript(vars, pathSegs, sh))
	return jsonout.ExitOK
}

// npmCacheDir 沿用已有 ~/.npm（绝不迁移不复制），不存在才用自己的缓存目录。
