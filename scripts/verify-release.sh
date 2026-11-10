#!/bin/sh
# 把**已发布**的产物拉回来，和本地 checksums.txt 逐条对 sha256。
#
#   sh scripts/verify-release.sh                    # 默认对 gitee release
#   sh scripts/verify-release.sh --base https://your-host/packnest
#   PK_VERSION=0.7.0 sh scripts/verify-release.sh
#
# 为什么要有这一步：0.7.0 发布时，Homebrew tap / Scoop bucket 里的 sha256 是**另一次构建**
# 的产物（tar/gzip 会写入 mtime，同一版本重建一次 sha256 就变），结果 brew install 报
# "reports different checksum"。发布后立刻对一遍，能在用户踩到之前发现。
set -eu
cd "$(dirname "$0")/.."

DIST="${DIST:-dist}"
VER="${PK_VERSION:-$(cat "$DIST/latest" 2>/dev/null | tr -d ' \t\r\n')}"
BASE="${PK_RELEASE_BASE:-https://gitee.com/null_593_5354/packnest/releases/download/v${VER}}"
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="${2%/}"; shift 2 ;;
    --version) VER="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

SUMS="$DIST/v$VER/checksums.txt"
[ -f "$SUMS" ] || { echo "找不到 ${SUMS}（先跑 make release，或用 --version 指定）" >&2; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail=0
printf '核对 v%s 的已发布产物：%s\n' "$VER" "$BASE"
while read -r want file; do
  [ -n "${file:-}" ] || continue
  printf '  %-46s ' "$file"
  if ! curl -fsSL --max-time 300 -o "$tmp/$file" "$BASE/$file"; then
    echo "下载失败"; fail=1; continue
  fi
  got=$(shasum -a 256 "$tmp/$file" 2>/dev/null | cut -d' ' -f1)
  [ -n "$got" ] || got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
  if [ "$got" = "$want" ]; then
    echo "OK"
  else
    echo "不符"
    printf '      清单 %s\n      实际 %s\n' "$want" "$got"
    fail=1
  fi
done < "$SUMS"

# scripts/latest.txt 是安装脚本在 gitee API 不可用时的版本来源：必须与本次发布一致，
# 否则用户跑 `curl … | sh` 会拿到"版本号指向一个不存在的产物"或旧版本。
if [ -f scripts/latest.txt ]; then
  pinned=$(tr -d ' \t\r\n' < scripts/latest.txt)
  if [ "$pinned" != "$VER" ]; then
    echo "scripts/latest.txt = $pinned，但发布版本是 $VER（安装脚本的兜底版本源过期）" >&2
    fail=1
  fi
else
  echo "缺少 scripts/latest.txt（安装脚本的兜底版本源）" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  cat >&2 <<'EOS'

有产物与 checksums.txt 不一致。常见原因与处置：
  1) 发布后又重跑了一次 make release（同一版本被重新构建）→ 重新上传产物，或按当前
     checksums.txt 重新生成并推送渠道清单：sh scripts/gen-manifests.sh && sh scripts/release-taps.sh
  2) Homebrew/Scoop 的用户侧清单不是从这份 checksums.txt 生成的 → 同上，重推清单
  3) 上传过程中被截断 → 重新上传该文件
EOS
  exit 1
fi
echo "全部一致（$(grep -c . "$SUMS") 个产物）"
