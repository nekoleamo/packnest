package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/packnest/packnest/internal/paths"
)

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"5GB":     5 << 30,
		"500MB":   500 << 20,
		"1TB":     1 << 40,
		"2KB":     2 << 10,
		"100B":    100,
		"1048576": 1 << 20,
		" 1.5GB ": 1610612736,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d/%v，期望 %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "5GG", "-1GB"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) 应报错", bad)
		}
	}
}

func TestSweepStale(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	now := time.Now()

	old := filepath.Join(p.Tool("node"), "22.22.2.tmp.abcd")
	fresh := filepath.Join(p.Tool("node"), "22.22.2.tmp.efgh")
	oldDir := filepath.Join(p.Tool("deno"), "2.9.8.old")
	part := filepath.Join(p.CacheArchives(), "node.part")
	for _, dir := range []string{filepath.Dir(old), p.CacheArchives()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{old, fresh, oldDir, part} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "x"), []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 把三个设成 48h 前，一个保持新鲜
	stale := now.Add(-48 * time.Hour)
	for _, path := range []string{old, oldDir, part} {
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatal(err)
		}
	}

	res, err := SweepStale(p, now, StaleMaxAge)
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if len(res.Removed) != 3 {
		t.Fatalf("应删 3 个残留，得到 %v", res.Removed)
	}
	if res.Bytes == 0 {
		t.Fatal("应统计释放的字节数")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("新鲜的临时目录不该删")
	}
	for _, path := range []string{old, oldDir, part} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("残留应被删除: %s", path)
		}
	}
	// 空目录也要能跑
	p2, _ := paths.New(t.TempDir())
	if res, err := SweepStale(p2, now, 0); err != nil || len(res.Removed) != 0 {
		t.Fatalf("空根目录应无事发生: %+v %v", res, err)
	}
}

func TestCacheIndexRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	idx, err := LoadCacheIndex(path)
	if err != nil || idx.Schema != CacheSchema || len(idx.Entries) != 0 {
		t.Fatalf("缺失索引应返回空: %+v %v", idx, err)
	}
	now := time.Now()
	idx.Touch("node-1.tar.gz", 100, now)
	if idx.Total() != 100 {
		t.Fatalf("Total = %d", idx.Total())
	}
	if err := idx.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadCacheIndex(path)
	if err != nil || back.Entries["node-1.tar.gz"].Size != 100 {
		t.Fatalf("往返失败: %+v %v", back, err)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, err := LoadCacheIndex(path)
	if err != nil || len(broken.Entries) != 0 {
		t.Fatalf("损坏索引应重建为空: %+v %v", broken, err)
	}
}

func TestPruneCacheLRU(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	if err := os.MkdirAll(p.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// 三个各 100 字节；工期最久的是 oldest
	names := []string{"oldest.tar.gz", "middle.tar.gz", "newest.tar.gz"}
	for i, name := range names {
		path := filepath.Join(p.CacheArchives(), name)
		if err := os.WriteFile(path, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := now.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := LoadCacheIndex(p.CacheIndex())
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		idx.Touch(name, 100, now.Add(time.Duration(i)*time.Hour))
	}
	if err := idx.Save(p.CacheIndex()); err != nil {
		t.Fatal(err)
	}

	// 上限 250 → 需要腾出至少 50 → 删最老的 1 个（剩 200）
	res, err := PruneCache(p, 250, now, false)
	if err != nil {
		t.Fatalf("PruneCache: %v", err)
	}
	if len(res.Removed) != 1 || filepath.Base(res.Removed[0]) != "oldest.tar.gz" {
		t.Fatalf("应淘汰最久未用的一个: %v", res.Removed)
	}
	if _, err := os.Stat(filepath.Join(p.CacheArchives(), "oldest.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("被淘汰的文件应删除")
	}
	back, _ := LoadCacheIndex(p.CacheIndex())
	if _, ok := back.Entries["oldest.tar.gz"]; ok {
		t.Fatal("索引里也应删掉")
	}

	// dry-run 不动磁盘
	res, err = PruneCache(p, 50, now, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if len(res.Removed) == 0 {
		t.Fatal("dry-run 也应报告要删什么")
	}
	if _, err := os.Stat(filepath.Join(p.CacheArchives(), "middle.tar.gz")); err != nil {
		t.Fatal("dry-run 不该删文件")
	}

	// 超限但索引缺失 → 用 mtime 补齐后照样能压
	if err := os.Remove(p.CacheIndex()); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneCache(p, 1, now, false); err != nil {
		t.Fatalf("索引缺失时也应能压: %v", err)
	}
	entries, _ := os.ReadDir(p.CacheArchives())
	if len(entries) != 0 {
		t.Fatalf("上限 1 字节应清空缓存，剩 %v", entries)
	}
}

func TestPruneCacheUnderLimit(t *testing.T) {
	root := t.TempDir()
	p, _ := paths.New(root)
	if err := os.MkdirAll(p.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.CacheArchives(), "a.tar.gz"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := PruneCache(p, 1<<20, time.Now(), false)
	if err != nil || len(res.Removed) != 0 {
		t.Fatalf("未超限不该删: %+v %v", res, err)
	}
	if _, err := LoadCacheIndex(p.CacheIndex()); err != nil {
		t.Fatal("应写出缓存索引")
	}
}

func TestLoadLedgerWithRecoveryNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installs.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 没有 .bak → 必须报原始错误（不能因为"缺失=空台账"而假装恢复成功）
	if _, recovered, err := LoadLedgerWithRecovery(path); err == nil || recovered {
		t.Fatalf("没有备份时应报错: recovered=%v err=%v", recovered, err)
	}
}

func TestLoadLedgerWithRecoveryBackupBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installs.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("{also-broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, recovered, err := LoadLedgerWithRecovery(path); err == nil || recovered {
		t.Fatalf("备份也坏时应报错: recovered=%v err=%v", recovered, err)
	}
}

func TestPruneCacheRemoveError(t *testing.T) {
	// 索引里记着一个不存在的文件 → 应被静默清理，不报错
	root := t.TempDir()
	p, _ := paths.New(root)
	if err := os.MkdirAll(p.CacheArchives(), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, _ := LoadCacheIndex(p.CacheIndex())
	idx.Touch("ghost.tar.gz", 500, time.Now())
	if err := idx.Save(p.CacheIndex()); err != nil {
		t.Fatal(err)
	}
	res, err := PruneCache(p, 100, time.Now(), false)
	if err != nil {
		t.Fatalf("索引里的幽灵文件不该让 prune 失败: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("磁盘上没有的文件不该算进删除列表: %v", res.Removed)
	}
	back, _ := LoadCacheIndex(p.CacheIndex())
	if _, ok := back.Entries["ghost.tar.gz"]; ok {
		t.Fatal("幽灵记录应被清掉")
	}
}
