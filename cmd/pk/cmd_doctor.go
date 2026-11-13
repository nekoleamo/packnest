package main

import (
	"fmt"
	"io"
	"slices"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/toolchain"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cmdDoctor 体检并给可执行建议（M0 覆盖：配置/镜像档/provider/台账/shim/PATH 顺序）。
func (a *app) cmdDoctor(args []string) int {
	fs := a.newFlagSet("doctor")
	globals := fs.Bool("globals", false, "检查全局包（engines / 原生 ABI / 命令遮蔽）")
	foreign := fs.Bool("foreign", false, "检测其他管理器（proto/nvm/volta/asdf/mise/fnm 等）并列版本")
	probe := fs.Bool("probe", false, "探测镜像可达性；配合 --globals 还会用 node 实测 dlopen 原生模块")
	fix := fs.Bool("fix", false, "自动修可自动修的问题（PATH 托管块、pk/packnest 入口），并打印做了什么")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	var issues []jsonout.Issue
	add := func(sev jsonout.Severity, code, msg string, detail map[string]any) {
		issues = append(issues, jsonout.Issue{Code: code, Severity: sev, Message: msg, Detail: detail})
	}

	// 1. provider 清单（内嵌 TOML 自洽性 + 外部清单）
	if _, err := provider.All(); err != nil {
		add(jsonout.SeverityErr, "PROVIDERS_BAD", err.Error(), nil)
	}
	add(jsonout.SeverityInfo, "PROVIDERS_DIRS", "外部清单目录（不存在即未配置）", map[string]any{"dirs": provider.ExternalDirs(), "env": provider.ProvidersDirEnv})
	for _, is := range provider.ExternalIssues() {
		sev := jsonout.SeverityErr
		if is.Code == provider.IssueExternalConflict {
			sev = jsonout.SeverityWarn
		}
		add(sev, is.Code, is.Message, map[string]any{"file": is.File})
	}
	if all, err := provider.All(); err == nil {
		for _, p := range all {
			if p.Source == provider.SourceExternal && p.InsecureTransport {
				add(jsonout.SeverityWarn, "PROVIDER_HTTP_URL",
					p.ID+" 的清单里有 http:// 源（已由 "+provider.AllowHTTPEnv+"=1 放行，校验强度最高 weak）",
					map[string]any{"tool": p.ID, "file": p.Dir})
			}
		}
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
	// 3a. 根目录落在云同步盘：同步进程会和 pk 抢文件（锁失效、台账可能被回滚成旧版本）
	if env.OnSyncDir(a.paths.Root) {
		add(jsonout.SeverityWarn, "ROOT_ON_SYNC_DIR",
			fmt.Sprintf("%s 在云同步目录里：同步进程会与 pk 抢文件（锁可能失效、台账可能被回滚）", a.paths.Root),
			map[string]any{"root": a.paths.Root, "advice": "换到本机盘：PK_ROOT=~/.packnest，或 `pk --root <本机路径>`"})
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
		// 台账 schema 比本程序新：多半是用旧版 pk 打开了新版写过的数据（降级运行）
		if ledger.Schema > store.LedgerSchema {
			add(jsonout.SeverityErr, "LEDGER_SCHEMA_NEWER",
				fmt.Sprintf("台账 schema 是 %d，本程序只支持到 %d：先升级 pk 再操作", ledger.Schema, store.LedgerSchema),
				map[string]any{"schema": ledger.Schema, "supported": store.LedgerSchema,
					"advice": "brew upgrade packnest，或重跑安装脚本拿最新版"})
		}
	}

	// 5. 自身双名 shim
	selfTarget := map[string]string{}
	for _, name := range []string{shim.SelfName, shim.AliasName} {
		path, isShim, err := shim.Resolve(a.paths.Bin(), name, goos())
		if err == nil && path != "" {
			selfTarget[name] = path
		}
		switch {
		case err != nil:
			add(jsonout.SeverityWarn, "SHIM_BROKEN", fmt.Sprintf("%s: %v", name, err), map[string]any{"name": name})
		case path == "":
			add(jsonout.SeverityWarn, "SHIM_MISSING", fmt.Sprintf("缺少 %s 入口（`pk install` 会补齐）", name), map[string]any{"name": name})
		case !isShim:
			add(jsonout.SeverityInfo, "SHIM_PLAIN", fmt.Sprintf("%s 是普通文件，不是我们建的入口", name), map[string]any{"name": name, "path": path})
		}
	}

	// 5a. pk 与 packnest 指向不同二进制：双名入口被别的安装覆盖了一半
	if pkPath, ok := selfTarget[shim.SelfName]; ok {
		if aliasPath, ok2 := selfTarget[shim.AliasName]; ok2 && pkPath != aliasPath {
			add(jsonout.SeverityWarn, "SHIM_ALIAS_MISMATCH",
				fmt.Sprintf("pk 与 packnest 指向不同的文件：%s / %s", pkPath, aliasPath),
				map[string]any{"pk": pkPath, "packnest": aliasPath, "advice": "`pk install` 或 `pk uninstall --all` 后重装，让两个入口重指"})
		}
	}

	// 6. PATH 顺序（bin 必须在最前，方案 §6.7）
	segs := splitPath(os.Getenv("PATH"))
	want := a.paths.Bin()
	if len(segs) == 0 || segs[0] != want {
		detail := map[string]any{"want_first": want, "hint": env.PathHint(a.paths.PathSegments())}
		if slices.Contains(segs, want) {
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
	if ledger != nil && len(ledger.Versions("go")) > 0 {
		if msg := toolchain.CheckGoEnv(os.Getenv); msg != "" {
			add(jsonout.SeverityWarn, "GO_TOOLCHAIN_NOT_LOCAL", msg, map[string]any{"current": os.Getenv("GOTOOLCHAIN")})
		}
	}

	// 6b2. go install 的产物落在 GOBIN（默认 ~/go/bin），不在 pk 治理范围内：只提示，不改 GOBIN。
	if ledger != nil && len(ledger.Versions("go")) > 0 {
		bin := os.Getenv("GOBIN")
		if bin == "" {
			home, _ := os.UserHomeDir()
			bin = filepath.Join(home, "go", "bin")
		}
		if !slices.Contains(splitPath(os.Getenv("PATH")), bin) {
			add(jsonout.SeverityInfo, "GO_INSTALL_NOT_IN_PATH",
				fmt.Sprintf("`go install` 的命令会落到 %s，但它不在 PATH（我们不替你设 GOBIN）", bin),
				map[string]any{"dir": bin, "advice": "把该目录加进 PATH，或 `GOBIN=<某个已在 PATH 的目录> go install ...`"})
		}
	}

	// 6b3. pip 用户级安装（~/.local/lib/pythonX）不在治理范围：诚实说明，不接管 site-packages。
	if ledger != nil && len(ledger.Versions("python")) > 0 {
		add(jsonout.SeverityInfo, "PIP_USER_SITE_UNMANAGED",
			"pip 的用户级安装（~/.local/lib/pythonX）不归 pk 管；要隔离请用 venv（我们不改 site-packages）", nil)
	}

	// 6c. 全局包 L1/L2/L3 体检（方案 §8.5）：npm 前缀 + pnpm/bun 的全局目录（方案 B.5），只读不改。
	if *globals && ledger != nil {
		nodeVersion := a.cfg.Tools["node"]
		if nodeVersion == "" {
			nodeVersion = ledger.Active("node")
		}
		for _, m := range a.globalPrefixes(nodeVersion) {
			pkgs, err := env.ScanGlobals(m.prefix, goos())
			if err != nil {
				add(jsonout.SeverityWarn, "GLOBALS_SCAN_FAILED", err.Error(), map[string]any{"prefix": m.prefix, "manager": m.name})
			}
			for _, problem := range env.CheckGlobals(pkgs, nodeVersion) {
				problem.Detail["manager"] = m.name // 码沿用 GLOBAL_*，来源靠 detail.manager 区分
				add(jsonout.SeverityWarn, problem.Code, problem.Message, problem.Detail)
			}
			for _, problem := range env.CheckShadowing(pkgs, projectBins(a.cwd())) {
				problem.Detail["manager"] = m.name
				add(jsonout.SeverityInfo, problem.Code, problem.Message, problem.Detail)
			}
			if *probe { // --probe：真用 node 的 dlopen 试加载 .node（只加载二进制，不跑包代码）
				for _, problem := range env.ProbeNative(pkgs, a.nodeBin(ledger), 5*time.Second) {
					problem.Detail["manager"] = m.name
					add(jsonout.SeverityWarn, problem.Code, problem.Message, problem.Detail)
				}
			}
			if len(pkgs) == 0 && m.name == "npm" {
				add(jsonout.SeverityInfo, "GLOBALS_NONE", fmt.Sprintf("全局包目录里没有包（%s）", m.prefix), map[string]any{"prefix": m.prefix})
			}
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
	if *fix {
		out.Fixed = a.fixIssues(issues)
	}
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
		if *fix {
			if len(out.Fixed) == 0 {
				fmt.Fprintln(a.out, "没有可自动修的问题（其余项需要人工处理）")
			}
			for _, line := range out.Fixed {
				fmt.Fprintf(a.out, "  已修 %s\n", line)
			}
			fmt.Fprintln(a.out, "重跑 `pk doctor` 复核；新开窗口后 PATH 生效")
		}
	}
	if !out.OK {
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}

// fixIssues 只修能安全自愈的两类：PATH 托管块（复用 `pk env --write`）与 pk/packnest 入口。
// 其余问题（锁、镜像不可达、台账损坏等）绝不自动改。
func (a *app) fixIssues(issues []jsonout.Issue) []string {
	needPath, needShim, done := false, false, []string{}
	for _, is := range issues {
		switch is.Code {
		case "PATH_ORDER", "PATH_MISSING":
			needPath = true
		case "SHIM_MISSING", "SHIM_BROKEN", "SHIM_ALIAS_MISMATCH":
			needShim = true
		}
	}
	if needShim {
		if err := a.ensureSelfShims(); err != nil {
			done = append(done, "重建 pk/packnest 入口失败: "+err.Error())
		} else {
			done = append(done, "重建 pk/packnest 入口（指向当前二进制）")
		}
	}
	if needPath {
		// cmdEnv 会往 a.out 写人看的文案（--json 时还会写 JSON）：调到静默呈再调，避免污染 doctor 输出。
		out, j := a.out, a.json
		a.out, a.json = io.Discard, false
		code := a.cmdEnv([]string{"--write"})
		a.out, a.json = out, j
		if code == jsonout.ExitOK {
			done = append(done, "重写 shell 的 PATH 托管块（`pk env --write`，幂等）")
		}
	}
	return done
}

type globalPrefix struct {
	name   string
	prefix string
}

// nodeBin 返回当前生效 node 的真实路径（--probe 用它加载原生模块，走 shim 会多一层）。
func (a *app) nodeBin(ledger *store.Ledger) string {
	v := a.cfg.Tools["node"]
	if v == "" {
		v = ledger.Active("node")
	}
	if v == "" {
		return ""
	}
	name := "node"
	if goos() == "windows" {
		name = "node.exe"
	}
	return filepath.Join(a.paths.ToolVersion("node", v), "bin", name)
}

// globalPrefixes 列出要体检的全局前缀：npm（含 per-version）→ pnpm（global/<N>）→ bun；
// 三者都是 node_modules 布局，复用同一套 GLOBAL_* 码，用 detail.manager 标来源（方案 B.5）。
func (a *app) globalPrefixes(nodeVersion string) []globalPrefix {
	prefix := a.paths.Global()
	if a.cfg.GlobalMode == "per-version" && nodeVersion != "" {
		prefix = filepath.Join(a.paths.Root, "global", "node-"+nodeVersion)
	}
	out := []globalPrefix{{"npm", prefix}}
	// pnpm 的 store 版本号不固定，逐个扫，不猜
	if entries, err := os.ReadDir(filepath.Join(prefix, "pnpm", "global")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, globalPrefix{"pnpm", filepath.Join(prefix, "pnpm", "global", e.Name())})
			}
		}
	}
	// bun：<BUN_INSTALL>/install/global
	return append(out, globalPrefix{"bun", filepath.Join(prefix, "bun", "install", "global")})
}

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
