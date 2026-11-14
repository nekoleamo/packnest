# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.6.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.6.0/packnest_1.6.0_darwin_arm64.tar.gz"
      sha256 "33b4221f603a59ab9a479555a6a9b9857597596fd75b6ba3161d365d46cfa3eb"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.6.0/packnest_1.6.0_darwin_amd64.tar.gz"
      sha256 "f4ed560c458d5370427a791b60a0aae6888c7d4c2db1123574a93c9aed66a1ad"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.6.0/packnest_1.6.0_linux_arm64.tar.gz"
      sha256 "ab7c915fc9d8db511cef2cd32a262db5a1dd786463f4d34d3b31a556973f0e40"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.6.0/packnest_1.6.0_linux_amd64.tar.gz"
      sha256 "8d15215b1e5c379e037244aed9130b0184c529dbab448cb08dae41cf74537543"
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
