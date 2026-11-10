# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.9"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.9/packnest_0.7.9_darwin_arm64.tar.gz"
      sha256 "6abfc8b845736e50a76d043e353ef9a11bd44663d7657d814e0718b652da36f9"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.9/packnest_0.7.9_darwin_amd64.tar.gz"
      sha256 "702e4c9a36a9964ac42c96cb6194ac1f0e7538e1e16466c73298258e773f662d"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.9/packnest_0.7.9_linux_arm64.tar.gz"
      sha256 "3854554959ba6272e2f5f6711c8c7e6d17d962c44ed9e2dcab63f9b2b04c4221"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.9/packnest_0.7.9_linux_amd64.tar.gz"
      sha256 "b1cab3881cee5bd5e32152fdcf9f64725d6ffecde7a4c527acad5c72d814faa8"
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
