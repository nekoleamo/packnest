#!/bin/sh
# 构建并上传 **GUI（pk-desktop）** 的 mac 产物到已发布的 release（gitee + GitHub）。
#
#   sh scripts/release-gui.sh [--dry-run] [--version 0.10.1]
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
PRUNE=""  # --prune [N]：只保留最近 N 个 gui-v* release（默认 3）。gitee 单仓库附件总量 1GB
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --version) VER="$2"; shift 2 ;;
    --from-dir) FROM="$2"; shift 2 ;;
    --tag) TAG="$2"; shift 2 ;;
    --prune) PRUNE="3"; if [ $# -ge 2 ] && [ "${2#-}" = "$2" ]; then PRUNE="$2"; shift 2; else shift; fi ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done
[ -n "$VER" ] || VER=$(cat "$DIST/latest")
[ -n "$TAG" ] || TAG="gui-v$VER"

PYBIN=$(command -v python3 || command -v python || true)
[ -n "$PYBIN" ] || { echo "需要 python（列/删 release 用）" >&2; exit 2; }

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

# ── 校验清单 + 发布签名 ────────────────────────────────────────────────
# GUI 产物也要能被独立核对：下载 .app.zip 的人拿 checksums.txt 对 sha256，
# 再拿 checksums.txt.sig 验"清单没被镜像站换过"。签名与 CLI 共用
# scripts/lib/sign.sh（一把密钥、一个发布者）。
gui_artifacts() {
  # sort -u：mac 的 .app.zip 同时匹配 *.app.zip 与 *.zip，不去重会算两遍
  ls "$DIST/gui"/packnest-desktop_*.app.zip "$DIST/gui"/packnest-desktop_*.zip \
     "$DIST/gui"/packnest-desktop_*.tar.gz 2>/dev/null | sort -u || true
}
# 先列清单再算：--from-dir 进来的 CI 产物与本地构建的产物一视同仁
_gui_list=$(gui_artifacts)
[ -n "$_gui_list" ] || { echo "没有 GUI 产物可发布（先构建，或用 --from-dir）" >&2; exit 4; }
# ⚠️ 这里必须在 `cd` **之后**重新列出文件，而且不能走 `xargs`：
#   ① gui_artifacts 里的路径带 $DIST 前缀，cd 进 dist/gui 之后 glob 一个都匹配不到；
#   ② 文件列表为空时 `shasum -a 256`（无参数）会去读 stdin，产出一行 `e3b0c…  -`
#      —— 于是 checksums.txt 看起来"有内容"、签名也有效，但**没有任何产物的哈希**，
#      用户侧 install-gui.sh 一定报"拿不到 sha256"。2026-11-15 实测就是这么发出去的
#      （gui-v0.11.0 及更早干脆一个 checksums.txt 都没有）。
_gui_names=$(printf '%s\n' "$_gui_list" | xargs -n1 basename | sort -u)
[ -n "$_gui_names" ] || { echo "没有 GUI 产物可算校验和（glob 没匹配到）" >&2; exit 4; }
# shellcheck disable=SC2086
( cd "$DIST/gui" && { shasum -a 256 $_gui_names 2>/dev/null || sha256sum $_gui_names; } | sort -k2 > checksums.txt )
_lines=$(grep -c . "$DIST/gui/checksums.txt" || true)
_expected=$(printf '%s\n' "$_gui_names" | grep -c . || true)
[ "$_lines" = "$_expected" ] || {
  echo "校验和行数不对：应有 ${_expected} 行，实际 ${_lines} 行（先看 $DIST/gui/checksums.txt）" >&2
  exit 4
}
printf '%s\n' "$VER" > "$DIST/gui/latest"   # verify-release --gui / gen-manifests 靠它取版本
echo "已生成 $DIST/gui/checksums.txt（${_lines} 个产物）"

. scripts/lib/sign.sh
if [ -n "${PK_SIGNING_KEY:-}" ]; then
  sign_checksums "$DIST/gui" >/dev/null || { echo "GUI 签名失败" >&2; exit 6; }
  echo "已签名 $DIST/gui/checksums.txt"
elif [ "$DRY" -eq 1 ]; then
  echo "提示：没有 PK_SIGNING_KEY，dry-run 跳过签名（真发版必须有 .sig）" >&2
else
  echo "缺少 PK_SIGNING_KEY：GUI 产物必须带 checksums.txt.sig（唯一发布者的密钥）" >&2
  exit 6
fi

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
for f in "$DIST/gui/packnest-desktop_${VER}"_*.zip "$DIST/gui/packnest-desktop_${VER}"_*.tar.gz \
         "$DIST/gui/checksums.txt" "$DIST/gui/checksums.txt.sig"; do
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
  # 只传**真实存在**的产物：直接写通配符时，某个平台还没构建（例如只在本机打了 mac）
  # 会让整条 gh 命令每次都失败，最后却照样打印"已上传"（0.10.3 就这样丢过 GitHub 附件）。
  files=$(ls "$DIST/gui/packnest-desktop_${VER}"_*.zip "$DIST/gui/packnest-desktop_${VER}"_*.tar.gz \
             "$DIST/gui/checksums.txt" "$DIST/gui/checksums.txt.sig" 2>/dev/null || true)
  [ -n "$files" ] || { echo "没有可上传的 GUI 产物（先构建或 --from-dir）" >&2; exit 4; }
  n=0
  ok=0
  until [ "$ok" = 1 ]; do
    if gh release upload "$TAG" --repo "$GH_REPO" --clobber $files; then ok=1; break; fi
    n=$((n + 1)); [ "$n" -le 5 ] || break
    sleep 8
  done
  [ "$ok" = 1 ] || { echo "GitHub 上传失败（重试 $n 次）：$files" >&2; exit 5; }
  printf '%s\n' "$files" | sed 's|^|  github  |'
  echo "已上传 GitHub Releases（${TAG}）"
fi
# gui_release_list 读 stdin 的 gitee releases JSON，输出 `tag id`（按版本升序）。
# 抽成独立函数是为了能离线测：喂一段 fixture JSON 就能验证"删哪几个"。
gui_release_list() {
  "$PYBIN" -c '
import json, sys
try:
    data = json.load(sys.stdin) or []
except Exception:
    raise SystemExit(0)
rows = [(r.get("tag_name", ""), r.get("id")) for r in data if (r.get("tag_name") or "").startswith("gui-v")]

def key(tag):
    try:
        return tuple(int(x) for x in tag[5:].split("."))
    except Exception:
        return (0,)

for tag, rid in sorted(rows, key=lambda r: key(r[0])):
    print(tag, rid)
'
}

# ── 回收旧 GUI release（配额）──────────────────────────────────────────
# gitee 单仓库附件总量 1GB，且**附件删不掉**（API 不返回附件 id），只有删整条 release 才回收。
# GUI 每版约 25-40MB 且重发频繁 → 默认只保留最近 3 个 gui-v*（当前这个永远保留）。
prune_gui() {
  keep="$1"
  echo "回收旧 GUI release（保留最近 ${keep} 个）"
  list=$(curl -s --max-time 30 "$api/releases?access_token=$(printf '%s' "$TOKEN" | sed 's/ /%20/g')&per_page=100&page=1" | gui_release_list)
  case "$list" in ERR*) echo "  跳过回收（列 release 失败：${list}）" >&2; return 0 ;; esac
  total=$(printf '%s\n' "$list" | sed '/^$/d' | wc -l | tr -d ' ')
  echo "  线上 gui-v* release：${total} 个"
  [ "$total" -gt "$keep" ] || { echo "  不需要回收"; return 0; }
  # 只删最旧的 (total-keep) 个，且绝不碰当前 TAG
  i=0
  printf '%s\n' "$list" | sed '/^$/d' | while read -r tag rid; do
    i=$((i + 1))
    [ "$i" -le $((total - keep)) ] || break
    [ "$tag" = "$TAG" ] && continue
    if [ "$DRY" -eq 1 ]; then
      printf '  dry-run：会删 %s（id=%s）\n' "$tag" "$rid"
      continue
    fi
    code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$api/releases/$rid?access_token=$TOKEN")
    printf '  已删 %s（id=%s）→ %s\n' "$tag" "$rid" "$code"
  done
}

if [ -n "$PRUNE" ]; then
  prune_gui "$PRUNE"
fi

echo "完成。用户：下载 zip → 解压 → open packnest.app（首次启动 macOS 会问权限）"
