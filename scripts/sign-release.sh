#!/bin/sh
# 给已构建好的 dist/v<版本>/checksums.txt 打 sshsig 发布签名（人工、离线私钥）。
#
#   PK_SIGNING_KEY=/Volumes/usb/pk-release-2026 sh scripts/sign-release.sh
#
# 签名实现只有一处：scripts/lib/sign.sh（GUI 产物用同一个函数，见 scripts/release-gui.sh）。
# 为什么不在 make release 里自动签：私钥一旦常驻发布机，"发布机被接管"就能签出恶意产物，
# 签名等于零增益（见 ~/Documents/Plan/17-包管理器/pn-1.7-产物签名-方案.md §二）。
set -eu
cd "$(dirname "$0")/.."
. scripts/lib/sign.sh

DIST="${DIST:-dist}"
VER="${PK_VERSION:-$(cat "$DIST/latest" 2>/dev/null | tr -d ' \t\r\n' || true)}"
[ -n "$VER" ] || { echo "拿不到版本号（先 make release，或用 PK_VERSION=vX.Y.Z）" >&2; exit 1; }
VER=${VER#v}

sign_checksums "$DIST/v$VER"
