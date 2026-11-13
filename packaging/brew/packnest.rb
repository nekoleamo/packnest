# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.5.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.0/packnest_1.5.0_darwin_arm64.tar.gz"
      sha256 "242d1e6801d1524883f404821fb64e351c609083aab097f5d063e0f3f4fa417f"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.0/packnest_1.5.0_darwin_amd64.tar.gz"
      sha256 "f35998484a5aef5a0dd06b92908907b3c7cb9d0ef024115424d169744e2af1d5"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.0/packnest_1.5.0_linux_arm64.tar.gz"
      sha256 "df642fe847659a3a6e74703cc767dc0b738ce832f97a64aa6477fbd5100b8f06"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.5.0/packnest_1.5.0_linux_amd64.tar.gz"
      sha256 "7d817ae8018b14a56c3e230005dc436a5cbec8eb49e60de91c3eb1d1bb31861c"
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
