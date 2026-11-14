package main

import (
	"fmt"

	"github.com/packnest/packnest/cmd/pk/present"
)

// 呈现层已经搬到 cmd/pk/present（E6 分层）：文案、表格、进度、问题码行都在那边。
// 这里只留薄别名 —— 300+ 个 tr(...) 调用点不值得为搬家改一遍，改了只会扩大回归面，
// 而"实现只有一处"这个目的已经达到。
var tr = present.T

// errT 是 fmt.Errorf 的薄包装：只是让"格式串来自 tr(...)"的调用点读起来短一点。
// 注意：只有带参数时才走这里 —— 无参数的格式串要拼 errors.New（否则 vet 报 non-constant format string）。
func errT(msg string, args ...any) error { return fmt.Errorf(msg, args...) }

// setLang 解析 --lang / PK_LANG（命令行在 parseGlobals 里优先）。
func setLang(v string) error { return present.SetLang(v) }

// initLangFromEnv 在解析命令行之前读 PK_LANG。
func initLangFromEnv() error { return present.InitLangFromEnv() }
