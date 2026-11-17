package update

import (
	"testing"
)

// 版本解析与比较：GUI 的"要不要提示更新"完全依赖这两个函数，错了就是不停提示或永不提示。

func TestParseVersion(t *testing.T) {
	ok := []struct {
		in   string
		nums [3]int
		pre  bool
	}{
		{"1.2.3", [3]int{1, 2, 3}, false},
		{"v1.2.3", [3]int{1, 2, 3}, false},
		{"  v0.12.3  ", [3]int{0, 12, 3}, false},
		{"1.2.3-rc.1", [3]int{1, 2, 3}, true},
		{"1.2.3+build.7", [3]int{1, 2, 3}, false},
	}
	for _, c := range ok {
		v, err := parseVersion(c.in)
		if err != nil {
			t.Errorf("parseVersion(%q) 报错: %v", c.in, err)
			continue
		}
		if v.nums != c.nums || v.pre != c.pre {
			t.Errorf("parseVersion(%q) = %+v，期望 %v pre=%v", c.in, v, c.nums, c.pre)
		}
	}

	bad := []string{"", "   ", "1.2", "1.2.3.4", "1.2.x", "x.2.3", "-1.2.3", "1.-2.3"}
	for _, in := range bad {
		if _, err := parseVersion(in); err == nil {
			t.Errorf("parseVersion(%q) 应当报错", in)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.10.0", "1.9.9", true},     // 逐段比，不是字符串比
		{"1.2.3", "1.2.3", false},     // 同号不提示
		{"1.2.3", "1.2.3-rc.1", true}, // 正式 > 同号预发布
		{"1.2.3-rc.1", "1.2.3", false},
		{"0.13.0", "0.12.3", true},
		{"dev", "1.0.0", false}, // 远端版本读不懂：不提示
		{"1.0.0", "dev", true},  // 本机版本读不懂：远端有正式版就提示
		{"1.0.0", "", true},     // 空当前版本（首次运行）
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q,%q) = %v，期望 %v", c.a, c.b, got, c.want)
		}
	}
}
