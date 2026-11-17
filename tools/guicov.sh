#!/bin/sh
# GUI 测试 + 覆盖率门禁（GUI 1.0 口径 H3，见 ~/Documents/Plan/17-包管理器/pn-GUI-1.0-口径.md）。
#
# 为什么 GUI 也要下限：internal/update 是"会替换自己可执行文件"的代码，
# internal/state 是"会调 pk 改用户环境"的代码。这两块没有测试兜着，1.0 就只是"能开窗"。
# 根包（视图层）不设下限：首帧、对比度这类渲染断言比行覆盖率更能说明问题。
#
# 用法：make gui-test / sh tools/guicov.sh
set -eu
cd "$(dirname "$0")/../cmd/pk-desktop"

# 首次达标值，不是理想值：调高可以，调低必须在口径文档里写理由。
COV_UPDATE_MIN=85
COV_STATE_MIN=70
COV_PKCLI_MIN=70
COV_AUTOSTART_MIN=80

# 先跑测试（含 -race）：测试挂了直接红，不看覆盖率。
out=$(go test -race -count=1 -cover ./... 2>&1) || {
	printf '%s\n' "$out"
	echo "" >&2
	echo "GUI 测试失败（覆盖率不参与判决）" >&2
	exit 1
}

printf '%s\n' "$out" | awk \
	-v U="$COV_UPDATE_MIN" -v S="$COV_STATE_MIN" \
	-v P="$COV_PKCLI_MIN" -v A="$COV_AUTOSTART_MIN" \
	-f ../../tools/guicover.awk
