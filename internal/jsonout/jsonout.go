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
type ListOutput struct {
	Schema int         `json:"schema"`
	Tools  []ToolEntry `json:"tools"`
}

// Issue 是 `pk doctor --json` 的一条问题。
type Issue struct {
	Code     string         `json:"code"`
	Severity Severity       `json:"severity"`
	Message  string         `json:"message,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// DoctorOutput 是 `pk doctor --json` 的输出。
type DoctorOutput struct {
	Schema int     `json:"schema"`
	OK     bool    `json:"ok"`
	Issues []Issue `json:"issues"`
}

// Event 是 `pk install --json` 的 NDJSON 事件。
type Event struct {
	Ev      string `json:"ev"`
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Source  string `json:"source,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Path    string `json:"path,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
	Msg     string `json:"msg,omitempty"`
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
