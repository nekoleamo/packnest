package main

// GUI 1.0 口径里的两道门（`~/Documents/Plan/17-包管理器/pn-GUI-1.0-口径.md` H2）。
// 两条都是 0.12.x 线上补丁钉回来的回归项：黑窗 / 退出不退 / 第二个实例还能开。
//
//   GUI1_CrossCompile          —— 5 个发布目标都编得出来、版本串在里面、Windows 是 GUI 子系统
//   GUI2_TrayAndSingleInstance —— 单实例真的互斥（且退出后能再开）；托盘缺失时的降级是"关窗即退"

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseTargets 是 `scripts/release-gui.sh` / `make gui-cross` 真正产出的目标
// （macOS 只发 arm64：2026-11-12 拍板放弃 Intel）。
var releaseTargets = []struct{ os, arch string }{
	{"darwin", "arm64"},
	{"windows", "amd64"},
	{"windows", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
}

// TestAcceptance_GUI1_CrossCompile 守"一台 mac 就能出全套产物"：
// 交叉编 → 版本串必须在二进制里（GUI 版本是编译期常量，`--version` 与自更新都靠它）→
// 目标格式与架构对得上 → Windows 的子系统必须是 GUI(2)，不是控制台(3)。
func TestAcceptance_GUI1_CrossCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：交叉编译慢，跳过")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("没有 go 工具链，跳过交叉编译")
	}
	out := t.TempDir()
	for _, tg := range releaseTargets {
		name := tg.os + "/" + tg.arch
		bin := filepath.Join(out, "pk-desktop_"+tg.os+"_"+tg.arch)
		ldf := "-s -w"
		if tg.os == "windows" {
			bin += ".exe"
			// 0.12.0 的坑：不加 -H windowsgui 就是控制台子系统，双击先弹一个黑窗
			ldf += " -H windowsgui"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldf, "-o", bin, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+tg.os, "GOARCH="+tg.arch)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s 交叉编译失败: %v\n%s", name, err, b)
		}
		data, err := os.ReadFile(bin)
		if err != nil {
			t.Fatalf("%s 读产物失败: %v", name, err)
		}
		if !bytes.Contains(data, []byte(guiVersion)) {
			t.Errorf("%s 的产物里找不到版本串 %q：--version 与自更新都会拿错版本", name, guiVersion)
		}
		switch tg.os {
		case "windows":
			f, err := pe.NewFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s 不是合法 PE 文件: %v", name, err)
			}
			var sub uint16
			switch oh := f.OptionalHeader.(type) {
			case *pe.OptionalHeader64:
				sub = oh.Subsystem
			case *pe.OptionalHeader32:
				sub = oh.Subsystem
			default:
				t.Fatalf("%s 没有可识别的可选头", name)
			}
			if sub != 2 { // IMAGE_SUBSYSTEM_WINDOWS_GUI
				t.Errorf("%s 的子系统 = %d，期望 2（GUI）：双击会先弹一个黑色控制台窗口", name, sub)
			}
		case "linux":
			f, err := elf.NewFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s 不是合法 ELF: %v", name, err)
			}
			want := elf.EM_AARCH64
			if tg.arch == "amd64" {
				want = elf.EM_X86_64
			}
			if f.Machine != want {
				t.Errorf("%s 的 ELF Machine = %v，期望 %v", name, f.Machine, want)
			}
		case "darwin":
			f, err := macho.NewFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s 不是合法 Mach-O: %v", name, err)
			}
			if f.Cpu != macho.CpuArm64 {
				t.Errorf("%s 的 Mach-O CPU = %v，期望 arm64", name, f.Cpu)
			}
		}
	}
}

// TestAcceptance_GUI2_TrayAndSingleInstance 守两件事：
//  1. 单实例互斥真的互斥 —— 第二个实例占不到同一个回环端口，且用户看到的是
//     "已经有一个 packnest 界面在运行"+下一步；第一个退出后必须能再开（否则关掉就打不开了）；
//  2. 关窗语义 —— 有托盘收进托盘、没托盘（Linux 缺 libayatana-appindicator3）关窗即退、
//     正在退出必须优先放行（0.12.1 的坑：退出被自己的 PreventDefault 拦掉，进程留在后台）。
func TestAcceptance_GUI2_TrayAndSingleInstance(t *testing.T) {
	t.Run("单实例互斥", func(t *testing.T) {
		// 先借一个空闲端口再还回去，避免和真实的 7799 打架
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("拿不到空闲端口: %v", err)
		}
		addr := probe.Addr().String()
		probe.Close()

		first := &singleInstance{}
		if err := first.acquire(addr); err != nil {
			t.Fatalf("第一个实例占不到 %s: %v", addr, err)
		}
		second := &singleInstance{}
		err = second.acquire(addr)
		if err == nil {
			second.release()
			t.Fatal("第二个实例占到了同一个端口：单实例锁失效，会开出两个窗口")
		}
		msg := singleInstanceMsg(addr, err)
		for _, want := range []string{"已经有一个 packnest 界面在运行", "退出"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("单实例文案缺 %q：%s", want, msg)
			}
		}

		// 释放之后必须能再占：否则"关掉再打开"会永远报已有一个在运行
		first.release()
		third := &singleInstance{}
		if err := third.acquire(addr); err != nil {
			t.Fatalf("第一个实例释放后仍占不到 %s（关掉就打不开了）: %v", addr, err)
		}
		third.release()
	})

	t.Run("关窗语义", func(t *testing.T) {
		cases := []struct {
			name     string
			quitting bool
			hasTray  bool
			want     closeAction
		}{
			{"有托盘：关窗收进托盘", false, true, closeHide},
			{"没托盘：关窗即退（不是崩，也不是什么都不做）", false, false, closeQuit},
			{"正在退出：放行，托盘也不能拦", true, true, closeAllow},
			{"正在退出且没托盘：放行", true, false, closeAllow},
		}
		for _, c := range cases {
			if got := closeBehavior(c.quitting, c.hasTray); got != c.want {
				t.Errorf("%s: closeBehavior(%v,%v) = %v，期望 %v", c.name, c.quitting, c.hasTray, got, c.want)
			}
		}
	})

	t.Run("托盘图标", func(t *testing.T) {
		if len(iconForTray()) == 0 {
			t.Fatal("托盘图标为空：有托盘的平台会是一个空白图标")
		}
	})
}
