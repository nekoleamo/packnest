# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.3.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.1/packnest_1.3.1_darwin_arm64.tar.gz"
      sha256 "4436a1ab72e70d946354f7d9bb02a564ebf92bbf793dd36375772f01a7d765a1"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.1/packnest_1.3.1_darwin_amd64.tar.gz"
      sha256 "7fbfd90d4d261f779737858d884cd9f67b8272ca93ced3efdfebec2c8f4dee40"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.1/packnest_1.3.1_linux_arm64.tar.gz"
      sha256 "30eaa3344408399c37af17d87fd7f02886b69ba432f77f0433012038738c58e5"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.1/packnest_1.3.1_linux_amd64.tar.gz"
      sha256 "8329b950bab50bdbec3dd71ec8a900dd99ba647d79ee92d46ab5a90508aed0bb"
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
