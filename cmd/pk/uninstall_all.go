package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/env"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/store"
)

// uninstallAll 清掉 pk 装过的一切（运行时、shim、PATH/rc 托管块），并按渠道告诉用户怎么删掉 pk 自己。
func (a *app) uninstallAll(inst *install.Installer, purge, keepCache, self bool) int {
	plan := inst.PlanUninstallAll(install.UninstallAllOptions{Purge: purge, KeepCache: keepCache})
	exe, _ := os.Executable()
	ch := install.DetectInstallChannel(exe, goos())
	// 安装脚本会记下tr("装到哪个目录", "install directory")，比按路径猜更准（自托管/自定义 --dir 都覆盖）
	info := store.LoadInstallInfo(a.paths.InstallInfo())
	if info != nil && info.Dir != "" && (ch.Name == "manual" || strings.HasPrefix(ch.Name, "script")) {
		name := ch.Name
		if info.Channel != "" && name == "manual" {
			name = info.Channel
		}
		ch = install.InstallChannel{Name: name, Hint: install.SelfRemoveHint(info.Dir, goos()), Dir: info.Dir}
	}

	// 先把tr("会删什么", "what will be deleted")列清楚，再二次确认（--yes 跳过；非交互且没有 --yes 一律不动）
	if !a.json {
		fmt.Fprintln(a.out, tr("将要删除：", "about to delete:"))
		if len(plan.Tools) == 0 {
			fmt.Fprintln(a.out, tr("  · 没有已装的运行时", "  · no runtimes installed"))
		} else {
			fmt.Fprintf(a.out, tr("  · %d 个已装运行时：\n", "  · %d installed runtime(s):\n"), len(plan.Tools))
			for _, t := range plan.Tools {
				mark := " "
				parts := strings.SplitN(t, "@", 2)
				if len(parts) == 2 && plan.Active[parts[0]] == parts[1] {
					mark = "*"
				}
				fmt.Fprintf(a.out, "      %s %s\n", mark, t)
			}
			fmt.Fprintln(a.out, tr("    （* 是当前全局默认）", "    (* marks the current global default)"))
		}
		fmt.Fprintf(a.out, tr("  · shim 目录与 PATH/rc 里的 packnest 托管块（%s）\n", "  · shim dir and packnest managed blocks in PATH/rc (%s)\n"), plan.BinDir)
		if purge {
			if keepCache {
				fmt.Fprintf(a.out, tr("  · %s 下除 cache/ 之外的全部内容\n", "  · everything under %s except cache/\n"), plan.Root)
			} else {
				fmt.Fprintf(a.out, tr("  · 整个 %s（含缓存）\n", "  · the whole %s (including cache)\n"), plan.Root)
			}
			// PK_CACHE 指到 PK_ROOT 外面时，--purge 不动它：那通常是 CI 挂的共享卷
			if plan.Cache != "" {
				fmt.Fprintf(a.out, tr("  · 缓存目录 %s 由 PK_CACHE 单独指定，--purge 不动它（要清就自己删）\n", "  · cache dir %s is set by PK_CACHE; --purge leaves it alone (delete it yourself if you want)\n"), plan.Cache)
			}
		} else {
			fmt.Fprintf(a.out, tr("  · 保留 %s（缓存/台账仍在；要连根删加 --purge）\n", "  · keep %s (cache and ledger stay; add --purge to remove it all)\n"), plan.Root)
		}
		fmt.Fprintf(a.out, tr("\npk 二进制本身（%s 渠道）：%s\n", "\nthe pk binary itself (installed via %s): %s\n"), ch.Name, ch.Hint)
	}
	if !a.confirm(fmt.Sprintf(tr("确认删除以上 %d 个运行时及其 shim/PATH 项？", "delete these %d runtime(s) plus their shims/PATH entries?"), len(plan.Tools))) {
		return jsonout.ExitFail
	}

	// 真正的删除（运行时 + 自入口 shim + 可选连根删）由 install 层负责，cmd 只管输出
	res := inst.ApplyUninstallAll(plan)
	removed, failures := res.Removed, res.Failures

	// PATH / rc 里的托管块：posix 的 rc、Windows 的 profile（两份）+ 注册表用户 Path 的四段
	unset := a.cmdEnv([]string{"--unset"})
	// install.ps1 另外把「安装目录」写进了用户 Path，这一段常被漏掉（删了目录还留个死 PATH 项）
	if goos() == "windows" && info != nil && info.Dir != "" {
		if changed, _, _, err := env.RemoveUserPath([]string{info.Dir}); err != nil {
			failures = append(failures, fmt.Sprintf(tr("从用户 Path 移除 %s 失败: %v", "failed to remove %s from the user Path: %v"), info.Dir, err))
		} else if changed && !a.json {
			fmt.Fprintf(a.out, tr("已从用户 Path 移除安装目录 %s\n", "removed install dir %s from the user Path\n"), info.Dir)
		}
	}
	if unset != jsonout.ExitOK && unset != jsonout.ExitFail {
		failures = append(failures, fmt.Sprintf(tr("清理 PATH/rc 托管块失败（退出码 %d）", "failed to clean the PATH/rc managed block (exit code %d)"), unset))
	}

	selfHandled := false
	if self {
		if ok, msg := removeSelf(exe, goos()); ok {
			selfHandled = true
			if !a.json {
				if goos() == "windows" {
					fmt.Fprintln(a.out, tr("已安排删除 pk 自己：", "scheduled removal of pk itself:")+msg)
				} else {
					fmt.Fprintln(a.out, tr("已删除 pk 自己：", "removed pk itself:")+msg)
				}
			}
		} else if !a.json {
			fmt.Fprintln(a.out, tr("pk 二进制没法自己删（", "the pk binary cannot delete itself (")+msg+tr("），请执行：", "), run:"))
			fmt.Fprintln(a.out, "  "+ch.Hint)
		}
	}
	if purge && !a.json {
		if keepCache {
			fmt.Fprintf(a.out, tr("已清空 %s（保留了 cache/）\n", "cleared %s (cache/ kept)\n"), plan.Root)
		} else if res.Purged {
			fmt.Fprintf(a.out, tr("已删除整个 %s\n", "deleted %s entirely\n"), plan.Root)
		}
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
		fmt.Fprintf(a.out, tr("\n已卸载 %d 个运行时：%s\n", "\nuninstalled %d runtime(s): %s\n"), len(removed), strings.Join(removed, ", "))
	} else {
		fmt.Fprintln(a.out, tr("\n没有需要卸载的运行时。", "\nnothing to uninstall."))
	}
	fmt.Fprintln(a.out, tr("已清理：shim 目录、shell rc 里的托管块、Windows 用户 Path 里的 packnest 目录", "cleaned: shim dir, managed blocks in shell rc, packnest dirs in the Windows user Path"))
	if selfHandled {
		if goos() == "windows" {
			fmt.Fprintln(a.out, tr("\npk 已安排删除：关掉所有窗口后约 3 秒，安装目录里的 pk.exe / packnest.exe 会被删掉（目录空了再一并移除）。", "\npk scheduled its own removal: about 3 seconds after every window closes, pk.exe / packnest.exe are deleted (the empty dir goes too)."))
		} else {
			fmt.Fprintln(a.out, tr("\npk 二进制已经删掉了（同目录下的 pk / packnest 也一并清理）。", "\nthe pk binary has been deleted (pk / packnest in the same dir too)."))
		}
	} else {
		fmt.Fprintf(a.out, tr("\n最后一步 —— 删掉 pk 自己（检测到 %s 渠道）：\n  %s\n", "\nlast step - remove pk itself (installed via %s):\n  %s\n"), ch.Name, ch.Hint)
	}
	if len(failures) > 0 {
		fmt.Fprintln(a.errOut, tr("\n以下没做成，需手动处理：", "\nnot done, handle manually:"))
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
			fmt.Fprintln(a.errOut, tr("pk: npm 是 node 的伴生包，没有独立版本可卸；删 node 时它一起走。", "pk: npm ships with node and has no independent version to uninstall; it goes with node."))
			fmt.Fprintln(a.errOut, tr("    想换 npm 版本：pk install node@<版本>（会按 node 重新配对）或 pk use node@<已装版本>", "    to change npm: pk install node@<version> (repairs the pairing) or pk use node@<installed-version>"))
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
		return a.fail(errT(tr("%s 没有已装版本（`pk list` 看已装工具）", "%s has no installed version (see `pk list`)"), tool))
	}
	sort.Strings(versions)
	if !a.json {
		fmt.Fprintf(a.out, tr("将要从 %s 卸载 %s 的全部 %d 个版本：\n", "about to uninstall %s from %s: all %d version(s)\n"), inst.Root(), tool, len(versions))
		for _, v := range versions {
			mark := " "
			if v == active {
				mark = "*"
			}
			fmt.Fprintf(a.out, "  %s %s@%s\n", mark, tool, v)
		}
		fmt.Fprintln(a.out, tr("（缓存保留，离线可重装；* 是当前全局默认）", "(cache kept, offline reinstall possible; * marks the current global default)"))
	}
	if !a.confirm(fmt.Sprintf(tr("确认卸载 %s 的这 %d 个版本？", "%s: uninstall all %d version(s)?"), tool, len(versions))) {
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
		fmt.Fprintf(a.out, tr("已卸载 %s 的 %d 个版本：%s（缓存保留，离线可重装）\n", "uninstalled %s: %d version(s) (%s) (cache kept, offline reinstall possible)\n"),
			tool, len(removed), strings.Join(removed, ", "))
	}
	if err != nil {
		return a.fail(err)
	}
	if len(removed) == 0 {
		return a.fail(errT(tr("%s 没有已装版本（`pk list` 看已装工具）", "%s has no installed version (see `pk list`)"), tool))
	}
	return jsonout.ExitOK
}

// confirm 二次确认：--yes 直接通过；交互终端读一行 y/yes；
// 非交互（管道、CI、测试）一律当tr("不确认", "declined")，避免脚本误删。
func (a *app) confirm(prompt string) bool {
	if a.yes {
		return true
	}
	if a.json {
		fmt.Fprintln(a.errOut, tr("pk: --json 下不做交互确认，需加 --yes 才会执行", "pk: no interactive prompts under --json; add --yes to proceed"))
		return false
	}
	if !stdinIsTerminal() {
		fmt.Fprintln(a.errOut, tr("pk: 非交互环境不会执行破坏性操作，确认无误后加 --yes 重跑", "pk: non-interactive shells never run destructive work; rerun with --yes once you are sure"))
		return false
	}
	fmt.Fprintf(a.out, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	fmt.Fprintln(a.out, tr("已取消。", "cancelled."))
	return false
}
