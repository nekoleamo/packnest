#!/bin/sh
# 给已构建好的 dist/v<版本>/checksums.txt 打 sshsig 发布签名（人工、离线私钥）。
#
#   PK_SIGNING_KEY=/Volumes/usb/pk-release-2026 sh scripts/sign-release.sh
#
# 为什么不在 make release 里自动签：私钥一旦常驻发布机，"发布机被接管"就能签出恶意产物，
# 签名等于零增益（见 ~/Documents/Plan/17-包管理器/pn-1.7-产物签名-方案.md §二）。
set -eu
cd "$(dirname "$0")/.."

DIST="${DIST:-dist}"
VER="${PK_VERSION:-$(cat "$DIST/latest" 2>/dev/null | tr -d ' \t\r\n' || true)}"
[ -n "$VER" ] || { echo "拿不到版本号（先 make release，或用 PK_VERSION=vX.Y.Z）" >&2; exit 1; }
VER=${VER#v}
KEY="${PK_SIGNING_KEY:-}"
[ -n "$KEY" ] || { echo "需要 PK_SIGNING_KEY 指向离线私钥（如 /Volumes/usb/pk-release-2026）" >&2; exit 2; }
[ -f "$KEY" ] || { echo "私钥不存在: $KEY" >&2; exit 2; }

SUMS="$DIST/v$VER/checksums.txt"
[ -f "$SUMS" ] || { echo "找不到 $SUMS（先跑 make release）" >&2; exit 1; }

# 先确认私钥能读：设了口令的私钥在非交互环境会卡在提示上，提前给出明确说明。
if ! ssh-keygen -y -f "$KEY" >/dev/null 2>&1; then
  echo "读不出私钥 $KEY：多半是设了口令（非交互环境没法输入）。" >&2
  echo "用无口令的离线密钥，或在交互终端里手动跑 ssh-keygen -Y sign。" >&2
  exit 2
fi

umask 077
rm -f "$SUMS.sig"
ssh-keygen -Y sign -f "$KEY" -n pk-release "$SUMS" >/dev/null
[ -f "$SUMS.sig" ] || { echo "签名没生成（ssh-keygen 失败？）" >&2; exit 1; }

# 自验：从私钥推导公钥，确认签名可验（防止用错密钥发出）
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
ssh-keygen -y -f "$KEY" > "$tmp/pub" 2>/dev/null || { echo "读不出公钥（私钥损坏？）" >&2; exit 1; }
printf 'release@packnest.dev %s\n' "$(cat "$tmp/pub")" > "$tmp/allowed_signers"
ssh-keygen -Y verify -f "$tmp/allowed_signers" -I release@packnest.dev -n pk-release \
  -s "$SUMS.sig" < "$SUMS" >/dev/null || { echo "自验失败：签名与私钥不匹配" >&2; exit 1; }

echo "已签名：$SUMS → $SUMS.sig"
echo "这把公钥要同时写进两处（否则 install.sh / pk doctor 认不出）："
echo "  internal/verify/keys/allowed_signers"
echo "  scripts/install.sh 的 ALLOWED_SIGNERS"
cat "$tmp/pub"
