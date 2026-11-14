package spec

import (
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

// 性质测试（完善路线 §E8c）：不是"再补几个例子"，而是断言对所有输入都必须成立的关系。
// 例子测试挡不住"某类输入没想到"，性质测试能把不变量钉死。

// TestPropertyParseIdempotent：parse → String → parse 必须收敛（第二次与第一次完全一致）。
func TestPropertyParseIdempotent(t *testing.T) {
	corpus := []string{
		"22", "22.22.2", "^22", ">=20 <24", "~20.5", "22.x", "latest", "lts", "lts/iron",
		"3.12", "1.27.3", "0.10", "v22.22.2", "  22  ", "12.2.2-pre.0.0",
		"22.22.2-rc.1", "22 || 24", ">=20,<24", "*", "0",
	}
	for _, in := range corpus {
		s1, err := Parse(in)
		if err != nil {
			continue // 非法输入本身就是合法结果（Parse 的契约是"拒绝"）
		}
		s2, err := Parse(s1.String())
		if err != nil {
			t.Fatalf("%q：二次解析失败: %v", in, err)
		}
		if s1.String() != s2.String() || s1.Kind() != s2.Kind() || s1.Alias() != s2.Alias() {
			t.Fatalf("%q 不幂等: %+v → %+v", in, s1, s2)
		}
	}
}

// TestPropertySortDescTotalOrder：版本排序必须是全序（反对称 + 传递）+ 幂等。
// 契约：丢掉预发布、丢掉解析不了的，稳定版本一个不许丢、一个不许重。
func TestPropertySortDescTotalOrder(t *testing.T) {
	raw := []string{
		"22.22.2", "22.9.0", "22.10.0", "0.10.0", "0.12.0", "26.0.0",
		"22.22.2-rc.1", "1.0.0", "10.0.0", "1.0.0-alpha", "20.5.1", "20.5.0",
	}
	want := 0
	for _, v := range raw {
		if ver, err := ParseVersion(v); err == nil && ver.Prerelease() == "" {
			want++
		}
	}
	sorted := SortDesc(raw)
	if len(sorted) != want {
		t.Fatalf("稳定版本应全保留（%d 个），实际 %d: %v", want, len(sorted), sorted)
	}
	seen := map[string]bool{}
	for _, v := range sorted {
		if seen[v] {
			t.Fatalf("排序后出现重复: %s", v)
		}
		seen[v] = true
	}
	// 反对称 + 传递：用 semver 的 Compare 独立复算一遍
	for i := 0; i+1 < len(sorted); i++ {
		a, err1 := semver.NewVersion(sorted[i])
		b, err2 := semver.NewVersion(sorted[i+1])
		if err1 != nil || err2 != nil {
			t.Fatalf("非法版本: %v", sorted)
		}
		if a.Compare(b) <= 0 {
			t.Fatalf("降序被破坏: %s !> %s", sorted[i], sorted[i+1])
		}
	}
	// 幂等：再排一次不变（全序的直接推论）
	again := SortDesc(sorted)
	for i := range sorted {
		if sorted[i] != again[i] {
			t.Fatalf("排序幂等被破坏: %v vs %v", sorted, again)
		}
	}
}

// TestPropertyBestMatchesMatch：Best 选出来的版本必须让 Match 为真，
// 且不小于候选集里任何"同样匹配"的版本（否则"最新"就是假的）。
func TestPropertyBestMatchesMatch(t *testing.T) {
	candidates := []string{"22.9.0", "22.22.2", "22.10.0", "20.5.1", "22.22.2-rc.1"}
	// 别名（latest/lts）不参与 Best：调用方必须先解析别名，这里只断言"别乱选"
	if got, ok := Best(candidates, MustParse("latest")); ok {
		t.Fatalf("别名不该由 Best 解析，却选了 %s", got)
	}
	for _, q := range []string{"22", "^22", ">=22.10", "22.x", ">=0"} {
		s, err := Parse(q)
		if err != nil {
			t.Fatalf("%q 解析失败: %v", q, err)
		}
		got, ok := Best(candidates, s)
		if !ok {
			t.Fatalf("%q 应该能选出结果", q)
		}
		if !s.Match(got) {
			t.Fatalf("%q 选出的 %s 自己不匹配", q, got)
		}
		if strings.Contains(got, "-") {
			t.Fatalf("%q 不该选到预发布版: %s", q, got)
		}
		gv, _ := semver.NewVersion(got)
		for _, c := range candidates {
			cv, err := semver.NewVersion(c)
			if err != nil || !s.MatchVersion(cv) || strings.Contains(c, "-") {
				continue
			}
			if cv.GreaterThan(gv) {
				t.Fatalf("%q：%s 比选出来的 %s 更新", q, c, got)
			}
		}
	}
}
