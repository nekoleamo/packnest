# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.0.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.1/packnest_1.0.1_darwin_arm64.tar.gz"
      sha256 "c7ac7c4aa9d7221af37a68db4b59ff486effaf3b814fe63eede8daec3806653e"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.1/packnest_1.0.1_darwin_amd64.tar.gz"
      sha256 "141c956bd29eb7300cb0621504d663883bf11293a1aaad0d67194809a8f115e4"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.1/packnest_1.0.1_linux_arm64.tar.gz"
      sha256 "4fbcf11171b7c8007d6aa6208c65cbf604f1d3e9865a2d0e1c5bf09524b213ce"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.1/packnest_1.0.1_linux_amd64.tar.gz"
      sha256 "e48268d86f1c3adc6d1d1c97c74fd685e7eba9e46f87a520a11713fa9e706fa2"
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
