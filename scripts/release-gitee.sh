#!/bin/sh
# 把 dist/v<版本>/ 的产物传到 gitee release（国内二进制主源，免费）。
#
#   sh scripts/release-gitee.sh [--dry-run] [--tag v0.6.1] [--repo owner/name]
#
# 凭据：GITEE_TOKEN 从环境变量或 ~/.pi/agent/local/secrets.env 读（不打印不入库）。
# 需要 token 勾选 projects / releases 权限。
set -eu
. "$(dirname "$0")/lib/secrets.sh"

TOKEN_FILE="${PK_SECRETS_FILE:-$HOME/.pi/agent/local/secrets.env}"
DIST="${DIST:-dist}"
REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
TAG=""
DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --tag) TAG="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

[ -f "$DIST/latest" ] || { echo "先跑 make release" >&2; exit 1; }
VERSION=$(cat "$DIST/latest")
[ -n "$TAG" ] || TAG="v$VERSION"
VER_DIR="$DIST/v$VERSION"
[ -d "$VER_DIR" ] || { echo "找不到 $VER_DIR" >&2; exit 1; }

echo "将发布 ${REPO} ${TAG}（来自 ${VER_DIR}）"
for f in "$VER_DIR"/packnest_* "$VER_DIR"/checksums.txt "$VER_DIR"/checksums.txt.sig; do
  [ -f "$f" ] || continue
  [ -f "$f" ] && printf '  %s (%s)\n' "$(basename "$f")" "$(du -h "$f" | cut -f1)"
done
if [ "$DRY" -eq 1 ]; then echo "dry-run：没有上传"; exit 0; fi

TOKEN="${GITEE_TOKEN:-$(secret_from_file "$TOKEN_FILE" GITEE_TOKEN GITEE_ACCESS_TOKEN)}"
[ -n "$TOKEN" ] || { echo "没有 GITEE_TOKEN（${TOKEN_FILE}），或用 GITEE_TOKEN=xxx 临时给" >&2; exit 3; }

api="https://gitee.com/api/v5/repos/$REPO"
# 1) 建 release（已存在则复用）
rel_id=$(curl -s -X POST "$api/releases" \
  -d "access_token=$TOKEN" -d "tag_name=$TAG" -d "name=$TAG" \
  --data-urlencode "body=packnest ${VERSION}（6 平台产物 + checksums.txt）" -d "target_commitish=main" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or "")' 2>/dev/null)
if [ -z "$rel_id" ]; then
  rel_id=$(curl -s "$api/releases/tags/$TAG" \
    | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or "")' 2>/dev/null)
fi
[ -n "$rel_id" ] || { echo "建 release 失败（token 权限？tag 是否已推送到 gitee？）" >&2; exit 3; }
echo "release id: $rel_id"

# 2) 逐个上传附件
for f in "$VER_DIR"/packnest_* "$VER_DIR"/checksums.txt "$VER_DIR"/checksums.txt.sig; do
  [ -f "$f" ] || continue
  code=$(curl -s -o /tmp/gitee-upload.json -w '%{http_code}' -X POST "$api/releases/$rel_id/attach_files" \
    -F "access_token=$TOKEN" -F "file=@$f")
  printf '  %-46s %s\n' "$(basename "$f")" "$code"
done
rm -f /tmp/gitee-upload.json
echo "完成：https://gitee.com/${REPO}/releases/tag/${TAG}"
