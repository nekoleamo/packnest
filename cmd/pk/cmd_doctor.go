package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/toolchain"
)

// cmdDoctor 体检并给可执行建议（M0 覆盖：配置/镜像档/provider/台账/shim/PATH 顺序）。
func (a *app) cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "doctor")
	globals := fs.Bool("globals", false, "检查全局包（engines / 原生 ABI / 命令遮蔽）")
	foreign := fs.Bool("foreign", false, "检测其他管理器（M5 后续）")
	probe := fs.Bool("probe", false, "探测镜像索引是否可达（会发 HEAD 请求）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	var issues []jsonout.Issue
	add := func(sev jsonout.Severity, code, msg string, detail map[string]any) {
		issues = append(issues, jsonout.Issue{Code: code, Severity: sev, Message: msg, Detail: detail})
	}

	// 1. provider 清单（内嵌 TOML 自洽性）
	if _, err := provider.All(); err != nil {
		add(jsonout.SeverityErr, "PROVIDERS_BAD", err.Error(), nil)
	}

	// 1b. 配置可读性
	if a.cfgErr != nil {
		add(jsonout.SeverityErr, "CONFIG_BROKEN", a.cfgErr.Error(), map[string]any{"path": a.paths.Config()})
	}

	// 2. 镜像档
	if _, err := mirror.Get(a.cfg.Mirror); err != nil {
		add(jsonout.SeverityErr, "MIRROR_PROFILE_INVALID", err.Error(), map[string]any{"profile": a.cfg.Mirror})
	}
	if p, err := mirror.Get(a.cfg.Mirror); err == nil && p.Name == mirror.AutoProfile {
		add(jsonout.SeverityInfo, "MIRROR_AUTO", "镜像档为 auto：首次安装会探测并缓存 6h", nil)
	}

	// 3. 根目录状态（首次运行不算错）
	if _, err := os.Stat(a.paths.Root); err != nil {
		add(jsonout.SeverityInfo, "ROOT_ABSENT", "还没有安装过任何工具（目录不存在）", map[string]any{"root": a.paths.Root})
	}

	// 4. 台账自洽
	ledger, recovered, err := store.LoadLedgerWithRecovery(a.paths.Ledger())
	if err != nil {
		add(jsonout.SeverityErr, "LEDGER_BROKEN", err.Error(), map[string]any{"path": a.paths.Ledger()})
	} else if recovered {
		add(jsonout.SeverityWarn, "LEDGER_RECOVERED",
			"台账主文件损坏，已用 .bak 恢复；下次写入会重建主文件", map[string]any{"path": a.paths.Ledger()})
	}
	if err == nil {
		for _, msg := range ledger.Validate() {
			add(jsonout.SeverityErr, "LEDGER_INCONSISTENT", msg, nil)
		}
	}

	// 5. 自身双名 shim
	for _, name := range []string{shim.SelfName, shim.AliasName} {
		path, isShim, err := shim.Resolve(a.paths.Bin(), name, goos())
		switch {
		case err != nil:
			add(jsonout.SeverityWarn, "SHIM_BROKEN", fmt.Sprintf("%s: %v", name, err), map[string]any{"name": name})
		case path == "":
			add(jsonout.SeverityWarn, "SHIM_MISSING", fmt.Sprintf("缺少 %s 入口（`pk install` 会补齐）", name), map[string]any{"name": name})
		case !isShim:
			add(jsonout.SeverityInfo, "SHIM_PLAIN", fmt.Sprintf("%s 是普通文件，不是我们建的入口", name), map[string]any{"name": name, "path": path})
		}
	}

	// 6. PATH 顺序（bin 必须在最前，方案 §6.7）
	segs := splitPath(os.Getenv("PATH"))
	want := a.paths.Bin()
	if len(segs) == 0 || segs[0] != want {
		detail := map[string]any{"want_first": want, "hint": env.PathHint(a.paths.PathSegments())}
		if contains2(segs, want) {
			add(jsonout.SeverityWarn, "PATH_ORDER", fmt.Sprintf("%s 不在 PATH 最前", want), detail)
		} else {
			add(jsonout.SeverityWarn, "PATH_MISSING", fmt.Sprintf("%s 不在 PATH 里（`pk env --shell bash` 取片段）", want), detail)
		}
	}

	// 6a. 镜像可达性（--probe 才探网，默认零网络）
	if *probe {
		loader := &mirror.Prober{Client: a.client, Path: a.paths.Probe(), TTL: mirror.DefaultTTL}
		all, err := provider.All()
		if err == nil {
			for _, p := range all {
				urls := p.IndexURLs(a.cfg.Mirror)
				if len(urls) == 0 {
					continue
				}
				res := loader.ProbeOne(a.context(), urls[0])
				if !res.OK {
					add(jsonout.SeverityErr, "MIRROR_UNREACHABLE",
						fmt.Sprintf("%s 的索引源不可达：%s", p.ID, urls[0]),
						map[string]any{"tool": p.ID, "url": urls[0], "err": res.Err,
							"advice": "用 `pk mirror use cn` 换档，或用 PK_MIRROR 指向内网源"})
				}
			}
		}
	}

	// 6b. go 的 GOTOOLCHAIN（方案 §6.6：不是 local 就多一个下载面）
	if ledger != nil && len(installedTools(ledger, "go")) > 0 {
		if msg := toolchain.CheckGoEnv(os.Getenv); msg != "" {
			add(jsonout.SeverityWarn, "GO_TOOLCHAIN_NOT_LOCAL", msg, map[string]any{"current": os.Getenv("GOTOOLCHAIN")})
		}
	}

	// 6c. 全局包 L1/L2/L3 体检（方案 §8.5）
	if *globals && ledger != nil {
		nodeVersion := a.cfg.Tools["node"]
		if nodeVersion == "" {
			nodeVersion = ledger.Active("node")
		}
		prefix := a.paths.Global()
		if a.cfg.GlobalMode == "per-version" && nodeVersion != "" {
			prefix = filepath.Join(a.paths.Root, "global", "node-"+nodeVersion)
		}
		pkgs, err := env.ScanGlobals(prefix, goos())
		if err != nil {
			add(jsonout.SeverityWarn, "GLOBALS_SCAN_FAILED", err.Error(), map[string]any{"prefix": prefix})
		}
		for _, problem := range env.CheckGlobals(pkgs, nodeVersion) {
			add(jsonout.SeverityWarn, problem.Code, problem.Message, problem.Detail)
		}
		for _, problem := range env.CheckShadowing(pkgs, projectBins(a.cwd())) {
			add(jsonout.SeverityInfo, problem.Code, problem.Message, problem.Detail)
		}
		if len(pkgs) == 0 {
			add(jsonout.SeverityInfo, "GLOBALS_NONE", fmt.Sprintf("全局包目录里没有包（%s）", prefix), map[string]any{"prefix": prefix})
		}
	}

	// 6d. 其他管理器（--foreign 才逐个列版本；默认只做一次轻量探测）
	if *foreign {
		home, _ := os.UserHomeDir()
		managers := scanForeignManagers(home)
		if len(managers) == 0 {
			add(jsonout.SeverityInfo, "FOREIGN_NONE", "没有检测到 proto/nvm/volta/asdf/mise/fnm/nodenv 的 node 版本目录", nil)
		}
		for _, m := range managers {
			add(jsonout.SeverityInfo, "FOREIGN_MANAGER",
				fmt.Sprintf("%s 里有 %d 个 node 版本（%s…）", m.Name, len(m.Versions), strings.Join(tailVersions(m.Versions, 3), ", ")),
				map[string]any{"manager": m.Name, "path": m.Path, "versions": m.Versions,
					"advice": "只让一方管 node：把 ~/.packnest/bin 放到它前面，或移除对方的 shell 初始化行（我们不会自动改别人目录）"})
		}
		if conflicts := foreignPathConflicts(os.Getenv("PATH"), a.paths.Bin()); len(conflicts) > 0 {
			add(jsonout.SeverityWarn, "FOREIGN_SHIM_PRIORITY",
				"PATH 里这些目录排在 pk 前面，会抢走 node 命令："+strings.Join(conflicts, ", "),
				map[string]any{"dirs": conflicts, "advice": "把 ~/.packnest/bin 移到 PATH 最前（pk env --write 会加托管块）"})
		}
	}

	// 7. 其他管理器共存提示（只读，不自动改，方案 §18.10）
	for _, other := range []string{".proto", ".nvm", ".volta", ".asdf", ".mise"} {
		home, err := os.UserHomeDir()
		if err != nil {
			continue
		}
		dir := filepath.Join(home, other)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			add(jsonout.SeverityInfo, "FOREIGN_MANAGER",
				fmt.Sprintf("检测到 %s；两者都管 node 时请明确优先级（我们不自动改别人目录）", dir),
				map[string]any{"path": dir})
		}
	}

	out := jsonout.NewDoctor(issues)
	if a.json {
		if err := jsonout.WriteJSON(a.out, out); err != nil {
			return a.fail(err)
		}
	} else {
		if len(issues) == 0 {
			fmt.Fprintln(a.out, "体检通过：没有发现问题。")
		}
		for _, is := range issues {
			fmt.Fprintf(a.out, "  [%-5s] %-22s %s\n", is.Severity, is.Code, is.Message)
		}
	}
	if !out.OK {
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}

// cmdEnv 输出 shell 环境片段（默认只打印，不落盘）。
func (a *app) cmdEnv(args []string) int {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "env")
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
		dialect = detectShell()
	}
	sh, err := env.ParseShell(dialect)
	if err != nil {
		return a.failUsage(err)
	}

	vars := append(env.RegistryVars(a.cfg.Mirror), env.SharedVars(a.paths, npmCacheDir(a.paths.Root))...)
	if p, err := provider.Get("go"); err == nil && p.Toolchain != nil {
		vars = append(vars, env.ToolchainVars(p.Toolchain.Env)...)
	}
	vars = mergeVars(vars)

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
		rc := *rcFile
		if rc == "" {
			rc = env.RCFileFor(sh, goos(), home, os.Getenv("SHELL"))
		}
		if *unset {
			if err := env.RemoveManagedBlock(rc); err != nil {
				return a.fail(err)
			}
			if !a.json {
				fmt.Fprintf(a.out, "已删掉 %s 里的 packnest 托管块\n", rc)
			}
			return jsonout.ExitOK
		}
		block := env.ExportScript(vars, pathSegs, sh)
		if err := env.WriteManagedBlock(rc, block); err != nil {
			return a.fail(err)
		}
		if a.json {
			return a.emit(struct {
				Schema int    `json:"schema"`
				RC     string `json:"rc"`
				Block  string `json:"block"`
			}{jsonout.SchemaVersion, rc, block})
		}
		fmt.Fprintf(a.out, "已写入 %s 的托管块（块外内容未改动；备份在 %s.pk.bak）\n", rc, rc)
		fmt.Fprintln(a.out, "新开一个 shell，或 `source "+rc+"` 生效")
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
func npmCacheDir(root string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		existing := filepath.Join(home, ".npm")
		if st, err := os.Stat(existing); err == nil && st.IsDir() {
			return existing
		}
	}
	return filepath.Join(root, "cache", "npm")
}

// mergeVars 按键去重（后者覆盖前者，保留首次出现的位置）。
func mergeVars(vars []env.KV) []env.KV {
	index := map[string]int{}
	out := make([]env.KV, 0, len(vars))
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

// detectShell 从 $SHELL 猜方言（PowerShell 由 --shell 显式给）。
func detectShell() string {
	switch base := filepath.Base(os.Getenv("SHELL")); base {
	case "zsh", "bash", "sh", "dash", "ksh":
		return base
	}
	return "bash"
}

// projectBins 读取项目 node_modules/.bin 的命令名 → 所属包（L3 命令遮蔽检测）。
func projectBins(dir string) map[string]string {
	binDir := filepath.Join(dir, "node_modules", ".bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, e := range entries {
		out[e.Name()] = "node_modules/.bin"
	}
	return out
}

// installedTools 返回某工具的已装版本（doctor 用来决定要不要检查该工具的特情）。
func installedTools(l *store.Ledger, tool string) []string {
	var out []string
	for _, in := range l.Installs {
		if in.Tool == tool {
			out = append(out, in.Version)
		}
	}
	return out
}

// tailVersions 取最后 n 个版本（列表太长时只显示最新的几个）。
func tailVersions(versions []string, n int) []string {
	if len(versions) <= n {
		return versions
	}
	return versions[len(versions)-n:]
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	sep := string(os.PathListSeparator)
	parts := strings.Split(p, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func contains2(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// projectPathSegments 读取当前目录的 pk.toml / pk.lock，返回项目锁定版本的 bin 目录。
//
// 用法：项目里 `.envrc` 写 `eval "$(pk env --project)"`，direnv 会在 cd 进来时把该项目的
// node/pnpm 等前置到 PATH —— 这就是"项目级覆盖全局默认"的落地方式，不必把 shim 改成
// 每次调用都跑一遍解析的壳脚本（那会给每个 node 调用加几毫秒）。
func (a *app) projectPathSegments() ([]string, error) {
	project, err := install.LoadProject(a.cwd())
	if err != nil || len(project) == 0 {
		return nil, err
	}
	lock, err := install.LoadLock(a.cwd())
	if err != nil {
		return nil, err
	}
	ledger, err := store.LoadLedger(a.paths.Ledger())
	if err != nil {
		return nil, err
	}
	var segs []string
	for tool := range project {
		if tool == "npm" {
			continue // npm 跟着 node 的 prefix 走
		}
		version := ""
		if entry, ok := lock.Find(tool); ok {
			version = entry.Version
		} else if v := a.cfg.Tools[tool]; v != "" {
			version = v
		}
		if version == "" {
			continue
		}
		in := ledger.Find(tool, version)
		if in == nil {
			continue // 锁了但还没装：跳过，不阻断其余工具
		}
		segs = append(segs, filepath.Join(in.Path, "bin"))
	}
	return segs, nil
}
