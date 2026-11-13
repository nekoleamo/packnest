package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnvOverride(t *testing.T) {
	t.Setenv(RootEnv, "~/tmp-root")
	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	home, _ := os.UserHomeDir()
	if p.Root != filepath.Join(home, "tmp-root") {
		t.Fatalf("Root = %q，期望 %q", p.Root, filepath.Join(home, "tmp-root"))
	}
}

func TestResolveDefault(t *testing.T) {
	t.Setenv(RootEnv, "")
	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	home, _ := os.UserHomeDir()
	if p.Root != filepath.Join(home, ".packnest") {
		t.Fatalf("Root = %q，期望 ~/.packnest", p.Root)
	}
}

func TestLayout(t *testing.T) {
	p := Paths{Root: "/r"}
	cases := map[string]string{
		p.Bin():           "/r/bin",
		p.Tools():         "/r/tools",
		p.Tool("node"):    "/r/tools/node",
		p.Ledger():        "/r/state/installs.json",
		p.Known():         "/r/state/known.json",
		p.Probe():         "/r/state/probe.json",
		p.Config():        "/r/config.toml",
		p.CacheArchives(): "/r/cache/archives",
		p.CacheMeta():     "/r/cache/meta",
		p.GlobalBin():     "/r/global/bin",
		p.GlobalEtc():     "/r/global/etc",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("layout = %q，期望 %q", got, want)
		}
	}
	if got := p.ToolVersion("node", "22.22.2"); got != "/r/tools/node/22.22.2" {
		t.Errorf("ToolVersion = %q", got)
	}
	segs := p.PathSegments()
	// 五段、顺序固定（方案 B2-2）：shim → corepack → npm 全局 bin → PNPM_HOME → BUN_INSTALL/bin
	want := []string{"/r/bin", "/r/global/corepack/bin", "/r/global/bin", "/r/global/pnpm", "/r/global/bun/bin"}
	if strings.Join(segs, ":") != strings.Join(want, ":") {
		t.Errorf("PathSegments = %v，期望 %v", segs, want)
	}
}

func TestNewRejectsEmpty(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("空根目录应报错")
	}
}

func TestSafeJoin(t *testing.T) {
	ok := map[string]string{
		"a/b":     "a/b",
		"a/./b/c": "a/b/c",
	}
	for in, want := range ok {
		got, err := SafeJoin("/base", in)
		if err != nil {
			t.Fatalf("SafeJoin(%q): %v", in, err)
		}
		if got != filepath.Join("/base", filepath.FromSlash(want)) {
			t.Errorf("SafeJoin(%q) = %q", in, got)
		}
	}
	for _, bad := range []string{"../x", "/abs/path", `C:\x`, "a/../../b", ".."} {
		if _, err := SafeJoin("/base", bad); err == nil {
			t.Errorf("SafeJoin(%q) 应报错", bad)
		}
	}
}

func TestWindowsSeparatorsAccepted(t *testing.T) {
	got, err := SafeJoin("/base", `lib\node_modules\npm\package.json`)
	if err != nil {
		t.Fatalf("SafeJoin: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("lib", "node_modules", "npm", "package.json")) {
		t.Errorf("got %q", got)
	}
}

func TestRemainingLayoutHelpers(t *testing.T) {
	p := Paths{Root: "/r"}
	cases := map[string]string{
		p.State():                     "/r/state",
		p.CacheIndex():                "/r/state/cache.json",
		p.Lock():                      "/r/state/lock",
		p.Logs():                      "/r/logs",
		p.Global():                    "/r/global",
		p.GlobalPnpmBin():             "/r/global/pnpm",
		p.GlobalBunBin():              "/r/global/bun/bin",
		p.CacheArchives():             "/r/cache/archives",
		p.ToolVersion("go", "1.23.4"): "/r/tools/go/1.23.4",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("layout = %q，期望 %q", got, want)
		}
	}
}

func TestCacheEnvOverride(t *testing.T) {
	t.Setenv(RootEnv, "")
	t.Setenv(CacheEnv, "~/ci-cache")
	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "ci-cache")
	if p.Cache() != want {
		t.Fatalf("Cache = %q，期望 %q", p.Cache(), want)
	}
	if got := p.CacheArchives(); got != filepath.Join(want, "archives") {
		t.Errorf("CacheArchives = %q", got)
	}
	if got := p.CacheMeta(); got != filepath.Join(want, "meta") {
		t.Errorf("CacheMeta = %q", got)
	}
	if got := p.CacheDir("corepack"); got != filepath.Join(want, "corepack") {
		t.Errorf("CacheDir = %q", got)
	}
	// 台账/锁/产物目录不受影响，仍在 PK_ROOT 里
	if got := p.Ledger(); got == "" || filepath.Dir(got) != filepath.Join(p.Root, "state") {
		t.Errorf("Ledger = %q，应留在 PK_ROOT", got)
	}
}

func TestCacheDefaultsUnderRoot(t *testing.T) {
	t.Setenv(CacheEnv, "")
	p := Paths{Root: "/r"}
	if got := p.Cache(); got != "/r/cache" {
		t.Fatalf("Cache = %q，期望 /r/cache", got)
	}
	if got := p.CacheDir("npm"); got != "/r/cache/npm" {
		t.Fatalf("CacheDir = %q", got)
	}
}

// WriteAtomic 是全局唯一一份原子落盘：自动建父目录、失败清 .tmp、内容完整替换。
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state", "deep", "x.json")
	if err := WriteAtomic(target, []byte("v1")); err != nil {
		t.Fatalf("应自动建父目录: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "v1" {
		t.Fatalf("写入内容不对: %q %v", got, err)
	}
	if err := WriteAtomic(target, []byte("v2")); err != nil {
		t.Fatalf("覆盖写失败: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "v2" {
		t.Fatalf("覆盖后内容不对: %q", got)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("不该留下 .tmp")
	}

	// 目标是个目录 → rename 失败，且不留临时文件
	blocked := t.TempDir()
	if err := WriteAtomic(blocked, []byte("x")); err == nil {
		t.Fatal("目标是目录应报错")
	}
	if _, err := os.Stat(blocked + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("失败应清掉 .tmp")
	}

	// 父路径是个文件 → 建目录失败
	notDir := filepath.Join(dir, "afile")
	if err := os.WriteFile(notDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(notDir, "y.json"), []byte("x")); err == nil {
		t.Fatal("父路径是文件应报错")
	}
}
