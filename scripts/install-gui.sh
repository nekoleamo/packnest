#!/bin/sh
# packnest **桌面端（GUI）** 安装脚本 —— 与 CLI 分开装，且**必须先装 CLI**。
#
#   curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.sh | sh
#
# 选项：
#   --base URL         任意静态源（指向一个 gui-v<版本> 的目录），优先级最高
#   --version vX.Y.Z   GUI 版本（默认取最新 gui-v* release）
#   --dir DIR          装到哪（默认 ~/Applications，**只写用户目录**，永不 sudo）
#   --repo owner/name  gitee 仓库（默认 null_593_5354/packnest）
#   --skip-verify      跳过 sha256 与签名校验（不推荐）
#   --dry-run          只打印将要做什么
#
# 校验强度与 CLI 一致：先验 checksums.txt 的 sshsig（发布密钥），再比 .app.zip 的 sha256。
# 产物是 **ad-hoc 签名**（未公证），首次打开要去「系统设置 → 隐私与安全性」点「仍要打开」——脚本会把这句话打出来。
set -eu

BASE="${PK_GUI_BASE:-}"
VERSION=""
DIR="${HOME}/Applications"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
SKIP_VERIFY="${PK_SKIP_VERIFY:-0}"
DRY_RUN=0

# 发布签名公钥（与 pk 内置、install.sh、install.ps1 同源；`make keys` 会核对这几处）
ALLOWED_SIGNERS="release@packnest.dev ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJEVfrMrDRIA2v/PHcPC6gzhU4q0cc+ak8KRr5idL/Xm pk-release-2026 pk-release-2026"
SIGNER="release@packnest.dev"
SSHSIG_NAMESPACE="pk-release"

sha256_of() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | cut -d' ' -f1
  elif command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --repo) GITEE_REPO="$2"; shift 2 ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin) ;;
  *) echo "GUI 安装脚本只做 macOS；Windows 用 install-gui.ps1，Linux 用 make gui 或解压 tar.gz" >&2; exit 2 ;;
esac
arch=$(uname -m)
case "$arch" in
  arm64) ;;
  x86_64) echo "只有 Apple Silicon（arm64）产物；Intel Mac 需自己 make gui-app" >&2; exit 2 ;;
  *) echo "不支持的架构: ${arch}" >&2; exit 2 ;;
esac

# CLI 必须先装：GUI 只是 pk 的可视化壳，没有 pk 它开起来也是空的
if ! command -v pk >/dev/null 2>&1; then
  cat >&2 <<'MSG'
先装 CLI，再装桌面端：
  curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
MSG
  exit 3
fi

VER="${VERSION#v}"
if [ -z "$VER" ]; then
  # 给了 --base 就先问它（自托管源与离线镜像只认自己的 latest），否则问 gitee 的 gui-v* 列表
  # （CLI 的 releases/latest 会指向 v*，不能混用）
  if [ -n "$BASE" ]; then
    VER=$(curl -fsSL --max-time 15 "${BASE%/}/latest" 2>/dev/null | tr -d ' \t\r\nv' || true)
  fi
  if [ -z "$VER" ]; then
    VER=$(curl -fsSL --max-time 20 "https://gitee.com/api/v5/repos/${GITEE_REPO}/releases?per_page=50&page=1" 2>/dev/null \
      | tr ',' '\n' | sed -n 's/.*"tag_name":"gui-v\([0-9][0-9.]*\)".*/\1/p' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1 || true)
  fi
fi
[ -n "$VER" ] || { echo "拿不到 GUI 版本；用 --version 指定，或 --base 指向一个含 latest 的目录" >&2; exit 3; }

name="packnest-desktop_${VER}_${os}_${arch}"
file="${name}.app.zip"
if [ -n "$BASE" ]; then
  base="${BASE%/}"
else
  base="https://gitee.com/${GITEE_REPO}/releases/download/gui-v${VER}"
fi
url="${base}/${file}"
sums_url="${base}/checksums.txt"

echo "将安装 packnest 桌面端 ${VER}（${os}-${arch}）"
echo "  来源: $url"
echo "  目录: $DIR"
[ "$DRY_RUN" -eq 1 ] && exit 0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if ! curl -fsSL --retry 3 --retry-delay 1 --retry-connrefused --max-time 600 -o "$tmp/gui.zip" "$url"; then
  cat >&2 <<MSG
下载失败：${url}
  · 重试一次（gitee 国内偶发连接重置）
  · 或换自托管源：sh install-gui.sh --base https://你的静态源/packnest/gui-v${VER}
MSG
  exit 3
fi

if [ "$SKIP_VERIFY" -eq 0 ]; then
  sums=$(curl -fsSL --retry 2 --retry-connrefused --max-time 60 "$sums_url" 2>/dev/null || true)
  if [ -z "$sums" ]; then
    echo "拿不到校验清单 ${sums_url}（该版本没有清单？）；确认来源可信后可加 --skip-verify" >&2
    exit 4
  fi
  # ① 先验清单的 sshsig：镜像站换不了清单，也就换不掉产物
  sig=$(curl -fsSL --retry 2 --retry-connrefused --max-time 60 "${sums_url}.sig" 2>/dev/null || true)
  if [ -n "$sig" ] && [ -n "$ALLOWED_SIGNERS" ] && command -v ssh-keygen >/dev/null 2>&1; then
    printf '%s\n' "$sig" > "$tmp/checksums.txt.sig"
    printf '%s\n' "$ALLOWED_SIGNERS" > "$tmp/allowed_signers"
    printf '%s\n' "$sums" > "$tmp/checksums.txt"
    if ! ssh-keygen -Y verify -f "$tmp/allowed_signers" -I "$SIGNER" \
         -n "$SSHSIG_NAMESPACE" -s "$tmp/checksums.txt.sig" < "$tmp/checksums.txt" >/dev/null 2>&1; then
      echo "签名校验失败：${sums_url}.sig 与内置公钥不匹配（清单或产物可能被篡改）" >&2
      exit 4
    fi
    echo "  校验：sshsig OK（发布签名）"
  elif [ -n "$sig" ]; then
    echo "  提示：未验签（本机没有 ssh-keygen，或脚本未内置公钥），本次只比对 sha256" >&2
  else
    echo "  提示：该版本没有发布签名（${sums_url}.sig），本次只比对 sha256" >&2
  fi
  # ② 再比产物 sha256
  want=$(printf '%s\n' "$sums" | awk -v f="$file" '$2==f {print $1; exit}')
  got=$(sha256_of "$tmp/gui.zip")
  if [ -z "$want" ] || [ -z "$got" ]; then
    echo "拿不到 ${file} 的 sha256（${sums_url}）；确认来源可信后可加 --skip-verify" >&2
    exit 4
  fi
  [ "$want" = "$got" ] || {
    echo "sha256 校验失败：期望 ${want}，实际 ${got}（产物可能被篡改或损坏）" >&2
    exit 4
  }
  echo "  校验：sha256 OK（checksums.txt）"
fi

# 解压到临时目录再整体搬过去：避免覆盖到一半留下半个 .app
( cd "$tmp" && mkdir -p unz && unzip -q -o gui.zip -d unz )
app="$tmp/unz/packnest.app"
[ -d "$app" ] || { echo "压缩包里没有 packnest.app（产物格式不对？）" >&2; exit 4; }
mkdir -p "$DIR"
rm -rf "$DIR/packnest.app"
mv "$app" "$DIR/packnest.app"

echo "装好了：$DIR/packnest.app"
echo "首次打开：系统设置 → 隐私与安全性 → 找到 packnest → 点「仍要打开」（ad-hoc 签名，未公证）"
echo "也可以：open \"$DIR/packnest.app\""
