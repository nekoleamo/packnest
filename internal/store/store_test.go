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
	l.Upsert(Install{Tool: "deno", Version: "2.1.9", Path: "/x"})
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
