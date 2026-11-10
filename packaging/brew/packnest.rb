# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.8"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.8/packnest_0.7.8_darwin_arm64.tar.gz"
      sha256 "5d689d98e558c5e0241c5bc8627e7ad73b6a7cd6361191f6867b501f4c69ebd4"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.8/packnest_0.7.8_darwin_amd64.tar.gz"
      sha256 "13bd5c3f9340d5543b312785c9762673f670e22c39022d0f852f55113f03be61"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.8/packnest_0.7.8_linux_arm64.tar.gz"
      sha256 "3c062c3027ff2c7340031260337b7f64efa4738d0474cd373231e44cdf25a754"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.8/packnest_0.7.8_linux_amd64.tar.gz"
      sha256 "758cf68400c9f4f7e1b91fff5085f13d84e25a77eef58caabeffa1f22b59742f"
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
