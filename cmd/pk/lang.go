package main

import (
	"fmt"
	"os"
	"strings"
)

// langEN 为 true 时所有 tr(zh, en) 取英文；默认中文（不跟随 LANG，避免 CI 日志静默变英文）。
var langEN bool

// tr 是唯一的翻译入口：中文在前、英文在后。不做 key 表 —— 预算只剩几百行，
// 表驱动要多一整套 key 与数据文件，收益只是"文案集中"，代价是代码里全是 key。
func tr(zh, en string) string {
	if langEN {
		return en
	}
	return zh
}

// errT 是 fmt.Errorf 的薄包装：只是让"格式串来自 tr(...)"的调用点读起来短一点。
// 注意：只有带参数时才走这里 —— 无参数的格式串要拼 errors.New（否则 vet 报 non-constant format string）。
func errT(msg string, args ...any) error { return fmt.Errorf(msg, args...) }

// setLang 解析 --lang / PK_LANG（命令行在 parseGlobals 里优先）。
func setLang(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "zh", "cn", "zh-cn", "zh_cn", "chinese":
		langEN = false
	case "en", "en-us", "en_us", "english":
		langEN = true
	default:
		return fmt.Errorf("--lang 只接受 en|zh（得到 %q）; --lang accepts en|zh", v)
	}
	return nil
}

// initLangFromEnv 在解析命令行之前读 PK_LANG。
func initLangFromEnv() error {
	if v, ok := os.LookupEnv("PK_LANG"); ok && v != "" {
		return setLang(v)
	}
	return nil
}
