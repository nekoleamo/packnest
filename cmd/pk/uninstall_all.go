package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/shim"
)

// installChannel 说明"这个 pk 二进制是怎么装进来的" —— 不同渠道要用不同的命令才能删干净。
type installChannel struct {
	Name string // brew / npm / scoop / winget / script-posix / script-windows / manual
	Hint string // 给用户照抄的命令
}

// detectInstallChannel 按可执行文件的路径判断渠道（纯函数，便于单测）。
//
// 判不出来不算错：自托管/手动拷贝就是这种，直接告诉用户删哪个文件。
func detectInstallChannel(exePath, goos string) installChannel {
	// 手工归一化：filepath.ToSlash 在非 Windows 上不会把 "\\" 转成 "/"，
	// 而渠道判断必须是跨平台的（同一套规则要能识别 cnpm/scoop/winget 的路径）。
	p := strings.ToLower(strings.ReplaceAll(exePath, `\`, "/"))
	switch {
	case strings.Contains(p, "/cellar/packnest/") || strings.Contains(p, "/homebrew/cellar/packnest/"):
		return installChannel{"brew", "brew uninstall packnest && brew untap nekoleamo/packnest"}
	case strings.Contains(p, "/node_modules/packnest/") || strings.Contains(p, "/node_modules/@packnest/"):
		return installChannel{"npm", "npm uninstall -g packnest"}
	case strings.Contains(p, "/scoop/apps/packnest/"):
		return installChannel{"scoop", "scoop uninstall packnest && scoop bucket rm nekoleamo"}
	case strings.Contains(p, "/winget/packages/") || strings.Contains(p, "/microsoft/winget/"):
		return installChannel{"winget", "winget uninstall nekoleamo.packnest"}
	case goos == "windows" && strings.Contains(p, "/packnest/bin/"):
		return installChannel{"script-windows", `删除 "%LOCALAPPDATA%\packnest\bin"（pk env --unset 会把 PATH 项一并去掉）`}
	case goos != "windows" && (strings.Contains(p, "/.local/bin/") || strings.Contains(p, "/.packnest/bin/")):
		return installChannel{"script-posix", "rm -f ~/.local/bin/pk ~/.local/bin/packnest（若是自托管目录，删对应文件）"}
	}
	return installChannel{"manual", "直接删除这个文件：" + exePath}
}

// uninstallAll 清掉 pk 装过的一切（运行时、shim、PATH/rc 托管块），并按渠道告诉用户怎么删掉 pk 自己。
func (a *app) uninstallAll(inst *install.Installer, purge, keepCache bool) int {
	ledger := inst.Ledger()
	targets := make([]string, 0, len(ledger.Installs))
	for _, in := range ledger.Installs {
		targets = append(targets, in.Tool+"@"+in.Version)
	}
	sort.Strings(targets)

	exe, _ := os.Executable()
	ch := detectInstallChannel(exe, goos())

	if !a.yes {
		fmt.Fprintln(a.out, "将要删除（加 --yes 才会真的执行）：")
		if len(targets) == 0 {
			fmt.Fprintln(a.out, "  · 没有已装的运行时")
		} else {
			fmt.Fprintf(a.out, "  · %d 个已装运行时：%s\n", len(targets), strings.Join(targets, ", "))
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

	// PATH / rc 里的托管块：posix 的 rc、Windows 的 profile（两份）+ 注册表用户 Path
	unset := a.cmdEnv([]string{"--unset"})
	if unset != jsonout.ExitOK && unset != jsonout.ExitFail {
		failures = append(failures, fmt.Sprintf("清理 PATH/rc 托管块失败（退出码 %d）", unset))
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
	fmt.Fprintf(a.out, "\n最后一步 —— 删掉 pk 自己（检测到 %s 渠道）：\n  %s\n", ch.Name, ch.Hint)
	if len(failures) > 0 {
		fmt.Fprintln(a.errOut, "\n以下没做成，需手动处理：")
		for _, f := range failures {
			fmt.Fprintf(a.errOut, "  · %s\n", f)
		}
		return jsonout.ExitFail
	}
	return jsonout.ExitOK
}
