# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.2.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.2.0/packnest_1.2.0_darwin_arm64.tar.gz"
      sha256 "5d2c7572595442b442a2de55804e17fc259b8df2d8e4ff1fbc3ea63a69196faa"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.2.0/packnest_1.2.0_darwin_amd64.tar.gz"
      sha256 "06748a7d5e0f3caff582972fdd62e5b36b3b3160caeb1390733e0cc4f47ec309"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.2.0/packnest_1.2.0_linux_arm64.tar.gz"
      sha256 "94dae3ae65ef331cbd010eb4b720ac1fd318d07137a9615d1cfdf12933e10e22"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.2.0/packnest_1.2.0_linux_amd64.tar.gz"
      sha256 "feed9f390f774414dac05f7dd4447b92898cd9cd10345b4d915bed3e67864f93"
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
