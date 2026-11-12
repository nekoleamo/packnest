#!/bin/sh
# 打包 macOS .app（不做签名/公证：本地自用与 CI 冒烟够用；分发要另外走 Developer ID + notarize）。
#
#   sh cmd/pk-desktop/packaging/build-macos.sh [版本号]
set -eu
cd "$(dirname "$0")/.."            # → cmd/pk-desktop
VER="${1:-$(cat ../../dist/latest 2>/dev/null || echo 0.0.0)}"
APP="../../dist/gui/packnest.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" \
  -o "$APP/Contents/MacOS/pk-desktop" .
sed "s/__VERSION__/$VER/g" packaging/macos/Info.plist > "$APP/Contents/Info.plist"
chmod +x "$APP/Contents/MacOS/pk-desktop"
# 本地自用：ad-hoc 签名（换成 Developer ID 才能过 Gatekeeper 首次启动）
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "（ad-hoc 签名跳过：未安装 codesign 或权限不足）"
echo "已生成 ${APP}（packnest.app，版本 ${VER}）"
echo "运行：open \"${APP}\"   或  \"${APP}/Contents/MacOS/pk-desktop\""
