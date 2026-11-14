#!/bin/sh
# 行数门禁（方案 §0.1 决策 1 + 完善路线 §六「行数放开后的替代纪律」）。
#
# 口径与判据：
#   每个包   → 对着 tools/loc-budgets.txt 的上限比，超了退 1（新增包必须登记，否则也算超）
#   总计     → **只统计不设硬顶**（2026-11-14 拍板：总量翻倍也不会让人看不懂，
#              失控的是"某个包长成一个怪物"）。数字仍然打印出来，方便肉眼发现异常增长。
#   单文件   → > 600 行只 warn（抽函数是自然结果，不该由门禁逼出来）
#
# 统计口径（三处门禁共用同一份文件列表，避免"门禁说一套、方案算一套"）：
# 不含 *_test.go、不含 internal/testkit（纯测试夹具）、不含 cmd/pk-desktop（GUI 单列预算）。
#
# 用法：make loc / sh tools/loccheck.sh
set -eu
cd "$(dirname "$0")/.."

FILE_MAX=600
BUDGETS=tools/loc-budgets.txt

files=$(find cmd internal -name '*.go' ! -name '*_test.go' ! -path 'internal/testkit/*' \
  ! -path 'cmd/pk-desktop/*' | sort)
total=$(printf '%s\n' "$files" | xargs wc -l | tail -1 | awk '{print $1}')

echo "行数预算（cmd/ + internal/，不含 *_test.go 与 internal/testkit）"
printf '  总计    %5d（只统计，不设硬顶）\n' "$total"

fail=0
# 逐包对预算：目录 -> 该目录下（含子目录）非测试行数
seen=""
budget_for() {
  awk -v p="$1" '$1 == p { print $2; found=1 } END { if (!found) print "" }' "$BUDGETS"
}
for dir in $(find cmd internal -type d ! -path 'internal/testkit*' ! -path 'cmd/pk-desktop*' | sort); do
  loc=$(find "$dir" -maxdepth 1 -name '*.go' ! -name '*_test.go' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')
  [ -n "${loc:-}" ] || continue
  want=$(budget_for "$dir")
  if [ -z "$want" ]; then
    printf '  %-22s %5d  未登记预算（新增包要在 %s 里给上限）\n' "$dir" "$loc" "$BUDGETS"
    fail=1
    continue
  fi
  seen="$seen $dir"
  if [ "$loc" -gt "$want" ]; then
    printf '  %-22s %5d / %d  ✘ 超预算 %d\n' "$dir" "$loc" "$want" "$((loc - want))"
    fail=1
  else
    printf '  %-22s %5d / %d\n' "$dir" "$loc" "$want"
  fi
done

# 登记了但不存在的包：改名/删包后忘了清预算表，同样算不一致
while read -r pkg cap; do
  case "$pkg" in ''|'#'*) continue ;; esac
  case " $seen " in
    *" $pkg "*) ;;
    *) printf '  %-22s 预算表里有，但找不到这个包（删包要同步删预算）\n' "$pkg"; fail=1 ;;
  esac
done < "$BUDGETS"

printf '%s\n' "$files" | xargs wc -l \
  | awk -v max="$FILE_MAX" '$2 != "total" && $1 > max { printf "  warn：%s %d 行（> %d，建议抽函数）\n", $2, $1, max }'

if [ "$fail" -ne 0 ]; then
  echo "✘ 有包超出预算：先在 $BUDGETS 里给理由（docs/progress.md 记一笔），别偷偷放宽" >&2
  exit 1
fi
echo "✔ 行数预算通过"
