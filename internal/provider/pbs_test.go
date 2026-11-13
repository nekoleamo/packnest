package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionDirs(t *testing.T) {
	data := []byte(`[
	  {"name":"20181218/","type":"dir"},
	  {"name":"20261026/","type":"dir"},
	  {"name":"20261020/","type":"dir"},
	  {"name":"README.md","type":"file"}
	]`)
	tags, err := versionDirs(data)
	if err != nil {
		t.Fatalf("versionDirs: %v", err)
	}
	if len(tags) != 3 || tags[0] != "20261026" {
		t.Fatalf("应按日期降序，得到 %v", tags)
	}
	if _, err := versionDirs([]byte(`[]`)); err == nil {
		t.Fatal("没有标签目录应报错")
	}
	if _, err := versionDirs([]byte(`{}`)); err == nil {
		t.Fatal("非数组应报错")
	}
}

func TestParseFiles(t *testing.T) {
	names := []struct{ name string }{}
	for _, n := range []string{
		"cpython-3.13.18+20261026-aarch64-apple-darwin-install_only.tar.gz",
		"cpython-3.13.18+20261026-aarch64-apple-darwin-install_only_stripped.tar.gz",
		"cpython-3.13.18+20261026-aarch64-apple-darwin-debug-full.tar.zst",
		"cpython-3.12.16+20261026-aarch64-apple-darwin-install_only.tar.gz",
		"cpython-3.13.18+20261026-riscv64-unknown-linux-gnu-install_only.tar.gz",
		"SHA256SUMS",
	} {
		names = append(names, struct{ name string }{n})
	}
	body, err := json.Marshal(func() []map[string]string {
		out := make([]map[string]string, 0, len(names))
		for _, n := range names {
			out = append(out, map[string]string{"name": n.name, "type": "file"})
		}
		return out
	}())
	if err != nil {
		t.Fatal(err)
	}
	platforms := map[string]string{
		"aarch64-apple-darwin":     "darwin-arm64",
		"x86_64-unknown-linux-gnu": "linux-amd64",
	}
	files, err := parsePBSFiles(body, "20261026", platforms)
	if err != nil {
		t.Fatalf("parseFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应只留 install_only（非 stripped、声明过的平台），得到 %+v", files)
	}
	for _, f := range files {
		if f.Tag != "20261026" {
			t.Fatalf("tag 应带上: %+v", f)
		}
		if strings.Contains(f.Filename, "stripped") {
			t.Fatalf("stripped 变体不该入选: %+v", f)
		}
	}
}

func TestGroupPBS(t *testing.T) {
	platforms := map[string]string{
		"aarch64-apple-darwin":     "darwin-arm64",
		"x86_64-unknown-linux-gnu": "linux-amd64",
	}
	files := []pbsFile{
		{Version: "3.13.18", Tag: "20261026", Platform: "aarch64-apple-darwin", Filename: "a.gz"},
		{Version: "3.13.18", Tag: "20261026", Platform: "x86_64-unknown-linux-gnu", Filename: "b.gz"},
		{Version: "3.12.16", Tag: "20261026", Platform: "aarch64-apple-darwin", Filename: "c.gz"},
	}
	versions, err := groupPBS(files, platforms, "20261026")
	if err != nil {
		t.Fatalf("groupPBS: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("应聚合成 2 个版本: %+v", versions)
	}
	if len(versions[0].Files) != 2 {
		t.Fatalf("3.13.18 应有两个平台产物: %+v", versions[0])
	}
	if f, ok := versions[0].FileFor("linux", "amd64"); !ok || f.Filename != "b.gz" {
		t.Fatalf("FileFor 应能按平台取到文件名: %+v", f)
	}
	if versions[0].Tag != "20261026" {
		t.Fatalf("版本应带 tag: %+v", versions[0])
	}
	if _, err := groupPBS(nil, platforms, "x"); err == nil {
		t.Fatal("空文件列表应报错")
	}
}

// TestLoadPBSIndexEndToEnd 走假镜像，验证两级列举 + 新标签优先。

func TestSplitPlatform(t *testing.T) {
	os, arch := splitPlatform("darwin-arm64")
	if os != "darwin" || arch != "arm64" {
		t.Fatalf("splitPlatform = %q/%q", os, arch)
	}
	if os, arch := splitPlatform("nohyphen"); os != "nohyphen" || arch != "" {
		t.Fatalf("无连字符时应整串作为 os: %q/%q", os, arch)
	}
}

func TestIsTag(t *testing.T) {
	for _, ok := range []string{"20261026", "20181218"} {
		if !isTag(ok) {
			t.Errorf("%q 应是合法标签", ok)
		}
	}
	for _, bad := range []string{"2026102", "202610266", "canary", "2026-10-26", ""} {
		if isTag(bad) {
			t.Errorf("%q 不该是合法标签", bad)
		}
	}
}

func TestVersionDirsSkipsFilesAndJunk(t *testing.T) {
	data := []byte(`[
	  {"name":"20261026","type":"dir"},
	  {"name":"README.md","type":"file"},
	  {"name":"canary/","type":"dir"},
	  {"name":"20261020/"}
	]`)
	tags, err := versionDirs(data)
	if err != nil {
		t.Fatalf("versionDirs: %v", err)
	}
	if len(tags) != 2 || tags[0] != "20261026" || tags[1] != "20261020" {
		t.Fatalf("应只留 8 位日期目录: %v", tags)
	}
}

func TestParseFilesErrors(t *testing.T) {
	platforms := map[string]string{"aarch64-apple-darwin": "darwin-arm64"}
	if _, err := parsePBSFiles([]byte(`{}`), "20261026", platforms); err == nil {
		t.Fatal("非数组应报错")
	}
	// 只有未声明平台/变体 → 报错
	body := []byte(`[{"name":"cpython-3.13.18+20261026-riscv64-unknown-linux-gnu-install_only.tar.gz","type":"file"}]`)
	if _, err := parsePBSFiles(body, "20261026", platforms); err == nil {
		t.Fatal("没有匹配平台时应报错")
	}
}
