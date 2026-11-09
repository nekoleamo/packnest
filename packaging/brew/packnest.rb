# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.6.3"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.3/packnest_0.6.3_darwin_arm64.tar.gz"
      sha256 "0d0e7045e4ec3e66aacaf3e5f553b2094dc025a05e99be6778f8964a3da51392"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.3/packnest_0.6.3_darwin_amd64.tar.gz"
      sha256 "24d251024c908ce6979dc94d2016cc553efe4c7459592976a96670fc69ae5400"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.3/packnest_0.6.3_linux_arm64.tar.gz"
      sha256 "3f6742db86c67ef19763957a2147e9f4d1760efc2d57391a523681136bad6036"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.3/packnest_0.6.3_linux_amd64.tar.gz"
      sha256 "acdb9691fcfe6836cab26f5a95c1ee3a38274378ff1673124e07fd366f52e052"
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
