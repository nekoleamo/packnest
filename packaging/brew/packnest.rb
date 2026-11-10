# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.6.5"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.5/packnest_0.6.5_darwin_arm64.tar.gz"
      sha256 "a4694fa625afa4eb45c0dd8687f81993f2b2d03cae2dc23b559e85f3c829d0ed"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.5/packnest_0.6.5_darwin_amd64.tar.gz"
      sha256 "98fadeb2206f9302a76a112b6b0e060ec15a53d94a66af898bb857c37cdd17b5"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.5/packnest_0.6.5_linux_arm64.tar.gz"
      sha256 "1cc4f92a7cc9c2493904223e8622355018ff103f5d1a3e62c67c8e3ef83e9656"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.6.5/packnest_0.6.5_linux_amd64.tar.gz"
      sha256 "056bdf5db617738ba015247fc9fc459216d38845ada4ef564679b039972c816d"
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
