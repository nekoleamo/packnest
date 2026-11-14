#!/bin/sh
# 建 Homebrew tap 与 Scoop bucket 仓库，并把渠道清单推上去（用 gh，免费）。
#
#   gh auth login          # 一次
#   sh scripts/release-taps.sh [--dry-run] [--owner nekoleamo]
#
# 依赖：gh、git；清单由 scripts/gen-manifests.sh 生成（make release 后跑）。
set -eu

OWNER="${PK_GH_OWNER:-}"
DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --owner) OWNER="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

command -v gh >/dev/null 2>&1 || { echo "需要 gh（brew install gh / scoop install gh）" >&2; exit 1; }
[ -z "$OWNER" ] && OWNER=$(gh api user -q .login)
VER=$(cat dist/latest 2>/dev/null || echo "")
[ -n "$VER" ] || { echo "先跑 make release && sh scripts/gen-manifests.sh" >&2; exit 1; }
[ -f packaging/brew/packnest.rb ] && [ -f packaging/scoop/packnest.json ] || {
  echo "缺清单：先跑 sh scripts/gen-manifests.sh" >&2; exit 1; }

echo "owner=${OWNER} version=${VER}"

# 预检（不联网）：**清单里的每个 hash 都必须在 checksums.txt 里**，且 checksums.txt 里的
# 每个归档都必须在清单里。
#
# 为什么是"子集 + 覆盖归档"而不是"完全相等"：1.7 起 checksums.txt 里还有 provenance.json /
# sbom.cdx.json（它们不进 brew/scoop 清单），1.8 发版时就因为这条判据写成了相等而卡住发布。
# 为什么要双向：只查"清单 ⊆ checksums"会放过"某个平台产品忘了进清单"；只查"归档 ⊆ 清单"
# 会放过"清单里混进上一次构建的 hash"（0.8.0 就这么把 tap 覆盖成了旧 hash）。
SUMS="dist/v${VER}/checksums.txt"
[ -f "$SUMS" ] || { echo "缺 ${SUMS}：先跑 make release && sh scripts/gen-manifests.sh" >&2; exit 1; }
WANT=$(awk '{print tolower($1)}' "$SUMS" | sort)
ARCHIVES=$(awk '/packnest_[0-9]/ {print tolower($1)}' "$SUMS" | sort)
GOT=$(grep -hoE '[0-9a-f]{64}' packaging/brew/packnest.rb packaging/scoop/packnest.json | sort || true)
if [ "$GOT" != "$ARCHIVES" ]; then
  echo "清单与 ${SUMS} 不一致（推上去用户必踩校验失败）：" >&2
  w=$(mktemp); g=$(mktemp); a=$(mktemp); b=$(mktemp)
  printf '%s\n' "$ARCHIVES" > "$a"; printf '%s\n' "$GOT" > "$b"
  printf '%s\n' "$WANT" > "$w"; printf '%s\n' "$GOT" > "$g"
  comm -13 "$w" "$g" | sed 's/^/  只在清单里（checksums 里没有这个 hash）: /' >&2
  comm -23 "$a" "$b" | sed 's/^/  归档没进清单: /' >&2
  comm -13 "$a" "$b" | sed 's/^/  清单里有 checksums 不认识的 hash: /' >&2
  rm -f "$w" "$g" "$a" "$b"
  echo "先跑 sh scripts/gen-manifests.sh 重新生成" >&2
  exit 1
fi
echo "预检 OK：清单覆盖 checksums.txt 里的全部归档（$(printf '%s\n' "$ARCHIVES" | wc -l | tr -d ' ') 个），且没有多余 hash"

# GUI 清单（如果这次也发了桌面端）：单独对自己的 checksums.txt 核对
GUI_JSON="packaging/scoop/packnest-desktop.json"
GUI_SUMS="dist/gui/checksums.txt"
if [ -f "$GUI_JSON" ]; then
  [ -f "$GUI_SUMS" ] || { echo "有 GUI 清单但缺 ${GUI_SUMS}（先跑 release-gui.sh）" >&2; exit 1; }
  # scoop 清单只覆盖 windows 产物 → 判据是"子集"：清单里的每个 hash 都必须出现在 checksums.txt 里
  want_gui=$(awk '{print tolower($1)}' "$GUI_SUMS" | sort)
  got_gui=$(grep -oE '[0-9a-f]{64}' "$GUI_JSON" | sort || true)
  _w=$(mktemp); _g=$(mktemp)
  printf '%s\n' "$want_gui" > "$_w"
  printf '%s\n' "$got_gui" > "$_g"
  bogus=$(comm -13 "$_w" "$_g" || true)
  rm -f "$_w" "$_g"
  if [ -n "$bogus" ]; then
    echo "GUI 清单里有 ${GUI_SUMS} 不认识的 hash（用户 scoop install packnest-desktop 会校验失败）：" >&2
    printf '%s\n' "$bogus" | sed 's/^/  /' >&2
    exit 1
  fi
  if [ -z "$got_gui" ]; then
    echo "GUI 清单里没有 hash（生成脚本坏了？）" >&2
    exit 1
  fi
  echo "预检 OK：GUI 清单的 hash 都能在 ${GUI_SUMS} 里找到"
fi

# 线上核对：清单必须和"用户真正下载到的文件"一致。本地 dist 自洽不够 ——
# 曾经因为 mac 与 ubuntu 各发一次而出现两套 sha256（见 docs/publishing.md「唯一发布者」）。
# 经 make publish-manifests 调用时 verify-release 已经跑过，用 PK_SKIP_LIVE_CHECK=1 跳过重复下载。
if [ "${PK_SKIP_LIVE_CHECK:-}" != "1" ] && [ "$DRY" -eq 0 ]; then
  sh scripts/verify-release.sh >/dev/null || {
    echo "线上产物与本地清单不一致（先 make verify-release 看明细）；拒绝推清单" >&2
    exit 1
  }
  echo "线上核对 OK：已发布产物的 sha256 与本地清单一致"
fi

# 读远端清单用 git 而不是 raw.githubusercontent：raw 有几十秒到几分钟的 CDN 缓存，
# 会把刚推上去的正确清单误报成"旧"（首发时就这样被坑过）。
remote_hashes() { # 读远端两处清单里的 sha256（tap 的 .rb + bucket 的 .json），排序输出
  tmp=$(mktemp -d)
  for r in homebrew-packnest scoop-bucket; do
    git clone -q --depth 1 "https://github.com/${OWNER}/$r.git" "$tmp/$r" 2>/dev/null \
      || { rm -rf "$tmp"; return 1; }
  done
  grep -hoE '[0-9a-f]{64}' "$tmp/homebrew-packnest/Formula/packnest.rb" \
    "$tmp/scoop-bucket/bucket/packnest.json" | sort
  rm -rf "$tmp"
}

verify_pushed_gui() { # GUI 清单单独回读（不与 CLI 的 hash 集合混在一起比较）
  [ -f "$GUI_JSON" ] || return 0
  want=$(grep -oE '[0-9a-f]{64}' "$GUI_JSON" | sort)
  work=$(mktemp -d)
  i=0
  while [ $i -lt 4 ]; do
    rm -rf "$work/b"
    got=""
    if git clone -q --depth 1 "https://github.com/${OWNER}/scoop-bucket.git" "$work/b" 2>/dev/null; then
      got=$(grep -oE '[0-9a-f]{64}' "$work/b/bucket/packnest-desktop.json" 2>/dev/null | sort || true)
    fi
    if [ "$got" = "$want" ]; then
      rm -rf "$work"
      echo "远端核对 OK：bucket 里的 packnest-desktop.json 与本地一致"
      return 0
    fi
    i=$((i + 1))
    sleep 5
  done
  rm -rf "$work"
  echo "⚠ 远端 GUI 清单与本地不一致（推送没生效，或推的是旧清单）" >&2
  return 1
}

verify_pushed() { # 推完回读：远端 hash 必须与本地**归档**清单逐条一致（防"推了旧文件"）
  # 注意比的是 $ARCHIVES 而不是 $WANT：checksums.txt 里还有 provenance/sbom，它们不进渠道清单。
  i=0
  while [ $i -lt 4 ]; do
    if [ "$(remote_hashes 2>/dev/null | sort)" = "$ARCHIVES" ]; then
      echo "远端核对 OK：tap + bucket 的 hash 与本地清单一致"
      return 0
    fi
    i=$((i + 1)); sleep 5
  done
  echo "⚠ 远端清单与本地不一致（推送没生效，或推的是旧清单）" >&2
  printf '%s\n' "$ARCHIVES" | sed 's|^|  本地 |' >&2
  return 1
}

ensure_repo() { # $1=repo名
  if gh repo view "${OWNER}/$1" >/dev/null 2>&1; then
    echo "仓库 ${OWNER}/$1 已存在"
  else
    echo "创建 ${OWNER}/$1"
    [ "$DRY" -eq 1 ] || gh repo create "${OWNER}/$1" --public \
      --description "packnest 的 $2" >/dev/null
  fi
}

push_dir() { # $1=repo名 $2=本地待推目录
  work=$(mktemp -d)
  if gh repo view "${OWNER}/$1" >/dev/null 2>&1; then
    git clone -q "https://github.com/${OWNER}/$1.git" "$work/repo"
  else
    mkdir -p "$work/repo" && ( cd "$work/repo" && git init -q )
  fi
  cp -R "$2"/. "$work/repo"/
  ( cd "$work/repo"
    git add -A
    git -c user.name="packnest release" -c user.email="release@packnest.invalid" \
      commit -q -m "packnest ${VER}" || echo "无改动，跳过提交"
    git branch -M main
    if [ -n "${GH_TOKEN:-}" ]; then
      git remote set-url origin "https://x-access-token:${GH_TOKEN}@github.com/${OWNER}/$1.git"
    else
      git remote get-url origin >/dev/null 2>&1 || \
        git remote add origin "https://github.com/${OWNER}/$1.git"
    fi
    git push -q -u origin main )
  rm -rf "$work"
  echo "已推送 ${OWNER}/$1"
}

# 1) Homebrew tap：仓库名必须是 homebrew-<tap>，formula 放 Formula/
tap_dir=$(mktemp -d); mkdir -p "$tap_dir/Formula"
cp packaging/brew/packnest.rb "$tap_dir/Formula/packnest.rb"
cat > "$tap_dir/README.md" <<MD
# homebrew-packnest

\`\`\`sh
brew install ${OWNER}/packnest/packnest   # 命令名是 pk
\`\`\`
MD

# 2) Scoop bucket：仓库名任意（这里 scoop-bucket），manifest 放 bucket/
bucket_dir=$(mktemp -d); mkdir -p "$bucket_dir/bucket"
cp packaging/scoop/packnest.json "$bucket_dir/bucket/packnest.json"
if [ -f packaging/scoop/packnest-desktop.json ]; then
  cp packaging/scoop/packnest-desktop.json "$bucket_dir/bucket/packnest-desktop.json"
fi
cat > "$bucket_dir/README.md" <<MD
# scoop-bucket

\`\`\`powershell
scoop bucket add ${OWNER} https://github.com/${OWNER}/scoop-bucket
scoop install packnest   # 命令名 pk / packnest
\`\`\`
MD

ensure_repo homebrew-packnest "Homebrew tap"
ensure_repo scoop-bucket "Scoop bucket"
if [ "$DRY" -eq 1 ]; then
  echo "dry-run：将要推送的文件"
  find "$tap_dir" "$bucket_dir" -type f | sed 's|^|  |'
else
  push_dir homebrew-packnest "$tap_dir"
  push_dir scoop-bucket "$bucket_dir"
  verify_pushed
  verify_pushed_gui
  echo ""
  echo "完成。用户可以："
  echo "  brew install ${OWNER}/packnest/packnest"
  echo "  scoop bucket add ${OWNER} https://github.com/${OWNER}/scoop-bucket && scoop install packnest"
fi
rm -rf "$tap_dir" "$bucket_dir"
