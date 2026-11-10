# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.5"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.5/packnest_0.7.5_darwin_arm64.tar.gz"
      sha256 "1f032f22992d61d3d188644068cc328d66badc7419d280dcf742371ad19ee3bb"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.5/packnest_0.7.5_darwin_amd64.tar.gz"
      sha256 "abd838fd84b939ec41f5405c95a18acfb8b879e91e94ebbafd51295006ea4f64"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.5/packnest_0.7.5_linux_arm64.tar.gz"
      sha256 "6e6b1167b180d3c8b307b710f58f227c1dacb3d90a878abdc2c1e4302781872d"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.5/packnest_0.7.5_linux_amd64.tar.gz"
      sha256 "b765135757d71f4f721ecf04fa78e6b318b7f5239a9c1159d9b8b256e2dc5c95"
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
