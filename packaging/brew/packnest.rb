# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.10.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.10.0/packnest_0.10.0_darwin_arm64.tar.gz"
      sha256 "f628aaa842ee198ac03781d5a6b310e84ddccbbcf005e7709a806d3b3a5ba533"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.10.0/packnest_0.10.0_darwin_amd64.tar.gz"
      sha256 "2fa8164169d9a52f0446fe179b880705d27170ece3aa570c215455e583693de8"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.10.0/packnest_0.10.0_linux_arm64.tar.gz"
      sha256 "8f5ffb52b46c90e7fd0c991311f75e359562999f4fc54ce9e8862805e5da0d0f"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.10.0/packnest_0.10.0_linux_amd64.tar.gz"
      sha256 "3e125c2c5f92d8fec792c31f7ea8555ac5cc5eb47d22c8ba79c4b2448c10abfb"
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
