package pkcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakePK 造一个假 pk 脚本：GUI 侧的所有断言都不需要真装 node（也不需要网络）。
func fakePK(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假 pk 是 sh 脚本，Windows 用真实 CLI 验证")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pk")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const versionOK = `if [ "$1" = version ]; then echo '{"schema":1,"name":"packnest","version":"0.10.0"}'; exit 0; fi
`

// TestLocateAndVersion：能定位并读出契约版本；schema 不匹配或版本太旧要明确报错。
func TestLocateAndVersion(t *testing.T) {
	p := fakePK(t, versionOK)
	cl, info, err := locate([]string{p}) // 只认显式候选：本机可能真装了 pk
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if cl.Path != p || info.Version != "0.10.0" || info.Schema != 1 {
		t.Fatalf("定位结果不对: %+v %+v", cl, info)
	}

	old := fakePK(t, `if [ "$1" = version ]; then echo '{"schema":1,"name":"packnest","version":"0.9.0"}'; fi`)
	if _, _, err := locate([]string{old}); err == nil || !strings.Contains(err.Error(), "太旧") {
		t.Fatalf("0.9.0 应被拒（缺 progress 事件）: %v", err)
	}
	bad := fakePK(t, `if [ "$1" = version ]; then echo '{"schema":2,"name":"packnest","version":"0.10.0"}'; fi`)
	if _, _, err := locate([]string{bad}); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("schema 不匹配应报错: %v", err)
	}
	if _, _, err := locate([]string{filepath.Join(t.TempDir(), "nope")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("找不到时应返回 ErrNotFound: %v", err)
	}
}

// TestInstallStreamsProgressAndBusy：install 的 NDJSON 要逐行回调；退出码 5 归为 ErrBusy。
func TestInstallStreamsProgressAndBusy(t *testing.T) {
	p := fakePK(t, versionOK+`if [ "$1" = install ]; then
  printf '{"ev":"resolve","tool":"node","version":"22.23.5"}\n'
  printf '{"ev":"progress","tool":"node","bytes_done":50,"bytes_total":100}\n'
  printf '{"ev":"done","tool":"node","version":"22.23.5"}\n'
  exit 0
fi
if [ "$1" = use ]; then echo '{"schema":1}' >&2; echo "另一个 pk 正在运行" >&2; exit 5; fi
`)
	cl := &Client{Path: p, Env: os.Environ()}
	var evs []Event
	if err := cl.Install(context.Background(), "node@22", func(ev Event) { evs = append(evs, ev) }); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(evs) != 3 || evs[1].Ev != "progress" || evs[1].BytesTotal != 100 {
		t.Fatalf("事件流不对: %+v", evs)
	}
	if err := cl.Use(context.Background(), "node", "22.23.5"); !errors.Is(err, ErrBusy) {
		t.Fatalf("退出码 5 应为 ErrBusy: %v", err)
	}
}

// TestListAndDoctor：list 的空数组契约、doctor 的"退出码 1 但 JSON 合法"都要能吃下。
func TestListAndDoctor(t *testing.T) {
	p := fakePK(t, versionOK+`if [ "$1" = list ]; then
  echo '{"schema":1,"tools":[],"outdated":[{"name":"node","installed":"22.23.4","latest":"26.13.0","advice":"pk install node@latest"}]}'
  exit 0
fi
if [ "$1" = doctor ]; then
  echo '{"schema":1,"ok":false,"issues":[{"code":"PATH_MISSING","severity":"warn","message":"bin 不在 PATH"}],"fixed":["重建 pk/packnest 入口"]}'
  exit 1
fi
`)
	cl := &Client{Path: p, Env: os.Environ()}
	tools, err := cl.List(context.Background())
	if err != nil || tools.Tools == nil || len(tools.Tools) != 0 || tools.Outdated == nil {
		t.Fatalf("空列表应为 []（契约）: %v %+v", err, tools)
	}
	if len(tools.Outdated) != 1 || tools.Outdated[0].Latest != "26.13.0" || tools.Outdated[0].Advice == "" {
		t.Fatalf("outdated 契约解析不对: %+v", tools.Outdated)
	}
	issues, err := cl.Doctor(context.Background(), false, false, false)
	if err != nil || len(issues.Issues) != 1 || issues.Issues[0].Code != "PATH_MISSING" {
		t.Fatalf("doctor 退出码 1 但 JSON 合法时不该报错: %v %+v", err, issues)
	}
	if len(issues.Fixed) != 1 {
		t.Fatalf("--fix 的 fixed[] 契约解析不对: %+v", issues)
	}
}
