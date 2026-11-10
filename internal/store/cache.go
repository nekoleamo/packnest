// 缓存治理与启动清扫（方案 §18.4 / §18.5）。
//
// 两条硬规则：
//  1. 断电/强杀留下的 `*.tmp.*`、`*.old`、`*.part` 在写操作前清扫（超过 24h 才删，
//     避免把正在跑的另一个进程的临时目录当成垃圾）；
//  2. 产物缓存默认上限 5GB，按 LRU 淘汰（`state/cache.json` 记 last_used）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/paths"
)

// CacheSchema 是缓存索引的格式版本。
const CacheSchema = 1

// StaleMaxAge 是临时残留被视为"垃圾"的阈值。
const StaleMaxAge = 24 * time.Hour

// CacheEntry 是缓存里一个产物的使用记录。
type CacheEntry struct {
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"last_used"`
}

// CacheIndex 是 state/cache.json。
type CacheIndex struct {
	Schema  int                   `json:"schema"`
	Entries map[string]CacheEntry `json:"entries"`
}

// LoadCacheIndex 读取缓存索引；缺失返回空索引。
func LoadCacheIndex(path string) (*CacheIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CacheIndex{Schema: CacheSchema, Entries: map[string]CacheEntry{}}, nil
		}
		return nil, fmt.Errorf("读取缓存索引失败: %w", err)
	}
	var idx CacheIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		// 缓存索引坏掉不是致命问题：当成空索引重建。
		return &CacheIndex{Schema: CacheSchema, Entries: map[string]CacheEntry{}}, nil
	}
	if idx.Entries == nil {
		idx.Entries = map[string]CacheEntry{}
	}
	idx.Schema = CacheSchema
	return &idx, nil
}

// Save 原子写缓存索引。
func (c *CacheIndex) Save(path string) error {
	c.Schema = CacheSchema
	if c.Entries == nil {
		c.Entries = map[string]CacheEntry{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, data)
}

// Touch 记录一次使用（命中缓存时调用）。
func (c *CacheIndex) Touch(name string, size int64, now time.Time) {
	if c.Entries == nil {
		c.Entries = map[string]CacheEntry{}
	}
	c.Entries[name] = CacheEntry{Size: size, LastUsed: now.UTC()}
}

// Total 返回缓存的已记录总大小。
func (c *CacheIndex) Total() int64 {
	var total int64
	for _, e := range c.Entries {
		total += e.Size
	}
	return total
}

// ParseSize 解析 "5GB" / "500MB" / "1073741824" 这类写法。
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("容量为空")
	}
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1},
	} {
		if strings.HasSuffix(s, unit.suffix) {
			s = strings.TrimSpace(strings.TrimSuffix(s, unit.suffix))
			multiplier = unit.mult
			break
		}
	}
	value, err := strconv.ParseFloat(s, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("容量 %q 无法解析（示例：5GB / 500MB / 1073741824）", s)
	}
	return int64(value * float64(multiplier)), nil
}

// SweepResult 是清扫结果。
type SweepResult struct {
	Removed []string
	Bytes   int64
}

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
		filepath.Join(p.CacheMeta(), "*.tmp"),
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
	sort.Strings(res.Removed)
	return res, nil
}

// PruneCache 把产物缓存压到 limit 以内（LRU，按 last_used 从旧到新删）。
//
// dryRun 时只报告要删什么，不动磁盘。
func PruneCache(p paths.Paths, limit int64, now time.Time, dryRun bool) (SweepResult, error) {
	idx, err := LoadCacheIndex(p.CacheIndex())
	if err != nil {
		return SweepResult{}, err
	}
	dir := p.CacheArchives()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return SweepResult{}, err
	}

	// 先把磁盘上存在但索引里没有的文件补进来（按文件 mtime 当 last_used）。
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".meta") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if _, ok := idx.Entries[name]; !ok {
			idx.Entries[name] = CacheEntry{Size: info.Size(), LastUsed: info.ModTime().UTC()}
		}
	}
	// 索引里有但磁盘上没有的，顺手清掉。
	for name := range idx.Entries {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			delete(idx.Entries, name)
		}
	}

	total := idx.Total()
	var res SweepResult
	if total <= limit {
		if err := idx.Save(p.CacheIndex()); err != nil {
			return res, err
		}
		return res, nil
	}

	type candidate struct {
		name  string
		entry CacheEntry
	}
	cands := make([]candidate, 0, len(idx.Entries))
	for name, e := range idx.Entries {
		cands = append(cands, candidate{name, e})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].entry.LastUsed.Before(cands[j].entry.LastUsed) })

	for _, c := range cands {
		if total <= limit {
			break
		}
		path := filepath.Join(dir, c.name)
		if !dryRun {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				// 删不掉（Windows 占用常见）：不计入已释放，台账留着下次再试。
				continue
			}
		}
		res.Removed = append(res.Removed, path)
		res.Bytes += c.entry.Size
		total -= c.entry.Size
		delete(idx.Entries, c.name)
	}
	if !dryRun {
		if err := idx.Save(p.CacheIndex()); err != nil {
			return res, err
		}
	}
	sort.Strings(res.Removed)
	return res, nil
}

// DirSize 递归算目录大小。
func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
