// Package coded 是 `internal/*` 与 CLI 之间的错误契约（完善路线 §E1 的 L2）。
//
// 规矩只有一条：**内部包不产出人类文案，只产出"稳定 code + 参数 + 因果"**；
// 文案由 `cmd/pk/present` 按当前语言渲染（中文默认，`--lang en` 切英文）。
//
// 为什么值得：文案是产品决策（会改措辞、要翻译、要跟 CLI 风格统一），而 code 是接口
// （脚本、GUI、`errors.Is` 都靠它）。两者混在一个 `fmt.Errorf("中文...")` 里时，
// 每次改标点都可能让断言变红，每次翻译都要满仓库找字符串。
//
// 兼容性：`Error()` 只给日志/调试看（形如 `sig.magic_missing`），**不要**拿它当用户可见文本；
// 需要人读时走 `present.Text(err)`。`Unwrap` 返回 Cause，所以 `errors.Is(err, ErrSignature)`
// 这类既有判断全部照旧工作。
package coded

import (
	"errors"
	"fmt"
	"strings"
)

// Error 是编码错误：Code 稳定、Args 是渲染模板的参数、Cause 保留因果链（哨兵错误/底层 err）。
type Error struct {
	Code  string
	Args  []any
	Cause error
}

// New 造一个没有底层因果的编码错误。
func New(code string, args ...any) error { return &Error{Code: code, Args: args} }

// Wrap 在既有错误外面套一个 code（保留 errors.Is 链）。
func Wrap(cause error, code string, args ...any) error {
	return &Error{Code: code, Args: args, Cause: cause}
}

// Error 是调试用的紧凑形式（`code(a, b)`），不是用户可见文案。
func (e *Error) Error() string {
	if len(e.Args) == 0 {
		return e.Code
	}
	parts := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		parts = append(parts, fmt.Sprint(a))
	}
	return e.Code + "(" + strings.Join(parts, ", ") + ")"
}

// Unwrap 让 errors.Is/As 能穿过编码错误找到底层哨兵。
func (e *Error) Unwrap() error { return e.Cause }

// Of 沿 Unwrap 链找第一个编码错误（渲染入口用它）。
func Of(err error) (*Error, bool) {
	var ce *Error
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}
