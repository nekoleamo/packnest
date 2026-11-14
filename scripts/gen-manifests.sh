#!/bin/sh
# 从 dist/v<版本>/checksums.txt 生成 brew / scoop 两份渠道清单（不含任何机密）。
# 有 GUI 产物时（dist/gui/checksums.txt）再生成 GUI 的 scoop 清单（packnest-desktop.json）。
set -eu
DIST="${DIST:-dist}"
VER=$(cat "$DIST/latest")
DIR="$DIST/v$VER"
SUMS="$DIR/checksums.txt"
[ -f "$SUMS" ] || { echo "先跑 make release" >&2; exit 1; }
sha() { awk -v f="$1" '$2==f {print $1}' "$SUMS"; }
REPO="null_593_5354/packnest"
BASE="https://gitee.com/$REPO/releases/download/v$VER"

mkdir -p packaging/brew packaging/scoop
cat > packaging/brew/packnest.rb <<RB
# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/$REPO"
  version "$VER"
  license "MIT"

  on_macos do
    on_arm do
      url "$BASE/packnest_${VER}_darwin_arm64.tar.gz"
      sha256 "$(sha "packnest_${VER}_darwin_arm64.tar.gz")"
    end
    on_intel do
      url "$BASE/packnest_${VER}_darwin_amd64.tar.gz"
      sha256 "$(sha "packnest_${VER}_darwin_amd64.tar.gz")"
    end
  end

  on_linux do
    on_arm do
      url "$BASE/packnest_${VER}_linux_arm64.tar.gz"
      sha256 "$(sha "packnest_${VER}_linux_arm64.tar.gz")"
    end
    on_intel do
      url "$BASE/packnest_${VER}_linux_amd64.tar.gz"
      sha256 "$(sha "packnest_${VER}_linux_amd64.tar.gz")"
    end
  end

  def install
    bin.install "packnest"
    bin.install_symlink "packnest" => "pk"
  end

  test do
    assert_match "packnest v", shell_output("#{bin}/pk --version")
  end
end
RB

cat > packaging/scoop/packnest.json <<JSON
{
  "version": "$VER",
  "description": "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）",
  "homepage": "https://gitee.com/$REPO",
  "license": "MIT",
  "architecture": {
    "64bit": {
      "url": "$BASE/packnest_${VER}_windows_amd64.zip",
      "hash": "$(sha "packnest_${VER}_windows_amd64.zip")"
    },
    "arm64": {
      "url": "$BASE/packnest_${VER}_windows_arm64.zip",
      "hash": "$(sha "packnest_${VER}_windows_arm64.zip")"
    }
  },
  "bin": [["packnest.exe", "pk"], "packnest.exe"],
  "checkver": { "url": "https://gitee.com/$REPO/releases", "regex": "v([\\\\d.]+)" },
  "autoupdate": {
    "architecture": {
      "64bit": { "url": "https://gitee.com/$REPO/releases/download/v\$version/packnest_\$version_windows_amd64.zip" },
      "arm64": { "url": "https://gitee.com/$REPO/releases/download/v\$version/packnest_\$version_windows_arm64.zip" }
    }
  }
}
JSON

# ── GUI（packnest-desktop）的 Scoop 清单 ────────────────────────────────
# 版本从产物文件名里取（GUI 与 CLI 版本解耦，不能拿 dist/latest）；没构建过 GUI 就跳过。
GUI_SUMS="$DIST/gui/checksums.txt"
if [ -f "$GUI_SUMS" ]; then
  gui_ver=$(awk '{print $2}' "$GUI_SUMS" | sed -n 's/^packnest-desktop_\([0-9][0-9.]*\)_windows_amd64\.zip$/\1/p' | head -1)
  if [ -n "$gui_ver" ]; then
    gui_sha() { awk -v f="$1" '$2==f {print $1}' "$GUI_SUMS"; }
    GUI_BASE="https://gitee.com/$REPO/releases/download/gui-v$gui_ver"
    win64="packnest-desktop_${gui_ver}_windows_amd64.zip"
    winarm="packnest-desktop_${gui_ver}_windows_arm64.zip"
    cat > packaging/scoop/packnest-desktop.json <<JSON
{
  "version": "$gui_ver",
  "description": "packnest 桌面端（GUI；只做 pk 的可视化补充，必须先有 CLI）",
  "homepage": "https://gitee.com/$REPO",
  "license": "MIT",
  "depends": "packnest",
  "architecture": {
    "64bit": {
      "url": "$GUI_BASE/$win64",
      "hash": "$(gui_sha "$win64")"
    },
    "arm64": {
      "url": "$GUI_BASE/$winarm",
      "hash": "$(gui_sha "$winarm")"
    }
  },
  "shortcuts": [["pk-desktop.exe", "packnest"]],
  "checkver": { "url": "https://gitee.com/$REPO/releases", "regex": "gui-v([\\d.]+)" },
  "autoupdate": {
    "architecture": {
      "64bit": { "url": "https://gitee.com/$REPO/releases/download/gui-v\$version/packnest-desktop_\$version_windows_amd64.zip" },
      "arm64": { "url": "https://gitee.com/$REPO/releases/download/gui-v\$version/packnest-desktop_\$version_windows_arm64.zip" }
    }
  }
}
JSON
    echo "已生成：packaging/scoop/packnest-desktop.json（GUI 版本 ${gui_ver}）"
  else
    echo "跳过 GUI 清单：${GUI_SUMS} 里没有 windows 产物" >&2
  fi
else
  echo "提示：没有 ${GUI_SUMS}，只生成 CLI 清单（GUI 清单要 sh scripts/release-gui.sh 先生成并签名）" >&2
fi

echo "已生成：packaging/brew/packnest.rb · packaging/scoop/packnest.json（版本 ${VER}）"
