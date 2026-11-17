# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.9.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.2/packnest_1.9.2_darwin_arm64.tar.gz"
      sha256 "b9d1feeaacd0daab1f7459dcfbbf1af92d71949dece728740e930895dd5307cf"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.2/packnest_1.9.2_darwin_amd64.tar.gz"
      sha256 "c64ea8ab282a4967f1f14ec77f9db4cb4cb277d32763c3bebf6f3a1d4bdb28bf"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.2/packnest_1.9.2_linux_arm64.tar.gz"
      sha256 "0b46133a25dabbfebd9f1ede33a7bab1091a8025d74315826f2d220b819598a7"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.2/packnest_1.9.2_linux_amd64.tar.gz"
      sha256 "e1ec315d507f71f3a04eba4ed785e664e5d5c0bd5a3e86253260a6bf3fa5d486"
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
