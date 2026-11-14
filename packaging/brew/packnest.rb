# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.8.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.0/packnest_1.8.0_darwin_arm64.tar.gz"
      sha256 "65c706b7cd4f4a51911bfd543754d8bfcf0d54a0c87f21f2a7ccb305f46b245b"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.0/packnest_1.8.0_darwin_amd64.tar.gz"
      sha256 "f21aca100ff1143398f1d1ee2d5e10bb8d74e8e0466f6fb09d56ed6057fa3d47"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.0/packnest_1.8.0_linux_arm64.tar.gz"
      sha256 "70f0fe6bc0c7f65ac711e8b036c9ff08d66713ca9bab96c738832523df2cdf56"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.8.0/packnest_1.8.0_linux_amd64.tar.gz"
      sha256 "a5f076b9d9155027f8e461c6dd32a04e2a90329c67bd24e3592e37475da380ef"
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
