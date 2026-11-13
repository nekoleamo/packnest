package jsonout

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "更新 golden 文件")

// golden 断言：契约漂移会让 GUI 与用户脚本静默坏掉（方案 §9.5 第 7 项）。
func checkGolden(t *testing.T, name string, v any) {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, v); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 失败（先跑 go test -run %s -update）: %v", name, err)
	}
	if buf.String() != string(want) {
		t.Fatalf("golden 不匹配 %s:\n--- got ---\n%s\n--- want ---\n%s", name, buf.String(), want)
	}
}

func TestListGolden(t *testing.T) {
	out := NewList([]ToolEntry{
		{Name: "node", Version: "26.13.0", Active: true, Path: "/Users/u/.packnest/tools/node/26.13.0",
			NPM: &ToolNPM{Version: "12.2.2", Replaced: "11.20.0"}, Requires: []string{}},
		{Name: "pnpm", Version: "10.4.1", Active: false, Requires: []string{"node"}},
	})
	out.Outdated = []OutdatedEntry{{Name: "node", Installed: "26.13.0", Latest: "26.14.0", Advice: "pk upgrade node"}}
	checkGolden(t, "list.golden.json", out)
}

func TestDoctorGolden(t *testing.T) {
	out := NewDoctor([]Issue{
		{Code: "GLOBAL_ENGINES_UNMET", Severity: SeverityWarn, Message: "全局包引擎要求不满足",
			Detail: map[string]any{"pkg": "vite", "needs": "^24", "have": "22.22.2"}},
		{Code: "PATH_ORDER", Severity: SeverityErr, Message: "bin 目录不在 PATH 最前"},
	})
	out.Fixed = []string{"重写 shell 的 PATH 托管块（`pk env --write`，幂等）"}
	checkGolden(t, "doctor.golden.json", out)
}

func TestDoctorOK(t *testing.T) {
	if out := NewDoctor(nil); !out.OK || out.Issues == nil || out.Schema != SchemaVersion {
		t.Fatalf("空问题应 OK 且 issues=[]: %+v", out)
	}
	if out := NewDoctor([]Issue{{Code: "X", Severity: SeverityWarn}}); !out.OK {
		t.Fatal("只有 warn 时仍应 OK")
	}
	if out := NewDoctor([]Issue{{Code: "X", Severity: SeverityErr}}); out.OK {
		t.Fatal("有 error 时不应 OK")
	}
}

func TestListNilSafe(t *testing.T) {
	out := NewList(nil)
	if out.Tools == nil {
		t.Fatal("tools 必须是 [] 而不是 null")
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"tools": []`)) {
		t.Fatalf("输出应含空数组: %s", buf.String())
	}
}

func TestWriteEventNDJSON(t *testing.T) {
	var buf bytes.Buffer
	ok := true
	if err := WriteEvent(&buf, Event{Ev: "download", Tool: "node", Bytes: 47238101, Source: "cn"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteEvent(&buf, Event{Ev: "verify", Tool: "node", OK: &ok, Mode: "tofu"}); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("应为 2 行 NDJSON: %s", buf.String())
	}
	var ev map[string]any
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatalf("第一行不是合法 JSON: %v", err)
	}
	if ev["ev"] != "download" || ev["bytes"].(float64) != 47238101 {
		t.Fatalf("事件字段异常: %v", ev)
	}
	if _, ok := ev["path"]; ok {
		t.Fatal("空字段应省略（omitempty）")
	}
	if err := json.Unmarshal(lines[1], &ev); err != nil {
		t.Fatal(err)
	}
	if ev["ok"] != true {
		t.Fatalf("ok 字段应保留 false/true 语义: %v", ev)
	}
}

func TestExitCodes(t *testing.T) {
	if ExitOK != 0 || ExitFail != 1 || ExitUsage != 2 || ExitNetwork != 3 || ExitChecksum != 4 || ExitLocked != 5 {
		t.Fatal("退出码语义固定，不许改（方案 §10）")
	}
}

func TestWriteCompact(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCompact(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "{\"a\":1}\n" {
		t.Fatalf("compact = %q", buf.String())
	}
}

func TestWriteErrors(t *testing.T) {
	bad := make(chan int)
	if err := WriteJSON(&bytes.Buffer{}, bad); err == nil {
		t.Fatal("不可序列化的值应报错")
	}
	if err := WriteCompact(&bytes.Buffer{}, bad); err == nil {
		t.Fatal("不可序列化的值应报错")
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errorString("boom") }

type errorString string

func (e errorString) Error() string { return string(e) }

func TestWriteToFailingWriter(t *testing.T) {
	if err := WriteJSON(failingWriter{}, map[string]int{"a": 1}); err == nil {
		t.Fatal("写入失败应返回错误")
	}
	if err := WriteCompact(failingWriter{}, map[string]int{"a": 1}); err == nil {
		t.Fatal("写入失败应返回错误")
	}
}
