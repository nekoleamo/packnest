package verify

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// FuzzSSHSIG 喂两条入口：
//  1. armored 文本 → VerifySSHSIG（外壳、base64、结构、白名单、摘要整链）；
//  2. 原始 blob → dearmor 剥出来的 / 或者直接就是二进制结构 → parseSSHSIG（跳过外壳的分支）。
//
// 断言只有一条：任何输入下要么通过、要么 errors.Is(err, ErrSignature)，绝不 panic、不超时。
// 上限由 sshsig.go 的 maxSSHString(1MB) / maxSSHBlob(64KB) 兜住。
func FuzzSSHSIG(f *testing.F) {
	// 种子全部代码构造（buildSSHSIG 复用生产侧的 signedData 拼装）。
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	msg := []byte("abc123  packnest_1.7.0_darwin_arm64.tar.gz\n")
	good := buildSSHSIG(priv, SSHSigNamespace, "sha512", ed25519Name, ed25519Name, msg)
	f.Add(msg, good)
	f.Add(msg, []byte(strings.ReplaceAll(string(good), "\n", "\r\n")))
	f.Add([]byte{}, good)
	f.Add(msg, []byte(sshSigArmorB+"\nAAAA\n"+sshSigArmorE))
	f.Add(msg, []byte("SSHSIG\x00\x00\x00\x01"))
	blob, err := dearmor(good)
	if err != nil {
		f.Fatalf("种子自身应能 dearmor: %v", err)
	}
	f.Add(msg, blob) // 第二路入口：raw blob

	pub := priv.Public().(ed25519.PublicKey)
	allowed := []SSHPublicKey{{Principal: "release@packnest.dev", Key: pub}}

	f.Fuzz(func(t *testing.T, msg, in []byte) {
		for _, allow := range [][]SSHPublicKey{allowed, nil} {
			if err := VerifySSHSIG(msg, in, allow); err != nil && !errors.Is(err, ErrSignature) {
				t.Fatalf("VerifySSHSIG 只允许返回 ErrSignature，得到 %v", err)
			}
		}
		// 第二路：把输入当 raw blob 直接拆结构（dearmor 成功时用剥出来的 blob）。
		raw := in
		if b, err := dearmor(in); err == nil {
			raw = b
		}
		if len(raw) > maxSSHBlob {
			return // 与 VerifySSHSIG 的整块上限一致，超限不重复测
		}
		if _, _, _, _, _, err := parseSSHSIG(raw); err != nil && !errors.Is(err, ErrSignature) {
			t.Fatalf("parseSSHSIG 只允许返回 ErrSignature，得到 %v", err)
		}
	})
}

// FuzzAllowedSigners 喂任意文本给 allowed_signers 解析：
// 不许 panic，返回的每把公钥都必须是合法 ed25519，且条数不超过行数。
func FuzzAllowedSigners(f *testing.F) {
	pub := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	b64 := base64.StdEncoding.EncodeToString(sshWirePub(pub))
	f.Add("# comment\n\nrelease@packnest.dev ssh-ed25519 " + b64 + " pk-release-2026\n")
	f.Add("release@packnest.dev,cI@example.com namespaces=\"pk-release\" ssh-ed25519 " + b64 + "\n")
	f.Add("bad ssh-rsa AAAA\nbad ssh-ed25519 not-base64\n")
	f.Add("")
	f.Add("p ssh-ed25519")

	f.Fuzz(func(t *testing.T, text string) {
		keys := ParseAllowedSigners(text)
		if max := strings.Count(text, "\n") + 1; len(keys) > max {
			t.Fatalf("解析出 %d 把公钥，超过行数 %d", len(keys), max)
		}
		for _, k := range keys {
			if len(k.Key) != ed25519.PublicKeySize {
				t.Fatalf("公钥长度非法: %d", len(k.Key))
			}
		}
	})
}
