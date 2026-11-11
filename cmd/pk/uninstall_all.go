package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/store"
)

// uninstallAll 清掉 pk 装过的一切（运行时、shim、PATH/rc 托管块），并按渠道告诉用户怎么删掉 pk 自己。
func (a *app) uninstallAll(inst *install.Installer, purge, keepCache, self bool) int {
	ledger := inst.Ledger()
	targets := make([]string, 0, len(ledger.Installs))
	for _, in := range ledger.Installs {
		targets = append(targets, in.Tool+"@"+in.Version)
	}
	sort.Strings(targets)

	exe, _ := os.Executable()
	ch := install.DetectInstallChannel(exe, goos())
	// 安装脚本会记下"装到哪个目录"，比按路径猜更准（自托管/自定义 --dir 都覆盖）
	info := store.LoadInstallInfo(a.paths.InstallInfo())
	if info != nil && info.Dir != "" && (ch.Name == "manual" || strings.HasPrefix(ch.Name, "script")) {
		name := ch.Name
		if info.Channel != "" && name == "manual" {
			name = info.Channel
		}
		ch = install.InstallChannel{Name: name, Hint: install.SelfRemoveHint(info.Dir, goos()), Dir: info.Dir}
	}

	// 先逐条列出会删掉什么，再二次确认（--yes 跳过；非交互没有 --yes 一律不动）
	if !a.json {
		fmt.Fprintln(a.out, "将要删除：")
		if len(targets) == 0 {
			fmt.Fprintln(a.out, "  · 没有已装的运行时")
		} else {
			fmt.Fprintf(a.out, "  · %d 个已装运行时：\n", len(targets))
			for _, t := range targets {
				mark := " "
				if strings.Contains(t, "@") {
					parts := strings.SplitN(t, "@", 2)
					if a.cfg.Tools[parts[0]] == parts[1] {
						mark = "*"
					}
				}
				fmt.Fprintf(a.out, "      %s %s\n", mark, t)
			}
			fmt.Fprintln(a.out, "    （* 是当前全局默认）")
		}
		fmt.Fprintf(a.out, "  · shim 目录与 PATH/rc 里的 packnest 托管块（%s）\n", a.paths.Bin())
		if purge {
			if keepCache {
				fmt.Fprintf(a.out, "  · %s 下除 cache/ 之外的全部内容\n", a.paths.Root)
			} else {
				fmt.Fprintf(a.out, "  · 整个 %s（含缓存）\n", a.paths.Root)
			}
		} else {
			fmt.Fprintf(a.out, "  · 保留 %s（缓存/台账仍在；要连根删加 --purge）\n", a.paths.Root)
		}
		fmt.Fprintf(a.out, "\npk 二进制本身（%s 渠道）：%s\n", ch.Name, ch.Hint)
	}
	if !a.confirm(fmt.Sprintf("确认删除以上 %d 个运行时及其 shim/PATH 项？", len(targets))) {
		return jsonout.ExitFail
	}

	removed := make([]string, 0, len(targets))
	failures := make([]string, 0)
	for _, t := range targets {
		tool, version, err := splitToolVersion(t)
		if err != nil {
			continue
		}
		// --all 是显式全清：引用关系不再拦路
		if err := inst.Uninstall(tool, version, true); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", t, err))
			continue
		}
		removed = append(removed, t)
	}

	// 自入口（pk / packnest）在 bin 里，留着会指向一个可能马上被删的二进制
	for _, name := range []string{shim.SelfName, shim.AliasName} {
		p := filepath.Join(a.paths.Bin(), name)
		if goos() == "windows" {
			p = shim.CmdPath(a.paths.Bin(), name)
			_ = os.Remove(p)
			_ = os.Remove(filepath.Join(a.paths.Bin(), name+".exe"))
			continue
		}
		_ = os.Remove(p)
	}

	// PATH / rc 里的托管块：posix 的 rc、Windows 的 profile（两份）+ 注册表用户 Path 的四段
	unset := a.cmdEnv([]string{"--unset"})
	// install.ps1 另外把「安装目录」写进了用户 Path，这一段常被漏掉（删了目录还留个死 PATH 项）
	if goos() == "windows" && info != nil && info.Dir != "" {
		if changed, _, _, err := env.RemoveUserPath([]string{info.Dir}); err != nil {
			failures = append(failures, fmt.Sprintf("从用户 Path 移除 %s 失败: %v", info.Dir, err))
		} else if changed && !a.json {
			fmt.Fprintf(a.out, "已从用户 Path 移除安装目录 %s\n", info.Dir)
		}
	}
	if unset != jsonout.ExitOK && unset != jsonout.ExitFail {
		failures = append(failures, fmt.Sprintf("清理 PATH/rc 托管块失败（退出码 %d）", unset))
	}

	selfHandled := false
	if self {
		if ok, msg := removeSelf(exe, goos()); ok {
			selfHandled = true
			if !a.json {
				if goos() == "windows" {
					fmt.Fprintln(a.out, "已安排删除 pk 自己："+msg)
				} else {
					fmt.Fprintln(a.out, "已删除 pk 自己："+msg)
				}
			}
		} else if !a.json {
			fmt.Fprintln(a.out, "pk 二进制没法自己删（"+msg+"），请执行：")
			fmt.Fprintln(a.out, "  "+ch.Hint)
		}
	}

	if purge {
		root := a.paths.Root
		if keepCache {
			entries, _ := os.ReadDir(root)
			for _, e := range entries {
				if e.Name() == "cache" {
					continue
				}
				if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
					failures = append(failures, fmt.Sprintf("删除 %s 失败: %v", e.Name(), err))
				}
			}
			if !a.json {
				fmt.Fprintf(a.out, "已清空 %s（保留了 cache/）\n", root)
			}
		} else if err := os.RemoveAll(root); err != nil {
			failures = append(failures, fmt.Sprintf("删除 %s 失败: %v", root, err))
		} else if !a.json {
			fmt.Fprintf(a.out, "已删除整个 %s\n", root)
		}
	} else if err := inst.Finalize(); err != nil {
		failures = append(failures, err.Error())
	}

	if a.json {
		out := struct {
			Schema   int      `json:"schema"`
			Removed  []string `json:"removed"`
			Purged   bool     `json:"purged"`
			Root     string   `json:"root"`
			Channel  string   `json:"channel"`
			SelfHint string   `json:"self_hint"`
			Failures []string `json:"failures,omitempty"`
		}{jsonout.SchemaVersion, removed, purge, a.paths.Root, ch.Name, ch.Hint, failures}
		if err := jsonout.WriteJSON(a.out, out); err != nil {
			return a.fail(err)
		}
		if len(failures) > 0 {
			return jsonout.ExitFail
		}
		return jsonout.ExitOK
	}

	if len(removed) > 0 {
		fmt.Fprintf(a.out, "\n已卸载 %d 个运行时：%s\n", len(removed), strings.Join(removed, ", "))
	} else {
		fmt.Fprintln(a.out, "\n没有需要卸载的运行时。")
	}
	fmt.Fprintln(a.out, "已清理：shim 目录、shell rc 里的托管块、Windows 用户 Path 里的 packnest 目录")
	if selfHandled {
		if goos() == "windows" {
			fmt.Fprintln(a.out, "\npk 已安排删除：关掉所有窗口后约 3 秒，安装目录里的 pk.exe / packnest.exe 会被删掉（目录空了再一并移除）。")
		} else {
			fmt.Fprintln(a.out, "\npk 二进制已经删掉了（同目录下的 pk / packnest 也一并清理）。")
		}
	} else {
		fmt.Fprintf(a.out, "\n最后一步 —— 删掉 pk 自己（检测到 %s 渠道）：\n  %s\n", ch.Name, ch.Hint)
	}
	if len(failures) > 0 {
		fmt.Fprintln(a.errOut, "\n以下没做成，需手动处理：")
		for _, f := range failures {
			fmt.Fprintf(a.errOut, "  · %s\n", f)
		}
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}

// uninstallTool 删掉某个工具的全部已装版本（`pk uninstall go`）。
func (a *app) uninstallTool(inst *install.Installer, tool string, force bool) int {
	if tool == "npm" {
		// npm 不是独立工具，它跟着 node 走
		if _, err := provider.Get("npm"); err == nil {
			fmt.Fprintln(a.errOut, "pk: npm 是 node 的伴生包，没有独立版本可卸；删 node 时它一起走。")
			fmt.Fprintln(a.errOut, "    想换 npm 版本：pk install node@<版本>（会按 node 重新配对）或 pk use node@<已装版本>")
			return jsonout.ExitUsage
		}
	}
	// 先列出会删掉什么，再二次确认（--yes 跳过；非交互环境没有 --yes 一律不动）
	versions := make([]string, 0, 4)
	active := inst.ActiveVersion(tool)
	for _, in := range inst.Ledger().Installs {
		if in.Tool == tool {
			versions = append(versions, in.Version)
		}
	}
	if len(versions) == 0 {
		return a.fail(fmt.Errorf("%s 没有已装版本（`pk list` 看已装工具）", tool))
	}
	sort.Strings(versions)
	if !a.json {
		fmt.Fprintf(a.out, "将要从 %s 卸载 %s 的全部 %d 个版本：\n", inst.Root(), tool, len(versions))
		for _, v := range versions {
			mark := " "
			if v == active {
				mark = "*"
			}
			fmt.Fprintf(a.out, "  %s %s@%s\n", mark, tool, v)
		}
		fmt.Fprintln(a.out, "（缓存保留，离线可重装；* 是当前全局默认）")
	}
	if !a.confirm(fmt.Sprintf("确认卸载 %s 的这 %d 个版本？", tool, len(versions))) {
		return jsonout.ExitFail
	}

	removed, err := inst.UninstallTool(tool, force)
	if ferr := inst.Finalize(); ferr != nil {
		return a.fail(ferr)
	}
	if a.json {
		out := struct {
			Schema  int      `json:"schema"`
			Tool    string   `json:"tool"`
			Removed []string `json:"removed"`
			Err     string   `json:"error,omitempty"`
		}{jsonout.SchemaVersion, tool, removed, ""}
		if err != nil {
			out.Err = err.Error()
		}
		if werr := jsonout.WriteJSON(a.out, out); werr != nil {
			return a.fail(werr)
		}
		if err != nil {
			return jsonout.ExitFail
		}
		return jsonout.ExitOK
	}
	if len(removed) > 0 {
		fmt.Fprintf(a.out, "已卸载 %s 的 %d 个版本：%s（缓存保留，离线可重装）\n",
			tool, len(removed), strings.Join(removed, ", "))
	}
	if err != nil {
		return a.fail(err)
	}
	if len(removed) == 0 {
		return a.fail(fmt.Errorf("%s 没有已装版本（`pk list` 看已装工具）", tool))
	}
	return jsonout.ExitOK
}

// confirm 二次确认：--yes 直接通过；交互终端读一行 y/yes；
// 非交互（管道、CI、测试）一律当"不确认"，避免脚本误删。
func (a *app) confirm(prompt string) bool {
	if a.yes {
		return true
	}
	if a.json {
		fmt.Fprintln(a.errOut, "pk: --json 下不做交互确认，需加 --yes 才会执行")
		return false
	}
	if !stdinIsTerminal() {
		fmt.Fprintln(a.errOut, "pk: 非交互环境不会执行破坏性操作，确认无误后加 --yes 重跑")
		return false
	}
	fmt.Fprintf(a.out, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	fmt.Fprintln(a.out, "已取消。")
	return false
}
