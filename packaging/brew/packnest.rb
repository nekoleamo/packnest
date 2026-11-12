# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.1.0/packnest_1.1.0_darwin_arm64.tar.gz"
      sha256 "067616bc27c74985198bba2c1c1543af31cac83cf4054c40657dfdd8916d7a8f"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.1.0/packnest_1.1.0_darwin_amd64.tar.gz"
      sha256 "72d65cba4bc36c33d4e54353dd66f9ef9d6784f5dc9e8de58954ce08f495729e"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.1.0/packnest_1.1.0_linux_arm64.tar.gz"
      sha256 "e0d53d3d0de0f451566f45405a113515cc072e047383f5035f1e6f758010cfc4"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.1.0/packnest_1.1.0_linux_amd64.tar.gz"
      sha256 "d722e0cfa2c83116f0c7b81b3f0a5e9dd3d524d1bcdcb3574a98a903f1589365"
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
