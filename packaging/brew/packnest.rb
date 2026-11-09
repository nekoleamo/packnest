# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.6.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.2/packnest_0.6.2_darwin_arm64.tar.gz"
      sha256 "1b5425d162bdf861e749430ecdaf1c0aac50f01425b7a65268ea0d9cae2fbffb"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.2/packnest_0.6.2_darwin_amd64.tar.gz"
      sha256 "e25523ad2a34b57810ba31fc942742348607f6ad67ced2d73a670d46a3e52732"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.2/packnest_0.6.2_linux_arm64.tar.gz"
      sha256 "ede4b6c70dd76d1bf0442b5f814226b0157c97e0421e3e81ef789d4403ce75a4"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.2/packnest_0.6.2_linux_amd64.tar.gz"
      sha256 "c219fd30cb8a8d9bf9fe5232745bf1d6f910c73bbd595ce1c416a71d131984bc"
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
