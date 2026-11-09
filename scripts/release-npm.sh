#!/bin/sh
# 把 dist/ 里的 6 平台产物打成 npm 包（免费分发路径，不需要自建 OSS）。
#
#   sh scripts/release-npm.sh [--publish] [--registry URL]
#
# 产物：dist/npm/
#   @packnest/<platform>-<arch>/   ← 每个平台一个包，内含 bin/packnest[.exe]
#   packnest/                      ← 主包（bin/pk.js 转发 + optionalDependencies）
#
# 免费链条（方案 §13.2 的 P0，不花一分钱）：
#   发布到 npm（免费） → npmmirror 自动镜像（免费）→ 国内 npm i -g packnest 秒装。
#   不需要对象存储、不需要 CDN 备案、不需要 GitHub。
set -eu

DIST="${DIST:-dist}"
OUT="$DIST/npm"
PUBLISH=0
REGISTRY="${NPM_REGISTRY:-https://registry.npmjs.org}"

while [ $# -gt 0 ]; do
  case "$1" in
    --publish) PUBLISH=1; shift ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

[ -d "$DIST" ] || { echo "先跑 make release（没有 $DIST/）" >&2; exit 1; }
VERSION=$(cat "$DIST/latest")
VER_DIR="$DIST/v$VERSION"
[ -d "$VER_DIR" ] || { echo "找不到 $VER_DIR" >&2; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT"

# 平台包：包名 @packnest/<os>-<arch>，解包后是 package/bin/packnest
for f in "$VER_DIR"/packnest_"$VERSION"_*; do
  base=$(basename "$f")
  case "$base" in
    *.tar.gz)
      rest=${base#packnest_"$VERSION"_}; rest=${rest%.tar.gz}
      exe="packnest"; archive="tar" ;;
    *.zip)
      rest=${base#packnest_"$VERSION"_}; rest=${rest%.zip}
      exe="packnest.exe"; archive="zip" ;;
    *) continue ;;
  esac
  os=${rest%_*}; arch=${rest#*_}
  pkg="$OUT/@packnest/$os-$arch"
  mkdir -p "$pkg/bin"
  tmp="$OUT/.tmp-$os-$arch"; mkdir -p "$tmp"
  abs=$(cd "$(dirname "$f")" && pwd)/$(basename "$f")
  if [ "$archive" = "tar" ]; then
    tar -xzf "$abs" -C "$tmp"
  else
    (cd "$tmp" && unzip -q "$abs")
  fi
  mv "$tmp/packnest" "$pkg/bin/packnest" 2>/dev/null || mv "$tmp/packnest.exe" "$pkg/bin/$exe"
  chmod +x "$pkg/bin/$exe"
  cat > "$pkg/package.json" <<JSON
{
  "name": "@packnest/$os-$arch",
  "version": "$VERSION",
  "description": "packnest 的 $os-$arch 二进制（由主包 packnest 依赖）",
  "os": ["$os"], "cpu": ["$arch"],
  "files": ["bin/$exe"],
  "license": "MIT"
}
JSON
  rm -rf "$tmp"
done

# 主包：JS 入口 + optionalDependencies 指向平台包
mkdir -p "$OUT/packnest"
cp -R packaging/npm/bin packaging/npm/scripts "$OUT/packnest/"
node - "$OUT/packnest/package.json" "$VERSION" <<'NODE'
const fs = require('fs');
const [, , out, version] = process.argv;
const template = JSON.parse(fs.readFileSync('packaging/npm/package.json', 'utf8'));
template.version = version;
for (const name of Object.keys(template.optionalDependencies || {})) {
  template.optionalDependencies[name] = version;
}
fs.writeFileSync(out, JSON.stringify(template, null, 2) + '\n');
NODE

echo "已生成 npm 包："
find "$OUT" -name package.json -maxdepth 3 | sed 's|^|  |'

if [ "$PUBLISH" -eq 1 ]; then
  ( cd "$OUT" && for p in @packnest/*/ packnest/; do
      ( cd "$p" && npm publish --registry "$REGISTRY" --access public ) || exit 1
    done )
  echo "已发布到 $REGISTRY（npmmirror 会在几分钟内自动镜像）"
fi
