#!/bin/sh
# 构建并上传 **GUI（pk-desktop）** 的 mac 产物到已发布的 release（gitee + GitHub）。
#
#   sh scripts/release-gui.sh [--dry-run] [--version 0.10.0]
#
# 只做 macOS：Windows/Linux 需要各自的 WebView 工具链（见 cmd/pk-desktop/README.md），
# 由 CI 的 gui-build 任务产出后再下载，用同一条路径上传（保持"本机唯一发布者"）。
#
# 产物：dist/gui/packnest-desktop_<ver>_<os>_<arch>.{app.zip,zip,tar.gz}
#
# 挂在**独立的 release tag**（默认 `gui-v<cliVer>`）而不是 CLI 的 `v<cliVer>`：
# gitee 的 release API 不返回附件 id，同名附件**删不掉也换不了**；GUI 会频繁重发，
# 用一个可整体删除重建的 tag 才不会把旧产物留在同一个 release 里。
set -eu
DIST="${DIST:-dist}"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
GH_REPO="${PK_GH_REPO:-nekoleamo/packnest}"
DRY=0
VER=""
FROM=""   # --from-dir：上传 CI 构建好的产物（不再本机重编）
TAG=""    # 默认 gui-v<ver>；GUI 单独一个 release，便于整体重建
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --version) VER="$2"; shift 2 ;;
    --from-dir) FROM="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done
[ -n "$VER" ] || VER=$(cat "$DIST/latest")
[ -n "$TAG" ] || TAG="gui-v$VER"

mkdir -p "$DIST/gui"
if [ -n "$FROM" ]; then
  if [ "$(cd "$FROM" && pwd)" = "$(cd "$DIST/gui" && pwd)" ]; then
    echo "产物已在 $DIST/gui，跳过拷贝"
    FROM=""
    build_gui=0
  fi
fi
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
# 只发 Apple Silicon（2026-11-12 拍板：mac intel/amd64 放弃 —— 自用机器是 arm64，
# macos-15-intel 那个 CI job 也一直失败；Intel Mac 需自己 make gui）
for arch in arm64; do
  if [ "${build_gui}" = "0" ]; then break; fi
  app="$DIST/gui/packnest.app"
  rm -rf "$app"
  # 打包逻辑只有一处：cmd/pk-desktop/packaging/build-macos.sh（图标 / Info.plist / 签名都在里面）
  ( cd cmd/pk-desktop && sh packaging/build-macos.sh "$VER" "$arch" >/dev/null )
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
rel_id=$(curl -s "$api/releases/tags/$TAG" | python3 -c 'import sys,json
try: print((json.load(sys.stdin) or {}).get("id") or "")
except Exception: print("")' 2>/dev/null || true)
if [ -z "$rel_id" ]; then
  # 不存在就建：GUI 单独一个 release，之后要整体重发可以先删掉它（API 支持删 release）
  code=$(curl -s -o /tmp/gitee-gui-rel.json -w '%{http_code}' -X POST "$api/releases" \
    --data-urlencode "access_token=$TOKEN" --data-urlencode "tag_name=$TAG" \
    --data-urlencode "name=$TAG" --data-urlencode "body=packnest GUI ${VER} (requires pk CLI)" \
    --data-urlencode "target_commitish=main")
  rel_id=$(python3 -c 'import json
try: print((json.load(open("/tmp/gitee-gui-rel.json")) or {}).get("id") or "")
except Exception: print("")' 2>/dev/null || true)
  echo "  建 GUI release $TAG → $code (id=$rel_id)"
  rm -f /tmp/gitee-gui-rel.json
fi
[ -n "$rel_id" ] || { echo "gitee 上建/找 release $TAG 失败（token 权限？）" >&2; exit 3; }
# gitee 的 release API 不提供附件 id（只有名字 + URL），**删不掉**已上传的附件：
# 所以上传前先查一遍，同名直接跳过，避免同名重复累积。
existing=$(curl -s "$api/releases/tags/$TAG" | python3 -c '
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
  # GitHub API 偶尔 EOF：建 release / 上传都重试几次
  n=0
  until gh release view "$TAG" --repo "$GH_REPO" >/dev/null 2>&1; do
    n=$((n + 1)); [ "$n" -le 5 ] || break
    gh release create "$TAG" --repo "$GH_REPO" --title "$TAG" \
      --notes "packnest GUI ${VER} (requires pk CLI)" >/dev/null 2>&1 && break
    sleep 8
  done
  n=0
  until gh release upload "$TAG" --repo "$GH_REPO" --clobber \
    "$DIST/gui/packnest-desktop_${VER}"_*.zip "$DIST/gui/packnest-desktop_${VER}"_*.tar.gz
  do
    n=$((n + 1)); [ "$n" -le 5 ] || break
    sleep 8
  done
  echo "已上传 GitHub Releases（${TAG}）"
fi
echo "完成。用户：下载 zip → 解压 → open packnest.app（首次启动 macOS 会问权限）"
