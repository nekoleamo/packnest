# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.3"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.3/packnest_0.7.3_darwin_arm64.tar.gz"
      sha256 "2341e7287699611b6bfd2b70879613bfcfe00f9c5cadd06b6efadea631076d78"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.3/packnest_0.7.3_darwin_amd64.tar.gz"
      sha256 "8b452d294a2be2dc3634dbd1d4d57c2b207d162f57977c3e1db9e82361c89772"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.3/packnest_0.7.3_linux_arm64.tar.gz"
      sha256 "78f79b2a6e5972504198bd94917874c2f5e847ec76f3af47504f5b7d42dd9ad2"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.3/packnest_0.7.3_linux_amd64.tar.gz"
      sha256 "2bdfa266ced4d2dccbbff1029dd7990773497e474e5077b8142b0a79f5da2a96"
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
