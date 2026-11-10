# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.2/packnest_0.7.2_darwin_arm64.tar.gz"
      sha256 "3c49f1daf0e67a9c03044900cfb346dd11858aa8bdfc3ab4bf6a781fff6cc8b5"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.2/packnest_0.7.2_darwin_amd64.tar.gz"
      sha256 "830881f03b9cf83f5434e6304be6578e85a3b9f890dea4a3f414d6f49bb9a8f0"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.2/packnest_0.7.2_linux_arm64.tar.gz"
      sha256 "afc89d76c9bf4f7da7bc3f9f79521713c073f360683c1276ba95c3d91e2b7440"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.2/packnest_0.7.2_linux_amd64.tar.gz"
      sha256 "16356ef6638e63ac2a51756deef6ff49e7f1eb1b7b19183f234493e70a09cb84"
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
