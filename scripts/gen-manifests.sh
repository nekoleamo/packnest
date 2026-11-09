#!/bin/sh
# 从 dist/v<版本>/checksums.txt 生成 brew / scoop / winget 三份渠道清单（不含任何机密）。
set -eu
DIST="${DIST:-dist}"
VER=$(cat "$DIST/latest")
DIR="$DIST/v$VER"
SUMS="$DIR/checksums.txt"
[ -f "$SUMS" ] || { echo "先跑 make release" >&2; exit 1; }
sha() { awk -v f="$1" '$2==f {print $1}' "$SUMS"; }
REPO="null_593_5354/packnest"
BASE="https://gitee.com/$REPO/releases/download/v$VER"

mkdir -p packaging/brew packaging/scoop packaging/winget
cat > packaging/brew/packnest.rb <<RB
# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
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
  "description": "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）",
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
      "64bit": { "url": "$BASE/packnest_\$version_windows_amd64.zip" },
      "arm64": { "url": "$BASE/packnest_\$version_windows_arm64.zip" }
    }
  }
}
JSON

cat > packaging/winget/nekoleamo.packnest.yaml <<YML
# winget manifest 草稿（需配合 wingetcreate 提交到 microsoft/winget-pkgs）
PackageIdentifier: nekoleamo.packnest
PackageVersion: $VER
PackageName: packnest
Publisher: nekoleamo
License: MIT
ShortDescription: 国内网络开箱可用的多语言运行时安装器（pk）
PackageUrl: https://gitee.com/$REPO
Installers:
  - Architecture: x64
    InstallerUrl: $BASE/packnest_${VER}_windows_amd64.zip
    InstallerSha256: $(sha "packnest_${VER}_windows_amd64.zip")
    InstallerType: zip
    NestedInstallerType: portable
    NestedInstallerFiles:
      - RelativeFilePath: packnest.exe
        PortableCommandAlias: pk
ManifestType: singleton
ManifestVersion: 1.6.0
YML

echo "已生成：packaging/brew/packnest.rb · packaging/scoop/packnest.json · packaging/winget/nekoleamo.packnest.yaml（版本 ${VER}）"
