# packnest 的 Homebrew formula（自有 tap：brew install nekoleamo/packnest/packnest）
#
# formula 名是 packnest，装出来的命令是 pk（brew 允许两者不同名，先例 ripgrep→rg）。
# 由 scripts/gen-manifests.sh 生成，别手改。
class Packnest < Formula
  desc "国内网络开箱可用的多语言运行时安装器（node/pnpm/bun/deno/go/python）"
  homepage "https://gitee.com/null_593_5354/packnest"
  version "0.7.6"
  license "MIT"

  on_macos do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.6/packnest_0.7.6_darwin_arm64.tar.gz"
      sha256 "6ec55a922f7c6e6b2466bdf7b8ab0134df5d7c76e417550ccae95be8f7c912bd"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.6/packnest_0.7.6_darwin_amd64.tar.gz"
      sha256 "1a08529c47f32c91742a7b877e828157dbbc59e4b45b04ca45f79787a746aa26"
    end
  end

  on_linux do
    on_arm do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.6/packnest_0.7.6_linux_arm64.tar.gz"
      sha256 "310d4fcbfd39f4c1e231a26f5328cd8a3671b7a4576302c5a7220ad5eecbe515"
    end
    on_intel do
      url "https://gitee.com/null_593_5354/packnest/releases/download/v0.7.6/packnest_0.7.6_linux_amd64.tar.gz"
      sha256 "e1a866f08e45359a9f3b47d0b452e3807fd7571eb97c8f9f6a2cc5ddaba9555c"
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
