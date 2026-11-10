# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.0/packnest_0.7.0_darwin_arm64.tar.gz"
      sha256 "7d5ecdaea0bd97fd4bbeb73e0f55062ecd3210777a8bfaa401cfdc27ed19de3f"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.0/packnest_0.7.0_darwin_amd64.tar.gz"
      sha256 "395675f655aaa113e59cee34626bc7dcfb6324787c1eadf0e4c97e846efc7ec7"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.0/packnest_0.7.0_linux_arm64.tar.gz"
      sha256 "223ff420a4ad45fa973784aa4d2e99b7ccd1429fe3ceece06266fc8c337eec28"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.0/packnest_0.7.0_linux_amd64.tar.gz"
      sha256 "aa1e8323e6c2873ab20218a196b65fc5ad7705ea67e17060bba9d2f176078571"
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
