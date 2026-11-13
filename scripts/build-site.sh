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

# 版本号优先级：显式 PK_VERSION → git tag → 仓库里的 scripts/latest → dist/latest → dev。
# scripts/latest 是发版时由 `make release` 写、并且随库提交的，**没有 tag 的 clone 也能拿到正确版本**
# （2026-11-12：CI 的 clone 没 tag，站点一度渲染成 vdev）。
VER="${PK_VERSION:-}"
if [ -z "$VER" ]; then
  # 只认 vX.Y.Z 形式的 tag：不用 --always（它会退化成短 SHA，`635c593` 这种还会被下面的数字匹配误当成版本号），
  # --match 同时把 gui-v* 这类别的 tag 排除在外。
  VER=$(git describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null | sed 's/^v//')
fi
if [ -z "$VER" ]; then
  VER=$(tr -d ' \t\r\n' < scripts/latest 2>/dev/null || true)
fi
if [ -z "$VER" ]; then
  VER=$(tr -d ' \t\r\n' < dist/latest 2>/dev/null || true)
fi
[ -n "$VER" ] || VER=dev

# GUI 版本号单独注入（取自 cmd/pk-desktop/main.go 的 guiVersion）：GUI 有独立版本、独立 tag。
GUI_VER=$(grep -oE 'guiVersion = "[0-9.]+"' cmd/pk-desktop/main.go 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true)
[ -n "$GUI_VER" ] || GUI_VER=dev

OUT=site/dist
rm -rf "$OUT"
mkdir -p "$OUT"

# 逐层复制（保留 en/ 子目录），文本文件统一做版本号替换。
find site -type f ! -path 'site/dist/*' | while read -r f; do
  rel=${f#site/}
  mkdir -p "$OUT/$(dirname "$rel")"
  case "$rel" in
    *.html|*.css|*.svg)
      sed -e "s/__PK_VERSION__/${VER}/g" -e "s/__GUI_VERSION__/${GUI_VER}/g" "$f" > "$OUT/$rel" ;;
    *)
      cp "$f" "$OUT/$rel" ;;
  esac
done

echo "site/dist 已生成（CLI v${VER} · GUI ${GUI_VER}）：$(cd "$OUT" && find . -type f | sed 's|^\./||' | sort | tr '\n' ' ')"
