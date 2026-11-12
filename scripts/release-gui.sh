#!/bin/sh
# 构建并上传 **GUI（pk-desktop）** 的 mac 产物到已发布的 release（gitee + GitHub）。
#
#   sh scripts/release-gui.sh [--dry-run] [--version 0.10.0]
#
# 只做 macOS：Windows/Linux 需要各自的 WebView 工具链（见 cmd/pk-desktop/README.md），
# 由 CI 的 gui-build 任务产出后再下载，用同一条路径上传（保持"本机唯一发布者"）。
#
# 产物：dist/gui/packnest-desktop_<ver>_darwin_{arm64,amd64}.app.zip（内部是 packnest.app）
set -eu
DIST="${DIST:-dist}"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
GH_REPO="${PK_GH_REPO:-nekoleamo/packnest}"
DRY=0
VER=""
FROM=""   # --from-dir：上传 CI 构建好的产物（不再本机重编）
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --version) VER="$2"; shift 2 ;;
    --from-dir) FROM="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done
[ -n "$VER" ] || VER=$(cat "$DIST/latest")

mkdir -p "$DIST/gui"
if [ -n "$FROM" ]; then
  # CI（gui-build.yml）已经构建好三平台产物：拷进来直接上传，字节与 CI 一致
  echo "使用预构建产物：${FROM}"
  for f in "$FROM"/*; do
    case "$f" in *.zip|*.tar.gz|*.exe) cp "$f" "$DIST/gui"/ ;; esac
  done
  ls "$DIST/gui" | sed 's|^|  |'
  build_gui=0
else
  build_gui=1
  echo "构建 GUI（pk-desktop ${VER}）"
fi
for arch in arm64 amd64; do
  if [ "${build_gui}" = "0" ]; then break; fi
  app="$DIST/gui/packnest.app"
  rm -rf "$app"
  ( cd cmd/pk-desktop && \
    MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" \
    CC="clang -arch $([ "$arch" = amd64 ] && echo x86_64 || echo arm64)" \
    go build -trimpath -ldflags "-s -w" -o "$OLDPWD/$app/Contents/MacOS/pk-desktop" . 2>/dev/null ) || {
      echo "构建 $arch 失败" >&2; exit 1; }
  mkdir -p "$app/Contents/Resources"
  sed "s/__VERSION__/${VER}/g" cmd/pk-desktop/packaging/macos/Info.plist > "$app/Contents/Info.plist"
  chmod +x "$app/Contents/MacOS/pk-desktop"
  codesign --force --deep --sign - "$app" >/dev/null 2>&1 || true
  out="$DIST/gui/packnest-desktop_${VER}_darwin_${arch}.app.zip"
  rm -f "$out"
  ( cd "$DIST/gui" && zip -q -r -X "$(basename "$out")" packnest.app )
  printf '  %-52s %s\n' "$(basename "$out")" "$(du -h "$out" | cut -f1)"
done

if [ "$DRY" -eq 1 ]; then echo "dry-run：没有上传"; exit 0; fi

# 上传还是走"本机唯一发布者"：gitee 用 token（凭据文件），GitHub 用 gh
. "$(dirname "$0")/lib/secrets.sh"
TOKEN_FILE="${PK_SECRETS_FILE:-$HOME/.pi/agent/local/secrets.env}"
TOKEN="${GITEE_TOKEN:-$(secret_from_file "$TOKEN_FILE" GITEE_TOKEN GITEE_ACCESS_TOKEN)}"
api="https://gitee.com/api/v5/repos/$GITEE_REPO"
rel_id=$(curl -s "$api/releases/tags/v$VER" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id") or "")' 2>/dev/null)
[ -n "$rel_id" ] || { echo "gitee 上没有 v$VER 的 release（先发 CLI 版本）" >&2; exit 3; }
# gitee 的 release API 不提供附件 id（只有名字 + URL），**删不掉**已上传的附件：
# 所以上传前先查一遍，同名直接跳过，避免同名重复累积。
existing=$(curl -s "$api/releases/tags/v$VER" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print(""); raise SystemExit
print("\n".join(a.get("name","") for a in d.get("assets", [])))')
for f in "$DIST/gui/packnest-desktop_${VER}"_*.zip "$DIST/gui/packnest-desktop_${VER}"_*.tar.gz; do
  [ -f "$f" ] || continue
  if printf '%s\n' "$existing" | grep -qx "$(basename "$f")"; then
    printf '  gitee  %-46s 已存在，跳过\n' "$(basename "$f")"
    continue
  fi
  code=$(curl -s -o /tmp/gitee-gui.json -w '%{http_code}' -X POST "$api/releases/$rel_id/attach_files" \
    -F "access_token=$TOKEN" -F "file=@$f")
  printf '  gitee  %-46s %s\n' "$(basename "$f")" "$code"
done
rm -f /tmp/gitee-gui.json
if command -v gh >/dev/null 2>&1; then
  gh release upload "v$VER" --repo "$GH_REPO" --clobber \
    "$DIST/gui/packnest-desktop_${VER}"_*.zip "$DIST/gui/packnest-desktop_${VER}"_*.tar.gz
  echo "已上传 GitHub Releases"
fi
echo "完成。用户：下载 zip → 解压 → open packnest.app（首次启动 macOS 会问权限）"
