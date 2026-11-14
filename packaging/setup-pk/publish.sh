#!/bin/sh
# 把 packaging/setup-pk 发布成一个独立仓库（幂等；重复跑只更新文件）。
#
#   sh packaging/setup-pk/publish.sh --dry-run          # 只打印要做什么
#   sh packaging/setup-pk/publish.sh                    # 用到 v1.6.0 + 它的安装脚本 sha256
#   sh packaging/setup-pk/publish.sh --version 1.7.0 --sha256 <值>
#
# 做四件事：
#   1. 取/建 public 仓库（默认 nekoleamo/setup-pk）
#   2. 只推四件：action.yml、README.md、.github/workflows/self-test.yml、examples/ci.yml
#   3. 设两个仓库变量（self-test 用）：PK_VERSION、PK_INSTALLER_SHA256
#   4. 打 `v1` 标签（外部仓库才能 `uses: owner/setup-pk@v1`）
#
# 前置：gh 已登录（`gh auth login`，需要 repo + workflow 权限）。
set -eu
cd "$(dirname "$0")/../.."
SRC="packaging/setup-pk"

OWNER="${PK_GH_OWNER:-nekoleamo}"
REPO="${PK_SETUP_REPO:-setup-pk}"
GITEE_REPO="${PK_GITEE_REPO:-null_593_5354/packnest}"
VERSION=""
SHA256=""
DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --owner) OWNER="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --sha256) SHA256="$2"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

command -v gh >/dev/null 2>&1 || { echo "需要 gh（brew install gh）" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "需要 git" >&2; exit 1; }

# 默认版本：最近一个 v* tag（GUI 的 gui-v* 不算）
[ -n "$VERSION" ] || VERSION=$(git tag --list 'v[0-9]*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)
[ -n "$VERSION" ] || { echo "拿不到 CLI 版本（先打 tag，或用 --version 指定）" >&2; exit 2; }

# sha256 默认现算：pin 的是**那个 tag 上的** install.sh，而不是工作树里的
if [ -z "$SHA256" ]; then
  url="https://gitee.com/${GITEE_REPO}/raw/v${VERSION}/scripts/install.sh"
  echo "取 ${url} 的 sha256 …"
  tmp=$(mktemp)
  if ! curl -fsSL --max-time 60 -o "$tmp" "$url"; then
    echo "下载失败：tag v${VERSION} 上可能没有 scripts/install.sh" >&2
    rm -f "$tmp"
    exit 3
  fi
  if command -v shasum >/dev/null 2>&1; then SHA256=$(shasum -a 256 "$tmp" | cut -d' ' -f1)
  else SHA256=$(sha256sum "$tmp" | cut -d' ' -f1); fi
  rm -f "$tmp"
fi
echo "版本 v${VERSION} · install.sh sha256 ${SHA256}"

[ -f "$SRC/action.yml" ] || { echo "找不到 $SRC/action.yml" >&2; exit 1; }
for f in README.md examples/ci.yml examples/self-test.yml; do
  [ -f "$SRC/$f" ] || { echo "找不到 $SRC/$f" >&2; exit 1; }
done

if [ "$DRY" -eq 1 ]; then
  echo "dry-run：将要"
  echo "  1) 建/复用 ${OWNER}/${REPO}（public）"
  echo "  2) 推 action.yml · README.md · .github/workflows/self-test.yml · examples/ci.yml"
  echo "  3) 设变量 PK_VERSION=${VERSION} · PK_INSTALLER_SHA256=${SHA256}"
  echo "  4) 打 tag v1"
  exit 0
fi

# 1) 取/建仓库
if gh repo view "${OWNER}/${REPO}" >/dev/null 2>&1; then
  echo "仓库 ${OWNER}/${REPO} 已存在，复用"
else
  echo "创建 ${OWNER}/${REPO}（public）"
  gh repo create "${OWNER}/${REPO}" --public \
    --description "Install packnest (pk) and the toolchain declared in pk.toml" >/dev/null
fi

# 2) 只推那四件（用临时工作树，避免把 packnest 整棵树带过去）
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp "$SRC/action.yml" "$work/action.yml"
cp "$SRC/README.md" "$work/README.md"
mkdir -p "$work/.github/workflows" "$work/examples"
cp "$SRC/examples/self-test.yml" "$work/.github/workflows/self-test.yml"
cp "$SRC/examples/ci.yml" "$work/examples/ci.yml"
cat > "$work/.gitignore" <<'EOF'
# examples/ci.yml 是给人抄的示例；别让 Actions 把它当工作流跑
EOF

(
  cd "$work"
  git init -q
  git add -A
  git -c user.name="packnest release" -c user.email="release@packnest.invalid" \
    commit -q -m "setup-packnest action v1（pin v${VERSION}）"
  git branch -M main
  git remote add origin "https://github.com/${OWNER}/${REPO}.git"
  git push -q -u origin main --force
)
echo "已推送代码"

# 3) 仓库变量（self-test 用）
gh variable set PK_VERSION --repo "${OWNER}/${REPO}" --body "$VERSION"
gh variable set PK_INSTALLER_SHA256 --repo "${OWNER}/${REPO}" --body "$SHA256"
echo "已设置变量 PK_VERSION / PK_INSTALLER_SHA256"

# 4) 主版本标签（外部仓库按 @v1 引用，改 action 不用改所有调用方）
(
  cd "$work"
  git tag -a v1 -m "setup-packnest v1" -f
  git push -q -f origin v1
)
echo "已打标签 v1"

echo ""
echo "完成。用法："
echo "  - uses: ${OWNER}/${REPO}@v1"
echo "    with:"
echo "      version: ${VERSION}"
echo "      installer-sha256: ${SHA256}"
echo ""
echo "验收（DoD）：${OWNER}/${REPO} 的 self-test 工作流 cold/warm 两个 job 跑绿"
gh run list --repo "${OWNER}/${REPO}" --limit 3 2>/dev/null || true
