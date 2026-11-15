# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.9.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.0/packnest_1.9.0_darwin_arm64.tar.gz"
      sha256 "e906e4c2fe91c29c72c50728f9fb9c7678ec786798b8ae4b7834a7ec7ce34b0a"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.0/packnest_1.9.0_darwin_amd64.tar.gz"
      sha256 "92a3434dbc0a1183531afd399cf03c4bc6bbd718eae8902896f9e00e4d38145d"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.0/packnest_1.9.0_linux_arm64.tar.gz"
      sha256 "45e31828d4e2e0cb5fd24ad009ebb717e8a7c9a1a682dcb70a66546f744d1bcf"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.9.0/packnest_1.9.0_linux_amd64.tar.gz"
      sha256 "1b7277c818fe385dc9e4a872e81283e30c76b5da4ec7c86ee2fe1e21652f4bac"
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
