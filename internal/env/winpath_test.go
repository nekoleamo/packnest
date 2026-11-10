package env

import "testing"

// 注册表 Path 的解析：空项、多余空白、顺序都要稳（写坏用户 PATH 是不可接受的）。
func TestSplitPathList(t *testing.T) {
	got := splitPathList(`C:\Windows;; C:\Tools ;C:\Program Files\Git\cmd;`)
	want := []string{`C:\Windows`, `C:\Tools`, `C:\Program Files\Git\cmd`}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项 = %q，期望 %q", i, got[i], want[i])
		}
	}
	if len(splitPathList("")) != 0 {
		t.Fatal("空串应得到空列表")
	}
}

// 去重/删除时不能因为大小写或斜杠方向不同就漏判（Windows 路径就是这样）。
func TestSamePath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`C:\Users\me\.packnest\bin`, `c:\users\me\.packnest\bin`, true},
		{`C:\Users\me\.packnest\bin\`, `C:\Users\me\.packnest\bin`, true},
		{`C:/Users/me/.packnest/bin`, `C:\Users\me\.packnest\bin`, true},
		{`C:\Users\me\.packnest\bin`, `D:\Users\me\.packnest\bin`, false},
	}
	for _, c := range cases {
		if got := samePath(c.a, c.b); got != c.want {
			t.Errorf("samePath(%q,%q) = %v，期望 %v", c.a, c.b, got, c.want)
		}
	}
}
