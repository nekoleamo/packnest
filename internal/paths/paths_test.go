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
		p.CacheAliases():              "/r/cache/alias",
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
	// 缓存别名（R-CACHE-1）必须跟着 PK_CACHE 走：CI 只缓存这个目录
	if got := p.CacheAliases(); got != filepath.Join(want, "alias") {
		t.Errorf("CacheAliases = %q", got)
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

// TestWriteAtomicMode：写用户配置要"原子 + 保留原权限"。
// 裸 os.WriteFile 会在 Ctrl-C/断电时留下截断的 .zshrc；固定 0644 又会把 0600 放宽。
func TestWriteAtomicMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicMode(path, []byte("new\n"), 0); err != nil {
		t.Fatalf("WriteAtomicMode: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new\n" {
		t.Fatalf("内容没写对: %q %v", data, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("权限被改成 %o，应保持 0600", got)
	}
	// 文件不存在时给默认 0644
	fresh := filepath.Join(dir, ".npmrc")
	if err := WriteAtomicMode(fresh, []byte("registry=x\n"), 0); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(fresh); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("新文件应是 0644，实际 %v %v", fi.Mode().Perm(), err)
	}
	// 不留临时文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

// TestLongPath：Windows 长路径前缀化（纯字符串逻辑，任何平台可测）。
// 只有"真的碰文件系统"时才该加前缀：`--json` 输出、shim 内容、PATH 里出现 `\\?\` 都是 bug。
func TestLongPath(t *testing.T) {
	short := `C:\packnest\tools\node\22\bin`
	if got := longPath(short, "windows"); got != short {
		t.Fatalf("短路径不该加前缀: %q", got)
	}
	deep := `C:\Users\someone\Documents\packnest\tools\node\22.22.2\lib\node_modules\npm\node_modules\` +
		strings.Repeat(`nested\`, 20) + `build-ideal-tree.js`
	if len(deep) < 240 {
		t.Fatalf("测试路径要真的超过 240 字符，实际 %d", len(deep))
	}
	if !strings.HasPrefix(longPath(deep, "windows"), `\\?\`) {
		t.Fatalf("深层绝对路径应加 \\\\?\\ 前缀: %q", longPath(deep, "windows"))
	}
	// UNC：\\server\share\... → \\?\UNC\server\share\...
	unc := `\\server\share\` + strings.Repeat(`deep\`, 60)
	if got := longPath(unc, "windows"); !strings.HasPrefix(got, `\\?\UNC\server\share\`) {
		t.Fatalf("UNC 应转成 \\\\?\\UNC\\: %q", got)
	}
	// 已经带前缀的不重复加
	pref := `\\?\C:\` + strings.Repeat(`deep\`, 60)
	if got := longPath(pref, "windows"); strings.Count(got, `\\?\`) != 1 {
		t.Fatalf("不要重复加前缀: %q", got)
	}
	// 相对路径不加（前缀只对绝对路径合法）
	rel := strings.Repeat("deep/", 80)
	if got := longPath(rel, "windows"); got != rel {
		t.Fatalf("相对路径应原样返回: %q", got)
	}
	// 非 Windows 一律原样
	if got := longPath(deep, "darwin"); got != deep {
		t.Fatalf("非 Windows 不该动路径: %q", got)
	}
}
