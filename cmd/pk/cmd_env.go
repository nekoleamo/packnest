package main

import (
	"fmt"
	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"os"
	"path/filepath"
	"runtime"
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

	vars := append(env.RegistryVars(a.cfg.Mirror), env.SharedVars(a.paths, env.NpmCacheDir(a.paths))...)
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

// ---------------------------------------------------------------------------
// `pk mirror`（原 cmd_mirror.go）：与 `pk env` 同属"环境配置"这一组。
// ---------------------------------------------------------------------------

func goos() string { return runtime.GOOS }

// cmdMirror 是镜像档的门面命令：list / probe / use（write 属 M5）。
func (a *app) cmdMirror(args []string) int {
	if len(args) == 0 {
		return a.failUsage(fmt.Errorf("用法: pk mirror list|probe|use <档位>"))
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return a.mirrorList()
	case "probe":
		return a.mirrorProbe(rest)
	case "use":
		return a.mirrorUse(rest)
	case "write":
		return a.mirrorWrite(rest)
	}
	return a.failUsage(fmt.Errorf("未知子命令 pk mirror %s", sub))
}

func (a *app) mirrorList() int {
	current := a.cfg.Mirror
	if a.json {
		type row struct {
			Name     string `json:"name"`
			Display  string `json:"display"`
			Registry string `json:"registry"`
			Current  bool   `json:"current"`
		}
		rows := make([]row, 0, len(mirror.Profiles()))
		for _, p := range mirror.Profiles() {
			rows = append(rows, row{p.Name, p.Display, p.Registry, p.Name == current})
		}
		return a.emit(struct {
			Schema  int    `json:"schema"`
			Current string `json:"current"`
			Mirror  []row  `json:"mirrors"`
		}{jsonout.SchemaVersion, current, rows})
	}
	fmt.Fprintf(a.out, "当前档位: %s\n", current)
	for _, p := range mirror.Profiles() {
		mark := " "
		if p.Name == current {
			mark = "*"
		}
		fmt.Fprintf(a.out, "  %s %-8s %-16s %s\n      %s\n", mark, p.Name, p.Display, p.Registry, p.Note)
	}
	fmt.Fprintln(a.out, "  (auto 表示自动探测：普通命令只探首选 + 上轮最优，结果缓存 6h)")
	if current == mirror.AutoProfile {
		if p, err := provider.Get("node"); err == nil {
			prober := &mirror.Prober{Client: a.client, Path: a.paths.Probe()}
			if resolved := mirror.CachedProfile(prober, p.Index.URLs["default"], p.Index.URLs["cn"], a.now(), mirror.DefaultTTL); resolved != "" {
				fmt.Fprintf(a.out, "  auto 当前判定: %s（来自探测缓存；国外环境会判 default）\n", resolved)
			} else {
				fmt.Fprintln(a.out, "  auto 当前判定: 未探测（第一次 install 时会探，并把结果缓存）")
			}
		}
	}
	return jsonout.ExitOK
}

func (a *app) mirrorProbe(args []string) int {
	fs := a.newFlagSet("mirror probe")
	only := fs.String("tool", "", "只探测某个工具的索引源")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	all, err := provider.All()
	if err != nil {
		return a.fail(err)
	}
	prober := &mirror.Prober{Client: a.client, Path: a.paths.Probe(), TTL: mirror.DefaultTTL}

	var urls []string
	for _, p := range all {
		if *only != "" && p.ID != *only {
			continue
		}
		urls = append(urls, p.IndexURLs(a.cfg.Mirror)...)
	}
	if len(urls) == 0 {
		return a.fail(fmt.Errorf("没有可探测的源（检查 --tool 与 provider 清单）"))
	}
	results := prober.Probe(a.context(), urls)

	if a.json {
		return a.emit(struct {
			Schema  int             `json:"schema"`
			Results []mirror.Result `json:"results"`
		}{jsonout.SchemaVersion, results})
	}
	ok := 0
	for _, r := range results {
		status := "OK  "
		if !r.OK {
			status = "FAIL"
		} else {
			ok++
		}
		fmt.Fprintf(a.out, "  %s %5dms %s\n", status, r.LatencyMS, r.URL)
		if !r.OK && r.Err != "" {
			fmt.Fprintf(a.out, "       └ %s\n", r.Err)
		}
	}
	fmt.Fprintf(a.out, "\n%d/%d 可用（结果缓存 6h 于 %s）\n", ok, len(results), a.paths.Probe())
	if ok == 0 {
		return jsonout.ExitNetwork
	}
	return jsonout.ExitOK
}

// mirrorWrite 把 registry 写进 ~/.npmrc：写前备份、只增不改（方案 §6.6）。
func (a *app) mirrorWrite(args []string) int {
	fs := a.newFlagSet("mirror write")
	target := fs.String("file", "", "目标 npmrc（默认 ~/.npmrc）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return a.fail(err)
	}
	path := *target
	if path == "" {
		path = filepath.Join(home, ".npmrc")
	}
	registry := mirror.RegistryFor(a.cfg.Mirror)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return a.fail(err)
	}
	if len(existing) > 0 {
		if err := os.WriteFile(path+".pk.bak", existing, 0o644); err != nil {
			return a.fail(fmt.Errorf("备份 %s 失败: %w", path, err))
		}
	}
	// 已有 registry= 就改那一行，没有就追加（绝不删别的配置）
	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "registry=") {
			lines[i] = "registry=" + registry
			replaced = true
		}
	}
	if !replaced {
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
		lines = append(lines, "registry="+registry)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return a.fail(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return a.fail(err)
	}

	if a.json {
		return a.emit(struct {
			Schema   int    `json:"schema"`
			File     string `json:"file"`
			Registry string `json:"registry"`
			Replaced bool   `json:"replaced"`
		}{jsonout.SchemaVersion, path, registry, replaced})
	}
	fmt.Fprintf(a.out, "已写入 %s（registry=%s；备份在 %s.pk.bak）\n", path, registry, path)
	return jsonout.ExitOK
}

func (a *app) mirrorUse(args []string) int {
	if len(args) != 1 {
		return a.failUsage(fmt.Errorf("用法: pk mirror use <%s>", strings.Join(mirror.Names(), "|")))
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if _, err := mirror.Get(name); err != nil {
		return a.failUsage(err)
	}
	a.cfg.Mirror = name
	if err := a.cfg.Save(a.paths.Config()); err != nil {
		return a.fail(err)
	}
	if a.json {
		return a.emit(struct {
			Schema int    `json:"schema"`
			Mirror string `json:"mirror"`
			Config string `json:"config"`
		}{jsonout.SchemaVersion, name, a.paths.Config()})
	}
	fmt.Fprintf(a.out, "镜像档已切到 %s（写入 %s）\n", name, a.paths.Config())
	fmt.Fprintln(a.out, "下次安装即生效；`pk env --shell bash` 可查看随之变化的 registry 变量")
	return jsonout.ExitOK
}
