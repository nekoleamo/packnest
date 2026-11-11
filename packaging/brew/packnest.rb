# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.9.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.9.0/packnest_0.9.0_darwin_arm64.tar.gz"
      sha256 "5b8bdf5a2247cd82ebe1f9d60ea63838d48958f6b488e79c56dbb8d252d720f8"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.9.0/packnest_0.9.0_darwin_amd64.tar.gz"
      sha256 "29c1ffe562d9975f708781bb8da3a6377dd62be8e36da3a50feecfea9784d80d"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.9.0/packnest_0.9.0_linux_arm64.tar.gz"
      sha256 "e3610fd437a92c42baab5f0465fa09cc2789dccd1ae845663166954085d3cef5"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.9.0/packnest_0.9.0_linux_amd64.tar.gz"
      sha256 "1bf307debdaa54f75e988c55a341450c2aa41a3ca959363f989f9a0bd6e4e8d8"
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
