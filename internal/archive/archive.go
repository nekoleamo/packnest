// Package archive 只做两种归档：tar.gz 与 zip（方案 §4.2）。
//
// 三条硬约束：
//  1. 零第三方依赖（archive/tar + compress/gzip + archive/zip）；
//  2. 路径必须经 paths.SafeJoin —— 归档里的 "../" 与绝对路径一律拒绝（zip-slip）；
//  3. 解压永远落在调用方给的临时目录，成功后由 Replace 一次性换上去，
//     失败时临时目录整体删除，绝不留半成品（方案 §8.2 第 2 条）。
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"github.com/packnest/packnest/internal/coded"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/packnest/packnest/internal/paths"
)

// Format 是归档格式。
type Format string

const (
	// FormatTarGz 是 linux/darwin 的 .tar.gz。
	FormatTarGz Format = "tar.gz"
	// FormatTgz 是 npm/pnpm 的 .tgz（同样是 gzip+tar）。
	FormatTgz Format = "tgz"
	// FormatZip 是 windows 的 .zip（bun 也用 zip）。
	FormatZip Format = "zip"
)

// ErrNoSingle 表示单文件布局里产物数量不为 1。
var ErrNoSingle = errors.New("单文件布局的归档里应有且只有一个文件")

// Layout 描述解压后的落地形状（来自 provider 清单）。
type Layout struct {
	// Strip 是剥掉的路径层数（node tar 为 1）。
	Strip int
	// Single 非空表示归档里只有一个文件，落到该相对路径并 chmod 0755。
	Single string
	// Bins 是要暴露的命令名（用来保证可执行位）。
	Bins []string
	// BinTargets 是 bin 名 → 树内相对路径（npm/pnpm 的入口是 bin/npm-cli.js）。
	BinTargets map[string]string
}

// FormatFor 把扩展名映射成格式；不认识的直接报错（不猜）。
func FormatFor(ext string) (Format, error) {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "tar.gz":
		return FormatTarGz, nil
	case "tgz":
		return FormatTgz, nil
	case "zip":
		return FormatZip, nil
	}
	return "", coded.New(CodeFormatUnsupported, ext)
}

// Extract 把 archivePath 解到 dstDir。
//
// dstDir 必须是干净的（调用方给 tools/<tool>/<ver>.tmp.<rand>）。
func Extract(archivePath, dstDir string, format Format, layout Layout, goos string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return coded.Wrap(err, CodeTmpDir)
	}
	var err error
	switch format {
	case FormatTarGz, FormatTgz:
		err = extractTarGz(archivePath, dstDir, layout, goos)
	case FormatZip:
		err = extractZip(archivePath, dstDir, layout, goos)
	default:
		err = coded.New(CodeFormatUnsupported, format)
	}
	if err != nil {
		return err
	}
	return markExecutables(dstDir, layout)
}

func extractTarGz(archivePath, dstDir string, layout Layout, goos string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return coded.Wrap(err, CodeOpen)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return coded.Wrap(err, CodeGzip)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var singles []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return coded.Wrap(err, CodeTarRead)
		}
		rel, ok, err := stripPath(hdr.Name, layout.Strip)
		if err != nil {
			return err
		}
		if !ok || rel == "" {
			continue
		}
		target, err := paths.SafeJoin(dstDir, rel)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return coded.Wrap(err, CodeMkdir)
			}
		case tar.TypeSymlink:
			if goos == "windows" {
				// Windows 建符号链接需要开发者模式/管理员，且我们一律取 zip 产物。
				return coded.New(CodeLinkUnsupported, "tar", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := checkLinkTarget(dstDir, target, hdr.Linkname); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return coded.Wrap(err, CodeLinkCreate, rel)
			}
		case tar.TypeReg:
			n, err := writeEntry(target, tr, hdr.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			if layout.Single != "" && n >= 0 {
				singles = append(singles, target)
			}
		default:
			// 设备文件/管道等一律忽略（发行包里不该有）。
		}
	}
	return applySingle(dstDir, layout, singles)
}

func extractZip(archivePath, dstDir string, layout Layout, goos string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return coded.Wrap(err, CodeOpen, "zip")
	}
	defer zr.Close()

	var singles []string
	for _, f := range zr.File {
		rel, ok, err := stripPath(f.Name, layout.Strip)
		if err != nil {
			return err
		}
		if !ok || rel == "" {
			continue
		}
		target, err := paths.SafeJoin(dstDir, rel)
		if err != nil {
			return err
		}
		if strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return coded.Wrap(err, CodeMkdir)
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			if goos == "windows" {
				return coded.New(CodeLinkUnsupported, "zip", f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				return coded.Wrap(err, CodeLinkRead)
			}
			link, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := checkLinkTarget(dstDir, target, string(link)); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(string(link), target); err != nil {
				return coded.Wrap(err, CodeLinkCreate, rel)
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return coded.Wrap(err, CodeEntryOpen, rel)
		}
		n, err := writeEntry(target, rc, f.Mode().Perm())
		rc.Close()
		if err != nil {
			return err
		}
		if layout.Single != "" && n >= 0 {
			singles = append(singles, target)
		}
	}
	return applySingle(dstDir, layout, singles)
}

// writeEntry 写一个常规文件，返回写出的字节数。
func writeEntry(target string, r io.Reader, mode os.FileMode) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, coded.Wrap(err, CodeMkdir)
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, coded.Wrap(err, CodeCreate, target)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return 0, coded.Wrap(copyErr, CodeWriteTruncated, filepath.Base(target))
	}
	if closeErr != nil {
		return 0, coded.Wrap(closeErr, CodeWrite, filepath.Base(target))
	}
	// 显式 chmod：某些归档（尤其是 zip）不带权限位。
	if err := os.Chmod(target, perm); err != nil {
		return 0, coded.Wrap(err, CodeChmod, filepath.Base(target))
	}
	return n, nil
}

// checkLinkTarget 确认符号链接不会把写入引到解压根之外（tar-slip 的符号链接变体）。
//
// 解压时的路径校验（paths.SafeJoin）只看条目名，如果归档里先放一个指向 /etc 的
// 符号链接、再往该路径写常规文件，写入就跑到根目录外了。
func checkLinkTarget(dstDir, target, linkname string) error {
	clean := strings.ReplaceAll(linkname, `\`, "/")
	if clean == "" {
		return coded.New(CodeLinkEmpty, filepath.Base(target))
	}
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") || (len(clean) > 1 && clean[1] == ':') {
		return coded.New(CodeLinkAbsolute, filepath.Base(target), linkname)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(clean)))
	rel, err := filepath.Rel(dstDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return coded.New(CodeLinkEscape, filepath.Base(target), linkname)
	}
	return nil
}

// applySingle 处理单文件布局：把唯一产物挪到 layout.Single 并赋予 0755。
func applySingle(dstDir string, layout Layout, files []string) error {
	if layout.Single == "" {
		return nil
	}
	if len(files) != 1 {
		return coded.Wrap(ErrNoSingle, CodeSingleCount, len(files))
	}
	target, err := paths.SafeJoin(dstDir, layout.Single)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if files[0] != target {
		if err := os.Rename(files[0], target); err != nil {
			return coded.Wrap(err, CodeSingleMove)
		}
	}
	return os.Chmod(target, 0o755)
}

// markExecutables 保证 bins 里声明的命令有可执行位（zip 常见丢权限）。
func markExecutables(dstDir string, layout Layout) error {
	if layout.Single != "" {
		return nil
	}
	for _, name := range layout.Bins {
		if found, err := FindBin(dstDir, name, "unix", layout.BinTargets); err == nil && found != "" {
			if err := os.Chmod(found, 0o755); err != nil {
				return coded.Wrap(err, CodeChmod, name)
			}
			continue
		}
		for _, cand := range binCandidates(name, "windows") {
			p := filepath.Join(dstDir, filepath.FromSlash(cand))
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				if err := os.Chmod(p, 0o755); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// FindBin 在解压后的树里定位某个命令的真实文件（各工具布局不同）。
//
// 顺序：targets 里声明的相对路径（provider 的 layout.bin_targets）→
// bin/<name> → <name> → <name>.exe / <name>.cmd（Windows zip 是平铺的）。
func FindBin(toolDir, name, goos string, targets map[string]string) (string, error) {
	candidates := binCandidates(name, goos)
	if target, ok := targets[name]; ok && target != "" {
		candidates = append([]string{target}, candidates...)
	}
	for _, cand := range candidates {
		p := filepath.Join(toolDir, filepath.FromSlash(cand))
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", coded.New(CodeBinNotFound, name, strings.Join(candidates, ", "))
}

func binCandidates(name, goos string) []string {
	base := []string{
		path.Join("bin", name),
		path.Join("bin", name+".cjs"),
		path.Join("bin", name+".js"),
		name,
	}
	if goos == "windows" {
		base = append(base,
			path.Join("bin", name+".exe"),
			path.Join("bin", name+".cmd"),
			name+".exe",
			name+".cmd",
			name+".cjs",
		)
	}
	return base
}

// stripPath 去掉前 n 层路径，返回相对路径；层次不足时 ok=false（跳过该条目）。
func stripPath(name string, n int) (string, bool, error) {
	if name == "" {
		return "", false, nil
	}
	clean := path.Clean(strings.ReplaceAll(name, `\`, "/"))
	if clean == "." || clean == "/" {
		return "", false, nil
	}
	trimmed := strings.TrimPrefix(clean, "/")
	parts := strings.Split(trimmed, "/")
	if n > 0 {
		if len(parts) <= n {
			return "", false, nil
		}
		parts = parts[n:]
	}
	return path.Join(parts...), true, nil
}

// Replace 用 src 目录（已解压好的版本树）替换 dst。
//
// Windows 的 os.Rename 不能覆盖已存在目录，所以走三步：
// dst → dst.old → rename(src→dst) → 删 dst.old（方案 §18.3）。
func Replace(dst, src string) error {
	if _, err := os.Stat(dst); err != nil {
		if os.IsNotExist(err) {
			if err := os.Rename(src, dst); err != nil {
				return coded.Wrap(err, CodePlaceTmp, dst)
			}
			return nil
		}
		return err
	}

	// 三步法在所有平台都用：Unix 虽然能直接覆盖，但先挪开更便于失败回滚。
	old := dst + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(dst, old); err != nil {
		return coded.Wrap(err, CodeOldRename)
	}
	if err := os.Rename(src, dst); err != nil {
		// 回滚：把旧的放回去。
		_ = os.Rename(old, dst)
		return coded.Wrap(err, CodePlace)
	}
	if err := os.RemoveAll(old); err != nil {
		// 删不掉不算失败（Windows 上可能仍被占用），下次启动清扫。
		return nil
	}
	return nil
}
