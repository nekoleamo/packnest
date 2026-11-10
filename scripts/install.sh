#!/bin/sh
# packnest 安装脚本（不访问 GitHub）
#
#   curl -fsSL https://gitee.com/${GITEE_REPO}/raw/main/scripts/install.sh | sh
#
# 选项：
#   --mirror cn|default   产物来源档位（cn 从 npmmirror 取，default 从 gitee）
#   --from npm|gitee|base 分发通道（默认：给了 --base 就 base，否则 gitee）
#     npm  = 从 npm registry 取平台包（免费，国内走 npmmirror 镜像，不需要任何自建存储）
#   --registry URL        --from npm 时的 registry（默认 https://registry.npmmirror.com）
#   --base URL            任意静态源基地址（自己托管的 OSS/CDN/内网），优先级最高
#                         例：--base https://static.example.com/packnest
#   --version vX.Y.Z      指定版本（默认最新）
#   --dir DIR             安装目录（默认 ~/.local/bin，无需 sudo）
#   --skip-verify         跳过 sha256/sha1 校验（不推荐；只在拿不到校验和时用）
#   --dry-run             只打印将要做什么
#
# 只要能 GET 到 <base>/v<版本>/packnest_<版本>_<os>_<arch>.tar.gz，就能装：
# 你不需要把东西放到 gitee 或 npmmirror 上。
set -eu

MIRROR="default"
BASE="${PK_INSTALL_BASE:-}"
FROM=""
REGISTRY="${PK_NPM_REGISTRY:-https://registry.npmmirror.com}"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
SCOPE="${PK_NPM_SCOPE:-@packnest}"
VERSION=""
DIR="${HOME}/.local/bin"
DRY_RUN=0
SKIP_VERIFY="${PK_SKIP_VERIFY:-0}"

# 哈希：macOS 自带 shasum，Linux 一般是 sha256sum/sha1sum
sha256_of() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
  elif command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  fi
}
sha1_of() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 1 "$1" | cut -d' ' -f1
  elif command -v sha1sum >/dev/null 2>&1; then sha1sum "$1" | cut -d' ' -f1
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mirror) MIRROR="${2:-cn}"; shift 2 ;;
    --base) BASE="$2"; shift 2 ;;
    --from) FROM="$2"; shift 2 ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    --scope) SCOPE="$2"; shift 2 ;;
    --repo) GITEE_REPO="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "不支持的 OS: ${os}（Windows 请用 install.ps1）" >&2; exit 2 ;;
esac
arch=$(uname -m)
case "$arch" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "不支持的架构: $arch" >&2; exit 2 ;;
esac

# 通道决定：显式 --from > 给了 --base 就 base > 默认 gitee
if [ -z "$FROM" ]; then
  if [ -n "$BASE" ]; then FROM=base
  elif [ "$MIRROR" = "cn" ]; then FROM=npm     # 国内免费通道 = npm（npmmirror 镜像），不用自建存储
  else FROM=gitee; fi
fi

if [ "$FROM" = "npm" ]; then
  # 从 npm registry 取平台包（免费通道：发布一次，npmmirror 自动镜像）
  pkg="${SCOPE}/${os}-${arch}"
  encoded=$(printf '%s' "$pkg" | sed 's|/|%2F|')
  meta=$(curl -fsSL --max-time 30 "$REGISTRY/$encoded") || {
    echo "从 $REGISTRY 取不到 ${pkg}（可能还没发布）" >&2; exit 3; }
  # 用 python3 精确取 dist-tags.latest 对应的 tarball（用 sed 取"最后一个 tarball"会拿到旧版本）
  parsed=$(printf '%s' "$meta" | python3 -c '
import sys, json
d = json.load(sys.stdin)
ver = d["dist-tags"]["latest"]
dist = d["versions"][ver]["dist"]
print(ver)
print(dist["tarball"])
print(dist.get("shasum", ""))
' 2>/dev/null)
  if [ -n "$parsed" ]; then
    META_VER=$(printf '%s' "$parsed" | sed -n '1p')
    tarball=$(printf '%s' "$parsed" | sed -n '2p')
    META_SHA1=$(printf '%s' "$parsed" | sed -n '3p')
    [ -z "$VERSION" ] && VERSION="$META_VER"
  else
    tarball=$(printf '%s' "$meta" | sed -n 's/.*"tarball":[ ]*"\([^"]*\)".*/\1/p' | tail -1)
  fi
  [ -n "$tarball" ] || { echo "${pkg} 元数据里没有 tarball 地址" >&2; exit 3; }
  case "$tarball" in /*) tarball="${REGISTRY%/}${tarball}" ;; esac
  echo "将要安装 packnest ${VERSION:-latest}（${os}-${arch}）"
  echo "  来源: ${tarball}（npm 平台包）"
  echo "  目录: $DIR"
  [ "$DRY_RUN" -eq 1 ] && exit 0
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
  curl -fsSL --max-time 300 -o "$tmp/pkg.tgz" "$tarball"
  if [ "$SKIP_VERIFY" -eq 0 ]; then
    got=$(sha1_of "$tmp/pkg.tgz")
    if [ -n "${META_SHA1:-}" ] && [ -n "$got" ]; then
      [ "$got" = "$META_SHA1" ] || {
        echo "sha1 校验失败（registry 声明 ${META_SHA1}，实际 ${got}）；确认来源可信后可加 --skip-verify" >&2; exit 4; }
      echo "  校验：sha1 OK（registry dist.shasum）"
    else
      echo "  警告：拿不到 registry 的 shasum，跳过校验（不推荐）" >&2
    fi
  fi
  tar -xzf "$tmp/pkg.tgz" -C "$tmp"
  mkdir -p "$DIR"
  install -m 0755 "$tmp/package/bin/packnest" "$DIR/packnest"
  ln -sf "$DIR/packnest" "$DIR/pk"
  # 记一笔"自己是怎么装的"，`pk uninstall --all` 才能精确告诉你/替你删掉哪个文件
ROOT="${PK_ROOT:-$HOME/.packnest}"
mkdir -p "$ROOT/state" 2>/dev/null || true
cat > "$ROOT/state/install.json" <<JSON 2>/dev/null || true
{"schema":1,"channel":"script","platform":"${os}","version":"${VERSION}","dir":"${DIR}"}
JSON

echo "装好了：$DIR/pk（$("$DIR/pk" --version)）"
  case ":$PATH:" in
    *":$DIR:"*) ;;
    *) echo "把 $DIR 加进 PATH：export PATH=\"$DIR:\$PATH\"" ;;
  esac
  exit 0
fi

if [ -z "$VERSION" ] && [ -n "$BASE" ]; then
  VERSION=$(curl -fsSL --max-time 15 "$BASE/latest" 2>/dev/null | tr -d ' \t\r\n' || true)
  VERSION=${VERSION#v}
fi

if [ -z "$VERSION" ]; then
  # 版本号必须跟"下载渠道"对齐：走 gitee 就问 gitee 的最新 release。
  # 别再优先问 npm —— npm 的 latest 可能落后于 gitee（补丁版还在 staged 未 approve 时就会这样，
  # 实测过：gitee 已发 0.7.6，npm latest 还是 0.7.4，于是装到了旧版）。
  VERSION=$(curl -fsSL --max-time 15 "https://gitee.com/api/v5/repos/${GITEE_REPO}/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name":"\([^"]*\)".*/\1/p' | head -1 || true)
  if [ -z "$VERSION" ]; then
    # gitee API 匿名偶发 401 / 限流：退回仓库里的单一真源文件（发布时由 make release 更新）
    VERSION=$(curl -fsSL --max-time 15 "https://gitee.com/${GITEE_REPO}/raw/main/scripts/latest" 2>/dev/null \
      | tr -d ' \t\r\n' || true)
  fi
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL --max-time 15 "https://registry.npmmirror.com/packnest/latest" 2>/dev/null \
      | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' | head -1 || true)
  fi
  if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL --max-time 15 "https://registry.npmjs.org/packnest/latest" 2>/dev/null \
      | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' | head -1 || true)
  fi
  VERSION=${VERSION#v}
  [ -n "$VERSION" ] || { echo "拿不到最新版本，请用 --version 指定" >&2; exit 3; }
fi

name="packnest_${VERSION}_${os}_${arch}"
if [ -n "$BASE" ]; then
  url="${BASE%/}/v${VERSION}/${name}.tar.gz"
else
  case "$MIRROR" in
    cn)
      url="https://registry.npmmirror.com/-/binary/packnest/v${VERSION}/${name}.tar.gz" ;;
    *)
      url="https://gitee.com/${GITEE_REPO}/releases/download/v${VERSION}/${name}.tar.gz" ;;
  esac
fi

sums_url=""
if [ -n "$BASE" ]; then
  sums_url="${BASE%/}/v${VERSION}/checksums.txt"
else
  sums_url="https://gitee.com/${GITEE_REPO}/releases/download/v${VERSION}/checksums.txt"
fi

echo "将要安装 packnest ${VERSION}（${os}-${arch}）"
echo "  来源: $url"
echo "  目录: $DIR"
[ "$DRY_RUN" -eq 1 ] && exit 0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL --max-time 300 -o "$tmp/pk.tar.gz" "$url"
if [ "$SKIP_VERIFY" -eq 0 ]; then
  sums=$(curl -fsSL --max-time 60 "$sums_url" 2>/dev/null || true)
  want=$(printf '%s\n' "$sums" | awk -v f="${name}.tar.gz" '$2==f {print $1; exit}')
  got=$(sha256_of "$tmp/pk.tar.gz")
  if [ -z "$want" ] || [ -z "$got" ]; then
    echo "拿不到 ${name}.tar.gz 的 sha256（${sums_url}）；确认来源可信后可加 --skip-verify" >&2
    exit 4
  fi
  [ "$want" = "$got" ] || {
    echo "sha256 校验失败：期望 ${want}，实际 ${got}（产物可能被篡改或损坏）" >&2; exit 4; }
  echo "  校验：sha256 OK（checksums.txt）"
fi
tar -xzf "$tmp/pk.tar.gz" -C "$tmp"
mkdir -p "$DIR"
install -m 0755 "$tmp/packnest" "$DIR/packnest"
ln -sf "$DIR/packnest" "$DIR/pk"

# 记一笔"自己是怎么装的"，`pk uninstall --all` 才能精确告诉你/替你删掉哪个文件
ROOT="${PK_ROOT:-$HOME/.packnest}"
mkdir -p "$ROOT/state" 2>/dev/null || true
cat > "$ROOT/state/install.json" <<JSON 2>/dev/null || true
{"schema":1,"channel":"script","platform":"${os}","version":"${VERSION}","dir":"${DIR}"}
JSON

echo "装好了：$DIR/pk（$("$DIR/pk" --version)）"
case ":$PATH:" in
  *":$DIR:"*) ;;
  *) echo "把 $DIR 加进 PATH：export PATH=\"$DIR:\$PATH\"" ;;
esac
echo "下一步：pk install node@22（或 eval \"\$(pk env --shell bash)\" 接入镜像档）"
