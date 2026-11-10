# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.4"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.4/packnest_0.7.4_darwin_arm64.tar.gz"
      sha256 "f7d3119851ce81725bde312aebe71f6d7687dfbed02d236a511acc1a74cf885f"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.4/packnest_0.7.4_darwin_amd64.tar.gz"
      sha256 "555ae9a04211d2770b520611225b1d16125c07168caef0ed4a362ca109d5249a"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.4/packnest_0.7.4_linux_arm64.tar.gz"
      sha256 "9c644f417bb0f86f689db1806b952ddace48249f3b6a0002012e21054cc00a83"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.4/packnest_0.7.4_linux_amd64.tar.gz"
      sha256 "3c60eb0896249cd1417046567091eccb53d24c5725eeff0fa622fc00284f7905"
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
