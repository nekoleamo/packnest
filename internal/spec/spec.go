// Package spec 处理版本说明符、engines 范围匹配与预发布过滤。
//
// 语义约定（方案 §18.9 / §19.2）：
//   - "22"      → 22.x 的最新补丁（>=22.0.0 <23.0.0）
//   - "22.22"   → 22.22.x 的最新补丁
//   - "22.22.2" → 精确版本
//   - "^20.17" / ">=20" → 直接交给 semver 约束
//   - "latest" / "lts" → 别名，须由索引层解析（`--frozen` 下直接报错）
package spec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Kind 是说明符的类型。
type Kind int

const (
	// Exact 精确版本。
	Exact Kind = iota
	// Range 前缀或 semver 约束（"22"、"22.22"、"^20.17"）。
	Range
	// Alias 别名（latest/lts），需索引解析。
	Alias
)

// Spec 是解析后的版本说明符。
type Spec struct {
	raw        string
	kind       Kind
	alias      string
	constraint *semver.Constraints
	// allowPre 由说明符自身是否含预发布标记决定，决定 Match 是否放行预发布版本。
	allowPre bool
}

// Parse 解析版本说明符。
func Parse(s string) (Spec, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return Spec{}, fmt.Errorf("版本说明符不能为空")
	}
	lowered := strings.ToLower(strings.TrimPrefix(raw, "v"))
	if lowered == "latest" || lowered == "lts" || lowered == "lts/*" {
		return Spec{raw: raw, kind: Alias, alias: strings.TrimPrefix(lowered, "lts/")}, nil
	}

	// 1 或 2 段的纯数字 → 前缀范围；3 段纯数字 → 精确。
	if parts, ok := numericParts(lowered); ok {
		switch len(parts) {
		case 1:
			c, err := semver.NewConstraint(fmt.Sprintf(">=%d.0.0 <%d.0.0", parts[0], parts[0]+1))
			if err != nil {
				return Spec{}, err
			}
			return Spec{raw: raw, kind: Range, constraint: c}, nil
		case 2:
			c, err := semver.NewConstraint(fmt.Sprintf(">=%d.%d.0 <%d.%d.0", parts[0], parts[1], parts[0], parts[1]+1))
			if err != nil {
				return Spec{}, err
			}
			return Spec{raw: raw, kind: Range, constraint: c}, nil
		case 3:
			c, err := semver.NewConstraint("=" + lowered)
			if err != nil {
				return Spec{}, fmt.Errorf("版本 %q 非法: %w", raw, err)
			}
			return Spec{raw: raw, kind: Exact, constraint: c, allowPre: strings.Contains(lowered, "-")}, nil
		}
	}

	c, err := semver.NewConstraint(lowered)
	if err != nil {
		return Spec{}, fmt.Errorf("版本说明符 %q 无法解析: %w", raw, err)
	}
	return Spec{raw: raw, kind: Range, constraint: c, allowPre: strings.Contains(lowered, "-")}, nil
}

// MustParse 仅用于测试与常量。
func MustParse(s string) Spec {
	sp, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return sp
}

func numericParts(s string) ([]int, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return nil, false
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out, true
}

// String 返回原始说明符。
func (s Spec) String() string { return s.raw }

// Kind 返回说明符类型。
func (s Spec) Kind() Kind { return s.kind }

// IsAlias 说明符是否为 latest/lts 别名。
func (s Spec) IsAlias() bool { return s.kind == Alias }

// Alias 返回别名（latest/lts），非别名返回空串。
func (s Spec) Alias() string { return s.alias }

// Match 判断版本字符串是否满足说明符；别名说明符永远返回 false（须先解析索引）。
func (s Spec) Match(v string) bool {
	ver, err := ParseVersion(v)
	if err != nil {
		return false
	}
	return s.MatchVersion(ver)
}

// MatchVersion 判断已解析版本是否满足说明符。
func (s Spec) MatchVersion(v *semver.Version) bool {
	if s.kind == Alias || s.constraint == nil {
		return false
	}
	if v.Prerelease() != "" && !s.allowPre && s.kind == Exact {
		return false
	}
	return s.constraint.Check(v)
}

// ParseVersion 解析发布版本号，容忍 "v" 前缀与 1~2 段写法（"22" → 22.0.0）。
func ParseVersion(v string) (*semver.Version, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if trimmed == "" {
		return nil, fmt.Errorf("版本号为空")
	}
	ver, err := semver.NewVersion(trimmed)
	if err != nil {
		return nil, fmt.Errorf("版本号 %q 非法: %w", v, err)
	}
	return ver, nil
}

// Normalize 把版本号规范化为 semver 字符串（去 v 前缀）。
func Normalize(v string) (string, error) {
	ver, err := ParseVersion(v)
	if err != nil {
		return "", err
	}
	return ver.String(), nil
}

// IsPrerelease 判断是否预发布版本（"12.0.0-pre.0.0" → true）。
func IsPrerelease(v string) bool {
	ver, err := ParseVersion(v)
	if err != nil {
		return false
	}
	return ver.Prerelease() != ""
}

// Best 在候选版本里挑满足说明符的最大版本。
//
// ⭐ 默认排除预发布（方案 §7 的坑 1：npm 12.0.0-pre.0.0 会污染 node↔npm 反查）。
func Best(candidates []string, s Spec) (string, bool) {
	best := ""
	var bestVer *semver.Version
	for _, c := range candidates {
		ver, err := ParseVersion(c)
		if err != nil {
			continue
		}
		if ver.Prerelease() != "" && !s.allowPre {
			continue
		}
		if !s.MatchVersion(ver) {
			continue
		}
		if bestVer == nil || ver.GreaterThan(bestVer) {
			best, bestVer = c, ver
		}
	}
	return best, bestVer != nil
}

// Latest 返回候选里最大的稳定版本（无稳定版本时返回 false）。
func Latest(candidates []string) (string, bool) {
	return Best(candidates, MustParse(">=0"))
}

// SortDesc 返回降序排列的稳定版本（预发布丢弃），用于展示。
func SortDesc(candidates []string) []string {
	kept := make([]*semver.Version, 0, len(candidates))
	orig := make(map[string]string, len(candidates))
	for _, c := range candidates {
		ver, err := ParseVersion(c)
		if err != nil || ver.Prerelease() != "" {
			continue
		}
		kept = append(kept, ver)
		orig[ver.String()] = c
	}
	slices.SortFunc(kept, func(a, b *semver.Version) int { return b.Compare(a) })
	out := make([]string, 0, len(kept))
	for _, v := range kept {
		out = append(out, orig[v.String()])
	}
	return out
}

// EnginesSatisfied 判断版本是否满足 package.json 的 engines 范围。
// engines 为空或 "*" 视为不限制。
func EnginesSatisfied(engines, version string) (bool, error) {
	engines = strings.TrimSpace(engines)
	if engines == "" || engines == "*" {
		return true, nil
	}
	ver, err := ParseVersion(version)
	if err != nil {
		return false, err
	}
	c, err := semver.NewConstraint(engines)
	if err != nil {
		return false, fmt.Errorf("engines 范围 %q 无法解析: %w", engines, err)
	}
	return c.Check(ver), nil
}
