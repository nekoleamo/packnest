package install

import (
	"context"
	"sync"
	"testing"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/testkit"
)

// TestInstallEmitsProgressEvents：`install --json` 必须给出进度事件（GUI 画进度条的唯一来源），
// 且最后一个 progress 的 done == total。
func TestInstallEmitsProgressEvents(t *testing.T) {
	fm := testkit.NewFakeNode(t, testkit.FakeNodeOptions{
		Versions: []testkit.NodeSpec{{Version: "22.22.2", NPMVersion: "11.20.0"}},
		NPM:      npmMatrix(),
	})
	var mu sync.Mutex
	var events []jsonout.Event
	inst := newInstaller(t, fm, func(o *Options) {
		o.Events = func(ev jsonout.Event) { mu.Lock(); events = append(events, ev); mu.Unlock() }
	})
	if _, err := inst.Install(context.Background(), "node", "22.22.2"); err != nil {
		t.Fatalf("装 node: %v", err)
	}

	var progress, lastDone, lastTotal int
	for _, ev := range events {
		if ev.Ev != "progress" {
			continue
		}
		progress++
		if ev.Tool == "" || ev.BytesTotal <= 0 {
			t.Fatalf("progress 事件必须带 tool 与 bytes_total: %+v", ev)
		}
		lastDone, lastTotal = int(ev.BytesDone), int(ev.BytesTotal)
	}
	if progress == 0 {
		t.Fatal("没有任何 progress 事件")
	}
	if lastDone != lastTotal {
		t.Fatalf("最后一个 progress 应完成（done=%d total=%d）", lastDone, lastTotal)
	}
	// done 必须仍然出现（GUI 靠它收尾）
	found := false
	for _, ev := range events {
		if ev.Ev == "done" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少 done 事件")
	}
}
