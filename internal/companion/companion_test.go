package companion

import (
	"strings"
	"testing"
)

// npmMatrix 是方案 §7 的实测匹配矩阵（源 registry.npmmirror.com/npm，已排除预发布）。
var npmMatrix = []Candidate{
	{Version: "10.9.10", Engines: "^18.17.0 || >=20.5.0"},
	{Version: "11.21.2", Engines: "^20.17.0 || >=22.9.0"},
	{Version: "12.2.2", Engines: "^22.22.2 || ^24.15.0 || >=26.0.0"},
	{Version: "12.0.0-pre.0.0", Engines: "^20.17.0 || >=22.9.0"}, // 预发布陷阱
}

func TestCompanionMatrix(t *testing.T) {
	cases := []struct {
		node string
		want string
	}{
		{"18.20.4", "10.9.10"},
		{"20.18.1", "11.21.2"},
		{"22.21.1", "11.21.2"}, // ⭐ 补丁号分界：22.21.1 不是 12.x
		{"22.22.2", "12.2.2"},  // ⭐ 22.22.2 才是 12.x
		{"24.14.0", "11.21.2"},
		{"24.15.0", "12.2.2"},
		{"26.13.0", "12.2.2"},
	}
	for _, c := range cases {
		got, ok := MaxCompatible(c.node, npmMatrix)
		if !ok || got != c.want {
			t.Errorf("node %s → npm %s/%v，期望 %s", c.node, got, ok, c.want)
		}
	}
}

func TestCompanionExcludesPrerelease(t *testing.T) {
	// 不排除预发布会被装成 12.0.0-pre.0.0（本人第一版脚本就这么错的）。
	got, ok := MaxCompatible("20.18.1", npmMatrix)
	if !ok || got != "11.21.2" {
		t.Fatalf("node 20 应得 11.21.2，得到 %q/%v", got, ok)
	}
}

func TestCompanionPatchLevelBoundary(t *testing.T) {
	if got, _ := MaxCompatible("22.21.1", npmMatrix); got != "11.21.2" {
		t.Fatalf("22.21.1 必须落在 11.x，得到 %q", got)
	}
	if got, _ := MaxCompatible("22.22.2", npmMatrix); got != "12.2.2" {
		t.Fatalf("22.22.2 必须落在 12.x，得到 %q", got)
	}
}

func TestResolveLatestCompatible(t *testing.T) {
	dec, err := Resolve(Request{
		NodeVersion: "26.13.0", Bundled: "11.20.0",
		Mode: ModeLatestCompatible, Candidates: npmMatrix, IndexAvailable: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Version != "12.2.2" || !dec.Replaced || dec.Mode != ModeLatestCompatible {
		t.Fatalf("应替换为 12.2.2: %+v", dec)
	}
}

func TestResolveKeepsBundledWhenNewer(t *testing.T) {
	dec, err := Resolve(Request{
		NodeVersion: "22.22.2", Bundled: "12.2.2",
		Mode: ModeLatestCompatible, Candidates: npmMatrix, IndexAvailable: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Version != "12.2.2" || dec.Replaced {
		t.Fatalf("自带版本已是最优，不该替换: %+v", dec)
	}
}

func TestResolveFallbackWithoutIndex(t *testing.T) {
	dec, err := Resolve(Request{
		NodeVersion: "26.13.0", Bundled: "11.20.0", Mode: ModeLatestCompatible, IndexAvailable: false,
	})
	if err != nil {
		t.Fatalf("离线不该失败: %v", err)
	}
	if dec.Version != "11.20.0" || !dec.Fallback || dec.Replaced {
		t.Fatalf("离线应沿用自带: %+v", dec)
	}
}

func TestResolveBundledMode(t *testing.T) {
	dec, err := Resolve(Request{NodeVersion: "26.13.0", Bundled: "11.20.0", Mode: ModeBundled,
		Candidates: npmMatrix, IndexAvailable: true})
	if err != nil || dec.Version != "11.20.0" || dec.Replaced || dec.Mode != ModeBundled {
		t.Fatalf("bundled 模式应原样: %+v %v", dec, err)
	}
}

func TestResolveExplicitPin(t *testing.T) {
	dec, err := Resolve(Request{
		NodeVersion: "22.22.2", Bundled: "11.20.0", Explicit: "11.21.2",
		Mode: ModeLatestCompatible, Candidates: npmMatrix, IndexAvailable: true,
	})
	if err != nil {
		t.Fatalf("显式 pin 应优先于 latest-compatible: %v", err)
	}
	if dec.Version != "11.21.2" || dec.Mode != "explicit" || !dec.Replaced {
		t.Fatalf("显式 pin 结果异常: %+v", dec)
	}
}

func TestResolvePinConflict(t *testing.T) {
	// npm 11.21.2 要求 node ^20.17.0 || >=22.9.0，与 node 18.20.4 冲突 → 硬失败 + 给出上限。
	_, err := Resolve(Request{
		NodeVersion: "18.20.4", Bundled: "10.9.10", Explicit: "11.21.2",
		Candidates: npmMatrix, IndexAvailable: true,
	})
	if err == nil {
		t.Fatal("不兼容的显式 pin 必须硬失败")
	}
	if !strings.Contains(err.Error(), "该 node 可用的 npm 上限 = 10.9.10") {
		t.Fatalf("错误信息应给出兼容上限: %v", err)
	}
}

func TestResolvePinMissing(t *testing.T) {
	if _, err := Resolve(Request{NodeVersion: "22.22.2", Explicit: "9.9.9",
		Candidates: npmMatrix, IndexAvailable: true}); err == nil {
		t.Fatal("registry 里没有的版本应报错")
	} else if !strings.Contains(err.Error(), "registry 里没有") {
		t.Fatalf("错误信息应可执行: %v", err)
	}
}

func TestResolvePinOffline(t *testing.T) {
	dec, err := Resolve(Request{NodeVersion: "22.22.2", Bundled: "11.20.0", Explicit: "12.2.2",
		IndexAvailable: false})
	if err != nil {
		t.Fatalf("离线时显式 pin 不该失败: %v", err)
	}
	if dec.Version != "12.2.2" || !dec.Fallback || !dec.Replaced {
		t.Fatalf("离线 pin 结果异常: %+v", dec)
	}
}

func TestResolvePinnedModeNeedsExplicit(t *testing.T) {
	if _, err := Resolve(Request{NodeVersion: "22.22.2", Mode: ModePinned,
		Candidates: npmMatrix, IndexAvailable: true}); err == nil {
		t.Fatal("pinned 模式缺显式版本应报错")
	}
	dec, err := Resolve(Request{NodeVersion: "22.22.2", Bundled: "11.20.0", Explicit: "auto",
		Mode: ModeLatestCompatible, Candidates: npmMatrix, IndexAvailable: true})
	if err != nil || dec.Version != "12.2.2" {
		t.Fatalf("npm = \"auto\" 应视为未指定: %+v %v", dec, err)
	}
}

func TestResolveNoCompatibleFallback(t *testing.T) {
	dec, err := Resolve(Request{
		NodeVersion: "12.0.0", Bundled: "6.14.18", Mode: ModeLatestCompatible,
		Candidates: npmMatrix, IndexAvailable: true,
	})
	if err != nil {
		t.Fatalf("没有兼容版本也不该失败: %v", err)
	}
	if dec.Version != "6.14.18" || !dec.Fallback {
		t.Fatalf("应退回自带: %+v", dec)
	}
}

func TestResolveEmptyBundled(t *testing.T) {
	dec, err := Resolve(Request{NodeVersion: "22.22.2", Bundled: "", Explicit: "11.21.2",
		Candidates: npmMatrix, IndexAvailable: true})
	if err != nil || dec.Version != "11.21.2" || !dec.Replaced {
		t.Fatalf("没有自带版本时应替换: %+v %v", dec, err)
	}
}

func TestResolveDefaultMode(t *testing.T) {
	dec, err := Resolve(Request{NodeVersion: "26.13.0", Bundled: "11.20.0",
		Candidates: npmMatrix, IndexAvailable: true})
	if err != nil || dec.Mode != ModeLatestCompatible {
		t.Fatalf("默认模式应是 latest-compatible: %+v %v", dec, err)
	}
}

func TestMaxCompatibleTrimsVPrefix(t *testing.T) {
	got, ok := MaxCompatible("22.22.2", []Candidate{{Version: "v12.2.2", Engines: ">=22"}})
	if !ok || got != "12.2.2" {
		t.Fatalf("v 前缀应归一: %q/%v", got, ok)
	}
}
