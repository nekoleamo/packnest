package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "installs.json")
	l, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("缺失台账应是空台账: %v", err)
	}
	if l.Schema != LedgerSchema || len(l.Installs) != 0 {
		t.Fatalf("空台账异常: %+v", l)
	}

	l.Upsert(Install{Tool: "node", Version: "22.22.2", Path: "/p/node", Source: "cn",
		SHA256: "abc", Verified: "strong", NpmBundled: "11.20.0", NpmVersion: "12.2.2"})
	l.Upsert(Install{Tool: "pnpm", Version: "10.4.1", Path: "/p/pnpm", References: []Ref{{Kind: RefTool, Name: "node@22.22.2"}}})
	if err := l.AddRef("node", "22.22.2", Ref{Kind: RefGlobal}); err != nil {
		t.Fatalf("AddRef: %v", err)
	}
	if err := l.AddRef("node", "22.22.2", Ref{Kind: RefGlobal}); err != nil {
		t.Fatalf("重复 AddRef 应幂等: %v", err)
	}
	if err := l.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(back.Installs) != 2 {
		t.Fatalf("记录数 = %d", len(back.Installs))
	}
	if got := back.Active("node"); got != "22.22.2" {
		t.Fatalf("Active = %q", got)
	}
	in := back.Find("node", "22.22.2")
	if in == nil || in.NpmVersion != "12.2.2" || len(in.References) != 1 {
		t.Fatalf("记录内容异常: %+v", in)
	}
	if back.Find("node", "20.0.0") != nil {
		t.Fatal("不存在的记录应返回 nil")
	}
}

func TestLedgerBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installs.json")
	l := &Ledger{Schema: LedgerSchema}
	l.Upsert(Install{Tool: "node", Version: "1.0.0", Path: dir})
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	l.Upsert(Install{Tool: "node", Version: "2.0.0", Path: dir})
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	bak, err := LoadLedger(path + ".bak")
	if err != nil {
		t.Fatalf("应生成 .bak: %v", err)
	}
	if len(bak.Installs) != 1 || bak.Installs[0].Version != "1.0.0" {
		t.Fatalf(".bak 应是上一版: %+v", bak.Installs)
	}
}

func TestLedgerCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLedger(path); err == nil {
		t.Fatal("损坏台账应报错（doctor 给恢复建议）")
	}
}

func TestLedgerUpsertReplaces(t *testing.T) {
	l := &Ledger{Schema: LedgerSchema}
	l.Upsert(Install{Tool: "node", Version: "22.22.2", Path: "/a"})
	l.Upsert(Install{Tool: "node", Version: "22.22.2", Path: "/b"})
	if len(l.Installs) != 1 || l.Installs[0].Path != "/b" {
		t.Fatalf("同工具同版本应覆盖: %+v", l.Installs)
	}
	if l.Installs[0].InstalledAt.IsZero() {
		t.Fatal("InstalledAt 应自动填")
	}
	if !l.Remove("node", "22.22.2") {
		t.Fatal("Remove 应成功")
	}
	if l.Remove("node", "22.22.2") {
		t.Fatal("重复 Remove 应返回 false")
	}
}

func TestAddRefMissing(t *testing.T) {
	l := &Ledger{Schema: LedgerSchema}
	if err := l.AddRef("node", "1.0.0", Ref{Kind: RefGlobal}); err == nil {
		t.Fatal("记录不存在时应报错")
	}
}

func TestValidateFindsIssues(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "node")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	l := &Ledger{Schema: LedgerSchema}
	l.Upsert(Install{Tool: "node", Version: "1.0.0", Path: real})
	l.Installs = append(l.Installs, Install{Tool: "node", Version: "1.0.0", Path: real})
	l.Installs = append(l.Installs, Install{Tool: "bun", Version: "1.0.0", Path: filepath.Join(dir, "gone")})
	issues := l.Validate()
	if len(issues) < 2 {
		t.Fatalf("应发现重复与目录缺失: %v", issues)
	}
	if l := (&Ledger{Schema: LedgerSchema}); len(l.Validate()) != 0 {
		t.Fatal("空台账应无问题")
	}
}

func TestConfigDefaultsAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("缺失配置应返回默认: %v", err)
	}
	if cfg.Mirror != "auto" || cfg.GlobalMode != "shared" || cfg.CacheMax != "5GB" {
		t.Fatalf("默认配置异常: %+v", cfg)
	}
	cfg.Mirror = "cn"
	cfg.CompanionModes["npm"] = "pinned"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadConfig(path)
	if err != nil || back.Mirror != "cn" || back.CompanionModes["npm"] != "pinned" {
		t.Fatalf("往返失败: %+v %v", back, err)
	}
}

func TestConfigCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("mirror = [oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("损坏配置应报错")
	}
}

func TestLockExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lock")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err := Lock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("第二个锁应失败并返回 ErrLocked，得到 %v", err)
	}
	unlock()
	unlock2, err := Lock(path)
	if err != nil {
		t.Fatalf("释放后应能再取锁: %v", err)
	}
	unlock2()
}

func TestWriteAtomicCreatesDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c.json")
	if err := WriteAtomic(path, []byte("{}")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("临时文件应被清掉")
	}
}

func TestInstallTimeDefault(t *testing.T) {
	l := &Ledger{Schema: LedgerSchema}
	before := time.Now().Add(-time.Second)
	l.Upsert(Install{Tool: "bun", Version: "1.4.4", Path: "/x"})
	if l.Installs[0].InstalledAt.Before(before) {
		t.Fatal("InstalledAt 应被填成当前时间")
	}
}

func TestLedgerSchemaUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installs.json")
	if err := os.WriteFile(path, []byte(`{"installs":[{"tool":"node","version":"1.0.0","path":"/x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("缺少 schema 应能读: %v", err)
	}
	if l.Schema != LedgerSchema {
		t.Fatalf("schema 应补齐为 %d，得到 %d", LedgerSchema, l.Schema)
	}
}

func TestActiveEmpty(t *testing.T) {
	l := &Ledger{Schema: LedgerSchema}
	l.Upsert(Install{Tool: "node", Version: "1.0.0", Path: "/x"})
	if got := l.Active("node"); got != "" {
		t.Fatalf("无全局引用时 Active 应为空，得到 %q", got)
	}
}

func TestConfigFillsEmptyKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("mirror = \"\"\nglobal_mode = \"\"\ncache_max = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mirror != "auto" || cfg.GlobalMode != "shared" || cfg.CacheMax != "5GB" {
		t.Fatalf("空值应补默认: %+v", cfg)
	}
}

func TestWriteAtomicError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(blocker, "x.json"), []byte("{}")); err == nil {
		t.Fatal("父路径是文件时应报错")
	}
	if _, err := Lock(filepath.Join(blocker, "lock")); err == nil {
		t.Fatal("锁目录无法创建时应报错")
	}
}

// install.json：安装脚本记录"pk 自己装到哪了"，`uninstall --all --self` 靠它精确删除。
func TestInstallInfoRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "install.json")
	if got := LoadInstallInfo(path); got != nil {
		t.Fatal("缺失时应返回 nil")
	}
	info := &InstallInfo{Channel: "script", Platform: "darwin-arm64", Version: "0.7.7", Dir: "/home/me/.local/bin"}
	if err := info.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back := LoadInstallInfo(path)
	if back == nil || back.Dir != info.Dir || back.Version != "0.7.7" || back.Schema != 1 {
		t.Fatalf("往返失败: %+v", back)
	}
	// 损坏的 JSON 当没有，不要让卸载流程崩
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadInstallInfo(path); got != nil {
		t.Fatalf("损坏时应返回 nil，得到 %+v", got)
	}
}

// TestSaveFailures：三份状态文件的写失败路径都要把错误抛出来（不能静默丢状态）。
// 手法：把目标路径做成目录 —— rename 到目录上必失败。
func TestSaveFailures(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	if err := os.MkdirAll(filepath.Join(dir, "config.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(filepath.Join(dir, "config.toml")); err == nil {
		t.Fatal("config 写进目录应报错")
	}

	l := &Ledger{}
	if err := os.MkdirAll(filepath.Join(dir, "ledger.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(filepath.Join(dir, "ledger.json")); err == nil {
		t.Fatal("台账写进目录应报错")
	}

	idx := &CacheIndex{}
	if err := os.MkdirAll(filepath.Join(dir, "cache.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(filepath.Join(dir, "cache.json")); err == nil {
		t.Fatal("缓存索引写进目录应报错")
	}
}

// TestWriteAtomicWithBackup：已有文件先备份成 .bak；.bak 写不进时整体失败（不写正文）。
func TestWriteAtomicWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicWithBackup(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("正文应写成 new: %q", got)
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != "old" {
		t.Fatalf(".bak 应是旧内容: %q", got)
	}
	// .bak 路径被目录占住 → 备份失败，正文不许被改
	if err := os.Remove(path + ".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path+".bak", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicWithBackup(path, []byte("newer")); err == nil {
		t.Fatal("备份失败必须报错")
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("备份失败时正文不该被改: %q", got)
	}
}

// TestLoadInstallInfo：没有 / 坏掉的 install.json 都返回 nil（不是错误）。
func TestLoadInstallInfo(t *testing.T) {
	dir := t.TempDir()
	if got := LoadInstallInfo(filepath.Join(dir, "nope.json")); got != nil {
		t.Fatalf("不存在应返回 nil: %+v", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadInstallInfo(bad); got != nil {
		t.Fatalf("坏文件应返回 nil: %+v", got)
	}
	good := filepath.Join(dir, "install.json")
	if err := os.WriteFile(good, []byte(`{"schema":1,"channel":"script","version":"1.2.3","dir":"/x/bin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadInstallInfo(good); got == nil || got.Dir != "/x/bin" || got.Channel != "script" {
		t.Fatalf("正常文件解析不对: %+v", got)
	}
}

// TestVersionsSorted：Versions 返回该工具的全部版本，按**字符串**升序。
// 语义顺序不是它的职责：要"最新"的调用方自己用 spec.SortDesc（22.10.0 在字符串序里小于 22.9.0）。
func TestVersionsSorted(t *testing.T) {
	l := &Ledger{}
	l.Upsert(Install{Tool: "node", Version: "22.10.0", Path: "/x"})
	l.Upsert(Install{Tool: "node", Version: "22.9.0", Path: "/x"})
	l.Upsert(Install{Tool: "go", Version: "1.27.3", Path: "/y"})
	if got := l.Versions("node"); len(got) != 2 || got[0] != "22.10.0" || got[1] != "22.9.0" {
		t.Fatalf("应升序返回 2 个版本: %v", got)
	}
	if got := l.Versions("nope"); len(got) != 0 {
		t.Fatalf("没有记录应返回空: %v", got)
	}
}

// TestLoadLedgerWithRecovery：主台账坏 → 用 .bak；.bak 也坏 → 报原始错误；没备份 → 报原始错误。
func TestLoadLedgerWithRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installs.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Run("没有备份", func(t *testing.T) {
		if _, _, err := LoadLedgerWithRecovery(path); err == nil {
			t.Fatal("没有 .bak 时应报原始错误")
		}
	})
	t.Run("备份可用", func(t *testing.T) {
		good := &Ledger{Schema: LedgerSchema}
		good.Upsert(Install{Tool: "node", Version: "22.98.0", Path: "/x"})
		if err := good.Save(path + ".bak"); err != nil {
			t.Fatal(err)
		}
		l, recovered, err := LoadLedgerWithRecovery(path)
		if err != nil || !recovered || l == nil || len(l.Installs) != 1 {
			t.Fatalf("应从备份恢复: %+v recovered=%v err=%v", l, recovered, err)
		}
	})
	t.Run("备份也坏", func(t *testing.T) {
		if err := os.WriteFile(path+".bak", []byte("{also broken"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadLedgerWithRecovery(path); err == nil {
			t.Fatal("备份也坏时应报错")
		}
	})
}

// TestCacheIndexTouch：Touch 会初始化 nil 表并覆盖同名的旧记录。
func TestCacheIndexTouch(t *testing.T) {
	idx := &CacheIndex{}
	now := time.Now()
	idx.Touch("node.tar.gz", 100, now)
	if idx.Entries["node.tar.gz"].Size != 100 {
		t.Fatalf("Touch 应记下大小: %+v", idx.Entries)
	}
	idx.Touch("node.tar.gz", 200, now.Add(time.Hour))
	if idx.Entries["node.tar.gz"].Size != 200 || !idx.Entries["node.tar.gz"].LastUsed.After(now) {
		t.Fatalf("Touch 应覆盖旧记录: %+v", idx.Entries["node.tar.gz"])
	}
}
