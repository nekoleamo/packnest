// Command pk 是 packnest 的 CLI 入口。
//
// 命令面固定 9 个（方案 §4.4）：install / uninstall / use / list / which /
// mirror / doctor / env / gc。`pk` 与 `packnest` 是同一二进制的两个名字，
// argv[0] 不改变任何行为。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/store"
)

// Version 是 CLI 版本（渠道发布号由构建注入）。
var Version = "0.7.1"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type app struct {
	out     io.Writer
	errOut  io.Writer
	json    bool
	offline bool
	yes     bool
	root    string
	paths   paths.Paths
	cfg     *store.Config
	cfgErr  error
	client  *fetch.Client
	now     func() time.Time
	ctx     context.Context
	// refreshIndex 为 true 时忽略索引缓存（list --available --refresh）
	refreshIndex bool
}

// context 返回命令上下文（Ctrl-C 会取消，M1 的下载用它做中断清理）。
func (a *app) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// run 是可测试的入口：返回退出码（0 成功 / 1 通用 / 2 用法 / 3 网络 / 4 校验 / 5 锁占用）。
func run(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &app{out: out, errOut: errOut, now: time.Now, ctx: ctx}

	rest, err := a.parseGlobals(args)
	if err != nil {
		return a.failUsage(err)
	}

	if a.root != "" {
		a.paths, err = paths.New(a.root)
	} else {
		a.paths, err = paths.Resolve()
	}
	if err != nil {
		return a.fail(err)
	}

	a.client = fetch.New()
	a.client.Offline = a.offline

	// 配置坏了不让所有命令瘫痪：doctor 要把问题报出来，其余命令直接失败。
	a.cfg, a.cfgErr = store.LoadConfig(a.paths.Config())
	if a.cfg == nil {
		a.cfg = store.DefaultConfig()
	}

	if len(rest) == 0 {
		printUsage(out)
		return jsonout.ExitOK
	}

	cmd, cmdArgs := rest[0], rest[1:]
	if a.cfgErr != nil && cmd != "doctor" && cmd != "help" && cmd != "-h" && cmd != "--help" && cmd != "version" && cmd != "--version" {
		return a.fail(fmt.Errorf("配置不可用，先修好再操作: %w", a.cfgErr))
	}
	switch cmd {
	case "help", "-h", "--help":
		if len(cmdArgs) > 0 && printCommandHelp(out, cmdArgs[0]) {
			return jsonout.ExitOK
		}
		printUsage(out)
		helpSummary(out)
		return jsonout.ExitOK
	case "version", "--version":
		return a.cmdVersion()
	case "install":
		return a.cmdInstall(cmdArgs)
	case "uninstall":
		return a.cmdUninstall(cmdArgs)
	case "use":
		return a.cmdUse(cmdArgs)
	case "list":
		return a.cmdList(cmdArgs)
	case "which":
		return a.cmdWhich(cmdArgs)
	case "mirror":
		return a.cmdMirror(cmdArgs)
	case "doctor":
		return a.cmdDoctor(cmdArgs)
	case "env":
		return a.cmdEnv(cmdArgs)
	case "gc":
		return a.cmdGC(cmdArgs)
	}
	return a.failUsage(fmt.Errorf("未知命令 %q", cmd))
}

// parseGlobals 取出全局开关（可出现在任意位置），其余原样返回。
func (a *app) parseGlobals(args []string) ([]string, error) {
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			rest = append(rest, args[i:]...)
			return rest, nil
		case arg == "--json":
			a.json = true
		case arg == "--offline":
			a.offline = true
		case arg == "--yes" || arg == "-y":
			a.yes = true
		case arg == "--root":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--root 需要路径参数")
			}
			i++
			a.root = args[i]
		case strings.HasPrefix(arg, "--root="):
			a.root = strings.TrimPrefix(arg, "--root=")
		case arg == "--version" || arg == "-v":
			rest = append(rest, "version")
		case (arg == "--help" || arg == "-h") && len(rest) == 0:
			rest = append(rest, "help")
		default:
			rest = append(rest, arg)
		}
	}
	return rest, nil
}

func (a *app) cmdVersion() int {
	line := fmt.Sprintf("packnest v%s (%s, %s/%s)", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if a.json {
		return a.emit(struct {
			Schema  int    `json:"schema"`
			Name    string `json:"name"`
			Version string `json:"version"`
			Go      string `json:"go"`
			OS      string `json:"os"`
			Arch    string `json:"arch"`
		}{jsonout.SchemaVersion, "packnest", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH})
	}
	fmt.Fprintln(a.out, line)
	return jsonout.ExitOK
}

func (a *app) fail(err error) int {
	fmt.Fprintf(a.errOut, "pk: %v\n", err)
	return jsonout.ExitFail
}

// usageFor 给子命令设 Usage（`pk install --help` 输出详细说明）。
func (a *app) usageFor(fs *flag.FlagSet, name string) {
	fs.Usage = func() {
		if !printCommandHelp(a.errOut, name) {
			fmt.Fprintf(a.errOut, "用法见 `pk help %s`\n", name)
		}
	}
}

func (a *app) failUsage(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return jsonout.ExitOK // 子命令的 --help 是正常请求
	}
	fmt.Fprintf(a.errOut, "pk: %v\n", err)
	fmt.Fprintln(a.errOut, "运行 `pk help` 查看用法")
	return jsonout.ExitUsage
}

func (a *app) emit(v any) int {
	if err := jsonout.WriteJSON(a.out, v); err != nil {
		return a.fail(err)
	}
	return jsonout.ExitOK
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `packnest · 国内网络开箱可用的运行时安装器

用法: pk <命令> [参数] [--json] [--offline] [--yes]

命令:
  install [工具@版本]     安装（无参数按 pk.toml；7 个工具全支持）
  uninstall 工具@版本     台账驱动卸载（被引用需 --force）
  use 工具@版本           切换全局默认版本                  [M4]
  list [--available]      已装/可装工具
  which <命令>            解析 shim 到真实二进制
  mirror list|probe|use   镜像档：列出 / 探测 / 切换
  doctor                  环境体检（PATH/shim/台账/镜像）
  env [--shell S] [--write] 输出 shell 环境片段
  gc [--dry-run] [--cache-max]  回收零引用版本与超限缓存

全局开关:
  --json      机器可读输出（GUI 与脚本的唯一入口）
  --offline   禁止一切网络访问
  --yes       不交互确认
  --root DIR  覆盖 PK_ROOT（调试用）`)
}
