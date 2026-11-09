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
	"fmt"
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
	// FormatZip 是 windows 的 .zip（也用于 bun/deno）。
	FormatZip Format = "zip"
)

// ErrNoSingle 表示单文件布局里产物数量不为 1。
var ErrNoSingle = errors.New("单文件布局的归档里应有且只有一个文件")

// Layout 描述解压后的落地形状（来自 provider 清单）。
type Layout struct {
	// Strip 是剥掉的路径层数（node tar 为 1）。
	Strip int
	// Single 非空表示归档里只有一个文件，落到该相对路径并 chmod 0755（deno）。
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
	return "", fmt.Errorf("不支持的归档格式 %q（只支持 tar.gz / tgz / zip）", ext)
}

// Extract 把 archivePath 解到 dstDir。
//
// dstDir 必须是干净的（调用方给 tools/<tool>/<ver>.tmp.<rand>）。
func Extract(archivePath, dstDir string, format Format, layout Layout, goos string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("创建临时安装目录失败: %w", err)
	}
	var err error
	switch format {
	case FormatTarGz, FormatTgz:
		err = extractTarGz(archivePath, dstDir, layout, goos)
	case FormatZip:
		err = extractZip(archivePath, dstDir, layout, goos)
	default:
		err = fmt.Errorf("不支持的归档格式 %q", format)
	}
	if err != nil {
		return err
	}
	return markExecutables(dstDir, layout)
}

func extractTarGz(archivePath, dstDir string, layout Layout, goos string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开归档失败: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解 gzip 失败（归档损坏？）: %w", err)
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
			return fmt.Errorf("读 tar 失败（归档截断？）: %w", err)
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
				return fmt.Errorf("创建目录失败: %w", err)
			}
		case tar.TypeSymlink:
			if goos == "windows" {
				// Windows 建符号链接需要开发者模式/管理员，且我们一律取 zip 产物。
				return fmt.Errorf("Windows 不支持带符号链接的 tar 归档（应改取 zip 产物，见方案 §18.3）: %s", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("创建符号链接 %q 失败: %w", rel, err)
			}
		case tar.TypeReg, tar.TypeRegA:
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
		return fmt.Errorf("打开 zip 失败（归档损坏？）: %w", err)
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
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			if goos == "windows" {
				return fmt.Errorf("Windows 不支持 zip 里的符号链接: %s", f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("读取符号链接失败: %w", err)
			}
			link, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(string(link), target); err != nil {
				return fmt.Errorf("创建符号链接 %q 失败: %w", rel, err)
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("打开 %q 失败: %w", rel, err)
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
		return 0, fmt.Errorf("创建目录失败: %w", err)
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, fmt.Errorf("创建文件 %q 失败: %w", target, err)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("写入 %q 失败（归档截断？）: %w", filepath.Base(target), copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("写入 %q 失败: %w", filepath.Base(target), closeErr)
	}
	// 显式 chmod：某些归档（尤其是 zip）不带权限位。
	if err := os.Chmod(target, perm); err != nil {
		return 0, fmt.Errorf("设置权限失败: %w", err)
	}
	return n, nil
}

// applySingle 处理单文件布局：把唯一产物挪到 layout.Single 并赋予 0755。
func applySingle(dstDir string, layout Layout, files []string) error {
	if layout.Single == "" {
		if len(files) != 0 {
			return nil
		}
		return nil
	}
	if len(files) != 1 {
		return fmt.Errorf("%w（实际 %d 个）", ErrNoSingle, len(files))
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
			return fmt.Errorf("移动到单文件布局位置失败: %w", err)
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
				return fmt.Errorf("设置 %s 可执行位失败: %w", name, err)
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
	return "", fmt.Errorf("解压后的树里找不到命令 %q（查找过: %s）", name, strings.Join(candidates, ", "))
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
				return fmt.Errorf("落盘到 %q 失败: %w", dst, err)
			}
			return nil
		}
		return err
	}

	// 三步法在所有平台都用：Unix 虽然能直接覆盖，但先挪开更便于失败回滚。
	old := dst + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(dst, old); err != nil {
		return fmt.Errorf("旧目录改名失败（进程还占用？）: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		// 回滚：把旧的放回去。
		_ = os.Rename(old, dst)
		return fmt.Errorf("新目录落盘失败（已回滚旧版本）: %w", err)
	}
	if err := os.RemoveAll(old); err != nil {
		// 删不掉不算失败（Windows 上可能仍被占用），下次启动清扫。
		return nil
	}
	return nil
}
