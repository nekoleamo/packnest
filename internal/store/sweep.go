package store

// 崩溃残留清扫（从 store.go 抽出来：那个文件过 600 行就该拆）。
//
// 两类残留：
//   1. 顶层：`tools/*/*.tmp.*`、`tools/*/*.old`、`cache/archives/*.part*`、`cache/meta/*.tmp*`；
//   2. 版本树内部：伴生包替换留下的 `lib/node_modules/npm.tmp.<rand>` / `<target>.old`。
//      后者只有**有界下钻**才扫得到，而 node_modules 的正常嵌套动辄几万文件，
//      所以深度限制在 deepSweepDepth，超了就停。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/paths"
)

// SweepStale 清理崩溃/强杀留下的临时残留（超过 maxAge 才删）。
func SweepStale(p paths.Paths, now time.Time, maxAge time.Duration) (SweepResult, error) {
	if maxAge <= 0 {
		maxAge = StaleMaxAge
	}
	var res SweepResult
	patterns := []string{
		filepath.Join(p.Tools(), "*", "*.tmp.*"),
		filepath.Join(p.Tools(), "*", "*.old"),
		filepath.Join(p.CacheArchives(), "*.part"),
		filepath.Join(p.CacheArchives(), "*.part.meta"),
		filepath.Join(p.CacheMeta(), "*.tmp*"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			st, err := os.Lstat(match)
			if err != nil || now.Sub(st.ModTime()) < maxAge {
				continue
			}
			size := DirSize(match)
			if err := os.RemoveAll(match); err != nil {
				continue
			}
			res.Removed = append(res.Removed, match)
			res.Bytes += size
		}
	}
	// 深层残留：伴生包替换的临时目录在版本树**里面**
	// （`tools/node/<ver>/lib/node_modules/npm.tmp.<rand>`、`<target>.old`），
	// 上面那两条 glob 只到 `tools/*/*` 这一层，扫不到。这里按"最多 6 层"有界下钻：
	// 再深就是 node_modules 的正常嵌套（几万个文件），扫它不划算，残留也不会落在那里。
	deep, derr := sweepDeep(p, now, maxAge)
	if derr == nil {
		res.Removed = append(res.Removed, deep.Removed...)
		res.Bytes += deep.Bytes
	}
	sort.Strings(res.Removed)
	return res, nil
}

// deepSweepDepth 是"从版本目录再往下钻几层"（npm 的临时目录在第 3 层：
// lib/node_modules/npm.tmp.<rand>）。
const deepSweepDepth = 6

// skipDirOnly 只对目录返回 SkipDir：WalkDir 对**文件**返回 SkipDir 时会跳过
// 同一目录下剩下的条目（不是"跳过这个文件"），会漏掉真正的残留。
func skipDirOnly(d os.DirEntry) error {
	if d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// sweepDeep 在版本树里找伴生包替换留下的 `.tmp.` / `.old`（有界深度，见 deepSweepDepth）。
func sweepDeep(p paths.Paths, now time.Time, maxAge time.Duration) (SweepResult, error) {
	var res SweepResult
	tools := p.Tools()
	_ = filepath.WalkDir(tools, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, rerr := filepath.Rel(tools, path)
		if rerr != nil {
			return nil
		}
		// rel 形如 `node/22.22.2/lib/node_modules/npm.tmp.7`：深度 = 段数
		depth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if d.IsDir() && depth > 2+deepSweepDepth {
			return filepath.SkipDir
		}
		name := d.Name()
		if !(strings.Contains(name, ".tmp.") || strings.HasSuffix(name, ".old")) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || now.Sub(info.ModTime()) < maxAge {
			return skipDirOnly(d) // 没超时就别往下走（但别把同目录的兄弟条目一起跳过）
		}
		size := DirSize(path)
		if err := os.RemoveAll(path); err != nil {
			return skipDirOnly(d)
		}
		res.Removed = append(res.Removed, path)
		res.Bytes += size
		return skipDirOnly(d)
	})
	return res, nil
}
