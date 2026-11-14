# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.8.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.1/packnest_1.8.1_darwin_arm64.tar.gz"
      sha256 "686cd907e3759b86050b75009a58e0dfa811de5a33335d3e1fbd8214f68fdc59"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.1/packnest_1.8.1_darwin_amd64.tar.gz"
      sha256 "c5616165c2858ee6aa22c0c8b5ed3a586365e1165c0d608417e6423f743b5077"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.1/packnest_1.8.1_linux_arm64.tar.gz"
      sha256 "62daacce14f4c72dcdbc03f65ff1a5be862af1e0e16deb0e5c03f9e8b8fc7875"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.1/packnest_1.8.1_linux_amd64.tar.gz"
      sha256 "bda61709af54a9479a86d06f9dd25564e8ba2ba66fed6e9975a28db867ea05b8"
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
