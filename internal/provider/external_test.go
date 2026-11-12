package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest 在 root/providers 下写一个外部清单并返回路径。
func writeManifest(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, "providers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeIn 直接把清单写进指定目录（PK_PROVIDERS_DIR 的语义：目录本身就装 .toml）。
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// externalTOML 是最小的合法外部清单（https 源，走 dirlist）。
func externalTOML(base string) string {
	return fmt.Sprintf(`id = "demo"
display = "Demo"
kind = "binary"
[index]
kind = "dirlist"
[index.urls]
default = "%[1]s/demo/"
[artifact]
[artifact.urls]
default = "%[1]s/demo/v{version}/demo-{platform}.tar.gz"
[artifact.ext]
darwin = "tar.gz"
linux = "tar.gz"
windows = "tar.gz"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-amd64"
windows-amd64 = "windows-amd64"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "%[1]s/demo/v{version}/SHASUMS256.txt"
[layout]
bins = ["demo"]
`, base)
}

// useRoot 把外部清单目录指到临时根，并在测试结束时恢复进程内缓存。
func useRoot(t *testing.T, root string) {
	t.Helper()
	SetRoot(root)
	t.Cleanup(func() { SetRoot("") })
}

// TestLoadExternalProvider：合法外部清单进 All()，并标 source=external。
func TestLoadExternalProvider(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "demo.toml", externalTOML("https://example.invalid"))
	useRoot(t, root)

	p, err := Get("demo")
	if err != nil {
		t.Fatalf("外部清单应被加载: %v", err)
	}
	if p.Source != SourceExternal || p.Dir != filepath.Join(root, "providers") {
		t.Fatalf("来源标记不对: source=%q dir=%q", p.Source, p.Dir)
	}
	if len(ExternalIssues()) != 0 {
		t.Fatalf("合法清单不该产生 issue: %+v", ExternalIssues())
	}
	// 内嵌的 6 个工具照旧在
	if _, err := Get("node"); err != nil {
		t.Fatalf("内嵌清单不能被外部影响: %v", err)
	}
}

// TestExternalProviderRejects：外部清单的安全边界（方案 §A.4，表驱动）。
func TestExternalProviderRejects(t *testing.T) {
	https := "https://example.invalid"
	cases := []struct {
		name   string
		body   string
		want   string // 期望出现在 issue 里的片段
		httpOK bool
	}{
		{"http 默认拒绝", strings.ReplaceAll(externalTOML(https), https, "http://example.invalid"), "PK_ALLOW_HTTP", false},
		{"绝对路径 single", strings.Replace(externalTOML(https), `bins = ["demo"]`, "bins = [\"demo\"]\nsingle = \"/etc/passwd\"", 1), "相对路径", false},
		{"含 .. 的 single", strings.Replace(externalTOML(https), `bins = ["demo"]`, "bins = [\"demo\"]\nsingle = \"../demo\"", 1), "相对路径", false},
		{"pbs 索引", strings.Replace(externalTOML(https), `kind = "dirlist"`, `kind = "pbs"`, 1), "pbs", false},
		{"未知占位符", strings.ReplaceAll(externalTOML(https), "{version}", "{versoin}"), "未知占位符", false},
		{"id 与文件名不符", strings.Replace(externalTOML(https), `id = "demo"`, `id = "other"`, 1), "文件名", false},
		{"伴生 registry 不存在", externalTOML(https) + "\n[companions.npm]\nmode = \"bundled\"\nregistry = \"nope\"\ntarget = \"lib/node_modules/npm\"\n", "不是已存在的工具 id", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, "demo.toml", c.body)
			if c.httpOK {
				t.Setenv(AllowHTTPEnv, "1")
			}
			useRoot(t, root)

			if _, err := Get("demo"); err == nil {
				t.Fatalf("非法清单不该加载成功")
			}
			issues := ExternalIssues()
			if len(issues) != 1 || issues[0].Code != IssueExternalInvalid {
				t.Fatalf("应有一条 invalid issue: %+v", issues)
			}
			if !strings.Contains(issues[0].Message, c.want) {
				t.Fatalf("issue 应说明原因 %q，实际 %q", c.want, issues[0].Message)
			}
		})
	}
}

// TestExternalProviderConflictIgnored：同 id → 内嵌赢，外部只记 conflict。
func TestExternalProviderConflictIgnored(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "node.toml", strings.ReplaceAll(externalTOML("https://example.invalid"), `id = "demo"`, `id = "node"`))
	useRoot(t, root)

	p, err := Get("node")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != SourceEmbedded {
		t.Fatalf("同 id 必须内嵌赢，实际 source=%q", p.Source)
	}
	issues := ExternalIssues()
	if len(issues) != 1 || issues[0].Code != IssueExternalConflict {
		t.Fatalf("应有一条 conflict issue: %+v", issues)
	}
}

// TestExternalProvidersDirEnv：PK_PROVIDERS_DIR 是追加语义（多目录、PathListSeparator）。
func TestExternalProvidersDirEnv(t *testing.T) {
	root := t.TempDir()
	extra1, extra2 := t.TempDir(), t.TempDir()
	writeManifest(t, root, "demo.toml", externalTOML("https://example.invalid"))
	writeIn(t, extra1, "one.toml", strings.ReplaceAll(externalTOML("https://example.invalid"), `id = "demo"`, `id = "one"`))
	writeIn(t, extra2, "two.toml", strings.ReplaceAll(externalTOML("https://example.invalid"), `id = "demo"`, `id = "two"`))
	t.Setenv(ProvidersDirEnv, extra1+string(os.PathListSeparator)+" "+extra2)
	useRoot(t, root)

	for _, id := range []string{"demo", "one", "two"} {
		if _, err := Get(id); err != nil {
			t.Fatalf("%s 应该被加载（追加语义）: %v", id, err)
		}
	}
}

// TestExternalManifestTooLarge：超过 256KB 的"清单"直接拒绝（防止把二进制当清单）。
func TestExternalManifestTooLarge(t *testing.T) {
	root := t.TempDir()
	body := externalTOML("https://example.invalid") + "\n#" + strings.Repeat("x", MaxManifestBytes)
	writeManifest(t, root, "demo.toml", body)
	useRoot(t, root)

	if _, err := Get("demo"); err == nil {
		t.Fatal("超大文件不该加载")
	}
	if got := ExternalIssues(); len(got) != 1 || !strings.Contains(got[0].Message, "超过上限") {
		t.Fatalf("应报大小超限: %+v", got)
	}
}

// TestExternalHTTPOptInMarkInsecure：PK_ALLOW_HTTP=1 放行 http，并标记 InsecureTransport。
func TestExternalHTTPOptInMarkInsecure(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "demo.toml", strings.ReplaceAll(externalTOML("https://example.invalid"), "https://", "http://"))
	t.Setenv(AllowHTTPEnv, "1")
	useRoot(t, root)

	p, err := Get("demo")
	if err != nil {
		t.Fatalf("放行后应能加载: %v", err)
	}
	if !p.InsecureTransport {
		t.Fatal("http 源必须标记 InsecureTransport（校验强度要降级）")
	}
}

// TestExternalCompanionForwardRef：外部清单的 [companions] 可以指向**后面才加载**的另一个外部清单
// （两遍加载：先收集全部 id，再校验 companions）。
func TestExternalCompanionForwardRef(t *testing.T) {
	root := t.TempDir()
	// aaa.toml 引用 zzz.toml 里定义的 npm 风格 provider（按文件名排序，zzz 在后）
	writeManifest(t, root, "aaa.toml", `id = "aaa"
display = "A"
kind = "binary"
[index]
kind = "dirlist"
[index.urls]
default = "https://example.invalid/aaa/"
[artifact]
[artifact.urls]
default = "https://example.invalid/aaa/{version}/aaa-{platform}.tar.gz"
[artifact.ext]
darwin = "tar.gz"
linux = "tar.gz"
windows = "tar.gz"
[artifact.platform]
darwin-arm64 = "darwin-arm64"
linux-amd64 = "linux-amd64"
windows-amd64 = "windows-amd64"
[checksum]
format = "gnu-sums"
mode = "tofu-then-verify"
[checksum.urls]
default = "https://example.invalid/aaa/SHASUMS256.txt"
[layout]
bins = ["aaa"]
[companions.web]
mode = "pinned"
registry = "zzz"
target = "lib/web"
`)
	writeManifest(t, root, "zzz.toml", strings.ReplaceAll(strings.ReplaceAll(externalTOML("https://example.invalid"), `id = "demo"`, `id = "zzz"`), "demo", "zzz"))
	useRoot(t, root)

	if _, err := Get("aaa"); err != nil {
		t.Fatalf("前向引用的 companions 不该被拒: %v", err)
	}
	for _, is := range ExternalIssues() {
		t.Fatalf("不该有 issue: %+v", is)
	}
}
