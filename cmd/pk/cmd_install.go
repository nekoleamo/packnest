package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/verify"
)

// exitCodeFor 把错误映射成固定退出码（方案 §10）：0/1/2/3/4/5。
func exitCodeFor(err error) int {
	var statusErr *fetch.StatusError
	switch {
	case err == nil:
		return jsonout.ExitOK
	case errors.Is(err, store.ErrLocked):
		return jsonout.ExitLocked
	case errors.Is(err, verify.ErrChecksum):
		return jsonout.ExitChecksum
	case errors.Is(err, fetch.ErrOffline), errors.Is(err, install.ErrNetwork), errors.As(err, &statusErr):
		return jsonout.ExitNetwork
	}
	return jsonout.ExitFail
}

// cmdInstall 安装工具（M1：node + 伴生 npm）。
func (a *app) cmdInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "install")
	frozen := fs.Bool("frozen", false, "严格按 pk.lock，不做解析（M4）")
	insecure := fs.Bool("insecure", false, "允许无校验安装（标记 unverified）")
	jobs := fs.Int("jobs", 1, "并发下载数（M4；默认 1，国内 CDN 分片常更慢）")
	force := fs.Bool("force", false, "重装已存在的版本")
	quiet := fs.Bool("quiet", false, "只报错，不打印进度")
	dryRun := fs.Bool("dry-run", false, "只解析并打印计划（版本/URL/校验方式），不下载")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if *jobs != 1 {
		fmt.Fprintln(a.errOut, "pk: 当前版本单连接下载（--jobs 预留给后续版本；国内 CDN 分片往往更慢）")
	}

	project, err := install.LoadProject(a.cwd())
	if err != nil {
		return a.fail(err)
	}
	targets, err := installTargets(fs.Args(), project)
	if err != nil {
		return a.failUsage(err)
	}

	var lockFile *install.Lock
	if *frozen {
		lockFile, err = install.LoadLock(a.cwd())
		if err != nil {
			return a.fail(err)
		}
		if err := install.CheckFrozen(project, lockFile); err != nil {
			fmt.Fprintf(a.errOut, "pk: %v\n", err)
			return jsonout.ExitFail
		}
	}

	// 进度条：TTY 用 \r 刷新，非 TTY 每 25% 一行；--quiet 或 --json 时不画。
	var bar *progressBar
	if !*quiet && !a.json {
		bar = newProgress(a.out, "下载")
		a.client.Progress = bar.Update
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			fmt.Fprintln(a.errOut, "pk: 另一个 pk 正在运行（退出码 5）")
			return jsonout.ExitLocked
		}
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths:          a.paths,
		Client:         a.client,
		Cfg:            a.cfg,
		Profile:        a.cfg.Mirror,
		Offline:        a.offline,
		Insecure:       *insecure,
		Force:          *force,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		Project:        project,
		Frozen:         *frozen,
		DryRun:         *dryRun,
		Lock:           lockFile,
		Events:         a.eventWriterFor(*quiet),
		LookupProvider: nil,
	})
	if err != nil {
		return a.failWith(err)
	}
	// 写操作前先清扫崩溃残留（断电/强杀留下的 .tmp/.old/.part）。
	if swept, err := inst.SweepStale(); err == nil && len(swept.Removed) > 0 && !a.json {
		fmt.Fprintf(a.out, "清扫残留 %d 项（约 %.1f MB）\n", len(swept.Removed), float64(swept.Bytes)/(1<<20))
	}

	var failed bool
	for _, t := range targets {
		res, err := inst.Install(a.context(), t.tool, t.spec)
		if err != nil {
			if bar != nil {
				bar.Done()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintln(a.errOut, "pk: 已取消（缓存已保留，下次会续传/复用）")
				_ = inst.Finalize()
				return jsonout.ExitFail
			}
			fmt.Fprintf(a.errOut, "pk: %s@%s 安装失败: %v\n", t.tool, t.spec, err)
			if !a.json {
				fmt.Fprintln(a.errOut, "下一步：pk mirror probe（看哪个源通）· pk doctor（看环境）· pk install <工具> --insecure（确认产物可信后）")
			}
			failed = true
			code := exitCodeFor(err)
			if code != jsonout.ExitFail {
				// 网络/校验/锁这类错误直接返回对应退出码（全自动测试靠它断言）。
				_ = inst.Finalize()
				return code
			}
			continue
		}
		if bar != nil {
			bar.Done()
		}
		if !a.json && !*quiet {
			a.printInstallResult(res)
		}
	}
	if err := inst.Finalize(); err != nil {
		return a.fail(err)
	}
	if err := a.ensureSelfShims(); err != nil {
		return a.fail(err)
	}
	// 装完第一件要告诉用户的事：bin 目录在不在 PATH（否则 node/npm 这些命令还用不了）
	if !a.json && !*quiet && !inPath(a.paths.Bin()) {
		fmt.Fprintf(a.out, "\n提示：%s 不在 PATH，node/npm 等命令还不能直接用。\n", a.paths.Bin())
		fmt.Fprintln(a.out, "      运行一次 `pk env --write` 即可（写进 rc 的托管块，块外内容不动），或临时：")
		fmt.Fprintf(a.out, "      eval \"$(pk env --shell %s)\"\n", detectShell())
	}
	if failed {
		return jsonout.ExitFail
	}
	// 项目里安装成功就刷新 pk.lock（--frozen 不动锁）。
	if !*frozen && len(project) > 0 {
		active := map[string]string{}
		for tool, version := range a.cfg.Tools {
			active[tool] = version
		}
		lock, err := install.BuildProjectLock(project, inst.Ledger(), active)
		if err != nil {
			return a.fail(err)
		}
		if err := lock.Save(a.cwd()); err != nil {
			return a.fail(err)
		}
		if !a.json {
			fmt.Fprintf(a.out, "已更新 %s（%s）\n", install.LockFileName, lock.LockTools())
		}
	}
	return jsonout.ExitOK
}

// cmdUninstall 台账驱动卸载。
func (a *app) cmdUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	a.usageFor(fs, "uninstall")
	force := fs.Bool("force", false, "仍被引用也删除")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 {
		return a.failUsage(fmt.Errorf("用法: pk uninstall <工具>@<版本>"))
	}
	tool, version, err := splitToolVersion(fs.Arg(0))
	if err != nil {
		return a.failUsage(err)
	}

	unlock, err := store.Lock(a.paths.Lock())
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			fmt.Fprintln(a.errOut, "pk: 另一个 pk 正在运行（退出码 5）")
			return jsonout.ExitLocked
		}
		return a.fail(err)
	}
	defer unlock()

	inst, err := install.New(install.Options{
		Paths:   a.paths,
		Client:  a.client,
		Cfg:     a.cfg,
		Profile: a.cfg.Mirror,
		Offline: a.offline,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		Events:  a.eventWriter(),
	})
	if err != nil {
		return a.failWith(err)
	}
	if err := inst.Uninstall(tool, version, *force); err != nil {
		return a.fail(err)
	}
	if err := inst.Finalize(); err != nil {
		return a.fail(err)
	}
	if !a.json {
		fmt.Fprintf(a.out, "已卸载 %s@%s（缓存保留，离线可重装）\n", tool, version)
	}
	return jsonout.ExitOK
}

type installTarget struct{ tool, spec string }

// installTargets 决定装什么：显式参数 > pk.toml。
func installTargets(args []string, project map[string]string) ([]installTarget, error) {
	if len(args) > 0 {
		out := make([]installTarget, 0, len(args))
		for _, arg := range args {
			tool, specStr := arg, ""
			if idx := strings.LastIndex(arg, "@"); idx > 0 {
				tool, specStr = arg[:idx], arg[idx+1:]
			}
			if tool == "" {
				return nil, fmt.Errorf("安装目标 %q 非法（应为 工具 或 工具@版本）", arg)
			}
			out = append(out, installTarget{tool: tool, spec: specStr})
		}
		return out, nil
	}
	if len(project) == 0 {
		return nil, fmt.Errorf("没有 pk.toml，也没有指定工具；用 `pk install node@22` 或先写 pk.toml")
	}
	keys := make([]string, 0, len(project))
	for k := range project {
		if k == "npm" {
			continue // npm 是 node 的伴生包，不单独装
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]installTarget, 0, len(keys))
	for _, k := range keys {
		out = append(out, installTarget{tool: k, spec: project[k]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pk.toml 里没有可安装的工具")
	}
	return out, nil
}

func splitToolVersion(arg string) (string, string, error) {
	idx := strings.LastIndex(arg, "@")
	if idx <= 0 || idx == len(arg)-1 {
		return "", "", fmt.Errorf("需要精确版本: pk uninstall <工具>@<版本>（得到 %q）", arg)
	}
	return arg[:idx], arg[idx+1:], nil
}

// inPath 判断目录是否已在 PATH 里。
func inPath(dir string) bool {
	for _, seg := range splitPath(os.Getenv("PATH")) {
		if seg == dir {
			return true
		}
	}
	return false
}

// cwd 返回当前目录（调试/测试可用 PK_CWD 覆盖）。
func (a *app) cwd() string {
	if v := os.Getenv("PK_CWD"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

// eventWriter 返回事件输出函数（默认人类可读）。
func (a *app) eventWriter() func(jsonout.Event) { return a.eventWriterFor(false) }

// eventWriterFor 返回事件输出函数：--json 时写 NDJSON，--quiet 时静默，否则写人类可读进度。
func (a *app) eventWriterFor(quiet bool) func(jsonout.Event) {
	if quiet && !a.json {
		return func(jsonout.Event) {}
	}
	if a.json {
		return func(ev jsonout.Event) { _ = jsonout.WriteEvent(a.out, ev) }
	}
	return func(ev jsonout.Event) {
		switch ev.Ev {
		case "resolve":
			fmt.Fprintf(a.out, "解析 %s@%s\n", ev.Tool, ev.Version)
		case "download":
			fmt.Fprintf(a.out, "下载 %s@%s：%.1f MB\n", ev.Tool, ev.Version, float64(ev.Bytes)/(1<<20))
		case "cache":
			fmt.Fprintf(a.out, "缓存命中 %s@%s（%.1f MB，0 网络）\n", ev.Tool, ev.Version, float64(ev.Bytes)/(1<<20))
		case "verify":
			fmt.Fprintf(a.out, "校验通过 %s@%s（%s）\n", ev.Tool, ev.Version, ev.Mode)
		case "companion":
			fmt.Fprintf(a.out, "伴生包 %s@%s（%s）\n", ev.Tool, ev.Version, ev.Msg)
		case "mirror":
			fmt.Fprintf(a.out, "镜像判定 %s: %s\n", ev.Tool, ev.Msg)
		case "plan":
			fmt.Fprintf(a.out, "  产物 %s\n  校验 %s\n", ev.Source, ev.Mode)
		}
	}
}

func (a *app) printInstallResult(res install.Result) {
	if res.Planned {
		fmt.Fprintf(a.out, "计划：%s@%s（%s）\n", res.Tool, res.Version, res.PlanFormat)
		return
	}
	if res.AlreadyInstalled {
		fmt.Fprintf(a.out, "已是最新：%s@%s\n", res.Tool, res.Version)
		return
	}
	line := fmt.Sprintf("装好 %s@%s → %s", res.Tool, res.Version, res.Path)
	if res.NPMVersion != "" {
		line += fmt.Sprintf(" (npm %s", res.NPMVersion)
		if res.NPMReplaced != "" && res.NPMReplaced != res.NPMVersion {
			line += fmt.Sprintf(" ← 自带 %s", res.NPMReplaced)
		}
		line += ")"
	}
	if res.Verified == verify.VerifiedUnverified {
		line += " [未校验]"
	} else if res.Verified == verify.VerifiedWeak {
		line += " [校验: weak]"
	}
	fmt.Fprintln(a.out, line)
}

// ensureSelfShims 保证 bin/ 里有 pk 与 packnest 两个入口（同一文件）。
func (a *app) ensureSelfShims() error {
	self, err := os.Executable()
	if err != nil {
		return nil // 拿不到自身路径不算致命
	}
	if resolved, err := os.Readlink(self); err == nil {
		self = resolved
	}
	return shim.Self(a.paths.Bin(), self, runtime.GOOS)
}

// failWith 打印错误并按类型返回退出码。
func (a *app) failWith(err error) int {
	fmt.Fprintf(a.errOut, "pk: %v\n", err)
	return exitCodeFor(err)
}

var _ io.Writer
