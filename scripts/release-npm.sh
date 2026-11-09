#!/bin/sh
# 把 dist/ 里的 6 平台产物打成 npm 包（免费分发路径，不需要自建 OSS）。
#
#   sh scripts/release-npm.sh [--publish] [--dry-run] [--otp 123456] [--registry URL]
#
# 发布需要凭据（三选一）：
# 凭据来源（脚本自己读，不打印、不入库）：
#   1) 环境变量 NODE_AUTH_TOKEN（CI 的 secret 直接注入）
#   2) PK_NPM_TOKEN_FILE 指向的凭据文件里的 NPM_TOKEN=...
#      默认尝试 ~/.pi/agent/local/secrets.env（设置见 scripts/set-npm-token.sh）
#   3) npm login 产生的 ~/.npmrc（npm publish 自己会用）
# 提示：不要把 token 写进仓库里的 .npmrc；用环境变量或 600 权限的凭据文件。
# 账号开了 2FA 时：token 勾选 Bypass 2FA，或发布时传 --otp <6位>。
#
# 产物：dist/npm/
#   @packnest/<platform>-<arch>/   ← 每个平台一个包，内含 bin/packnest[.exe]
#   packnest/                      ← 主包（bin/pk.js 转发 + optionalDependencies）
#
# 免费链条（方案 §13.2 的 P0，不花一分钱）：
#   发布到 npm（免费） → npmmirror 自动镜像（免费）→ 国内 npm i -g packnest 秒装。
#   不需要对象存储、不需要 CDN 备案、不需要 GitHub。
set -eu

DIST="${DIST:-dist}"
OUT="$DIST/npm"
PUBLISH=0
DRY_RUN=0
OTP=""
TAG=""
TOKEN_FILE="${PK_NPM_TOKEN_FILE:-$HOME/.pi/agent/local/secrets.env}"
REGISTRY="${NPM_REGISTRY:-https://registry.npmjs.org}"

while [ $# -gt 0 ]; do
  case "$1" in
    --publish) PUBLISH=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --otp) OTP="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

[ -d "$DIST" ] || { echo "先跑 make release（没有 $DIST/）" >&2; exit 1; }
VERSION=$(cat "$DIST/latest")
VER_DIR="$DIST/v$VERSION"
[ -d "$VER_DIR" ] || { echo "找不到 $VER_DIR" >&2; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT"

# 平台包：包名 @packnest/<os>-<arch>，解包后是 package/bin/packnest
for f in "$VER_DIR"/packnest_"$VERSION"_*; do
  base=$(basename "$f")
  case "$base" in
    *.tar.gz)
      rest=${base#packnest_"$VERSION"_}; rest=${rest%.tar.gz}
      exe="packnest"; archive="tar" ;;
    *.zip)
      rest=${base#packnest_"$VERSION"_}; rest=${rest%.zip}
      exe="packnest.exe"; archive="zip" ;;
    *) continue ;;
  esac
  os=${rest%_*}; arch=${rest#*_}
  pkg="$OUT/@packnest/$os-$arch"
  mkdir -p "$pkg/bin"
  tmp="$OUT/.tmp-$os-$arch"; mkdir -p "$tmp"
  abs=$(cd "$(dirname "$f")" && pwd)/$(basename "$f")
  if [ "$archive" = "tar" ]; then
    tar -xzf "$abs" -C "$tmp"
  else
    (cd "$tmp" && unzip -q "$abs")
  fi
  mv "$tmp/packnest" "$pkg/bin/packnest" 2>/dev/null || mv "$tmp/packnest.exe" "$pkg/bin/$exe"
  chmod +x "$pkg/bin/$exe"
  cat > "$pkg/package.json" <<JSON
{
  "name": "@packnest/$os-$arch",
  "version": "$VERSION",
  "description": "packnest 的 $os-$arch 二进制（由主包 packnest 依赖）",
  "os": ["$os"], "cpu": ["$arch"],
  "files": ["bin/$exe"],
  "license": "MIT"
}
JSON
  rm -rf "$tmp"
done

# 主包：JS 入口 + optionalDependencies 指向平台包
mkdir -p "$OUT/packnest"
cp -R packaging/npm/bin packaging/npm/scripts "$OUT/packnest/"
node - "$OUT/packnest/package.json" "$VERSION" <<'NODE'
const fs = require('fs');
const [, , out, version] = process.argv;
const template = JSON.parse(fs.readFileSync('packaging/npm/package.json', 'utf8'));
template.version = version;
for (const name of Object.keys(template.optionalDependencies || {})) {
  template.optionalDependencies[name] = version;
}
fs.writeFileSync(out, JSON.stringify(template, null, 2) + '\n');
NODE

echo "已生成 npm 包："
find "$OUT" -name package.json -maxdepth 3 | sed 's|^|  |'

# 凭据：从环境变量或 600 权限的凭据文件取，只在子进程里 export，绝不打印。
load_token() {
  if [ -n "${NODE_AUTH_TOKEN:-}" ]; then
    echo "凭据：环境变量 NODE_AUTH_TOKEN"
    return 0
  fi
  if [ -f "$TOKEN_FILE" ]; then
    perm=$(ls -l "$TOKEN_FILE" | cut -c1-10)
    case "$perm" in
      -*------|*---------)
        ;;
      *) echo "警告：${TOKEN_FILE} 权限是 ${perm}，建议 chmod 600" >&2 ;;
    esac
    NODE_AUTH_TOKEN=$(grep -E '^NPM_TOKEN=' "$TOKEN_FILE" | head -1 | cut -d= -f2-)
    if [ -n "$NODE_AUTH_TOKEN" ]; then
      export NODE_AUTH_TOKEN
      echo "凭据：${TOKEN_FILE}（值不显示）"
      return 0
    fi
  fi
  if [ -f "$HOME/.npmrc" ] && grep -q '_authToken' "$HOME/.npmrc" 2>/dev/null; then
    echo "凭据：~/.npmrc（npm login）"
    return 0
  fi
  echo "没有 npm 凭据。三选一：" >&2
  echo "  1) sh scripts/set-npm-token.sh      # 交互输入，存到 600 权限的凭据文件" >&2
  echo "  2) export NODE_AUTH_TOKEN=npm_xxx   # CI secret 注入" >&2
  echo "  3) npm login                        # 本地交互" >&2
  return 1
}

if [ "$PUBLISH" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
  load_token || exit 3
  # 顺序很重要：先发 6 个平台子包，再发主包（主包的 optionalDependencies 指向它们）。
  # 反过来也能装上，但用户在执行 npm i 的瞬间子包不存在会报错。
  PUBLISH_ARGS="--access public --registry $REGISTRY"
  # 预发布版本（如 0.6.0-M5）npm 强制要求 --tag，否则报 "must specify a tag"
  if [ -z "$TAG" ]; then
    case "$VERSION" in
      *-*) TAG=next ;;
    esac
  fi
  [ -n "$TAG" ] && PUBLISH_ARGS="$PUBLISH_ARGS --tag $TAG"
  [ -n "$OTP" ] && PUBLISH_ARGS="$PUBLISH_ARGS --otp $OTP"
  [ "$DRY_RUN" -eq 1 ] && PUBLISH_ARGS="$PUBLISH_ARGS --dry-run"
  for p in "$OUT"/@packnest/*/ "$OUT"/packnest/; do
    echo "== 发布 $(basename "$(dirname "$p")")/$(basename "$p")"
    # shellcheck disable=SC2086
    ( cd "$p" && npm publish $PUBLISH_ARGS ) || exit 1
  done
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "dry-run 完成（没有真的发布；这 7 个包都已打包好并校验过 manifest）"
  else
    echo "已发布到 $REGISTRY"
    echo "npmmirror 自动同步通常几分钟内完成；要立刻触发："
    echo "  curl -X PUT https://registry.npmmirror.com/-/package/packnest/syncs"
    echo "  curl -X PUT https://registry.npmmirror.com/-/package/%40packnest%2Fdarwin-arm64/syncs"
  fi
fi
