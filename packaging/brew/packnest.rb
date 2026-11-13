# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.5.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.1/packnest_1.5.1_darwin_arm64.tar.gz"
      sha256 "e2a763af507528d306b677fb3120007be652f72a6b0aa8d53a39173e993433f3"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.1/packnest_1.5.1_darwin_amd64.tar.gz"
      sha256 "ce4ca8efde6bd844463beeee490a100b4f42c27a632be870f2b81c7f69e0c9c3"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.1/packnest_1.5.1_linux_arm64.tar.gz"
      sha256 "9acef3ec34efa11446299b7d1c9a826c822d2cb09009c3b873b2a09840f322d3"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.1/packnest_1.5.1_linux_amd64.tar.gz"
      sha256 "48fc8ea79120923fac972eddc2487c0fb6c0059df4f58fa95e65daba252cbb80"
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
