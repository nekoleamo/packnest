# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.8.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.8.0/packnest_0.8.0_darwin_arm64.tar.gz"
      sha256 "895f9aa9afed3b3f2feac555a5cce2dd472041e8469b1981569f2582da60faed"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.8.0/packnest_0.8.0_darwin_amd64.tar.gz"
      sha256 "abc788d806c0225c0b96491742753aa37119794bfd21aa99312ea2b49c2f9147"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.8.0/packnest_0.8.0_linux_arm64.tar.gz"
      sha256 "e93b5951b7015e38b51b7ff1169d81cdb67a3dfc32f938f1530e57e5529cc3c2"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.8.0/packnest_0.8.0_linux_amd64.tar.gz"
      sha256 "e8341aebd7374e15f356375d0f310d4ac04ada11d363f0130a1c00c7a37a0b8b"
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
