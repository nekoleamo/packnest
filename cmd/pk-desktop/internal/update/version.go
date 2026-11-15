package update

import (
	"fmt"
	"strconv"
	"strings"
)

// ver 是一个够用的三段式版本（GUI 与 CLI 都只有 X.Y.Z；预发布按"比正式小"处理）。
type ver struct {
	nums [3]int
	pre  bool
}

func parseVersion(s string) (ver, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" {
		return ver{}, fmt.Errorf("空版本号")
	}
	core := s
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core, s = core[:i], core[i:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return ver{}, fmt.Errorf("版本号 %q 不是 X.Y.Z", s)
	}
	var v ver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return ver{}, fmt.Errorf("版本号 %q 的第 %d 段不是数字", s, i+1)
		}
		v.nums[i] = n
	}
	v.pre = strings.HasPrefix(s, "-")
	return v, nil
}

// newer 判断 a 是否比 b 新（a 是预发布、b 是正式同号时算"不新"）。
func newer(a, b string) bool {
	av, aerr := parseVersion(a)
	bv, berr := parseVersion(b)
	if aerr != nil {
		return false
	}
	if berr != nil {
		return true // 当前版本是 dev / 空：远端有正式版本就提示
	}
	for i := 0; i < 3; i++ {
		if av.nums[i] != bv.nums[i] {
			return av.nums[i] > bv.nums[i]
		}
	}
	return !av.pre && bv.pre
}
