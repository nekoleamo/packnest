package jsonout

import (
	"bytes"
	"encoding/json"
	"testing"
)

// v1 契约（方案 §12.2 第 8 条）：这几个形状一旦发布就不许改。
// 每个用例都把整段 JSON 与 golden 对齐；改契约必须先改 testdata（-update）并在
// docs/json-schema.md 里记录破坏性变更。
func TestContractVersionJSON(t *testing.T) {
	checkGolden(t, "version.golden.json", struct {
		Schema  int    `json:"schema"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Go      string `json:"go"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}{SchemaVersion, "packnest", "1.2.3", "go1.27.3", "darwin", "arm64"})
}

func TestContractWhichJSON(t *testing.T) {
	checkGolden(t, "which.golden.json", struct {
		Schema int    `json:"schema"`
		Name   string `json:"name"`
		Path   string `json:"path"`
		Shim   bool   `json:"shim"`
	}{SchemaVersion, "node", "/Users/u/.packnest/tools/node/22.22.2/bin/node", true})
}

func TestContractGCJSON(t *testing.T) {
	type gcResult struct {
		Removed      []string `json:"Removed"`
		CacheRemoved []string `json:"CacheRemoved"`
		KeptRef      []string `json:"KeptRef"`
		KeptRecent   []string `json:"KeptRecent,omitempty"`
		FreedBytes   int64    `json:"FreedBytes"`
		DryRun       bool     `json:"DryRun"`
	}
	checkGolden(t, "gc.golden.json", struct {
		Schema int `json:"schema"`
		gcResult
		CacheMax int64 `json:"cache_max_bytes"`
		Keep     int   `json:"keep,omitempty"`
	}{
		Schema:   SchemaVersion,
		gcResult: gcResult{Removed: []string{"node@20.18.1"}, CacheRemoved: []string{"/u/.packnest/cache/archives/bun-1.0.0.tar.gz"}, KeptRef: []string{}, KeptRecent: []string{"node@22.23.4"}, FreedBytes: 1048576, DryRun: false},
		CacheMax: 5368709120,
		Keep:     2,
	})
}

// TestContractEvents 锁住 install 的 NDJSON 事件流（GUI 进度条吃这个）。
func TestContractEvents(t *testing.T) {
	ok := true
	events := []Event{
		{Ev: "resolve", Tool: "node", Version: "22.22.2"},
		{Ev: "cache", Tool: "node", Version: "22.22.2", Bytes: 47238101},
		{Ev: "download", Tool: "node", Version: "22.22.2", Bytes: 47238101, Source: "https://registry.npmmirror.com/x.tar.gz"},
		{Ev: "verify", Tool: "node", Version: "22.22.2", Mode: "network", OK: &ok},
		{Ev: "companion", Tool: "npm", Version: "12.2.2", Msg: "replaced 11.20.0"},
		{Ev: "done", Tool: "node", Version: "22.22.2", Path: "/u/.packnest/tools/node/22.22.2"},
	}
	var buf bytes.Buffer
	for _, ev := range events {
		if err := WriteEvent(&buf, ev); err != nil {
			t.Fatal(err)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != len(events) {
		t.Fatalf("事件数不符: %d", len(lines))
	}
	for _, line := range lines {
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			t.Fatalf("NDJSON 行非法: %v (%s)", err, line)
		}
		if _, ok := raw["ev"]; !ok {
			t.Fatalf("事件必须有 ev: %s", line)
		}
	}
	checkGolden(t, "events.golden.ndjson", goldenLines(lines))
}

// goldenLines 让 JSON 数组以"每行一个事件"的形式进 golden（便于肉眼对比）。
type goldenLines [][]byte

func (g goldenLines) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i, line := range g {
		buf.WriteString("  ")
		buf.Write(line)
		if i != len(g)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("]")
	return buf.Bytes(), nil
}
