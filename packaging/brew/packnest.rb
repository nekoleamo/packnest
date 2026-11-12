# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.0.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.0/packnest_1.0.0_darwin_arm64.tar.gz"
      sha256 "b62356959d47c79c6b26ac2a5de2da2c38a47474bfa0790098a7d78e1b7f72a8"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.0/packnest_1.0.0_darwin_amd64.tar.gz"
      sha256 "52a7806edd0f7a8035ccc515a453a694699364926de077c9142dbb54b70b1304"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.0/packnest_1.0.0_linux_arm64.tar.gz"
      sha256 "331a7f708de0ebba45b1974916300b41d4a0f02647b0e52207f8c60224bd4a15"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.0.0/packnest_1.0.0_linux_amd64.tar.gz"
      sha256 "8ae113199c40c6115bcbeb1b9603844080210e6da81e3f71316d223c50b96e9c"
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
