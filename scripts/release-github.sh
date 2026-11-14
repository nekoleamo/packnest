#!/bin/sh
# 把 dist/v<版本>/ 的产物同步到 GitHub Releases（海外主源 / CI 可验证的来源）。
#
#   sh scripts/release-github.sh [--dry-run] [--tag v0.9.0] [--repo owner/name]
#
# 为什么要有它：
#   - gitee 是国内主源，但**对海外数据中心 IP 常返回 404**（GitHub Actions runner 实测），
#     所以 smoke CI 没法从 gitee 取产物；
#   - 海外用户直连 gitee 也慢。
#   产物仍是本机构建的同一批文件（唯一发布者不变），只是多传一份到 GitHub，两个源的 sha256 必然一致。
#
# 凭据：`gh` 自己的 keyring（不需要往凭据文件里放 token）。
set -eu
DIST="${DIST:-dist}"
REPO="${PK_GH_REPO:-nekoleamo/packnest}"
DRY=0
TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --tag) TAG="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

[ -f "$DIST/latest" ] || { echo "先跑 make release" >&2; exit 1; }
VER=$(cat "$DIST/latest")
[ -n "$TAG" ] || TAG="v$VER"
DIR="$DIST/v$VER"
[ -d "$DIR" ] || { echo "找不到 $DIR" >&2; exit 1; }
command -v gh >/dev/null 2>&1 || { echo "需要 gh（brew install gh）" >&2; exit 1; }

echo "将发布 ${REPO} ${TAG}（来自 ${DIR}）"
ls "$DIR"/packnest_* "$DIR/provenance.json" "$DIR/sbom.cdx.json" "$DIR/checksums.txt" \
   $( [ -f "$DIR/checksums.txt.sig" ] && echo "$DIR/checksums.txt.sig" ) 2>/dev/null | sed 's|^|  |'
[ "$DRY" -eq 1 ] && { echo "dry-run：没有上传"; exit 0; }

# release 不存在就建（存在则复用，重复上传用 --clobber 覆盖同名文件）
if ! gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  gh release create "$TAG" --repo "$REPO" --title "$TAG" \
    --notes "packnest ${VER}（6 平台产物 + provenance.json + sbom.cdx.json + checksums.txt；海外主源，国内请用 gitee）" >/dev/null
fi
# 兜底：release 是 draft 时 /releases/latest 看不到它，smoke 会退而装上一个版本（1.3.1 踩过）
gh release edit "$TAG" --repo "$REPO" --draft=false >/dev/null
EXTRA=""
[ -f "$DIR/checksums.txt.sig" ] && EXTRA="$DIR/checksums.txt.sig"
# provenance.json / sbom.cdx.json 与归档同批上传：checksums.txt 里已经算进它们的 sha256
# shellcheck disable=SC2086
gh release upload "$TAG" --repo "$REPO" --clobber \
  "$DIR"/packnest_* "$DIR/provenance.json" "$DIR/sbom.cdx.json" "$DIR/checksums.txt" $EXTRA
echo "完成：https://github.com/${REPO}/releases/tag/${TAG}"
