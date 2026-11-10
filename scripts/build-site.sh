#!/bin/sh
# 把 site/ 渲染成 site/dist/：只做一件事 —— 注入版本号，不引任何前端工具链。
#
#   sh scripts/build-site.sh            # 版本号取自 git tag（回退 dist/latest、dev）
#   PK_VERSION=0.7.0 sh scripts/build-site.sh
#
# 语言：site/index.html（中文，默认）+ site/en/index.html（英文），共用 site/style.css。
# 为什么版本号要注入而不是写死在 HTML 里：发版后忘了改站点，是最常见的静默腐化。
set -eu
cd "$(dirname "$0")/.."

VER="${PK_VERSION:-}"
if [ -z "$VER" ]; then
  VER=$(git describe --tags --always 2>/dev/null | sed -n 's/^v\{0,1\}\([0-9][0-9.]*\).*/\1/p')
fi
if [ -z "$VER" ]; then
  VER=$(cat dist/latest 2>/dev/null | tr -d ' \t\r\n' || true)
fi
[ -n "$VER" ] || VER=dev

OUT=site/dist
rm -rf "$OUT"
mkdir -p "$OUT"

# 逐层复制（保留 en/ 子目录），文本文件统一做版本号替换。
find site -type f ! -path 'site/dist/*' | while read -r f; do
  rel=${f#site/}
  mkdir -p "$OUT/$(dirname "$rel")"
  case "$rel" in
    *.html|*.css|*.svg)
      sed "s/__PK_VERSION__/${VER}/g" "$f" > "$OUT/$rel" ;;
    *)
      cp "$f" "$OUT/$rel" ;;
  esac
done

echo "site/dist 已生成（v${VER}）：$(cd "$OUT" && find . -type f | sed 's|^\./||' | sort | tr '\n' ' ')"
