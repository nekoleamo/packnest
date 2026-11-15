// Package jsonout 是 `--json` 的唯一出口（方案 §18.8）。
//
// 规则：GUI 与用户脚本只吃这里的 schema；字段只增不改（schema 版本号固定为 1），
// 改动必须同步 testdata 下的 golden 文件。
package jsonout

import (
	"encoding/json"
	"fmt"
	"io"
)

// SchemaVersion 是 `--json` 输出契约版本（M0 冻结）。
const SchemaVersion = 1

// Severity 是 doctor 问题的严重级别。
type Severity string

const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
	SeverityErr  Severity = "error"
)

// 退出码（方案 §10）。
const (
	ExitOK       = 0
	ExitFail     = 1
	ExitUsage    = 2
	ExitNetwork  = 3
	ExitChecksum = 4
	ExitLocked   = 5
)

// ToolNPM 是 node 上伴生 npm 的信息（显式化，方案 §7 第 4 步）。
type ToolNPM struct {
	Version  string `json:"version"`
	Replaced string `json:"replaced,omitempty"`
}

// ToolEntry 是 `pk list --json` 里的一项。
type ToolEntry struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Active   bool     `json:"active"`
	Path     string   `json:"path,omitempty"`
	NPM      *ToolNPM `json:"npm,omitempty"`
	Requires []string `json:"requires"`
	Verified string   `json:"verified,omitempty"`
	// Source 是清单来源：embedded（内嵌）| external（$PK_ROOT/providers 等外部目录）。
	Source string `json:"source,omitempty"`
	// SizeBytes 是安装目录占用（人类可读的--json 也带，方便 GUI 画磁盘条）。
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// ListOutput 是 `pk list --json` 的输出。
// Outdated 是“有新版可装”的提示：只读索引缓存（不联网），没缓存就没提示。
type ListOutput struct {
	Schema   int             `json:"schema"`
	Tools    []ToolEntry     `json:"tools"`
	Outdated []OutdatedEntry `json:"outdated,omitempty"`
}

// OutdatedEntry 是一条可升级提示（Installed/Latest 都是实际版本号，不用大版本号比较）。
type OutdatedEntry struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Advice    string `json:"advice,omitempty"`
}

// Issue 是 `pk doctor --json` 的一条问题。
type Issue struct {
	Code     string         `json:"code"`
	Severity Severity       `json:"severity"`
	Message  string         `json:"message,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// DoctorOutput 是 `pk doctor --json` 的输出（Fixed = `--fix` 实际做过的动作）。
type DoctorOutput struct {
	Schema int      `json:"schema"`
	OK     bool     `json:"ok"`
	Issues []Issue  `json:"issues"`
	Fixed  []string `json:"fixed,omitempty"`
	// Reload 只在"这次修复改动了环境（rc 托管块 / 用户 Path）"时出现（1.8.2 新增，只增字段）：
	// 当前 shell 拿不到新 PATH，必须重开或 source 一次才生效。
	Reload *ReloadAdvice `json:"reload,omitempty"`
}

// ReloadAdvice 说明"怎么让刚写好的环境生效"（给 GUI 与脚本用，1.8.2 新增、只增字段）。
//
// 字段都是机器可判断的：`shell` 决定措辞、`command` 有值就能直接粘进当前 shell、
// `files` 是这次写过的地方（rc/profile，或 Windows 的 `registry:user-path`）。
// `msg` 只是人话（跟着 --lang 走），脚本不该拿它做匹配。
type ReloadAdvice struct {
	Needed  bool     `json:"needed"`
	Shell   string   `json:"shell,omitempty"`   // zsh / bash / fish / powershell / cmd / other
	Command string   `json:"command,omitempty"` // 当前 shell 里可直接执行；空 = 只能重开终端
	Files   []string `json:"files,omitempty"`   // 写入的 rc/profile；Windows 注册表写作 registry:user-path
	Msg     string   `json:"msg,omitempty"`     // 人读补充说明
}

// Event 是 `pk install --json` 的 NDJSON 事件。
type Event struct {
	Ev      string `json:"ev"`
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	// BytesDone / BytesTotal 只在 progress 事件里有意义（GUI 画进度条用）。
	BytesDone  int64  `json:"bytes_done,omitempty"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
	Source     string `json:"source,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Path       string `json:"path,omitempty"`
	OK         *bool  `json:"ok,omitempty"`
	// Replaced 是伴生包替换前的版本（npm 换掉 node 自带的那份时用）。
	Replaced string `json:"replaced,omitempty"`
	// LatencyMS / Cached 是 mirror 探测结果（GUI 想画"哪个源快"时不用去解析 msg）。
	LatencyMS int64 `json:"latency_ms,omitempty"`
	Cached    bool  `json:"cached,omitempty"`
	// Msg 是**人读**补充说明：可能中文、可能改措辞、可能不出现。
	// 脚本与 GUI 不许对它做字符串匹配 —— 机器判断请用 Ev / OK / Mode / Replaced / LatencyMS /
	// BytesDone / BytesTotal / Source / Path（契约见 docs/json-schema.md「字段语义」）。
	Msg string `json:"msg,omitempty"`
	// MsgCode / MsgArgs 是 Msg 的**结构化来源**（1.8 起）：`internal/*` 不产出人话，
	// 只给 stable code + 参数，由 CLI 按当前语言渲染进 Msg（`--lang en` 下 Msg 变英文）。
	// 两者都只在"这句话是产品文案"时出现；机器判断**照样不该**读 Msg，但可以用 MsgCode。
	MsgCode string `json:"msg_code,omitempty"`
	MsgArgs []any  `json:"msg_args,omitempty"`
}

// WriteJSON 输出一个缩进 JSON 文档（人可读，也便于 golden 比对）。
func WriteJSON(w io.Writer, v any) error { return writeJSON(w, v, true) }

// WriteCompact 输出单行 JSON（脚本读取用）。
func WriteCompact(w io.Writer, v any) error { return writeJSON(w, v, false) }

func writeJSON(w io.Writer, v any, indent bool) error {
	var data []byte
	var err error
	if indent {
		data, err = json.MarshalIndent(v, "", "  ")
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// WriteEvent 输出一行 NDJSON 事件。
func WriteEvent(w io.Writer, ev Event) error {
	return WriteCompact(w, ev)
}

// NewList 构造 list 输出（保证 Tools 不为 nil，golden 稳定）。
func NewList(tools []ToolEntry) ListOutput {
	if tools == nil {
		tools = []ToolEntry{}
	}
	return ListOutput{Schema: SchemaVersion, Tools: tools}
}

// NewDoctor 构造 doctor 输出，并按最严重级别决定 OK。
func NewDoctor(issues []Issue) DoctorOutput {
	if issues == nil {
		issues = []Issue{}
	}
	ok := true
	for _, is := range issues {
		if is.Severity == SeverityErr {
			ok = false
		}
	}
	return DoctorOutput{Schema: SchemaVersion, OK: ok, Issues: issues}
}
