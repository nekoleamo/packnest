#!/bin/sh
# 把 npm access token 存进凭据文件（**不经过 AI、不进对话、不写 shell history**）。
#
#   sh scripts/set-npm-token.sh                 # 交互输入（不回显）
#   PK_SECRETS_FILE=/path/to/secrets.env sh scripts/set-npm-token.sh
#
# 默认写到 ~/.pi/agent/local/secrets.env（本机既有凭据文件，权限 600，不入库）。
# 文件里只加/更新一行 NPM_TOKEN=...，其他行原样保留。
set -eu

SECRETS="${PK_SECRETS_FILE:-$HOME/.pi/agent/local/secrets.env}"
KEY="${PK_NPM_TOKEN_KEY:-NPM_TOKEN}"

# 已有别名键（NPM_ACCESS_TOKEN 等）就沿用，避免同一个文件里出现两个键
for candidate in NPM_ACCESS_TOKEN NPM_TOKEN NODE_AUTH_TOKEN NPM_AUTH_TOKEN; do
  if grep -qE "^${candidate}=" "$SECRETS" 2>/dev/null; then KEY="$candidate"; break; fi
done

mkdir -p "$(dirname "$SECRETS")"
printf 'npm access token（输入不回显，粘贴后回车）: '
stty -echo 2>/dev/null || true
IFS= read -r TOKEN
stty echo 2>/dev/null || true
echo

case "$TOKEN" in
  npm_*) ;;
  "") echo "没输入，取消" >&2; exit 1 ;;
  *) echo "警告：token 通常以 npm_ 开头（Granular）或以 40+ 位十六进制（Classic），请确认粘贴完整" >&2 ;;
esac

umask 077
touch "$SECRETS"
chmod 600 "$SECRETS"
tmp="$SECRETS.tmp.$$"
grep -v "^${KEY}=" "$SECRETS" > "$tmp" 2>/dev/null || true
printf '%s=%s\n' "$KEY" "$TOKEN" >> "$tmp"
mv "$tmp" "$SECRETS"
chmod 600 "$SECRETS"

echo "已写入 $SECRETS（权限 600，值不回显）"
echo "发布：sh scripts/release-npm.sh --publish            # 脚本自己读，不打印"
