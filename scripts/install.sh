#!/bin/sh
# packnest 安装脚本（不访问 GitHub）
#
#   curl -fsSL https://gitee.com/${GITEE_REPO}/raw/main/scripts/install.sh | sh
#
# 选项：
#   --base URL            任意静态源基地址（自己托管的 OSS/CDN/内网），优先级最高
#                         例：--base https://static.example.com/packnest
#   --from gitee|base     分发通道（默认：给了 --base 就 base，否则 gitee）
#   --mirror cn|default   保留兼容；两种取值现在都走 gitee（npm 渠道已于 2026-11-11 停用）
#   --repo owner/name     gitee 仓库（默认 null_593_5354/packnest）
#   --version vX.Y.Z      指定版本（默认最新）
#   --dir DIR             安装目录（默认 ~/.local/bin，无需 sudo）
#   --skip-verify         跳过 sha256 与签名校验（不推荐；只在拿不到校验和时用）
#   --dry-run             只打印将要做什么
#
# 只要能 GET 到 <base>/v<版本>/packnest_<版本>_<os>_<arch>.tar.gz + checksums.txt，就能装。
# npm 平台包这条通道已经停用（见 docs/publishing.md）：npm 的 stage/approve 要 2FA，
# 而且云端 CI 与本机构建出的归档字节不同，容易出现两套 sha256。
set -eu

MIRROR="default"
BASE="${PK_INSTALL_BASE:-}"
FROM=""
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
VERSION=""
DIR="${HOME}/.local/bin"
DRY_RUN=0
SKIP_VERIFY="${PK_SKIP_VERIFY:-0}"

# 发布签名公钥（allowed_signers 格式，与 pk 二进制内置的那份同源）。
# 发版前由 scripts/sign-release.sh 派生的一行填在这里；为空则只做 sha256 并打印提示。
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
    --mirror) MIRROR="${2:-cn}"; shift 2 ;;
    --base) BASE="$2"; shift 2 ;;
    --from) FROM="$2"; shift 2 ;;
    --repo) GITEE_REPO="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

case "$FROM" in
  ""|gitee|base) ;;
  npm)
    echo "npm 通道已于 2026-11-11 停用（npm 的 stage/approve 需要 2FA，且跨机器构建的归档字节不同）。" >&2
    echo "改用默认通道（gitee）或自托管：--base https://你的静态源/packnest" >&2
    exit 2 ;;
  *) echo "未知 --from 值: $FROM（支持 gitee|base）" >&2; exit 2 ;;
esac
[ "$MIRROR" = "cn" ] && echo "提示：--mirror cn 现在也走 gitee（npm 镜像通道已停用）" >&2

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
  if [ -n "$BASE" ]; then FROM=base; else FROM=gitee; fi
fi

if [ -z "$VERSION" ] && [ -n "$BASE" ]; then
  VERSION=$(curl -fsSL --max-time 15 "$BASE/latest" 2>/dev/null | tr -d ' \t\r\n' || true)
  VERSION=${VERSION#v}
fi

if [ -z "$VERSION" ]; then
  # 版本号跟着下载渠道走：gitee release API → 仓库里的单一真源 scripts/latest。
  VERSION=$(curl -fsSL --max-time 15 "https://gitee.com/api/v5/repos/${GITEE_REPO}/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name":"\([^"]*\)".*/\1/p' | head -1 || true)
  # releases/latest 会指到 GUI 的独立 tag（gui-vX.Y.Z）：那不是 CLI 的产物，直接不认
  # （2026-11-13 实测踩到：发完 gui-v0.10.4 后一行安装拼出了 vgui-v0.10.4 的 URL，404）。
  case "$VERSION" in
    v[0-9]*.[0-9]*.[0-9]*) : ;;
    *) VERSION="" ;;
  esac
  if [ -z "$VERSION" ]; then
    # gitee API 匿名偶发 401 / 限流：退回仓库里的单一真源文件（发布时由 make release 更新）
    VERSION=$(curl -fsSL --max-time 15 "https://gitee.com/${GITEE_REPO}/raw/main/scripts/latest" 2>/dev/null \
      | tr -d ' \t\r\n' || true)
  fi
  VERSION=${VERSION#v}
  [ -n "$VERSION" ] || { echo "拿不到最新版本；请用 --version 指定，或 --base 指向自托管源" >&2; exit 3; }
fi

name="packnest_${VERSION}_${os}_${arch}"
if [ -n "$BASE" ]; then
  url="${BASE%/}/v${VERSION}/${name}.tar.gz"
  sums_url="${BASE%/}/v${VERSION}/checksums.txt"
else
  url="https://gitee.com/${GITEE_REPO}/releases/download/v${VERSION}/${name}.tar.gz"
  sums_url="https://gitee.com/${GITEE_REPO}/releases/download/v${VERSION}/checksums.txt"
fi

echo "将要安装 packnest ${VERSION}（${os}-${arch}）"
echo "  来源: $url"
echo "  目录: $DIR"
[ "$DRY_RUN" -eq 1 ] && exit 0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if ! curl -fsSL --retry 3 --retry-delay 1 --retry-connrefused --max-time 300 -o "$tmp/pk.tar.gz" "$url"; then
  cat >&2 <<MSG
下载失败：${url}
  · 重试一次（gitee 国内偶发连接重置）
  · 换网络 / 开代理后再试
  · 或改用自托管源：sh install.sh --base https://你的静态源/packnest
MSG
  exit 3
fi
if [ "$SKIP_VERIFY" -eq 0 ]; then
  sums=$(curl -fsSL --retry 2 --retry-connrefused --max-time 60 "$sums_url" 2>/dev/null || true)
  if [ -z "$sums" ]; then
    # 清单都拉不到就别往下走：否则会被误报成"签名不匹配"
    echo "拿不到校验清单 ${sums_url}（网络不通或该版本没有这个文件）；确认来源可信后可加 --skip-verify" >&2
    exit 4
  fi
  # ① 签名：先验 checksums.txt 的 sshsig，再比产物 sha256 —— 镜像站改不了这里。
  sig_url="${sums_url}.sig"
  sig=$(curl -fsSL --retry 2 --retry-connrefused --max-time 60 "$sig_url" 2>/dev/null || true)
  if [ -n "$sig" ] && [ -n "$ALLOWED_SIGNERS" ] && command -v ssh-keygen >/dev/null 2>&1; then
    printf '%s\n' "$sig" > "$tmp/checksums.txt.sig"
    printf '%s\n' "$ALLOWED_SIGNERS" > "$tmp/allowed_signers"
    printf '%s\n' "$sums" > "$tmp/checksums.txt"
    if ! ssh-keygen -Y verify -f "$tmp/allowed_signers" -I "$SIGNER" \
         -n "$SSHSIG_NAMESPACE" -s "$tmp/checksums.txt.sig" < "$tmp/checksums.txt" >/dev/null 2>&1; then
      echo "签名校验失败：$sig_url 与内置公钥不匹配（产物或清单可能被篡改）。" >&2
      echo "确认来源可信后可加 --skip-verify，但请先核对官方公布的密钥指纹。" >&2
      exit 4
    fi
    echo "  校验：sshsig OK（发布签名）"
  elif [ -n "$sig" ]; then
    echo "  提示：未验签（本机没有 ssh-keygen，或脚本未内置公钥），本次只比对 sha256" >&2
  else
    echo "  提示：该版本没有发布签名（$sig_url），本次只比对 sha256" >&2
  fi

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
