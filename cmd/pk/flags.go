package main

import (
	"flag"
	"strings"
)

// parseFlexible 允许开关出现在位置参数之后（`pk install deno --insecure`）。
//
// 标准库 flag 遇到第一个非开关参数就停止解析，CLI 用户却习惯"命令 目标 --开关"，
// 所以这里先做一遍分离：开关（含需要值的）排到前面，位置参数排在后面。
func parseFlexible(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if idx := strings.Index(name, "="); idx >= 0 {
			continue // --flag=value 自带值
		}
		f := fs.Lookup(name)
		if f == nil {
			continue // 未知开关交给 fs.Parse 报错
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return fs.Parse(append(flags, positional...))
}
