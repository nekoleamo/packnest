package pkcli

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
)

// Tool 是 `pk list --json` 里的一条已装记录（只取 GUI 需要的字段）。
type Tool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Active    bool   `json:"active"`
	Path      string `json:"path"`
	Verified  string `json:"verified"`
	Source    string `json:"source"`
	SizeBytes int64  `json:"size_bytes"`
	NPM       *struct {
		Version  string `json:"version"`
		Replaced string `json:"replaced"`
	} `json:"npm"`
}

type listDoc struct {
	Schema int    `json:"schema"`
	Tools  []Tool `json:"tools"`
}

// List 返回已装工具（GUI 首屏）。
func (c *Client) List(ctx context.Context) ([]Tool, error) {
	var doc listDoc
	if err := c.runJSON(ctx, &doc, "list"); err != nil {
		return nil, err
	}
	if doc.Tools == nil {
		return []Tool{}, nil // 契约：数组永远不是 null
	}
	return doc.Tools, nil
}

// Issue 是 `pk doctor --json` 的一条问题。
type Issue struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Detail   map[string]any `json:"detail"`
}

type doctorDoc struct {
	Schema int     `json:"schema"`
	OK     bool    `json:"ok"`
	Issues []Issue `json:"issues"`
}

// Doctor 返回体检结果（GUI 的"告警页"直接渲染这个）。
func (c *Client) Doctor(ctx context.Context, globals bool) ([]Issue, error) {
	args := []string{"doctor"}
	if globals {
		args = append(args, "--globals")
	}
	args = append(args, "--json")
	// doctor 有 error 级问题时退出码是 1，但输出仍是合法 JSON：这里要吃掉退出码
	stdout, err := c.run(ctx, nil, args...)
	if err != nil && len(stdout) == 0 {
		return nil, err
	}
	var doc doctorDoc
	if jerr := json.Unmarshal(stdout, &doc); jerr != nil {
		return nil, err
	}
	if doc.Issues == nil {
		return []Issue{}, nil
	}
	return doc.Issues, nil
}

// Event 是 `pk install --json` 的一行 NDJSON。
type Event struct {
	Ev         string `json:"ev"`
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Bytes      int64  `json:"bytes"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total"`
	Source     string `json:"source"`
	Mode       string `json:"mode"`
	Path       string `json:"path"`
	Msg        string `json:"msg"`
}

// Install 跑 `pk install <spec> --json`，把事件逐行回调给 GUI（进度条直接用 progress 事件）。
func (c *Client) Install(ctx context.Context, spec string, onEvent func(Event)) error {
	_, err := c.run(ctx, func(line []byte) error {
		var ev Event
		if json.Unmarshal(line, &ev) != nil {
			return nil // 非 JSON 行忽略（stdout 只会是 NDJSON，但别为一行崩掉 UI）
		}
		if onEvent != nil {
			onEvent(ev)
		}
		return nil
	}, "install", spec, "--json")
	return err
}

// Use 切换全局默认版本（`pk use <tool>@<version>`）。
func (c *Client) Use(ctx context.Context, tool, version string) error {
	return c.runJSON(ctx, &struct{}{}, "use", tool+"@"+version)
}

// Mirror 是 `pk mirror list --json` 的投影（GUI 的"镜像页"）。
type Mirror struct {
	Current string `json:"current"`
	Mirrors []struct {
		Name     string `json:"name"`
		Display  string `json:"display"`
		Registry string `json:"registry"`
		Current  bool   `json:"current"`
	} `json:"mirrors"`
}

// Mirrors 读当前档位列表。
func (c *Client) Mirrors(ctx context.Context) (Mirror, error) {
	var m Mirror
	err := c.runJSON(ctx, &m, "mirror", "list")
	return m, err
}

// UseMirror 切档（`pk mirror use <name>`）。
func (c *Client) UseMirror(ctx context.Context, name string) error {
	return c.runJSON(ctx, &struct{}{}, "mirror", "use", name)
}

// GCResult 是 `pk gc --json` 的投影。
type GCResult struct {
	Removed      []string `json:"Removed"`
	CacheRemoved []string `json:"CacheRemoved"`
	KeptRef      []string `json:"KeptRef"`
	FreedBytes   int64    `json:"FreedBytes"`
	DryRun       bool     `json:"DryRun"`
}

// GC 跑回收（dryRun=true 时只报告）。
func (c *Client) GC(ctx context.Context, dryRun bool) (GCResult, error) {
	args := []string{"gc", "--json"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	var res GCResult
	err := c.runJSON(ctx, &res, args...)
	return res, err
}

// Uninstall 卸载某工具的全部版本（GUI 必须先自己确认，再传 --yes）。
func (c *Client) Uninstall(ctx context.Context, tool string) error {
	return c.runJSON(ctx, &struct{}{}, "uninstall", tool, "--yes")
}

// hideWindow 让 Windows 上不弹控制台窗口（其他平台是空操作）。
func hideWindow(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		setHideWindow(cmd)
	}
}
