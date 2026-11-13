# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.4.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.4.0/packnest_1.4.0_darwin_arm64.tar.gz"
      sha256 "21e322560424ad34db78b66172fdd2b8f6a9c609d9ef9ebef94a255100ab8829"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.4.0/packnest_1.4.0_darwin_amd64.tar.gz"
      sha256 "919c70cea9864c534f09e27acba55846a4c14ec2b17ecbbaa2b142e3a59a531f"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.4.0/packnest_1.4.0_linux_arm64.tar.gz"
      sha256 "56e7058a53adb8f19545a02d7cbbb417bd29d0c2ffd4b930639870f495d15166"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.4.0/packnest_1.4.0_linux_amd64.tar.gz"
      sha256 "5aa6fc81927b6390b591796900147f9cbb527783203d775c12671036ec59f6ba"
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
