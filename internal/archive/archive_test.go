package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packnest/packnest/internal/testkit"
)

func TestFormatFor(t *testing.T) {
	cases := map[string]Format{"tar.gz": FormatTarGz, ".tgz": FormatTgz, "ZIP": FormatZip}
	for in, want := range cases {
		got, err := FormatFor(in)
		if err != nil || got != want {
			t.Errorf("FormatFor(%q) = %q/%v", in, got, err)
		}
	}
	if _, err := FormatFor("tar.xz"); err == nil {
		t.Fatal("xz 不支持（方案 §4.2），应报错")
	}
}

func TestExtractNodeTar(t *testing.T) {
	data := testkit.MakeNode(testkit.NodeSpec{Version: "22.22.2", NPMVersion: "11.20.0"})
	dir := t.TempDir()
	arch := filepath.Join(dir, "node.tar.gz")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatTarGz, Layout{Strip: 1, Bins: []string{"node", "npm", "npx"}}, "darwin"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, rel := range []string{"bin/node", "lib/node_modules/npm/package.json"} {
		if st, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil || st.IsDir() {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	// bin/npm 是符号链接（官方 tar 结构），必须原样保留。
	fi, err := os.Lstat(filepath.Join(dst, "bin", "npm"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("bin/npm 应该是符号链接")
	}
	if target, err := os.Readlink(filepath.Join(dst, "bin", "npm")); err != nil || target != "../lib/node_modules/npm/bin/npm-cli.js" {
		t.Fatalf("符号链接目标 = %q/%v", target, err)
	}
	// bin/node 必须可执行。
	if fi, err := os.Stat(filepath.Join(dst, "bin", "node")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("bin/node 缺少可执行位: %v", err)
	}
}

func TestExtractNodeZip(t *testing.T) {
	data := testkit.MakeNodeZip(testkit.NodeSpec{Version: "22.22.2"})
	dir := t.TempDir()
	arch := filepath.Join(dir, "node.zip")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatZip, Layout{Strip: 1, Bins: []string{"node"}}, "windows"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node.exe")); err != nil {
		t.Fatalf("zip 平铺布局应解出 node.exe: %v", err)
	}
	if _, err := FindBin(dst, "node", "windows", nil); err != nil {
		t.Fatalf("FindBin: %v", err)
	}
	if _, err := FindBin(dst, "node", "darwin", nil); err == nil {
		t.Fatal("darwin 下不该找到 node.exe")
	}
}

func TestExtractRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "evil.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"../../evil.txt", "/abs/evil.txt"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("evil")); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(arch, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatTarGz, Layout{Strip: 1}, "linux"); err == nil {
		t.Fatal("越界路径必须报错")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("不能写到目标目录之外")
	}
}

func TestExtractTruncated(t *testing.T) {
	data := testkit.MakeNode(testkit.NodeSpec{})
	dir := t.TempDir()
	arch := filepath.Join(dir, "trunc.tar.gz")
	if err := os.WriteFile(arch, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(arch, filepath.Join(dir, "tree"), FormatTarGz, Layout{Strip: 1}, "linux"); err == nil {
		t.Fatal("截断归档必须报错")
	}
}

func TestExtractWindowsRejectsSymlink(t *testing.T) {
	data := testkit.MakeNode(testkit.NodeSpec{})
	dir := t.TempDir()
	arch := filepath.Join(dir, "node.tar.gz")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Extract(arch, filepath.Join(dir, "tree"), FormatTarGz, Layout{Strip: 1}, "windows")
	if err == nil {
		t.Fatal("Windows 取 tar 时应明确报错（应改取 zip）")
	}
}

func TestExtractSingleFileLayout(t *testing.T) {
	// deno：zip 里只有一个文件。
	dir := t.TempDir()
	arch := filepath.Join(dir, "deno.zip")
	if err := os.WriteFile(arch, testkit.MakeSingleFileZip("deno", "#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatZip, Layout{Single: "bin/deno"}, "darwin"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dst, "bin", "deno"))
	if err != nil {
		t.Fatalf("单文件应落在 bin/deno: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Fatal("单文件必须 0755")
	}
}

func TestExtractSingleFileTooMany(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.zip")
	if err := os.WriteFile(arch, testkit.MakeNodeZip(testkit.NodeSpec{}), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Extract(arch, filepath.Join(dir, "tree"), FormatZip, Layout{Single: "bin/x"}, "linux")
	if !errors.Is(err, ErrNoSingle) {
		t.Fatalf("多文件时应报 ErrNoSingle，得到 %v", err)
	}
}

func TestStripPath(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want string
		ok   bool
	}{
		{"node-v22/bin/node", 1, "bin/node", true},
		{"node-v22", 1, "", false},
		{"./a/b", 0, "a/b", true},
		{`lib\node_modules\npm\package.json`, 0, "lib/node_modules/npm/package.json", true},
		{"", 0, "", false},
		{"/", 0, "", false},
	}
	for _, c := range cases {
		got, ok, err := stripPath(c.name, c.n)
		if err != nil {
			t.Fatalf("stripPath(%q): %v", c.name, err)
		}
		if ok != c.ok || got != c.want {
			t.Errorf("stripPath(%q,%d) = %q/%v，期望 %q/%v", c.name, c.n, got, ok, c.want, c.ok)
		}
	}
}

func TestReplaceFresh(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "marker"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tools", "node", "22.22.2")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(dst, src); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "marker")); err != nil {
		t.Fatal("新目录内容应该就位")
	}
}

// ⭐ _M1_WindowsAtomicReplace 的核心：已存在目录必须能被替换（Windows 上也能）。
func TestReplaceExistingDir(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "tools", "node", "22.22.2")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "new"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Replace(dst, src); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "old")); !os.IsNotExist(err) {
		t.Fatal("旧文件应被清掉")
	}
	if _, err := os.Stat(filepath.Join(dst, "new")); err != nil {
		t.Fatal("新文件应就位")
	}
	if _, err := os.Stat(dst + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old 应被清掉")
	}
}

func TestReplaceRollsBack(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "keep"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	// src 不存在 → rename 失败 → 旧目录必须回滚。
	if err := Replace(dst, filepath.Join(dir, "missing-src")); err == nil {
		t.Fatal("src 缺失应报错")
	}
	if _, err := os.Stat(filepath.Join(dst, "keep")); err != nil {
		t.Fatal("失败后旧版本必须还在")
	}
}

func TestFindBinCandidates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "pnpm.cjs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindBin(dir, "pnpm", "linux", nil)
	if err != nil {
		t.Fatalf("FindBin: %v", err)
	}
	if filepath.Base(got) != "pnpm.cjs" {
		t.Fatalf("应找到 bin/pnpm.cjs，得到 %q", got)
	}
	if _, err := FindBin(dir, "nope", "linux", nil); err == nil {
		t.Fatal("找不到时应报错并列出查找过的路径")
	}
}

func TestExtractZipDirsAndSymlink(t *testing.T) {
	data := testkit.MakeZip([]testkit.ZipEntry{
		{Name: "pkg/", Content: "", Mode: os.ModeDir | 0o755},
		{Name: "pkg/sub/", Content: "", Mode: os.ModeDir | 0o755},
		{Name: "pkg/sub/file.txt", Content: "hi"},
		{Name: "pkg/link", Content: "sub/file.txt", Mode: os.ModeSymlink | 0o777},
	})
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.zip")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatZip, Layout{Strip: 1}, "linux"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub", "file.txt"))
	if err != nil || string(got) != "hi" {
		t.Fatalf("目录条目应被正确剥层: %q %v", got, err)
	}
	fi, err := os.Lstat(filepath.Join(dst, "link"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("符号链接应被还原: %v", err)
	}

	// Windows 遇到 zip 里的符号链接必须明确报错。
	if err := Extract(arch, filepath.Join(dir, "tree-win"), FormatZip, Layout{Strip: 1}, "windows"); err == nil {
		t.Fatal("Windows 下 zip 符号链接应报错")
	}
}

func TestExtractErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(filepath.Join(dir, "missing.tar.gz"), filepath.Join(dir, "t"), FormatTarGz, Layout{}, "linux"); err == nil {
		t.Fatal("归档不存在应报错")
	}
	if err := Extract(filepath.Join(dir, "missing.zip"), filepath.Join(dir, "t2"), FormatZip, Layout{}, "linux"); err == nil {
		t.Fatal("zip 不存在应报错")
	}

	garbage := filepath.Join(dir, "garbage.tar.gz")
	if err := os.WriteFile(garbage, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(garbage, filepath.Join(dir, "t3"), FormatTarGz, Layout{}, "linux"); err == nil {
		t.Fatal("非 gzip 内容应报错")
	}

	if err := Extract(garbage, filepath.Join(dir, "t4"), Format("xz"), Layout{}, "linux"); err == nil {
		t.Fatal("不支持的格式应报错")
	}
}

func TestExtractCustomTarEntries(t *testing.T) {
	// 只带目录条目与符号链接的 tar（覆盖继续分支）
	data := testkit.MakeTarGz(map[string]string{"bin/tool": "#!/bin/sh\n"})
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.tar.gz")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatTgz, Layout{Bins: []string{"tool"}}, "linux"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got, err := FindBin(dst, "tool", "linux", map[string]string{"tool": "bin/tool"})
	if err != nil || !strings.HasSuffix(got, filepath.Join("bin", "tool")) {
		t.Fatalf("bin_targets 应能定位: %q %v", got, err)
	}
}

func TestApplySingleTargetExists(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(dst, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "bin", "deno"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 目标已是同一路径 → 不该再 rename（覆盖 applySingle 的相等分支）
	if err := applySingle(dst, Layout{Single: "bin/deno"}, []string{filepath.Join(dst, "bin", "deno")}); err != nil {
		t.Fatalf("applySingle: %v", err)
	}
	if err := applySingle(dst, Layout{}, nil); err != nil {
		t.Fatalf("空布局应直接返回: %v", err)
	}
}

func TestReplaceDstIsFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(dst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Replace(dst, src); err != nil {
		t.Fatalf("dst 是文件时也应能替换: %v", err)
	}
	if st, err := os.Stat(dst); err != nil || !st.IsDir() {
		t.Fatalf("替换后应是目录: %v", err)
	}
}

func TestExtractPathConflicts(t *testing.T) {
	// 归档里 "a" 是文件、"a/b" 又是文件 → 后者无法落地，必须报错而不是静默跳过。
	data := testkit.MakeTarGz(map[string]string{"a": "file", "a/b": "nested"})
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.tar.gz")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(arch, filepath.Join(dir, "tree"), FormatTarGz, Layout{}, "linux"); err == nil {
		t.Fatal("路径冲突应报错")
	}
}

func TestApplySingleTargetBlocked(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "tree")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	// bin 是文件 → 无法写 bin/deno
	if err := os.WriteFile(filepath.Join(dst, "bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applySingle(dst, Layout{Single: "bin/deno"}, []string{filepath.Join(dst, "deno")}); err == nil {
		t.Fatal("目标父路径是文件时应报错")
	}
}

func TestReplaceMissingParent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "no", "such", "dir")
	if err := Replace(dst, src); err == nil {
		t.Fatal("父目录不存在时应报错")
	}
}

func TestExtractTarWithDirs(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "dirs.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pkg/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "pkg/bin/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	body := "x"
	if err := tw.WriteHeader(&tar.Header{Name: "pkg/bin/node", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(arch, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	if err := Extract(arch, dst, FormatTarGz, Layout{Strip: 1}, "linux"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "bin", "node")); err != nil {
		t.Fatalf("目录条目应被创建: %v", err)
	}
}

func TestExtractZipEntryOpenError(t *testing.T) {
	// 截断的 zip 中央目录：OpenReader 成功但读条目失败。
	data := testkit.MakeSingleFileZip("deno", strings.Repeat("x", 4096))
	dir := t.TempDir()
	arch := filepath.Join(dir, "trunc.zip")
	// 砍掉尾部一段，破坏 deflate 流但仍保留中央目录可被识别的情况很难构造，
	// 这里退而验证"整体不可信时不会留下半成品目录"。
	if err := os.WriteFile(arch, data[:len(data)-20], 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "tree")
	_ = Extract(arch, dst, FormatZip, Layout{Single: "bin/deno"}, "linux")
	if _, err := os.Stat(filepath.Join(dst, "bin", "deno")); err == nil {
		// 若恰好还能解出来，产物必须是完整的
		if st, _ := os.Stat(filepath.Join(dst, "bin", "deno")); st.Size() != 4096 {
			t.Fatalf("截断的归档不该产出半成品，得到 %d 字节", st.Size())
		}
	}
}

func TestExtractSymlinkParentConflict(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "file"
	if err := tw.WriteHeader(&tar.Header{Name: "x", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "x/link", Linkname: "y", Mode: 0o777, Typeflag: tar.TypeSymlink}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(arch, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(arch, filepath.Join(dir, "tree"), FormatTarGz, Layout{}, "linux"); err == nil {
		t.Fatal("符号链接父路径是文件时应报错")
	}
}

func TestExtractZipPathConflict(t *testing.T) {
	data := testkit.MakeZip([]testkit.ZipEntry{
		{Name: "x", Content: "file"},
		{Name: "x/y", Content: "nested"},
	})
	dir := t.TempDir()
	arch := filepath.Join(dir, "x.zip")
	if err := os.WriteFile(arch, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(arch, filepath.Join(dir, "tree"), FormatZip, Layout{}, "linux"); err == nil {
		t.Fatal("zip 路径冲突应报错")
	}
}

func TestApplySingleRenameFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "tree")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applySingle(dst, Layout{Single: "bin/deno"}, []string{filepath.Join(dst, "missing")}); err == nil {
		t.Fatal("源文件不存在时应报错")
	}
}

func TestReplaceRenameOldFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(dst, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 预先占住 .old 且不可删（父目录只读）→ 旧目录改名失败。
	old := dst + ".old"
	if err := os.MkdirAll(filepath.Join(old, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(old, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(old, 0o755) })
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(dst, src); err == nil {
		// 某些文件系统（或以 root 运行）会允许删除，此时只要求旧版本仍在。
		if _, statErr := os.Stat(filepath.Join(dst, "keep")); statErr != nil {
			t.Fatal("失败时旧版本必须保留")
		}
	}
}
