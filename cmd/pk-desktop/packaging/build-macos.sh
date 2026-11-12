#!/bin/sh
# 打包 macOS .app。
#
#   sh cmd/pk-desktop/packaging/build-macos.sh [版本号]
#
# 签名/公证（可选，靠环境变量，凭据不进仓库）：
#   PK_SIGN_IDENTITY="Developer ID Application: 你的名字 (TEAMID)"   # 不设则 ad-hoc 签名（本机自用）
#   PK_NOTARY_PROFILE="notary"                                     # xcrun notarytool 的 keychain profile
#                                                                  # 设了会自动 submit --wait + staple
set -eu
cd "$(dirname "$0")/.."            # → cmd/pk-desktop
VER="${1:-$(cat ../../dist/latest 2>/dev/null || echo 0.0.0)}"
ARCH="${2:-$(uname -m)}"      # arm64 / amd64（交叉编译用 clang -arch，macOS 上可行）
CC_ARCH=arm64; [ "$ARCH" = "amd64" ] && CC_ARCH=x86_64
APP="../../dist/gui/packnest.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOOS=darwin GOARCH="$ARCH" \
  CC="clang -arch $CC_ARCH" \
  go build -trimpath -ldflags "-s -w" -o "$APP/Contents/MacOS/pk-desktop" .
sed "s/__VERSION__/$VER/g" packaging/macos/Info.plist > "$APP/Contents/Info.plist"

# 应用图标（Finder/Dock）：PNG → iconset → icns，只用 macOS 自带的 sips/iconutil
if command -v sips >/dev/null 2>&1 && command -v iconutil >/dev/null 2>&1; then
  set_dir="$APP/Contents/Resources/AppIcon.iconset"
  mkdir -p "$set_dir"
  for sz in 16 32 64 128 256 512; do
    sips -z $sz $sz assets/icon.png --out "$set_dir/icon_${sz}x${sz}.png" >/dev/null 2>&1 || true
    dbl=$((sz * 2))
    sips -z $dbl $dbl assets/icon.png --out "$set_dir/icon_${sz}x${sz}@2x.png" >/dev/null 2>&1 || true
  done
  iconutil -c icns "$set_dir" -o "$APP/Contents/Resources/icon.icns" >/dev/null 2>&1 || true
  rm -rf "$set_dir"
  /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string icon" "$APP/Contents/Info.plist" >/dev/null 2>&1 || true
fi
chmod +x "$APP/Contents/MacOS/pk-desktop"
# 签名：有 Developer ID 就用它（含 hardened runtime，公证要求）；否则 ad-hoc（本机自用）
if [ -n "${PK_SIGN_IDENTITY:-}" ]; then
  codesign --force --deep --options runtime --timestamp --sign "${PK_SIGN_IDENTITY}" "$APP" \
    || { echo "Developer ID 签名失败" >&2; exit 1; }
  echo "已用 Developer ID 签名：${PK_SIGN_IDENTITY}"
  if [ -n "${PK_NOTARY_PROFILE:-}" ]; then
    zip_tmp="$APP/../notarize.zip"
    rm -f "$zip_tmp"; ( cd "$(dirname "$APP")" && zip -q -r -X "$(basename "$zip_tmp")" "$(basename "$APP")" )
    xcrun notarytool submit "$zip_tmp" --keychain-profile "${PK_NOTARY_PROFILE}" --wait \
      || { echo "公证失败（notarytool）" >&2; exit 1; }
    xcrun stapler staple "$APP" || true
    rm -f "$zip_tmp"
    echo "已公证并 staple"
  fi
else
  codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "（ad-hoc 签名跳过：未安装 codesign 或权限不足）"
  echo "签名方式：ad-hoc（分发要设 PK_SIGN_IDENTITY + PK_NOTARY_PROFILE）"
fi
echo "已生成 ${APP}（packnest.app，版本 ${VER}）"
echo "运行：open \"${APP}\"   或  \"${APP}/Contents/MacOS/pk-desktop\""
