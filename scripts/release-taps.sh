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

# 预检（不联网）：清单里的 sha256 集合必须与 dist/v<ver>/checksums.txt 完全一致。
# 为什么：0.8.0 的 tap 被一次"陈旧清单"覆盖，产物没变、hash 变成另一次构建的，
# 用户 brew install 直接报 reports different checksum（Homebrew 会重下一次再报错）。
SUMS="dist/v${VER}/checksums.txt"
[ -f "$SUMS" ] || { echo "缺 ${SUMS}：先跑 make release && sh scripts/gen-manifests.sh" >&2; exit 1; }
WANT=$(awk '{print tolower($1)}' "$SUMS" | sort)
GOT=$(grep -hoE '[0-9a-f]{64}' packaging/brew/packnest.rb packaging/scoop/packnest.json | sort || true)
if [ "$GOT" != "$WANT" ]; then
  echo "清单与 ${SUMS} 不一致（推上去用户必踩校验失败）：" >&2
  w=$(mktemp); g=$(mktemp)
  printf '%s\n' "$WANT" > "$w"; printf '%s\n' "$GOT" > "$g"
  comm -13 "$w" "$g" | sed 's/^/  只在清单里: /' >&2
  comm -23 "$w" "$g" | sed 's/^/  只在 checksums: /' >&2
  rm -f "$w" "$g"
  echo "先跑 sh scripts/gen-manifests.sh 重新生成" >&2
  exit 1
fi
echo "预检 OK：清单 6 个 hash 与 checksums.txt 一致"

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

verify_pushed() { # 推完回读：远端 hash 必须与本地清单逐条一致（防"推了旧文件"）
  i=0
  while [ $i -lt 4 ]; do
    if [ "$(remote_hashes 2>/dev/null | sort)" = "$WANT" ]; then
      echo "远端核对 OK：tap + bucket 的 hash 与本地清单一致"
      return 0
    fi
    i=$((i + 1)); sleep 5
  done
  echo "⚠ 远端清单与本地不一致（推送没生效，或推的是旧清单）" >&2
  printf '%s\n' "$WANT" | sed 's|^|  本地 |' >&2
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
  echo ""
  echo "完成。用户可以："
  echo "  brew install ${OWNER}/packnest/packnest"
  echo "  scoop bucket add ${OWNER} https://github.com/${OWNER}/scoop-bucket && scoop install packnest"
fi
rm -rf "$tap_dir" "$bucket_dir"
