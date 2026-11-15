# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.9.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.1/packnest_1.9.1_darwin_arm64.tar.gz"
      sha256 "203dde2499cf44041d7b6f9f7b37c49a13e42caf136472e2998cb806d7111d3c"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.1/packnest_1.9.1_darwin_amd64.tar.gz"
      sha256 "b8d3e97e36fe6e32598d4b884e211ef2634003c051ba94096e72858e1bc3e04e"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.1/packnest_1.9.1_linux_arm64.tar.gz"
      sha256 "d2a299fb00b13a11e488f1092e8032824355459ce86febca85d8b1e1f1543256"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.1/packnest_1.9.1_linux_amd64.tar.gz"
      sha256 "78a2fe3da0db970b2494618148a62c63195dd91c66f44869dab19cbefc5c4099"
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
