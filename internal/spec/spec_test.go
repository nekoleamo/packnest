package spec

import "testing"

func TestParseKinds(t *testing.T) {
	cases := []struct {
		in   string
		kind Kind
	}{
		{"22.22.2", Exact},
		{"v22.22.2", Exact},
		{"22", Range},
		{"22.22", Range},
		{"^20.17", Range},
		{">=20.5.0", Range},
		{"latest", Alias},
		{"LTS", Alias},
		{"lts/*", Alias},
	}
	for _, c := range cases {
		sp, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if sp.Kind() != c.kind {
			t.Errorf("Parse(%q).Kind = %v，期望 %v", c.in, sp.Kind(), c.kind)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "  ", "abc", "22.22.2.3", "1.x.y", ">>="} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) 应报错", bad)
		}
	}
}

func TestMatchPrefix(t *testing.T) {
	cases := []struct {
		spec, version string
		want          bool
	}{
		{"22", "22.22.2", true},
		{"22", "22.0.0", true},
		{"22", "23.0.0", false},
		{"22", "21.9.9", false},
		{"22.22", "22.22.2", true},
		{"22.22", "22.21.1", false},
		{"22.22.2", "22.22.2", true},
		{"22.22.2", "22.22.3", false},
		{"^20.17.0", "20.18.1", true},
		{">=22.9.0", "20.18.1", false},
	}
	for _, c := range cases {
		sp := MustParse(c.spec)
		if got := sp.Match(c.version); got != c.want {
			t.Errorf("%q.Match(%q) = %v，期望 %v", c.spec, c.version, got, c.want)
		}
	}
}

func TestAliasNeverMatches(t *testing.T) {
	for _, a := range []string{"latest", "lts"} {
		sp := MustParse(a)
		if !sp.IsAlias() || sp.Alias() != a {
			t.Fatalf("%q 应是别名 %q，实际 kind=%v alias=%q", a, a, sp.Kind(), sp.Alias())
		}
		if sp.Match("22.22.2") {
			t.Errorf("别名 %q 不应直接匹配版本", a)
		}
	}
}

func TestBestExcludesPrerelease(t *testing.T) {
	// 方案 §7 坑 1：不排除预发布，node 20 会被装成 npm 12.0.0-pre.0.0。
	candidates := []string{"11.21.2", "12.0.0-pre.0.0"}
	got, ok := Best(candidates, MustParse("20"))
	if ok {
		t.Fatalf("Best = %q，预发布不应入选", got)
	}
	got, ok = Best([]string{"11.21.2", "12.0.0-pre.0.0"}, MustParse("11"))
	if !ok || got != "11.21.2" {
		t.Fatalf("Best = %q/%v，期望 11.21.2", got, ok)
	}
}

func TestBestExplicitPrerelease(t *testing.T) {
	got, ok := Best([]string{"11.21.2", "12.0.0-pre.0.0"}, MustParse("12.0.0-pre.0.0"))
	if !ok || got != "12.0.0-pre.0.0" {
		t.Fatalf("显式钉预发布时应命中，得到 %q/%v", got, ok)
	}
}

func TestBestOrderInsensitive(t *testing.T) {
	cands := []string{"22.21.1", "22.22.2", "22.20.0"}
	got, ok := Best(cands, MustParse("22"))
	if !ok || got != "22.22.2" {
		t.Fatalf("Best = %q/%v", got, ok)
	}
}

func TestBestSkipsGarbage(t *testing.T) {
	got, ok := Best([]string{"not-a-version", "", "1.2.3"}, MustParse("1"))
	if !ok || got != "1.2.3" {
		t.Fatalf("Best = %q/%v", got, ok)
	}
}

func TestLatestAndSort(t *testing.T) {
	if v, ok := Latest([]string{"1.0.0", "2.0.0-pre", "1.5.0"}); !ok || v != "1.5.0" {
		t.Fatalf("Latest = %q/%v", v, ok)
	}
	sorted := SortDesc([]string{"1.0.0", "1.10.0", "1.2.0", "2.0.0-rc.1"})
	want := []string{"1.10.0", "1.2.0", "1.0.0"}
	if len(sorted) != len(want) {
		t.Fatalf("SortDesc = %v", sorted)
	}
	for i := range want {
		if sorted[i] != want[i] {
			t.Fatalf("SortDesc = %v，期望 %v", sorted, want)
		}
	}
}

func TestEnginesSatisfied(t *testing.T) {
	cases := []struct {
		engines, version string
		want             bool
		wantErr          bool
	}{
		{"", "22.22.2", true, false},
		{"*", "22.22.2", true, false},
		{"^20.17.0 || >=22.9.0", "20.18.1", true, false},
		{"^22.22.2 || ^24.15.0 || >=26.0.0", "22.21.1", false, false},
		{"^22.22.2 || ^24.15.0 || >=26.0.0", "22.22.2", true, false},
		{"^22.22.2 || ^24.15.0 || >=26.0.0", "24.14.0", false, false},
		{"^22.22.2 || ^24.15.0 || >=26.0.0", "24.15.0", true, false},
		{"^22.22.2 || ^24.15.0 || >=26.0.0", "26.13.0", true, false},
		{"bogus range", "1.0.0", false, true},
	}
	for _, c := range cases {
		got, err := EnginesSatisfied(c.engines, c.version)
		if (err != nil) != c.wantErr {
			t.Fatalf("EnginesSatisfied(%q,%q) err = %v", c.engines, c.version, err)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("EnginesSatisfied(%q,%q) = %v，期望 %v", c.engines, c.version, got, c.want)
		}
	}
}

func TestEnginesPrereleaseBoundary(t *testing.T) {
	// npm 12.0.0-pre.0.0 的 engines 确实满足 node 20 —— 靠版本侧预发布过滤兜住。
	ok, err := EnginesSatisfied("^20.17.0 || >=22.9.0", "20.18.1")
	if err != nil || !ok {
		t.Fatalf("应满足: %v/%v", ok, err)
	}
	if !IsPrerelease("12.0.0-pre.0.0") {
		t.Fatal("12.0.0-pre.0.0 应识别为预发布")
	}
	if IsPrerelease("12.2.2") {
		t.Fatal("12.2.2 不是预发布")
	}
}

func TestNormalize(t *testing.T) {
	if got, err := Normalize("v22.22.2"); err != nil || got != "22.22.2" {
		t.Fatalf("Normalize = %q/%v", got, err)
	}
	if _, err := Normalize("bogus"); err == nil {
		t.Fatal("非法版本应报错")
	}
	if _, err := Normalize(""); err == nil {
		t.Fatal("空版本应报错")
	}
}

func FuzzSpec(f *testing.F) {
	f.Add("22.22.2")
	f.Add("^20.17.0 || >=22.9.0")
	f.Add("lts")
	f.Fuzz(func(t *testing.T, in string) {
		sp, err := Parse(in)
		if err != nil {
			return
		}
		_ = sp.Kind()
		_ = sp.Match("22.22.2")
	})
}

// BenchmarkSpecParse 的门禁阈值是 < 50µs（方案 §9.5 第 6 项）。
func BenchmarkSpecParse(b *testing.B) {
	inputs := []string{"22.22.2", "22", "^20.17.0 || >=22.9.0", "lts"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(inputs[i%len(inputs)]); err != nil {
			b.Fatal(err)
		}
	}
}
