#!/bin/sh
# 把**已发布**的产物拉回来，和本地 checksums.txt 逐条对 sha256（并验发布签名）。
#
#   sh scripts/verify-release.sh                          # 默认对 CLI 的 gitee release
#   sh scripts/verify-release.sh --base https://your-host/packnest
#   PK_VERSION=0.7.0 sh scripts/verify-release.sh
#   sh scripts/verify-release.sh --gui                    # 核对 GUI 的 gui-v<版本> release
#   sh scripts/verify-release.sh --gui --base file://$PWD/dist/gui   # 离线核对本地产物
#
# 为什么要有这一步：0.7.0 发布时，Homebrew tap / Scoop bucket 里的 sha256 是**另一次构建**
# 的产物（tar/gzip 会写入 mtime，同一版本重建一次 sha256 就变），结果 brew install 报
# "reports different checksum"。发布后立刻对一遍，能在用户踩到之前发现。
set -eu
cd "$(dirname "$0")/.."

DIST="${DIST:-dist}"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
SIGNERS="${PK_SIGNERS_FILE:-internal/verify/keys/allowed_signers}"
GUI=0
VER=""
BASE=""
TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="${2%/}"; shift 2 ;;
    --version) VER="${2#v}"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --gui) GUI=1; shift ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

if [ "$GUI" = "1" ]; then
  # GUI 与 CLI 版本解耦（CLI 1.7.0 时 GUI 可能是 0.10.x），产物挂在独立的 gui-v<版本> tag
  [ -n "$VER" ] || VER=$(cat "$DIST/gui/latest" 2>/dev/null | tr -d ' \t\r\n' || true)
  [ -n "$VER" ] || VER=$(cat "$DIST/latest" 2>/dev/null | tr -d ' \t\r\n' || true)
  VER=${VER#v}
  [ -n "$TAG" ] || TAG="gui-v$VER"
  SUMS="$DIST/gui/checksums.txt"
else
  VER="${VER:-$(cat "$DIST/latest" 2>/dev/null | tr -d ' \t\r\n' || true)}"
  [ -n "$TAG" ] || TAG="v$VER"
  SUMS="$DIST/v$VER/checksums.txt"
fi
[ -n "$VER" ] || { echo "拿不到版本号（先构建产物，或用 --version 指定）" >&2; exit 1; }
[ -n "$BASE" ] || BASE="https://gitee.com/${GITEE_REPO}/releases/download/${TAG}"

[ -f "$SUMS" ] || {
  if [ "$GUI" = "1" ]; then
    echo "找不到 ${SUMS}（先 sh scripts/release-gui.sh 生成并签名）" >&2
  else
    echo "找不到 ${SUMS}（先跑 make release，或用 --version 指定）" >&2
  fi
  exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail=0
if [ "$GUI" = "1" ]; then
  printf '核对 GUI %s 的已发布产物：%s\n' "$TAG" "$BASE"
else
  printf '核对 v%s 的已发布产物：%s\n' "$VER" "$BASE"
fi

# 发布签名：远端有 checksums.txt.sig 就验（公钥与 pk 内置的那份同源）
if curl -fsSL --max-time 60 -o "$tmp/checksums.txt.sig" "$BASE/checksums.txt.sig" 2>/dev/null; then
  if ! command -v ssh-keygen >/dev/null 2>&1; then
    echo "  签名：跳过（本机没有 ssh-keygen）" >&2
  elif ! grep -q 'ssh-ed25519' "$SIGNERS" 2>/dev/null; then
    echo "  签名：跳过（$SIGNERS 里还没有公钥）" >&2
  elif ssh-keygen -Y verify -f "$SIGNERS" -I release@packnest.dev -n pk-release \
       -s "$tmp/checksums.txt.sig" < "$SUMS" >/dev/null 2>&1; then
    echo "  签名：OK（sshsig / pk-release）"
  else
    echo "  签名：不符（checksums.txt 与 .sig 对不上，或密钥已轮换未同步）" >&2
    fail=1
  fi
else
  if [ "$GUI" = "1" ]; then
    echo "  签名：缺失（GUI 产物必须带 .sig，否则下载方无法确认清单未被替换）" >&2
    fail=1
  else
    echo "  签名：该版本没有 .sig（兼容期正常）" >&2
  fi
fi

while read -r want file; do
  [ -n "${file:-}" ] || continue
  printf '  %-46s ' "$file"
  if ! curl -fsSL --max-time 300 -o "$tmp/$file" "$BASE/$file"; then
    echo "下载失败"; fail=1; continue
  fi
  got=$(shasum -a 256 "$tmp/$file" 2>/dev/null | cut -d' ' -f1)
  [ -n "$got" ] || got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
  if [ "$got" = "$want" ]; then
    echo "OK"
  else
    echo "不符"
    printf '      清单 %s\n      实际 %s\n' "$want" "$got"
    fail=1
  fi
done < "$SUMS"

# scripts/latest 是安装脚本在 gitee API 不可用时的版本来源：必须与本次发布一致，
# 否则用户跑 `curl … | sh` 会拿到"版本号指向一个不存在的产物"或旧版本。
# （只对 CLI 版本有意义：GUI 有自己的 gui-v<版本> tag，不参与这条兜底）
if [ "$GUI" = "0" ]; then
  if [ -f scripts/latest ]; then
    pinned=$(tr -d ' \t\r\n' < scripts/latest)
    if [ "$pinned" != "$VER" ]; then
      echo "scripts/latest = ${pinned}，但发布版本是 ${VER}（安装脚本的兜底版本源过期）" >&2
      fail=1
    fi
  else
    echo "缺少 scripts/latest（安装脚本的兜底版本源）" >&2
    fail=1
  fi
fi

if [ "$fail" -ne 0 ]; then
  cat >&2 <<'EOS'

有产物与 checksums.txt 不一致。常见原因与处置：
  1) 发布后又重跑了一次 make release（同一版本被重新构建）→ 重新上传产物，或按当前
     checksums.txt 重新生成并推送渠道清单：sh scripts/gen-manifests.sh && sh scripts/release-taps.sh
  2) Homebrew/Scoop 的用户侧清单不是从这份 checksums.txt 生成的 → 同上，重推清单
  3) 上传过程中被截断 → 重新上传该文件
EOS
  exit 1
fi
echo "全部一致（$(grep -c . "$SUMS") 个产物）"
