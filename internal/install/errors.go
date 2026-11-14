package install

import "errors"

// 哨兵错误（完善路线 §E1 试点）：`internal/*` 的失败要能被**程序**判断，不只是被人读懂。
//
// 为什么只加这几个：断言里"断中文子串"的地方一共就十几处，与其一次性把所有文案改成
// 错误码（那是 L2 全量，还要动 `--json` 的 msg 语义，得先拍板），不如先把**测试真的在断**
// 的那几类抽成哨兵错误 —— 文案可以改、翻译、换语言，`errors.Is` 不受影响。
//
// 用法：构造时用 %w 包住哨兵，前面保留人话，调用方按需 errors.Is / errors.As。
var (
	// ErrNotInstalled 表示台账里没有这个工具的该版本（`pk use` 切一个没装的版本）。
	ErrNotInstalled = errors.New("还没装")
	// ErrRequiresMissing 表示缺前置工具（pnpm 需要先装 node）。
	ErrRequiresMissing = errors.New("缺少前置工具")
	// ErrFrozenMismatch 表示 pk.lock 与 pk.toml/已有记录不一致（--frozen 拒绝解析）。
	ErrFrozenMismatch = errors.New("锁与清单不一致")
	// ErrCompanionCeiling 表示伴生包（npm）的版本上限和宿主对不上。
	ErrCompanionCeiling = errors.New("伴生包上限不匹配")
)
