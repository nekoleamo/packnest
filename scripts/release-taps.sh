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
    git remote get-url origin >/dev/null 2>&1 || \
      git remote add origin "https://github.com/${OWNER}/$1.git"
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
  echo ""
  echo "完成。用户可以："
  echo "  brew install ${OWNER}/packnest/packnest"
  echo "  scoop bucket add ${OWNER} https://github.com/${OWNER}/scoop-bucket && scoop install packnest"
fi
rm -rf "$tap_dir" "$bucket_dir"
