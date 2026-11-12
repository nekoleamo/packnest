# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "1.3.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.0/packnest_1.3.0_darwin_arm64.tar.gz"
      sha256 "f78e44ad6fef4b06330e1bbbac713234f901f35d912d741b5ad2f18eebe09aea"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.0/packnest_1.3.0_darwin_amd64.tar.gz"
      sha256 "bcd29ab5f799b24eb8a50831023ba845b79df6af2506a7a177325a5ac345a0bf"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.0/packnest_1.3.0_linux_arm64.tar.gz"
      sha256 "9a8965281dc8b06f2680efc631f49495abc9aacff7a1f50f79130af74a20a7a5"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v1.3.0/packnest_1.3.0_linux_amd64.tar.gz"
      sha256 "7c80f83e60726b3ce31057852031612053103fca491c3acf45a37bf5e3423124"
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
