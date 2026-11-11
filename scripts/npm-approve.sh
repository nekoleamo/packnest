#!/bin/sh
# 把 npm 的"浏览器批准"结果转成一次 approve —— 不需要能连 www.npmjs.com。
#
# 背景：npm stage approve 要 2FA。没有 OTP 时 npm 会打印
#   Open this URL in your browser to authenticate: https://www.npmjs.com/auth/cli/<authId>
#   After authenticating, your token can be retrieved from: https://registry.npmjs.org/-/v1/done?authId=<authId>
# 浏览器批准后，那条 /done 链接会回一个 JSON（含 token）。本脚本拿它当 OTP，
# 对当前版本（dist/latest）的全部 staged 包逐个 approve。
#
# 用法：
#   HTTPS_PROXY=http://127.0.0.1:7892 sh scripts/npm-approve.sh 'https://registry.npmjs.org/-/v1/done?authId=xxxx'
#   sh scripts/npm-approve.sh 'https://registry.npmjs.org/-/v1/done?authId=xxxx' --dry-run
#
# 注意：/done 链接是一次性的（用过就废）；返回里的 token 只在本脚本内部使用，不打印、不落盘。
set -eu
. "$(dirname "$0")/lib/secrets.sh"

DRY=0
URL=""
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    http*) URL="$a" ;;
    *) echo "未知参数: $a" >&2; exit 2 ;;
  esac
done
[ -n "$URL" ] || { echo "用法: sh scripts/npm-approve.sh '<registry.npmjs.org/-/v1/done?authId=…>' [--dry-run]" >&2; exit 2; }

: "${TOKEN_FILE:=$HOME/.pi/agent/local/secrets.env}"
: "${REGISTRY:=https://registry.npmjs.org}"
DIST="${DIST:-dist}"

# 1) 取回浏览器批准后的 token（值只用不打印）
fetch() {
  if [ -n "${HTTPS_PROXY:-}" ]; then curl -fsS --max-time 60 -x "$HTTPS_PROXY" "$1"
  else curl -fsS --max-time 60 "$1"; fi
}
json=$(fetch "$URL") || { echo "取 token 失败：链接可能已用过/过期，或需要 HTTPS_PROXY 指向代理（registry.npmjs.org 大陆直连常不通）" >&2; exit 3; }
otp=$(printf '%s' "$json" | python3 -c 'import sys,json
try: print((json.load(sys.stdin) or {}).get("token","") or "")
except Exception: print("")' 2>/dev/null)
[ -n "$otp" ] || { echo "返回里没有 token（链接可能已用过）；原始返回已省略" >&2; exit 4; }

# 2) 认证（与 release-npm.sh 同一套：token 从凭据文件定点取）
if [ -z "${NODE_AUTH_TOKEN:-}" ]; then
  NODE_AUTH_TOKEN=$(secret_from_file "$TOKEN_FILE" NPM_ACCESS_TOKEN NPM_TOKEN NODE_AUTH_TOKEN NPM_AUTH_TOKEN)
fi
[ -n "${NODE_AUTH_TOKEN:-}" ] || { echo "没找到 npm 凭据（见 docs/publishing.md「token 放哪儿」）" >&2; exit 5; }
export NODE_AUTH_TOKEN

VER=$(cat "$DIST/latest" 2>/dev/null || echo "")
ids=$(npm stage list --registry "$REGISTRY" 2>/dev/null |
  awk -v want="$VER" '/^id: /{id=$2} /^version: /{if (want==""||$2==want) print id}')
[ -n "$ids" ] || { echo "没有 staged 版本（版本 ${VER:-?}）"; exit 0; }

# 3) 逐个 approve（同一个 OTP 在有效期内可连续用；失败就提示重来一次）
fail=0
for id in $ids; do
  echo "== approve ${id}"
  if [ "$DRY" -eq 1 ]; then echo "  （dry-run，不真发）"; continue; fi
  if npm stage approve "$id" --otp "$otp" --registry "$REGISTRY" >/dev/null 2>&1; then
    echo "  ✓ ${id}"
  else
    echo "  ✗ ${id}（可能需要新的批准链接：重跑 release-npm.sh --promote ${id}，再拿新的 /done 链接）" >&2
    fail=1
  fi
done
[ "$fail" -eq 0 ] || exit 1
echo "完成。核对：npm view packnest version 应为 ${VER}"
