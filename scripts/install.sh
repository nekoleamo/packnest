#!/bin/sh
# packnest 安装脚本（不访问 GitHub）
#
#   curl -fsSL https://gitee.com/packnest/packnest/raw/master/scripts/install.sh | sh -s -- --mirror cn
#
# 选项：
#   --mirror cn|default   产物来源档位（cn 从 npmmirror 取，default 从 gitee）
#   --version vX.Y.Z      指定版本（默认最新）
#   --dir DIR             安装目录（默认 ~/.local/bin，无需 sudo）
#   --dry-run             只打印将要做什么
set -eu

MIRROR="default"
VERSION=""
DIR="${HOME}/.local/bin"
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --mirror) MIRROR="${2:-cn}"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "不支持的 OS: $os（Windows 请用 install.ps1）" >&2; exit 2 ;;
esac
arch=$(uname -m)
case "$arch" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "不支持的架构: $arch" >&2; exit 2 ;;
esac

if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL --max-time 15 https://gitee.com/api/v5/repos/packnest/packnest/releases/latest 2>/dev/null \
    | sed -n 's/.*"tag_name":"\([^"]*\)".*/\1/p' || true)
  VERSION=${VERSION#v}
  [ -n "$VERSION" ] || { echo "拿不到最新版本，请用 --version 指定" >&2; exit 3; }
fi

name="packnest_${VERSION}_${os}_${arch}"
case "$MIRROR" in
  cn)
    url="https://registry.npmmirror.com/-/binary/packnest/v${VERSION}/${name}.tar.gz" ;;
  *)
    url="https://gitee.com/packnest/packnest/releases/download/v${VERSION}/${name}.tar.gz" ;;
esac

echo "将要安装 packnest ${VERSION}（${os}-${arch}）"
echo "  来源: $url"
echo "  目录: $DIR"
[ "$DRY_RUN" -eq 1 ] && exit 0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL --max-time 300 -o "$tmp/pk.tar.gz" "$url"
tar -xzf "$tmp/pk.tar.gz" -C "$tmp"
mkdir -p "$DIR"
install -m 0755 "$tmp/packnest" "$DIR/packnest"
ln -sf "$DIR/packnest" "$DIR/pk"

echo "装好了：$DIR/pk（$("$DIR/pk" --version)）"
case ":$PATH:" in
  *":$DIR:"*) ;;
  *) echo "把 $DIR 加进 PATH：export PATH=\"$DIR:\$PATH\"" ;;
esac
echo "下一步：pk install node@22（或 eval \"\$(pk env --shell bash)\" 接入镜像档）"
