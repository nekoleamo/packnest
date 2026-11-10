# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.7"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.7/packnest_0.7.7_darwin_arm64.tar.gz"
      sha256 "5623ad4fd6025572814a542866198ff4578c7b9c688ff4b0f7083b3d3df87480"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.7/packnest_0.7.7_darwin_amd64.tar.gz"
      sha256 "1f9ae0d5ab764a287860c613cbc985eb4f797f1aac8a16048657162e3d793602"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.7/packnest_0.7.7_linux_arm64.tar.gz"
      sha256 "718f5915c8e7e167d7e1ec11ea5fa45f4a6c770e13d627e15a1db20f94dac2d6"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.7/packnest_0.7.7_linux_amd64.tar.gz"
      sha256 "4c77773cfc501792ac3fff8ba1da9dec98abda840b109695b915f42b7ee0be77"
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
