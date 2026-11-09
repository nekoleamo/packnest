package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/packnest/packnest/internal/env"
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
	globals := fs.Bool("globals", false, "检查全局包（M4 实现）")
	foreign := fs.Bool("foreign", false, "检测其他管理器（M4 实现）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if *globals || *foreign {
		return a.notImplemented("doctor --globals/--foreign", "M4")
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
	ledger, err := store.LoadLedger(a.paths.Ledger())
	if err != nil {
		add(jsonout.SeverityErr, "LEDGER_BROKEN", err.Error(), map[string]any{"path": a.paths.Ledger()})
	} else {
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

	// 6b. go 的 GOTOOLCHAIN（方案 §6.6：不是 local 就多一个下载面）
	if ledger != nil && len(installedTools(ledger, "go")) > 0 {
		if msg := toolchain.CheckGoEnv(os.Getenv); msg != "" {
			add(jsonout.SeverityWarn, "GO_TOOLCHAIN_NOT_LOCAL", msg, map[string]any{"current": os.Getenv("GOTOOLCHAIN")})
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
	shellName := fs.String("shell", "bash", "shell 方言: bash|zsh|sh|powershell")
	write := fs.Bool("write", false, "写入 shell rc（M5 实现）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if *write {
		return a.notImplemented("env --write", "M5")
	}
	sh, err := env.ParseShell(*shellName)
	if err != nil {
		return a.failUsage(err)
	}

	vars := append(env.RegistryVars(a.cfg.Mirror), env.SharedVars(a.paths, npmCacheDir(a.paths.Root))...)
	if p, err := provider.Get("go"); err == nil && p.Toolchain != nil {
		vars = append(vars, env.ToolchainVars(p.Toolchain.Env)...)
	}
	vars = mergeVars(vars)

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
	fmt.Fprint(a.out, env.ExportScript(vars, a.paths.PathSegments(), sh))
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
