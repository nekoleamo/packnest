// Package pkcli 是 GUI 与 CLI 之间唯一的胶水层：定位已安装的 pk、跑 `pk <cmd> --json`、
// 把 JSON / NDJSON 投影成 GUI 用的结构。**不 import 主模块的 internal/** ——
// GUI 是 CLI 的可视化补充，业务真相永远在 CLI 与 PK_ROOT 里（方案 §11 定案 2026-11-11）。
package pkcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// MinSchema 是 GUI 支持的最低契约版本（docs/json-schema.md）。
const MinSchema = 1

// MinVersion 是 GUI 要求的最低 CLI 版本：低于它缺 `progress` 事件与 `source` 字段。
const MinVersion = "0.10.0"

// ErrNotFound 表示机器上找不到 pk（用户还没装 CLI —— GUI 必须先引导安装）。
var ErrNotFound = errors.New("找不到 pk：GUI 需要先安装 CLI")

// ErrBusy 对应 CLI 的退出码 5：另一个 pk 正在运行（GUI 应提示而不是重试风暴）。
var ErrBusy = errors.New("另一个 pk 正在运行（CLI 退出码 5）")

// Info 是 `pk version --json` 的投影。
type Info struct {
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Path    string `json:"-"` // pk 二进制路径（GUI 自己补的）
}

// Client 是"已定位的 pk"。
type Client struct {
	Path string
	Env  []string // 继承的环境（代理/镜像变量自然生效）
}

// Locate 按 PATH → 常见安装目录 → 用户手填的顺序找 pk，并校验它能跑、契约版本够新。
//
// 顺序刻意是"PATH 优先"：用户用 brew/scoop/脚本装的那个才是权威版本，GUI 不内置。
func Locate(extra ...string) (*Client, Info, error) {
	candidates := []string{}
	if p, err := exec.LookPath("pk"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, defaultPaths()...)
	candidates = append(candidates, extra...)
	return locate(candidates)
}

// locate 是 Locate 的可测内核：只认给定候选（测试里可以精确控制）。
func locate(candidates []string) (*Client, Info, error) {
	seen := map[string]bool{}
	var unusable error // "找到了但版本/schema 不合格"：要报给用户，而不是说"找不到 pk"
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		st, err := os.Stat(c)
		if err != nil || st.IsDir() {
			continue
		}
		cl := &Client{Path: c, Env: os.Environ()}
		info, err := cl.Version(context.Background())
		if err != nil {
			unusable = err
			continue
		}
		info.Path = c
		return cl, info, nil
	}
	if unusable != nil {
		return nil, Info{}, unusable
	}
	return nil, Info{}, ErrNotFound
}

// defaultPaths 是各个渠道的默认落地位置（找不到 PATH 时的兜底）。
func defaultPaths() []string {
	home, _ := os.UserHomeDir()
	out := []string{filepath.Join(home, ".local", "bin", "pk")}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			out = append(out, filepath.Join(la, "packnest", "bin", "pk.exe"))
		}
	} else {
		out = append(out, "/usr/local/bin/pk", "/opt/homebrew/bin/pk")
	}
	return out
}

// Version 跑 `pk version --json` 并检查契约版本。
func (c *Client) Version(ctx context.Context) (Info, error) {
	var info Info
	if err := c.runJSON(ctx, &info, "version"); err != nil {
		return Info{}, err
	}
	if info.Schema != MinSchema {
		return Info{}, fmt.Errorf("CLI 契约 schema=%d，GUI 只支持 %d（升级 pk）", info.Schema, MinSchema)
	}
	if !versionAtLeast(info.Version, MinVersion) {
		return Info{}, fmt.Errorf("CLI 版本 %s 太旧（GUI 需要 ≥ %s）：请先升级 pk", info.Version, MinVersion)
	}
	return info, nil
}

// runJSON 跑一条输出单个 JSON 文档的子命令（自动补 `--json`，调用方不用记）。
func (c *Client) runJSON(ctx context.Context, out any, args ...string) error {
	args = append(args, "--json")
	stdout, err := c.run(ctx, nil, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("pk %s 的输出不是 JSON: %w", strings.Join(args, " "), err)
	}
	return nil
}

// run 执行子命令：stdout 全量返回，stderr 折算成错误，退出码 5 归为 ErrBusy。
func (c *Client) run(ctx context.Context, onLine func([]byte) error, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Env = c.Env
	hideWindow(cmd) // Windows 上不弹黑框
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var buf []byte
	if onLine != nil {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if err := onLine(line); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, err
			}
		}
	} else {
		buf, _ = io.ReadAll(stdout)
	}
	werr := cmd.Wait()
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) && ee.ExitCode() == 5 {
			return buf, ErrBusy
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		// 把已读到的 stdout 一并返回：`pk doctor` 在有 error 级问题时退出码是 1，但 JSON 是合法的。
		return buf, fmt.Errorf("pk %s 失败: %s", strings.Join(args, " "), msg)
	}
	return buf, nil
}

// versionAtLeast 比较 "0.10.0" 这类版本（只按数字段比，够用且不引依赖）。
func versionAtLeast(got, want string) bool {
	g, w := strings.Split(strings.TrimPrefix(got, "v"), "."), strings.Split(want, ".")
	for i := 0; i < len(w); i++ {
		var a, b int
		if i < len(g) {
			fmt.Sscanf(g[i], "%d", &a)
		}
		fmt.Sscanf(w[i], "%d", &b)
		if a != b {
			return a > b
		}
	}
	return true
}

// Ready 是一个"可以开始用"的简单判据（GUI 启动时用）。
func (c *Client) Ready(ctx context.Context) error {
	_, err := c.Version(ctx)
	return err
}

// WithTimeout 是给每次子进程调用套超时的便捷函数（list/doctor 走网络时要放宽）。
func WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
