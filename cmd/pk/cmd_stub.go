package main

import (
	"flag"
	"fmt"

	"github.com/packnest/packnest/internal/jsonout"
)

// notImplemented 明确告知尚未实现的命令与里程碑，绝不伪装成功。
func (a *app) notImplemented(name, milestone string) int {
	fmt.Fprintf(a.errOut, "pk: %s 还没实现（计划 %s，见 ~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md）\n", name, milestone)
	return jsonout.ExitFail
}

// cmdInstall 安装工具（M1 实现 node+npm）。
func (a *app) cmdInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	_ = fs.Bool("frozen", false, "严格按 pk.lock，不做解析（CI 用）")
	_ = fs.Bool("insecure", false, "允许无校验安装（标记 unverified）")
	_ = fs.Int("jobs", 1, "并发下载数（默认 1：国内 CDN 分片常更慢）")
	if err := fs.Parse(args); err != nil {
		return a.failUsage(err)
	}
	return a.notImplemented("install", "M1")
}

// cmdUninstall 台账驱动卸载（M4）。
func (a *app) cmdUninstall(args []string) int {
	if len(args) == 0 {
		return a.failUsage(fmt.Errorf("用法: pk uninstall <工具>@<版本> [--force]"))
	}
	return a.notImplemented("uninstall", "M4")
}

// cmdUse 切换全局默认版本（M4）。
func (a *app) cmdUse(args []string) int {
	if len(args) == 0 {
		return a.failUsage(fmt.Errorf("用法: pk use <工具>@<版本>"))
	}
	return a.notImplemented("use", "M4")
}

// cmdGC 回收零引用版本与过期缓存（M4）。
func (a *app) cmdGC(args []string) int {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	_ = fs.Bool("dry-run", false, "只报告不删除")
	_ = fs.String("cache-max", "", "覆盖产物缓存上限（如 20G）")
	if err := fs.Parse(args); err != nil {
		return a.failUsage(err)
	}
	return a.notImplemented("gc", "M4")
}
