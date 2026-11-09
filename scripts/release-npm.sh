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

. "$(dirname "$0")/lib/secrets.sh"

DIST="${DIST:-dist}"
OUT="$DIST/npm"
PUBLISH=0
DRY_RUN=0
OTP=""
TAG=""
# 平台包作用域：默认 @packnest；没有该组织时可改成自己的用户名作用域，如 @your-name
SCOPE="${PK_NPM_SCOPE:-@packnest}"
TOKEN_FILE="${PK_NPM_TOKEN_FILE:-$HOME/.pi/agent/local/secrets.env}"
CHECK_AUTH=0
REGISTRY="${NPM_REGISTRY:-https://registry.npmjs.org}"

while [ $# -gt 0 ]; do
  case "$1" in
    --publish) PUBLISH=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --check-auth) CHECK_AUTH=1; shift ;;
    --otp) OTP="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

# npm CLI **不会**自动使用 NODE_AUTH_TOKEN —— 它只读 .npmrc 里的 _authToken。
# 所以这里生成一个临时 userconfig（600，只含 ${NODE_AUTH_TOKEN} 占位与 registry 键），
# 值仍然只在环境变量里，磁盘上没有明文 token。
make_auth_config() {
  host=$(printf '%s' "$REGISTRY" | sed -e 's|^[a-z]*://||' -e 's|/*$||')
  AUTH_RC=$(mktemp "${TMPDIR:-/tmp}/pk-npmrc.XXXXXX")
  chmod 600 "$AUTH_RC"
  {
    printf 'registry=%s\n' "$REGISTRY"
    printf '//%s/:_authToken=${NODE_AUTH_TOKEN}\n' "$host"
    printf 'always-auth=true\n'
  } > "$AUTH_RC"
  export NPM_CONFIG_USERCONFIG="$AUTH_RC"
  trap 'rm -f "$AUTH_RC"' EXIT INT TERM
}

# --check-auth：只验证凭据可用（npm whoami），不打包、不发布。
if [ "$CHECK_AUTH" -eq 1 ]; then
  source_token() {
    value=$(secret_from_file "$TOKEN_FILE" NPM_ACCESS_TOKEN NPM_TOKEN NODE_AUTH_TOKEN NPM_AUTH_TOKEN)
    if [ -n "$value" ]; then NODE_AUTH_TOKEN="$value"; export NODE_AUTH_TOKEN; echo "凭据：${TOKEN_FILE}（值不显示）"; return 0; fi
    if [ -n "${NODE_AUTH_TOKEN:-}" ]; then echo "凭据：环境变量 NODE_AUTH_TOKEN"; return 0; fi
    return 1
  }
  source_token || {
    echo "没找到凭据。" >&2
    secret_diagnose "$TOKEN_FILE" >&2
    echo "可选键名：NPM_ACCESS_TOKEN / NPM_TOKEN / NODE_AUTH_TOKEN / NPM_AUTH_TOKEN" >&2
    echo "本会话临时给：export NODE_AUTH_TOKEN=npm_xxx" >&2
    exit 3
  }
  make_auth_config
  npm_user=$(npm whoami --registry "$REGISTRY" 2>&1) || {
    echo "凭据已读到，但 registry 不认（回答：401/需登录）。常见原因：" >&2
    echo "  · token 过期/被删  · scope 没勾 read-write  · 2FA 账号没勾 Bypass 2FA" >&2
    echo "  · 粘错（少了前缀）  · 用的是 npmjs 的 token 但 registry 指到了别处" >&2
    echo "  原文：$(printf '%s' "$npm_user" | head -3 | tr '\n' ' ')" >&2
    exit 3
  }
  echo "凭据有效：npm 账号 ${npm_user}（registry ${REGISTRY}）"
  exit 0
fi

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
  scopeName=${SCOPE#@}
  pkg="$OUT/@$scopeName/$os-$arch"
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
  "name": "$SCOPE/$os-$arch",
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
node - "$OUT/packnest/package.json" "$VERSION" "$SCOPE" <<'NODE'
const fs = require('fs');
const [, , out, version, scope] = process.argv;
const template = JSON.parse(fs.readFileSync('packaging/npm/package.json', 'utf8'));
template.version = version;
// 用实际作用域重建 optionalDependencies（平台包名必须与发布出去的一致）
const platforms = ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64', 'win32-arm64', 'win32-x64'];
template.optionalDependencies = {};
for (const p of platforms) template.optionalDependencies[scope + '/' + p] = version;
fs.writeFileSync(out, JSON.stringify(template, null, 2) + '\n');
NODE

echo "已生成 npm 包："
find "$OUT" -name package.json -maxdepth 3 | sed 's|^|  |'

# 凭据：从环境变量或 600 权限的凭据文件取，只在子进程里 export，绝不打印。
# 账号开了 auth-and-writes 2FA、token 又没勾 Bypass 时，可以用本地 TOTP 密钥自动出码。
auto_otp() {
  if [ -n "${OTP:-}" ]; then printf '%s' "$OTP"; return 0; fi
  secret="${NPM_TOTP_SECRET:-$(secret_from_file "$TOKEN_FILE" NPM_TOTP_SECRET)}"
  if [ -n "$secret" ]; then totp_code "$secret"; fi
}

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
    # 兼容常见键名：NPM_ACCESS_TOKEN / NPM_TOKEN / NODE_AUTH_TOKEN / NPM_AUTH_TOKEN
    value=$(secret_from_file "$TOKEN_FILE" NPM_ACCESS_TOKEN NPM_TOKEN NODE_AUTH_TOKEN NPM_AUTH_TOKEN)
    if [ -n "$value" ]; then
      NODE_AUTH_TOKEN="$value"
      export NODE_AUTH_TOKEN
      echo "凭据：${TOKEN_FILE}（值不显示）"
      return 0
    fi
  fi
  if [ -f "$HOME/.npmrc" ] && grep -q '_authToken' "$HOME/.npmrc" 2>/dev/null; then
    echo "凭据：~/.npmrc（npm login）"
    return 0
  fi
  echo "没有 npm 凭据。" >&2
  secret_diagnose "$TOKEN_FILE" >&2
  echo "三选一：" >&2
  echo "  1) sh scripts/set-npm-token.sh      # 交互输入，存到 600 权限的凭据文件" >&2
  echo "  2) export NODE_AUTH_TOKEN=npm_xxx   # CI secret 注入" >&2
  echo "  3) npm login                        # 本地交互" >&2
  return 1
}

# npmmirror / cnpm / taobao 镜像都是**只读**的：往它们 publish 会被拒
# （典型报错：Public registration is not allowed）。发布永远打 npmjs。
case "$REGISTRY" in
  *npmmirror*|*cnpm*|*taobao*|*npm.taobao*)
    if [ "$PUBLISH" -eq 1 ]; then
      echo "错误：$REGISTRY 是只读镜像，不能发布（会报 Public registration is not allowed）。" >&2
      echo "发布请用 --registry https://registry.npmjs.org；国内用户安装时再走 npmmirror 镜像。" >&2
      exit 2
    fi
    echo "提示：$REGISTRY 是只读镜像，dry-run 不受影响；真正发布时必须换成 registry.npmjs.org" >&2 ;;
esac

if [ "$PUBLISH" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
  if [ "$PUBLISH" -eq 1 ]; then
    load_token || exit 3
    make_auth_config
  else
    echo "dry-run：只打包并校验 manifest，不需要凭据"
  fi
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
  otp_value=$(auto_otp)
  [ -n "$otp_value" ] && PUBLISH_ARGS="$PUBLISH_ARGS --otp $otp_value"
  [ "$DRY_RUN" -eq 1 ] && PUBLISH_ARGS="$PUBLISH_ARGS --dry-run"
  for p in "$OUT"/@*/*/ "$OUT"/packnest/; do
    echo "== 发布 ${SCOPE}/$(basename "$p")"
    # shellcheck disable=SC2086
    out=$( cd "$p" && npm publish $PUBLISH_ARGS 2>&1 ) || {
      echo "$out" | grep -vE '^npm (notice|warn)' | tail -6
      echo "" >&2
      echo "发布被拒。最常见原因与处置（三选一，都在本机配一次即可全自动）：" >&2
      echo "  1) npmjs → Access Tokens 重建 Granular token 并**勾上 Bypass 2FA**（推荐，之后无需 OTP）" >&2
      echo "  2) 在凭据文件里加 NPM_TOTP_SECRET=<base32 2FA 密钥> —— 脚本会自动算 OTP 传 --otp" >&2
      echo "  3) 账号 Settings → Two-Factor Authentication 改成 auth-only（只在登录时要求 2FA）" >&2
      exit 1
    }
    echo "$out" | grep -vE '^npm (notice|warn)' | tail -1
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
