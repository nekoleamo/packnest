package verify

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// sshWirePub 把 ed25519 公钥包成 SSH wire 格式（ssh-keygen 的 allowed_signers 要它）。
func sshWirePub(pub ed25519.PublicKey) []byte {
	var buf bytes.Buffer
	writeSSHString(&buf, []byte(ed25519Name))
	writeSSHString(&buf, pub)
	return buf.Bytes()
}

// buildSSHSIG 代码构造一个合法的 armored sshsig（夹具不存二进制）。
// 不依赖 *testing.T：fuzz 种子在 *testing.F 上也要用同一个构造器。
func buildSSHSIG(priv ed25519.PrivateKey, ns, hashAlg string, pubName, sigName string, msg []byte) []byte {
	pub := priv.Public().(ed25519.PublicKey)

	var pubBlob bytes.Buffer
	writeSSHString(&pubBlob, []byte(pubName))
	writeSSHString(&pubBlob, pub)

	digest := sha512.Sum512(msg)
	var sigBlob bytes.Buffer
	writeSSHString(&sigBlob, []byte(sigName))
	writeSSHString(&sigBlob, ed25519.Sign(priv, signedData(ns, nil, hashAlg, digest[:])))

	var blob bytes.Buffer
	blob.WriteString(sshSigMagic)
	var ver [4]byte
	binary.BigEndian.PutUint32(ver[:], 1)
	blob.Write(ver[:])
	writeSSHString(&blob, pubBlob.Bytes())
	writeSSHString(&blob, []byte(ns))
	writeSSHString(&blob, nil)
	writeSSHString(&blob, []byte(hashAlg))
	writeSSHString(&blob, sigBlob.Bytes())

	enc := base64.StdEncoding.EncodeToString(blob.Bytes())
	var out strings.Builder
	out.WriteString(sshSigArmorB + "\n")
	for len(enc) > 64 {
		out.WriteString(enc[:64] + "\n")
		enc = enc[64:]
	}
	out.WriteString(enc + "\n" + sshSigArmorE + "\n")
	return []byte(out.String())
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// TestVerifySSHSIG 验签通过，且允许列表只认公钥本身。
func TestVerifySSHSIG(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte("abc123  packnest_1.7.0_darwin_arm64.tar.gz\n")
	sig := buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)
	if err := VerifySSHSIG(msg, sig, []SSHPublicKey{{Principal: "release@packnest.dev", Key: pub}}); err != nil {
		t.Fatalf("合法签名应通过: %v", err)
	}
}

// TestVerifySSHSIGRejects 四种篡改都必须失败（消息/哈希算法/算法名/公钥白名单）。
func TestVerifySSHSIGRejects(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte("checksums-v1")
	allowed := []SSHPublicKey{{Principal: "release@packnest.dev", Key: pub}}

	cases := []struct {
		name string
		msg  []byte
		sig  []byte
	}{
		{"消息改一字节", []byte("checksums-v2"), buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)},
		{"哈希算法降级", msg, buildSSHSIG(priv, SSHSigNamespace, "sha256", ed25519Name, ed25519Name, msg)},
		{"公钥算法不是 ed25519", msg, buildSSHSIG(priv, SSHSigNamespace, "sha512", "ssh-rsa", ed25519Name, msg)},
		{"签名算法不是 ed25519", msg, buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, "ssh-rsa", msg)},
		{"namespace 不符", msg, buildSSHSIG(priv, "other-ns", "sha512", ed25519Name, ed25519Name, msg)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := VerifySSHSIG(c.msg, c.sig, allowed); !errors.Is(err, ErrSignature) {
				t.Fatalf("应报 ErrSignature，得到 %v", err)
			}
		})
	}

	t.Run("公钥不在白名单", func(t *testing.T) {
		otherPub, _ := newKey(t)
		sig := buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)
		if err := VerifySSHSIG(msg, sig, []SSHPublicKey{{Principal: "x", Key: otherPub}}); !errors.Is(err, ErrSignature) {
			t.Fatalf("陌生公钥必须拒绝，得到 %v", err)
		}
	})
}

// TestVerifySSHSIGArmor 容忍 CRLF 与任意折行，拒绝缺外壳/坏 base64/截断。
func TestVerifySSHSIGArmor(t *testing.T) {
	pub, priv := newKey(t)
	msg := []byte("payload")
	sig := buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)
	allowed := []SSHPublicKey{{Key: pub}}

	crlf := bytes.ReplaceAll(sig, []byte("\n"), []byte("\r\n"))
	if err := VerifySSHSIG(msg, crlf, allowed); err != nil {
		t.Fatalf("CRLF 应该也能验: %v", err)
	}
	for name, bad := range map[string][]byte{
		"缺外壳":      []byte("bm90IGEgc2ln"),
		"坏 base64": []byte(sshSigArmorB + "\n!!!\n" + sshSigArmorE),
		"截断":       []byte(sshSigArmorB + "\nAAAA\n" + sshSigArmorE),
		// 结束标记紧跟在开始标记后面（与 BEGIN 末尾的 "-----" 重叠）：
		// fuzz 找到过这里的 slice 越界，换成 j < i+len(armorB) 兜住
		"END 紧贴 BEGIN": []byte(sshSigArmorB + "END SSH SIGNATURE-----"),
	} {
		if err := VerifySSHSIG(msg, bad, allowed); !errors.Is(err, ErrSignature) {
			t.Fatalf("%s 应报 ErrSignature，得到 %v", name, err)
		}
	}
}

// TestParseAllowedSigners 只认 ssh-ed25519 行，跳过注释与坏 base64。
func TestParseAllowedSigners(t *testing.T) {
	pub, _ := newKey(t)
	b64 := base64.StdEncoding.EncodeToString(sshWirePub(pub))
	text := "# 注释\n\nrelease@packnest.dev ssh-ed25519 " + b64 + " pk-release-2026\n" +
		"bad ssh-rsa AAAA\n" +
		"bad ssh-ed25519 not-base64\n"
	keys := ParseAllowedSigners(text)
	if len(keys) != 1 || !bytes.Equal(keys[0].Key, pub) || keys[0].Principal != "release@packnest.dev" {
		t.Fatalf("解析结果不对: %+v", keys)
	}
}

// TestParseAllowedSignersOptions 支持 principal 后面带 options 的写法。
func TestParseAllowedSignersOptions(t *testing.T) {
	pub, _ := newKey(t)
	b64 := base64.StdEncoding.EncodeToString(sshWirePub(pub))
	keys := ParseAllowedSigners("release@packnest.dev,cI@example.com namespaces=\"pk-release\" ssh-ed25519 " + b64 + "\n")
	if len(keys) != 1 || !bytes.Equal(keys[0].Key, pub) {
		t.Fatalf("带 options 的行应能解析: %+v", keys)
	}
}

// TestTrustedKeysEmpty 空配置不该 panic（自己构建时就是空）。
func TestTrustedKeysEmpty(t *testing.T) {
	if got := ParseAllowedSigners(""); len(got) != 0 {
		t.Fatalf("空文本应返回空切片: %+v", got)
	}
}
