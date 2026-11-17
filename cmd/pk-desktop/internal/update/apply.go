package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ApplyOptions 是"升级 GUI 自己"所需的依赖（全部可注入，测试零外网、零 ssh-keygen）。
type ApplyOptions struct {
	Current     string
	ArtifactURL string
	Exe         string // 当前可执行文件（macOS 上是 <app>/Contents/MacOS/pk-desktop）
	GOOS        string
	GOARCH      string
	Get         func(ctx context.Context, url string) ([]byte, error)
	Download    func(ctx context.Context, url, dst string) error
	// VerifySignature 验 checksums.txt 的 sshsig；默认用 ssh-keygen（测试可替换）。
	VerifySignature func(sumsPath, sigPath string) error
	Log             func(string)
}

// ApplyGUI 下载 → 验签 → 比 sha256 → 替换 GUI 自身。返回新版本号；成功后需要重启应用。
func ApplyGUI(ctx context.Context, opt ApplyOptions) (string, error) {
	if opt.GOOS == "" {
		opt.GOOS = runtimeGOOS()
	}
	tmp, err := os.MkdirTemp("", "pk-gui-update-")
	if err != nil {
		return "", fmt.Errorf("建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	logf(opt.Log, "下载 "+filepath.Base(opt.ArtifactURL))
	archivePath := filepath.Join(tmp, filepath.Base(opt.ArtifactURL))
	if err := opt.Download(ctx, opt.ArtifactURL, archivePath); err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	dirURL := strings.TrimSuffix(opt.ArtifactURL, "/"+filepath.Base(opt.ArtifactURL))
	sumsPath := filepath.Join(tmp, "checksums.txt")
	if err := opt.Download(ctx, dirURL+"/checksums.txt", sumsPath); err != nil {
		return "", fmt.Errorf("下载校验清单失败: %w", err)
	}
	sigPath := filepath.Join(tmp, "checksums.txt.sig")
	if err := opt.Download(ctx, dirURL+"/checksums.txt.sig", sigPath); err != nil {
		return "", fmt.Errorf("下载清单签名失败: %w", err)
	}

	verify := opt.VerifySignature
	if verify == nil {
		verify = sshKeygenVerify
	}
	if err := verify(sumsPath, sigPath); err != nil {
		return "", err
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", fmt.Errorf("读校验清单失败: %w", err)
	}
	want := sumFor(sums, filepath.Base(archivePath))
	if want == "" {
		return "", fmt.Errorf("校验清单里没有 %s 的 sha256", filepath.Base(archivePath))
	}
	got, err := sha256File(archivePath)
	if err != nil {
		return "", fmt.Errorf("读产物失败: %w", err)
	}
	if got != want {
		return "", fmt.Errorf("sha256 不符（期望 %s，实际 %s）", want, got)
	}

	unpacked := filepath.Join(tmp, "unpacked")
	if err := os.MkdirAll(unpacked, 0o755); err != nil {
		return "", err
	}
	if strings.HasSuffix(archivePath, ".zip") {
		if err := unzip(archivePath, unpacked); err != nil {
			return "", err
		}
	} else {
		if err := untargz(archivePath, unpacked); err != nil {
			return "", err
		}
	}
	if err := replaceGUI(opt.Exe, unpacked, opt.GOOS); err != nil {
		return "", err
	}
	logf(opt.Log, "已替换完成，重启后生效")
	return opt.Current, nil
}

// sshKeygenVerify 用 ssh-keygen 验清单签名（与 install-gui.sh 同一套做法与公钥）。
//
// 为什么不在 GUI 里自己实现 sshsig：那等于把 internal/verify 的密码学逻辑抄一份，
// 抄错了没人知道；而 ssh-keygen 在 macOS/Linux 一定有，Windows 缺就**拒绝自更新**
// （提示用安装脚本），不做"只比 sha256"的降级。
func sshKeygenVerify(sumsPath, sigPath string) error {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return errors.New("没找到 ssh-keygen（Windows 需安装 OpenSSH 客户端功能）；请改用 scripts/install-gui.sh / install-gui.ps1 升级")
	}
	signers := filepath.Join(filepath.Dir(sumsPath), "allowed_signers")
	if err := os.WriteFile(signers, []byte(allowedSigners), 0o644); err != nil {
		return err
	}
	sums, err := os.Open(sumsPath)
	if err != nil {
		return err
	}
	defer sums.Close()
	cmd := exec.Command(bin, "-Y", "verify", "-f", signers, "-I", "release@packnest.dev",
		"-n", "pk-release", "-s", sigPath)
	cmd.Stdin = sums
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("清单签名校验失败: %v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// replaceGUI 按平台替换自身。
//
//   - macOS：产物是 `.app.zip`，替换整个 `packnest.app`（把旧的改名挪开，新的放进去；旧目录删不掉就留着，
//     不影响运行 —— 正在运行的进程用的是已打开的文件）；
//   - Windows：产物是含 `pk-desktop.exe` 的 zip，正在运行的 exe 不能覆盖，先改名再放新的；
//   - Linux：产物是含 `pk-desktop` 的 tar.gz，直接替换文件。
func replaceGUI(exe, unpacked, goos string) error {
	switch goos {
	case "darwin":
		app := appBundleOf(exe)
		newApp := findDir(unpacked, "packnest.app")
		if newApp == "" {
			return fmt.Errorf("更新包里没有 packnest.app")
		}
		old := fmt.Sprintf("%s.old.%d", app, time.Now().Unix())
		if err := os.Rename(app, old); err != nil {
			return fmt.Errorf("挪开旧版本失败: %w", err)
		}
		if err := copyTree(newApp, app); err != nil {
			// 先把拷了一半的新版本清掉：留着它的话，下面的还原 rename 会因为
			// 目标目录已存在而失败——那就变成"旧应用没了、新应用也不完整"。
			_ = os.RemoveAll(app)
			if rerr := os.Rename(old, app); rerr != nil {
				return fmt.Errorf("放入新版本失败: %w（旧版本暂存在 %s，可手动改回）", err, old)
			}
			return fmt.Errorf("放入新版本失败: %w", err)
		}
		_ = os.RemoveAll(old) // 删不掉就算了（可能还在被引用）
		return nil
	case "windows":
		newExe := findFile(unpacked, "pk-desktop.exe")
		if newExe == "" {
			return fmt.Errorf("更新包里没有 pk-desktop.exe")
		}
		tmpNew := exe + ".new"
		if err := copyFileTo(newExe, tmpNew, 0o755); err != nil {
			return err
		}
		old := fmt.Sprintf("%s.old.%d", exe, time.Now().Unix())
		if err := os.Rename(exe, old); err != nil {
			_ = os.Remove(tmpNew)
			return fmt.Errorf("挪开旧版本失败: %w", err)
		}
		if err := os.Rename(tmpNew, exe); err != nil {
			_ = os.Rename(old, exe)
			_ = os.Remove(tmpNew)
			return fmt.Errorf("放入新版本失败: %w", err)
		}
		_ = os.Remove(old)
		return nil
	default:
		newBin := findFile(unpacked, "pk-desktop")
		if newBin == "" {
			return fmt.Errorf("更新包里没有 pk-desktop")
		}
		tmpNew := exe + ".new"
		if err := copyFileTo(newBin, tmpNew, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmpNew, exe); err != nil {
			_ = os.Remove(tmpNew)
			return fmt.Errorf("替换失败: %w", err)
		}
		return nil
	}
}

// CleanupOld 清掉上次替换留下的 `<exe>.old*` 与 macOS 的 `packnest.app.old*`（启动时调用）。
func CleanupOld(exe string, goos string) {
	if exe == "" {
		return
	}
	dirs := []string{filepath.Dir(exe)}
	if goos == "darwin" {
		if app := appBundleOf(exe); app != "" {
			dirs = append(dirs, filepath.Dir(app))
		}
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, ".old.") && (strings.HasPrefix(name, filepath.Base(exe)) || strings.HasPrefix(name, "packnest.app.old")) {
				_ = os.RemoveAll(filepath.Join(d, name))
			}
		}
	}
}

// appBundleOf 从 <app>/Contents/MacOS/pk-desktop 找回 <app>。
func appBundleOf(exe string) string {
	mac := filepath.Dir(exe)
	if filepath.Base(mac) != "MacOS" {
		return exe
	}
	contents := filepath.Dir(mac)
	if filepath.Base(contents) != "Contents" {
		return exe
	}
	return filepath.Dir(contents)
}

// ── 小工具（纯 stdlib，GUI 模块不 import internal/*）───────────────

func unzip(path, dst string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		target, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode()
		if mode&0o111 == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

func untargz(path, dst string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("解压失败: %w", err)
		}
		target, err := safeJoin(dst, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(h.Mode)
			if mode&0o111 == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

// safeJoin 防 Zip Slip（归档里的 ../ 不许跑出目标目录）。
func safeJoin(dst, name string) (string, error) {
	clean := filepath.Clean(filepath.Join(dst, name))
	if !strings.HasPrefix(clean, filepath.Clean(dst)+string(os.PathSeparator)) {
		return "", fmt.Errorf("归档里的路径越界: %s", name)
	}
	return clean, nil
}

func findFile(root, name string) string {
	var found string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found != "" {
			return nil
		}
		if info.Name() == name {
			found = p
		}
		return nil
	})
	return found
}

func findDir(root, name string) string {
	var found string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || found != "" {
			return nil
		}
		if info.IsDir() && info.Name() == name {
			found = p
		}
		return nil
	})
	return found
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		mode := info.Mode()
		if mode&0o111 == 0 {
			mode = 0o644
		}
		return copyFileTo(p, target, mode)
	})
}

func copyFileTo(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sumFor(sums []byte, name string) string {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func logf(fn func(string), msg string) {
	if fn != nil {
		fn(msg)
	}
}
