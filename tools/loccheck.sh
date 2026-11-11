#!/bin/sh
# 行数门禁（方案 §0.1 决策 1 + R2）。
#
#   总计   cmd/ + internal/，不含 *_test.go，也不含 internal/testkit（纯测试夹具）
#          > 10200 → 退出 1（红线，硬拦；2026-11-11 从 10000 上调，见方案 §0.1）
#   cmd/pk > 3200 → 只 warn（R2：硬拦会逼出"把代码搬进 internal 只为过门禁"的假优化）
#   单文件 > 600 → 只 warn（当前唯一超标者是阶段 0 要抽的 internal/install/install.go）
#
# 用法：make loc / sh tools/loccheck.sh
set -eu
cd "$(dirname "$0")/.."

LOC_MAX=10200
CMD_MAX=3200
FILE_MAX=600

# 统计口径（三处门禁共用同一份文件列表，避免"门禁说一套、方案算一套"）
files=$(find cmd internal -name '*.go' ! -name '*_test.go' ! -path 'internal/testkit/*' | sort)
total=$(printf '%s\n' "$files" | xargs wc -l | tail -1 | awk '{print $1}')
cmd=$(find cmd -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1 | awk '{print $1}')

echo "行数门禁（cmd/ + internal/，不含 *_test.go 与 internal/testkit）"
printf '  总计    %5d / %d  余量 %d\n' "$total" "$LOC_MAX" "$((LOC_MAX - total))"

if [ "$cmd" -gt "$CMD_MAX" ]; then
  printf '  cmd/pk  %5d / %d  warn（R2：只提示，不拦）\n' "$cmd" "$CMD_MAX"
else
  printf '  cmd/pk  %5d / %d\n' "$cmd" "$CMD_MAX"
fi

printf '%s\n' "$files" | xargs wc -l \
  | awk -v max="$FILE_MAX" '$2 != "total" && $1 > max { printf "  warn：%s %d 行（> %d，建议抽函数）\n", $2, $1, max }'

if [ "$total" -gt "$LOC_MAX" ]; then
  echo "✘ 超出红线 $LOC_MAX 行（方案 §0.1）：先腾预算，别放宽门禁" >&2
  exit 1
fi
echo "✔ 行数门禁通过"
