//go:build !windows

package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEnableDisableWritesUserLevelEntry：默认关；打开只写用户级文件；关闭只删自己那一条。
func TestEnableDisableWritesUserLevelEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	st := Get()
	if !st.Supported {
		t.Fatalf("unix 平台应支持自启: %+v", st)
	}
	if st.Enabled {
		t.Fatalf("默认必须是关的: %+v", st)
	}
	if want := filepath.Join(home, st.Path[len(home):]); want != st.Path {
		t.Fatalf("条目路径应在 $HOME 下: %s", st.Path)
	}

	on, err := Set(true)
	if err != nil {
		t.Fatalf("打开自启: %v", err)
	}
	if !on.Enabled {
		t.Fatalf("打开后应报告已启用: %+v", on)
	}
	data, err := os.ReadFile(on.Path)
	if err != nil {
		t.Fatalf("应写出自启条目: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("自启条目不能是空文件")
	}

	off, err := Set(false)
	if err != nil {
		t.Fatalf("关闭自启: %v", err)
	}
	if off.Enabled {
		t.Fatalf("关闭后应报告未启用: %+v", off)
	}
	if _, err := os.Stat(off.Path); !os.IsNotExist(err) {
		t.Fatal("关闭后条目文件应被删除")
	}
	// 再关一次：幂等，不该报错
	if _, err := Set(false); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}
}

// TestPlistEscapesPath：路径里有 `&` / `<` 时 plist 必须仍是合法 XML
// （直接 Sprintf 会写出非法 plist，launchd 静默不生效）。
func TestPlistEscapesPath(t *testing.T) {
	if got := xmlEscape("/Users/tom & jerry/app<1>"); got != "/Users/tom &amp; jerry/app&lt;1&gt;" {
		t.Fatalf("xmlEscape = %q", got)
	}
	if runtime.GOOS != "darwin" {
		t.Skip("只有 macOS 写 plist")
	}
	path := filepath.Join(t.TempDir(), "x.plist")
	if err := add(Status{Self: "/Users/a & b/pk-desktop", Path: path}); err != nil {
		t.Fatalf("写 plist: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), " & ") {
		t.Fatalf("plist 里还有没转义的 &：%s", data)
	}
	if !strings.Contains(string(data), "&amp;") {
		t.Fatalf("plist 里应有转义后的 &amp;：%s", data)
	}
}
