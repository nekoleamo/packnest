package env

import "strings"

// Path 列表的解析/比较（Windows 注册表 Path 用 ";" 分隔）。
// 放在不带 build tag 的文件里，是为了能在非 Windows 上单测这两个纯函数。

// splitPathList 按 ";" 切分并去掉空项与多余空白。
func splitPathList(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// samePath 比较两个 Windows 路径（大小写不敏感、分隔符不敏感、忽略尾部反斜杠）。
func samePath(a, b string) bool {
	norm := func(p string) string {
		return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, "/", `\`), `\`))
	}
	return norm(a) == norm(b)
}
