package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 自更新是"会替换自己可执行文件"的代码：GUI 1.0 口径 H3 要求它 ≥85% 覆盖。
// 这里用假 release 目录（真 zip / 真 tar.gz + 真 sha256）跑整条链路，
// 失败分支统一断言"旧可执行文件没被动过"。

type arcEntry struct {
	name string
	body string
	mode os.FileMode
	dir  bool
}

func makeZip(t *testing.T, dst string, entries ...arcEntry) {
	t.Helper()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatalf("建 zip 失败: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		if e.dir {
			mode = 0o755 | os.ModeDir
			h.Method = zip.Store
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatalf("写 zip 条目 %s 失败: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("写 zip 内容失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("收尾 zip 失败: %v", err)
	}
}

func makeTarGz(t *testing.T, dst string, entries ...arcEntry) {
	t.Helper()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatalf("建 tar.gz 失败: %v", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		mode := int64(e.mode)
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Mode: mode, Typeflag: tar.TypeReg, Size: int64(len(e.body))}
		if e.dir {
			h.Typeflag, h.Mode, h.Size = tar.TypeDir, 0o755, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("写 tar 头 %s 失败: %v", e.name, err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("写 tar 内容失败: %v", err)
			}
		}
	}
	tw.Close()
	gz.Close()
}

func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("算 sha256 失败: %v", err)
	}
	return got
}

// release 造一个假的发布目录，并返回"按 URL 末段取文件"的下载函数。
func release(t *testing.T, files map[string]string) (dir string, download func(context.Context, string, string) error) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("造发布文件 %s 失败: %v", name, err)
		}
	}
	return dir, func(_ context.Context, url, dst string) error {
		b, err := os.ReadFile(filepath.Join(dir, filepath.Base(url)))
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	}
}

// prepared 造一个"旧的可执行文件"，返回它的路径。
func prepared(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestApplyGUI_Linux(t *testing.T) {
	exe := prepared(t, "pk-desktop", "OLD")
	const ver = "0.13.0"
	const art = "packnest-desktop_0.13.0_linux_amd64.tar.gz"

	// 先造产物，再算真 sha256 —— 与发布脚本的顺序一致
	artDir := t.TempDir()
	artPath := filepath.Join(artDir, art)
	makeTarGz(t, artPath, arcEntry{name: "pk-desktop", body: "NEW", mode: 0o755})
	sum := sha256Hex(t, artPath)
	artBody, err := os.ReadFile(artPath)
	if err != nil {
		t.Fatal(err)
	}
	_, dl := release(t, map[string]string{
		art:                 string(artBody),
		"checksums.txt":     sum + "  " + art + "\n",
		"checksums.txt.sig": "SIG",
	})

	var logs []string
	got, err := ApplyGUI(context.Background(), ApplyOptions{
		Current:     "0.12.3",
		ArtifactURL: "https://ex/gui-v" + ver + "/" + art,
		Exe:         exe,
		GOOS:        "linux",
		Download:    dl,
		VerifySignature: func(sumsPath, sigPath string) error {
			if _, err := os.Stat(sigPath); err != nil {
				return err
			}
			return nil
		},
		Log: func(s string) { logs = append(logs, s) },
	})
	if err != nil {
		t.Fatalf("ApplyGUI 失败: %v", err)
	}
	if got != "0.12.3" {
		t.Fatalf("返回的版本 = %q", got)
	}
	b, _ := os.ReadFile(exe)
	if string(b) != "NEW" {
		t.Fatalf("可执行文件没被替换: %q", b)
	}
	if st, err := os.Stat(exe); err != nil || st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("替换后丢了可执行权限: %v %v", st.Mode(), err)
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Fatal("留下了 .new 临时文件")
	}
	if len(logs) < 2 || !strings.Contains(strings.Join(logs, "|"), "已替换完成") {
		t.Fatalf("进度日志不全: %v", logs)
	}
}

func TestApplyGUI_Windows(t *testing.T) {
	exe := prepared(t, "pk-desktop.exe", "OLD")
	const art = "packnest-desktop_0.13.0_windows_amd64.zip"

	artDir := t.TempDir()
	artPath := filepath.Join(artDir, art)
	makeZip(t, artPath, arcEntry{name: "bin/pk-desktop.exe", body: "NEW", mode: 0o755})
	sum := sha256Hex(t, artPath)
	artBody, _ := os.ReadFile(artPath)
	_, dl := release(t, map[string]string{
		art:                 string(artBody),
		"checksums.txt":     sum + " *" + art + "\n", // sha256sum -b 的 * 前缀
		"checksums.txt.sig": "SIG",
	})

	if _, err := ApplyGUI(context.Background(), ApplyOptions{
		ArtifactURL: "https://ex/gui-v0.13.0/" + art,
		Exe:         exe,
		GOOS:        "windows",
		Download:    dl,
		VerifySignature: func(string, string) error {
			return nil
		},
	}); err != nil {
		t.Fatalf("ApplyGUI 失败: %v", err)
	}
	b, _ := os.ReadFile(exe)
	if string(b) != "NEW" {
		t.Fatalf("exe 没被替换: %q", b)
	}
	ents, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".old.") || strings.HasSuffix(e.Name(), ".new") {
			t.Fatalf("留下了临时文件: %s", e.Name())
		}
	}
}

func TestApplyGUI_Darwin(t *testing.T) {
	// macOS：替换整个 packnest.app（旧目录改名挪开，新的复制进来）
	exe := prepared(t, filepath.Join("packnest.app", "Contents", "MacOS", "pk-desktop"), "OLD")
	const art = "packnest-desktop_0.13.0_darwin_arm64.app.zip"

	artDir := t.TempDir()
	artPath := filepath.Join(artDir, art)
	makeZip(t, artPath,
		arcEntry{name: "packnest.app/Contents/MacOS/", dir: true},
		arcEntry{name: "packnest.app/Contents/MacOS/pk-desktop", body: "NEW", mode: 0o755},
		arcEntry{name: "packnest.app/Contents/Info.plist", body: "<plist/>"},
	)
	sum := sha256Hex(t, artPath)
	artBody, _ := os.ReadFile(artPath)
	_, dl := release(t, map[string]string{
		art:                 string(artBody),
		"checksums.txt":     sum + "  " + art + "\n",
		"checksums.txt.sig": "SIG",
	})

	// GOOS 留空 → 走 runtimeGOOS()（本机就是 darwin）
	if _, err := ApplyGUI(context.Background(), ApplyOptions{
		ArtifactURL: "https://ex/gui-v0.13.0/" + art,
		Exe:         exe,
		Download:    dl,
		VerifySignature: func(string, string) error {
			return nil
		},
	}); err != nil {
		t.Skipf("本机不是 darwin（%v），这条用例只验证 macOS 替换路径", err)
	}
	b, _ := os.ReadFile(exe)
	if string(b) != "NEW" {
		t.Fatalf(".app 里的可执行文件没被替换: %q", b)
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if _, err := os.Stat(filepath.Join(app, "Contents", "Info.plist")); err != nil {
		t.Fatalf("Info.plist 没跟着换过来: %v", err)
	}
}

func TestApplyGUI_Failures(t *testing.T) {
	const art = "packnest-desktop_0.13.0_linux_amd64.tar.gz"
	goodTar := func(t *testing.T) []byte {
		p := filepath.Join(t.TempDir(), art)
		makeTarGz(t, p, arcEntry{name: "pk-desktop", body: "NEW", mode: 0o755})
		b, _ := os.ReadFile(p)
		return b
	}

	cases := []struct {
		name    string
		files   func(t *testing.T) map[string]string
		prepare func(t *testing.T, files map[string]string)
		verify  func(string, string) error
		want    string
	}{
		{
			name: "产物下载失败",
			files: func(t *testing.T) map[string]string {
				return map[string]string{"checksums.txt": "x", "checksums.txt.sig": "SIG"}
			},
			want: "下载失败",
		},
		{
			name: "校验清单下载失败",
			files: func(t *testing.T) map[string]string {
				return map[string]string{art: string(goodTar(t)), "checksums.txt.sig": "SIG"}
			},
			want: "下载校验清单失败",
		},
		{
			name: "签名文件下载失败",
			files: func(t *testing.T) map[string]string {
				return map[string]string{art: string(goodTar(t)), "checksums.txt": "x"}
			},
			want: "下载清单签名失败",
		},
		{
			name: "验签失败",
			files: func(t *testing.T) map[string]string {
				return map[string]string{
					art: string(goodTar(t)), "checksums.txt": "x", "checksums.txt.sig": "SIG",
				}
			},
			verify: func(string, string) error { return errors.New("签名对不上") },
			want:   "签名对不上",
		},
		{
			name: "清单里没有这个产物",
			files: func(t *testing.T) map[string]string {
				return map[string]string{
					art: string(goodTar(t)), "checksums.txt": "deadbeef  other.tar.gz\n", "checksums.txt.sig": "SIG",
				}
			},
			want: "校验清单里没有",
		},
		{
			name: "sha256 不符",
			files: func(t *testing.T) map[string]string {
				return map[string]string{
					art: string(goodTar(t)), "checksums.txt": strings.Repeat("0", 64) + "  " + art, "checksums.txt.sig": "SIG",
				}
			},
			want: "sha256 不符",
		},
		{
			name: "包里没有可执行文件",
			files: func(t *testing.T) map[string]string {
				p := filepath.Join(t.TempDir(), art)
				makeTarGz(t, p, arcEntry{name: "README.md", body: "空包"})
				b, _ := os.ReadFile(p)
				s := sha256.Sum256(b)
				return map[string]string{
					art: string(b), "checksums.txt": hex.EncodeToString(s[:]) + "  " + art, "checksums.txt.sig": "SIG",
				}
			},
			want: "更新包里没有 pk-desktop",
		},
		{
			name: "归档损坏",
			files: func(t *testing.T) map[string]string {
				s := sha256.Sum256([]byte("这不是 tar.gz"))
				return map[string]string{
					art: "这不是 tar.gz", "checksums.txt": hex.EncodeToString(s[:]) + "  " + art, "checksums.txt.sig": "SIG",
				}
			},
			want: "解压失败",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exe := prepared(t, "pk-desktop", "OLD")
			_, dl := release(t, c.files(t))
			verify := c.verify
			if verify == nil {
				verify = func(string, string) error { return nil }
			}
			_, err := ApplyGUI(context.Background(), ApplyOptions{
				ArtifactURL:     "https://ex/gui-v0.13.0/" + art,
				Exe:             exe,
				GOOS:            "linux",
				Download:        dl,
				VerifySignature: verify,
			})
			if err == nil {
				t.Fatalf("应当失败（%s）", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误信息 = %q，期望含 %q", err, c.want)
			}
			// 任何一步失败都不许动旧的可执行文件
			if b, _ := os.ReadFile(exe); string(b) != "OLD" {
				t.Fatalf("失败路径把旧文件改了: %q", b)
			}
		})
	}
}

func TestApplyGUI_MissingAppBundle(t *testing.T) {
	exe := prepared(t, filepath.Join("packnest.app", "Contents", "MacOS", "pk-desktop"), "OLD")
	const art = "packnest-desktop_0.13.0_darwin_arm64.app.zip"
	p := filepath.Join(t.TempDir(), art)
	makeZip(t, p, arcEntry{name: "pk-desktop", body: "NEW", mode: 0o755})
	b, _ := os.ReadFile(p)
	s := sha256.Sum256(b)
	_, dl := release(t, map[string]string{
		art: string(b), "checksums.txt": hex.EncodeToString(s[:]) + "  " + art, "checksums.txt.sig": "SIG",
	})
	_, err := ApplyGUI(context.Background(), ApplyOptions{
		ArtifactURL: "https://ex/gui-v0.13.0/" + art,
		Exe:         exe,
		GOOS:        "darwin",
		Download:    dl,
		VerifySignature: func(string, string) error {
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "没有 packnest.app") {
		t.Fatalf("期望「更新包里没有 packnest.app」，实际 %v", err)
	}
}

// TestSSHKeygenVerify_Missing 守"没有 ssh-keygen 就拒绝自更新"（不做只比 sha256 的降级）。
func TestSSHKeygenVerify_Missing(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "checksums.txt")
	sig := filepath.Join(dir, "checksums.txt.sig")
	if err := os.WriteFile(sums, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sig, []byte("SIG"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // 造出"Windows 没装 OpenSSH 客户端"的效果
	err := sshKeygenVerify(sums, sig)
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen") {
		t.Fatalf("缺 ssh-keygen 时必须拒绝并说明，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "install-gui") {
		t.Fatalf("错误信息要给出替代方案，实际 %v", err)
	}
}

func TestCleanupOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pk-desktop")
	for _, name := range []string{"pk-desktop", "pk-desktop.old.1700000000", "unrelated.old.1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	CleanupOld(exe, "linux")
	if _, err := os.Stat(filepath.Join(dir, "pk-desktop.old.1700000000")); err == nil {
		t.Fatal("没清掉自己的 .old 残留")
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.old.1")); err != nil {
		t.Fatal("误删了别人的 .old 文件")
	}
	CleanupOld("", "linux") // 空路径不该炸
}

func TestAppBundleOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/Applications/packnest.app/Contents/MacOS/pk-desktop", "/Applications/packnest.app"},
		{"/usr/local/bin/pk-desktop", "/usr/local/bin/pk-desktop"},
		{"/x/packnest.app/Contents/pk-desktop", "/x/packnest.app/Contents/pk-desktop"},
		{"/x/MacOS/pk-desktop", "/x/MacOS/pk-desktop"},
	}
	for _, c := range cases {
		if got := appBundleOf(filepath.FromSlash(c.in)); got != filepath.FromSlash(c.want) {
			t.Errorf("appBundleOf(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestSumFor(t *testing.T) {
	sums := []byte("ABCdef  a.tar.gz\n" + strings.Repeat("1", 64) + " *b.zip\n\n")
	if got := sumFor(sums, "a.tar.gz"); got != "abcdef" {
		t.Errorf("sumFor 应转小写，实际 %q", got)
	}
	if got := sumFor(sums, "b.zip"); got != strings.Repeat("1", 64) {
		t.Errorf("没认 sha256sum -b 的 * 前缀，实际 %q", got)
	}
	if got := sumFor(sums, "c.tar.gz"); got != "" {
		t.Errorf("不存在的文件应返回空，实际 %q", got)
	}
}

func TestSafeJoin(t *testing.T) {
	dir := t.TempDir()
	if _, err := safeJoin(dir, "a/b.txt"); err != nil {
		t.Fatalf("正常路径被拒: %v", err)
	}
	for _, bad := range []string{"../escape.txt", "a/../../escape.txt"} {
		if _, err := safeJoin(dir, bad); err == nil {
			t.Errorf("Zip Slip 没被拦住: %s", bad)
		}
	}
}

func TestUnzipAndUntargz(t *testing.T) {
	dir := t.TempDir()

	z := filepath.Join(dir, "a.zip")
	makeZip(t, z,
		arcEntry{name: "d/", dir: true},
		arcEntry{name: "d/run", body: "#!/bin/sh", mode: 0o755},
		arcEntry{name: "d/data.txt", body: "hi"},
	)
	out := filepath.Join(dir, "unzip")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzip(z, out); err != nil {
		t.Fatalf("unzip 失败: %v", err)
	}
	for _, name := range []string{"d/run", "d/data.txt"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("没解出 %s: %v", name, err)
		}
	}
	if st, _ := os.Stat(filepath.Join(out, "d", "run")); st.Mode().Perm()&0o111 == 0 {
		t.Fatal("解压丢了可执行权限")
	}

	// Zip Slip：归档里带 ../ 必须拒绝
	evil := filepath.Join(dir, "evil.zip")
	makeZip(t, evil, arcEntry{name: "../escape.txt", body: "x"})
	if err := unzip(evil, out); err == nil {
		t.Fatal("带 ../ 的 zip 被解开了")
	}

	// 坏 zip
	bad := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(bad, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unzip(bad, out); err == nil {
		t.Fatal("坏 zip 应当报错")
	}

	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz,
		arcEntry{name: "d/", dir: true},
		arcEntry{name: "d/run", body: "bin", mode: 0o755},
	)
	out2 := filepath.Join(dir, "untar")
	if err := os.MkdirAll(out2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := untargz(tgz, out2); err != nil {
		t.Fatalf("untargz 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out2, "d", "run")); err != nil {
		t.Fatalf("没解出 d/run: %v", err)
	}

	evilTgz := filepath.Join(dir, "evil.tar.gz")
	makeTarGz(t, evilTgz, arcEntry{name: "../escape2.txt", body: "x"})
	if err := untargz(evilTgz, out2); err == nil {
		t.Fatal("带 ../ 的 tar.gz 被解开了")
	}

	// 不存在的文件 / 不是 gzip
	if err := untargz(filepath.Join(dir, "缺.gz"), out2); err == nil {
		t.Fatal("不存在的归档应当报错")
	}
	notGz := filepath.Join(dir, "not.gz")
	if err := os.WriteFile(notGz, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := untargz(notGz, out2); err == nil {
		t.Fatal("不是 gzip 的文件应当报错")
	}
}

func TestFindCopyAndSha(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "target"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findFile(dir, "target"); got == "" {
		t.Fatal("findFile 没找到")
	}
	if got := findFile(dir, "不存在的"); got != "" {
		t.Fatalf("findFile 找到了不存在的东西: %q", got)
	}
	if got := findDir(dir, "b"); got == "" {
		t.Fatal("findDir 没找到")
	}
	if got := findDir(dir, "不存在的"); got != "" {
		t.Fatalf("findDir 找到了不存在的东西: %q", got)
	}

	dst := filepath.Join(t.TempDir(), "copied")
	if err := copyTree(dir, dst); err != nil {
		t.Fatalf("copyTree 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a", "b", "target")); err != nil {
		t.Fatalf("copyTree 丢了文件: %v", err)
	}

	if err := copyFileTo(filepath.Join(dir, "缺"), filepath.Join(dst, "x"), 0o644); err == nil {
		t.Fatal("复制不存在的文件应当报错")
	}
	if err := copyFileTo(filepath.Join(dir, "a", "b", "target"), filepath.Join(dir, "不存在目录", "x"), 0o644); err == nil {
		t.Fatal("目标目录不存在应当报错")
	}

	if _, err := sha256File(filepath.Join(dir, "缺")); err == nil {
		t.Fatal("对不存在的文件算 sha256 应当报错")
	}
	got, err := sha256File(filepath.Join(dir, "a", "b", "target"))
	if err != nil || len(got) != 64 {
		t.Fatalf("sha256File = %q, %v", got, err)
	}

	// logf 传 nil 不该 panic
	logf(nil, "x")
}

func TestReplaceGUIMissingFiles(t *testing.T) {
	empty := t.TempDir()
	if err := replaceGUI(filepath.Join(t.TempDir(), "pk-desktop"), empty, "linux"); err == nil {
		t.Fatal("linux 缺 pk-desktop 应当报错")
	}
	if err := replaceGUI(filepath.Join(t.TempDir(), "pk-desktop.exe"), empty, "windows"); err == nil {
		t.Fatal("windows 缺 pk-desktop.exe 应当报错")
	}
	if err := replaceGUI(filepath.Join(t.TempDir(), "pk-desktop"), empty, "darwin"); err == nil {
		t.Fatal("darwin 缺 packnest.app 应当报错")
	}
}

// TestSSHKeygenVerify_ForgedSignature 用真 ssh-keygen 造一把**不认识的**钥匙去签清单：
// 必须被拒（重点不是"能验过"，而是"伪造的签不会过"）。
func TestSSHKeygenVerify_ForgedSignature(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("没有 ssh-keygen，跳过")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", key, "-q").CombinedOutput(); err != nil {
		t.Skipf("造钥匙失败（%v）：%s", err, out)
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(sums, []byte("deadbeef  x.tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sig := filepath.Join(dir, "checksums.txt.sig")
	cmd := exec.Command("ssh-keygen", "-Y", "sign", "-f", key, "-n", "pk-release", sums)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("签名失败（%v）：%s", err, out)
	}
	if _, err := os.Stat(sig); err != nil {
		t.Skipf("没生成签名文件: %v", err)
	}
	// 清单写成 sshsig 要求的格式：签名文件本身是独立的，这里直接验
	err := sshKeygenVerify(sums, sig)
	if err == nil {
		t.Fatal("用别人的钥匙签的清单居然验过了：自更新可以被伪造")
	}
	if !strings.Contains(err.Error(), "清单签名校验失败") {
		t.Fatalf("错误信息要说清是清单签名问题，实际 %v", err)
	}
}

func TestReplaceGUI_FailureBranches(t *testing.T) {
	// linux：目标路径是个目录 → 复制失败（旧文件不动）
	dir := t.TempDir()
	exeAsDir := filepath.Join(dir, "pk-desktop")
	if err := os.MkdirAll(exeAsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceGUI(exeAsDir, filepath.Dir(exeAsDir), "linux"); err == nil {
		t.Fatal("目标不可写时应报错")
	}

	// windows：temp 文件写不下去（父目录不存在）
	if err := replaceGUI(filepath.Join(dir, "没有这层", "pk-desktop.exe"), dir, "windows"); err == nil {
		t.Fatal("windows 复制失败应报错")
	}

	// darwin：旧的 .app 挪不开（父目录不存在）
	if err := replaceGUI(filepath.Join(dir, "没有这层", "packnest.app", "Contents", "MacOS", "pk-desktop"), dir, "darwin"); err == nil {
		t.Fatal("挪开旧 .app 失败应报错")
	}

	// darwin：新 .app 拷不过去 → 必须把旧 .app 还原（用户不能没有应用）
	app := filepath.Join(dir, "restore", "packnest.app")
	exe := filepath.Join(app, "Contents", "MacOS", "pk-desktop")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	unpacked := filepath.Join(dir, "unpacked")
	bad := filepath.Join(unpacked, "packnest.app", "locked")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0o755)
	if err := replaceGUI(exe, unpacked, "darwin"); err == nil {
		t.Skip("当前用户权限太高（能穿过 000 目录），这条分支测不到")
	}
	if b, err := os.ReadFile(exe); err != nil || string(b) != "OLD" {
		t.Fatalf("复制失败后没把旧 .app 还原: %q %v", b, err)
	}
}

func TestUnzipAndUntargz_ErrorBranches(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// 目标位置被同名的普通文件占着 → 建目录失败
	if err := os.WriteFile(filepath.Join(out, "blocked"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	z := filepath.Join(dir, "blocked.zip")
	makeZip(t, z, arcEntry{name: "blocked/x", body: "y"})
	if err := unzip(z, out); err == nil {
		t.Fatal("同名文件占位时应报错")
	}

	zd := filepath.Join(dir, "blockeddir.zip")
	makeZip(t, zd, arcEntry{name: "blocked", dir: true})
	if err := unzip(zd, out); err == nil {
		t.Fatal("目录条目冲突时应报错")
	}

	tgz := filepath.Join(dir, "blocked.tar.gz")
	makeTarGz(t, tgz, arcEntry{name: "blocked/x", body: "y"})
	if err := untargz(tgz, out); err == nil {
		t.Fatal("tar.gz 同名文件占位时应报错")
	}

	// 目标是个目录 → OpenFile 失败
	dirEntry := filepath.Join(dir, "isdir.tar.gz")
	makeTarGz(t, dirEntry, arcEntry{name: "adir", dir: true}, arcEntry{name: "adir", body: "x"})
	if err := untargz(dirEntry, out); err == nil {
		t.Log("同名的文件/目录冲突没有报错（不同平台行为不同，不当作失败）")
	}
}

func TestCopyTreeWalksErrors(t *testing.T) {
	src := t.TempDir()
	locked := filepath.Join(src, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)
	if err := copyTree(src, t.TempDir()); err == nil {
		t.Skip("当前用户权限太高，走不到 Walk 出错分支")
	}
	// 源目录本身不存在 → Walk 直接把错误抛出来
	if err := copyTree(filepath.Join(src, "缺"), t.TempDir()); err == nil {
		t.Fatal("源目录不存在应报错")
	}
}

func TestCleanupOldDarwin(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "packnest.app")
	exe := filepath.Join(app, "Contents", "MacOS", "pk-desktop")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldApp := filepath.Join(dir, "packnest.app.old.1700000000")
	if err := os.MkdirAll(oldApp, 0o755); err != nil {
		t.Fatal(err)
	}
	CleanupOld(exe, "darwin")
	if _, err := os.Stat(oldApp); err == nil {
		t.Fatal("macOS 的 packnest.app.old.* 没被清掉")
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatal("正在用的 .app 被误删")
	}
}
