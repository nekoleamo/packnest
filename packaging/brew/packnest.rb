# packnest 的 Homebrew formula（自有 tap：brew install <tap>/packnest）
#
# 关键点（方案 §13.1）：formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，
# 先例：ripgrep→rg、go-task→task）。`brew install pk` 装的是别人的工具，文档别写错。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/npm/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/packnest/packnest"
  version "0.6.0"

  on_macos do
    on_arm do
      url "https://gitee.com/packnest/packnest/releases/download/v#{version}/packnest_#{version}_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_RELEASE_SHA256"
    end
    on_intel do
      url "https://gitee.com/packnest/packnest/releases/download/v#{version}/packnest_#{version}_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_RELEASE_SHA256"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/packnest/packnest/releases/download/v#{version}/packnest_#{version}_linux_arm64.tar.gz"
      sha256 "REPLACE_WITH_RELEASE_SHA256"
    end
    on_intel do
      url "https://gitee.com/packnest/packnest/releases/download/v#{version}/packnest_#{version}_linux_amd64.tar.gz"
      sha256 "REPLACE_WITH_RELEASE_SHA256"
    end
  end

  def install
    bin.install "packnest"
    # 同一二进制的第二个入口（方案 §13.1 的双名要求）
    bin.install_symlink "packnest" => "pk"
  end

  test do
    assert_match "packnest v", shell_output("#{bin}/pk --version")
  end
end
