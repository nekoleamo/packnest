//go:build sigtest

package verify

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSSHSIGInterop 双向验证格式没猜错（需要真 ssh-keygen，故不进 make check）：
//
//   - Go 侧验 ssh-keygen 产出的签名
//
//   - ssh-keygen 验 Go 侧构造的签名
//
//     go test -tags sigtest -run TestSSHSIGInterop ./internal/verify/
func TestSSHSIGInterop(t *testing.T) {
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("没有 ssh-keygen，跳过互操作测试")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	if out, err := exec.Command(sshKeygen, "-t", "ed25519", "-N", "", "-C", "pk-release-test", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("生成密钥失败: %v\n%s", err, out)
	}
	msg := []byte("abc123  packnest_1.6.0_darwin_arm64.tar.gz\nddd  packnest_1.6.0_linux_amd64.tar.gz\n")
	msgPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(msgPath, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sshKeygen, "-Y", "sign", "-f", key, "-n", SSHSigNamespace, msgPath).CombinedOutput(); err != nil {
		t.Fatalf("签名失败: %v\n%s", err, out)
	}
	sig, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	pubRaw, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	keys := ParseAllowedSigners("release@packnest.dev " + string(pubRaw))
	if len(keys) != 1 {
		t.Fatalf("公钥解析失败: %q", pubRaw)
	}
	if err := VerifySSHSIG(msg, sig, keys); err != nil {
		t.Fatalf("Go 侧验 ssh-keygen 签名失败（格式猜错了）: %v", err)
	}

	// 反向：Go 构造的签名交给 ssh-keygen 验（用 Go 自己生成的密钥对，不读 ssh-keygen 私钥）
	goPub, goPriv := newKey(t)
	built := buildSSHSIG(t, goPriv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)
	builtPath := filepath.Join(dir, "built.sig")
	if err := os.WriteFile(builtPath, built, 0o600); err != nil {
		t.Fatal(err)
	}
	signers := filepath.Join(dir, "allowed_signers")
	goPubLine := "release@packnest.dev ssh-ed25519 " + base64.StdEncoding.EncodeToString(sshWirePub(goPub)) + "\n"
	if err := os.WriteFile(signers, []byte(goPubLine), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sshKeygen, "-Y", "verify", "-f", signers, "-I", "release@packnest.dev",
		"-n", SSHSigNamespace, "-s", builtPath)
	cmd.Stdin = bytes.NewReader(msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen 验 Go 构造的签名失败: %v\n%s", err, out)
	}
}
