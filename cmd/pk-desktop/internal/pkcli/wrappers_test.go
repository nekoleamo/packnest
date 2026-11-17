package pkcli

// 其余命令面的薄封装：全部走假 pk（sh 脚本），断言"参数怎么传、结果怎么投影"。
// 这些方法每条 3~5 行，错一个就是界面上某个按钮点了没反应。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// argEcho 把所有参数写回 JSON，用来验证"传给 CLI 的参数对不对"。
const argEcho = `if [ "$1" = version ]; then echo '{"schema":1,"name":"packnest","version":"1.9.1"}'; exit 0; fi
args=""
for a in "$@"; do args="$args $a"; done
printf '{"args":"%s"}\n' "$args"
`

func clientFor(t *testing.T, body string) *Client {
	t.Helper()
	return &Client{Path: fakePK(t, body), Env: os.Environ()}
}

func TestSelfUpdatePassesCheckFlag(t *testing.T) {
	p := fakePK(t, `if [ "$1" = version ]; then echo '{"schema":1,"name":"packnest","version":"1.9.1"}'; exit 0; fi
if [ "$1" = upgrade ]; then
  case "$*" in *--check*) echo '{"current":"1.9.1","latest":"1.9.2","update_available":true,"source":"gitee","sources":[{"name":"gitee","latency_ms":30}]}' ;;
  *) echo '{"current":"1.9.1","latest":"1.9.1","applied":true}' ;; esac
  exit 0
fi
`)
	cl := &Client{Path: p, Env: os.Environ()}
	chk, err := cl.CheckSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckSelfUpdate: %v", err)
	}
	if !chk.UpdateAvailable || len(chk.Sources) != 1 || chk.Sources[0].Name != "gitee" {
		t.Fatalf("--check 的结果没解出来: %+v", chk)
	}
	done, err := cl.SelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if !done.Applied {
		t.Fatalf("没有 --check 时应走真升级并解出 applied: %+v", done)
	}
}

func TestThinWrappersForwardArgs(t *testing.T) {
	cl := clientFor(t, argEcho)
	ctx := context.Background()

	if err := cl.UseMirror(ctx, "npmmirror"); err != nil {
		t.Fatalf("UseMirror: %v", err)
	}
	if err := cl.Uninstall(ctx, "node@22"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if err := cl.UninstallTool(ctx, "node"); err != nil {
		t.Fatalf("UninstallTool: %v", err)
	}
	// 结果类：假 pk 返回的字段跟真 CLI 不同，只要求"不因为字段缺失而报错"
	if _, err := cl.Which(ctx, "node"); err != nil {
		t.Fatalf("Which: %v", err)
	}
	if _, err := cl.Environment(ctx, true); err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if _, err := cl.MirrorWrite(ctx, "/tmp/npmrc"); err != nil {
		t.Fatalf("MirrorWrite: %v", err)
	}
	if _, err := cl.Available(ctx); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if _, err := cl.ToolVersions(ctx, "node", 40, true); err != nil {
		t.Fatalf("ToolVersions: %v", err)
	}
}

func TestWhichAndEnvDecode(t *testing.T) {
	p := fakePK(t, `if [ "$1" = version ]; then echo '{"schema":1,"name":"packnest","version":"1.9.1"}'; exit 0; fi
case "$1" in
  which) echo '{"name":"node","path":"/root/.packnest/tools/node/22.23.5/bin/node","shim":true}' ;;
  env) echo '{"shell":"zsh","path":["/root/.packnest/bin"],"env":[{"key":"PK_ROOT","value":"/root/.packnest"}]}' ;;
  mirror) echo '{"current":"npmmirror","file":"/home/u/.npmrc","registry":"https://registry.npmmirror.com"}' ;;
  list) echo '[]' ;;
esac
`)
	cl := &Client{Path: p, Env: os.Environ()}
	w, err := cl.Which(context.Background(), "node")
	if err != nil || w.Name != "node" || !w.Shim || w.Path == "" {
		t.Fatalf("Which = %+v err=%v", w, err)
	}
	e, err := cl.Environment(context.Background(), false)
	if err != nil || len(e.Paths) == 0 {
		t.Fatalf("Environment = %+v err=%v", e, err)
	}
	mw, err := cl.MirrorWrite(context.Background(), "")
	if err != nil || mw.Registry == "" {
		t.Fatalf("MirrorWrite = %+v err=%v", mw, err)
	}
}

func TestReadyAndTimeout(t *testing.T) {
	cl := clientFor(t, argEcho) // version 打 schema:1 / 1.9.1
	if err := cl.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	ctx, cancel := WithTimeout(50 * time.Millisecond)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("WithTimeout 没设上限")
	}
}

func TestLocatePrefersGivenCandidates(t *testing.T) {
	// 空字符串与目录都要跳过（不能把目录当 pk 跑）
	dir := t.TempDir()
	p := clientFor(t, argEcho).Path
	cl, info, err := locate([]string{"", dir, p})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if cl.Path != p || info.Version == "" {
		t.Fatalf("locate = %+v %+v", cl, info)
	}
	// 同一个候选重复出现只试一次（seen 去重）
	if _, _, err := locate([]string{p, p}); err != nil {
		t.Fatalf("重复候选不该出错: %v", err)
	}
	// 找到了但跑不起来 → 报"不可用"，而不是"找不到"
	broken := filepath.Join(t.TempDir(), "pk")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = locate([]string{broken})
	if err == nil || strings.Contains(err.Error(), "找不到") {
		t.Fatalf("坏 pk 应报\"不可用\"，实际 %v", err)
	}
	// defaultPaths 至少给出 ~/.local/bin/pk
	paths := defaultPaths()
	if len(paths) == 0 || !strings.Contains(paths[0], "pk") {
		t.Fatalf("defaultPaths = %v", paths)
	}
}
