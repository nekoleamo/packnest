# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.1/packnest_0.7.1_darwin_arm64.tar.gz"
      sha256 "5dff3eca858842fd1ff9b10164c6f7e3aecb9697ccddc51850b366bec1e43da3"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.1/packnest_0.7.1_darwin_amd64.tar.gz"
      sha256 "ea7eebe9136af75afc23c3e8dc7b324d96df4f1ca42183771c4a0418b74e6148"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.1/packnest_0.7.1_linux_arm64.tar.gz"
      sha256 "53dbfae2b1d82ef9b2502d65837fc3bb3e80d15354728e3566884f3d8a5d6ac6"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.1/packnest_0.7.1_linux_amd64.tar.gz"
      sha256 "b29137d02dc991a5b7a3a8234cd2563e8b86050fa3a1781ac96b9bcb8a9102a6"
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
