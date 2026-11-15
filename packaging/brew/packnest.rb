# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.8.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.2/packnest_1.8.2_darwin_arm64.tar.gz"
      sha256 "67b028e38e6d58fd59bd589dd25e46b006a7cbc4bade370bb42ec48620e5ebb7"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.2/packnest_1.8.2_darwin_amd64.tar.gz"
      sha256 "100d3da71ace4c4d7ea864ef5607978fa68fb446e7f968fb4bcb1c2f047c55d9"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.2/packnest_1.8.2_linux_arm64.tar.gz"
      sha256 "a1743a716bad32042c0ca8743241b2e11b0c0c3444acb4e50b1c71d3c21d8d1b"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.2/packnest_1.8.2_linux_amd64.tar.gz"
      sha256 "a3d493ad6aa7cae6994f5d058d20e9d62bcc5b755aec56436da83a8c8fc76cca"
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
