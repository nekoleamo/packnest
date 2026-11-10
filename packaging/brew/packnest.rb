# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.6.6"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.6/packnest_0.6.6_darwin_arm64.tar.gz"
      sha256 "8f4b9201904ff8e91f60e193ebfb494c6d560c62ec0461f11b6dfe4f8be4642e"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.6/packnest_0.6.6_darwin_amd64.tar.gz"
      sha256 "7fb6f7ac7806c7f1f81704a862b11a78c8dd1ada3ff1c2498519ccf1dc4558fd"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.6/packnest_0.6.6_linux_arm64.tar.gz"
      sha256 "45a09df32653f127c943ccd7eb9b179e52233f2e1015d04059670ebc9e724fd0"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.6/packnest_0.6.6_linux_amd64.tar.gz"
      sha256 "7725daa2aadf4eaa2ed3ffb2c7d42c21cd1d51ecd8c46daffeb4464f166136a1"
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
