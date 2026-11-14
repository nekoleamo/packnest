package verify

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SSHSigNamespace 是发布签名的命名空间（与 ssh-keygen -Y sign -n 的参数一致）。
const SSHSigNamespace = "pk-release"

// ErrSignature 表示签名缺失/格式不符/验签失败（调用方按"产物不可信"处理）。
var ErrSignature = errors.New("签名校验失败")

const (
	sshSigMagic  = "SSHSIG"
	sshSigArmorB = "-----BEGIN SSH SIGNATURE-----"
	sshSigArmorE = "-----END SSH SIGNATURE-----"
	ed25519Name  = "ssh-ed25519"
	maxSSHString = 1 << 20  // 单个长度前缀字段的上限，防止畸形输入撑爆内存
	maxSSHBlob   = 64 << 10 // 整个签名块的上限（真实签名约 200 字节）
)

// SSHPublicKey 是一把可以参与验签的公钥。
type SSHPublicKey struct {
	Principal string
	Key       ed25519.PublicKey
}

// VerifySSHSIG 校验 armored sshsig 是否由白名单里的某把公钥签发。
//
// 只支持 ssh-ed25519 + sha512：比支持全套算法少一半代码，失败模式也更明确。
func VerifySSHSIG(msg, armored []byte, allowed []SSHPublicKey) error {
	blob, err := dearmor(armored)
	if err != nil {
		return err
	}
	if len(blob) > maxSSHBlob {
		return fmt.Errorf("%w: 签名块 %d 字节，超出上限", ErrSignature, len(blob))
	}
	pub, ns, reserved, hashAlg, sig, err := parseSSHSIG(blob)
	if err != nil {
		return err
	}
	if ns != SSHSigNamespace {
		return fmt.Errorf("%w: namespace 是 %q，期望 %q", ErrSignature, ns, SSHSigNamespace)
	}
	if hashAlg != "sha512" {
		return fmt.Errorf("%w: 哈希算法是 %q，只接受 sha512", ErrSignature, hashAlg)
	}
	if !containsKey(allowed, pub) {
		return fmt.Errorf("%w: 签名公钥不在内置白名单（可能换了发布密钥，先升级 pk）", ErrSignature)
	}
	// 签名数据不是"消息的摘要"本身，而是 PROTOCOL.sshsig §3 的结构：
	// SSHSIG || string(namespace) || string(reserved) || string(hash_alg) || string(H(message))
	digest := sha512.Sum512(msg)
	if !ed25519.Verify(pub, signedData(ns, reserved, hashAlg, digest[:]), sig) {
		return fmt.Errorf("%w: 摘要不匹配（清单被改过？）", ErrSignature)
	}
	return nil
}

// signedData 拼出被签名的字节串（PROTOCOL.sshsig §3）。fields 全部是 SSHSIG 长度前缀字符串。
func signedData(ns string, reserved []byte, hashAlg string, digest []byte) []byte {
	var b bytes.Buffer
	b.WriteString(sshSigMagic)
	writeSSHString(&b, []byte(ns))
	writeSSHString(&b, reserved)
	writeSSHString(&b, []byte(hashAlg))
	writeSSHString(&b, digest)
	return b.Bytes()
}

// writeSSHString 写一个 RFC4253 长度前缀字符串。
func writeSSHString(b *bytes.Buffer, s []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s)))
	b.Write(n[:])
	b.Write(s)
}

// parseSSHSIG 拆 SSHSIG 结构（PROTOCOL.sshsig）：全部是 uint32 长度前缀字符串。
func parseSSHSIG(blob []byte) (pub ed25519.PublicKey, ns string, reserved []byte, hashAlg string, sig []byte, err error) {
	r := bytes.NewReader(blob)
	magic := make([]byte, len(sshSigMagic))
	if _, err = io.ReadFull(r, magic); err != nil || string(magic) != sshSigMagic {
		return nil, "", nil, "", nil, fmt.Errorf("%w: 缺少 SSHSIG 魔数", ErrSignature)
	}
	if ver, e := readU32(r); e != nil || ver != 1 {
		return nil, "", nil, "", nil, fmt.Errorf("%w: 版本号不是 1", ErrSignature)
	}
	pubBlob, err := readSSHString(r)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	key, err := parseEd25519Pub(pubBlob)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	nsBytes, err := readSSHString(r)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	reserved, err = readSSHString(r) // 保留：它参与被签名数据（虽然当前总是空）
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	alg, err := readSSHString(r)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	sigBlob, err := readSSHString(r)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	sig, err = parseEd25519Signature(sigBlob)
	if err != nil {
		return nil, "", nil, "", nil, err
	}
	return key, string(nsBytes), reserved, string(alg), sig, nil
}

// parseEd25519Pub 从 SSH public key blob 里取 ed25519 公钥。
func parseEd25519Pub(blob []byte) (ed25519.PublicKey, error) {
	r := bytes.NewReader(blob)
	alg, err := readSSHString(r)
	if err != nil || string(alg) != ed25519Name {
		return nil, fmt.Errorf("%w: 公钥算法只接受 %s", ErrSignature, ed25519Name)
	}
	key, err := readSSHString(r)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: ed25519 公钥长度不对", ErrSignature)
	}
	return ed25519.PublicKey(key), nil
}

// parseEd25519Signature 从 SSH signature blob 里取 64 字节签名。
func parseEd25519Signature(blob []byte) ([]byte, error) {
	r := bytes.NewReader(blob)
	alg, err := readSSHString(r)
	if err != nil || string(alg) != ed25519Name {
		return nil, fmt.Errorf("%w: 签名算法只接受 %s", ErrSignature, ed25519Name)
	}
	sig, err := readSSHString(r)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: ed25519 签名长度不对", ErrSignature)
	}
	return sig, nil
}

// dearmor 剥掉 BEGIN/END 外壳与所有换行/空格，再做 base64 解码。
func dearmor(armored []byte) ([]byte, error) {
	s := string(armored)
	i := strings.Index(s, sshSigArmorB)
	j := strings.Index(s, sshSigArmorE)
	// 结束标记必须落在开始标记之后（否则 s[i+len(B):j] 会 slice 越界）。
	if i < 0 || j < i+len(sshSigArmorB) {
		return nil, fmt.Errorf("%w: 缺少 %s 外壳", ErrSignature, sshSigArmorB)
	}
	body := s[i+len(sshSigArmorB) : j]
	body = strings.NewReplacer("\r", "", "\n", "", "\t", "", " ", "").Replace(body)
	blob, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("%w: base64 解码失败: %v", ErrSignature, err)
	}
	return blob, nil
}

func readU32(r *bytes.Reader) (uint32, error) {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return 0, fmt.Errorf("%w: 结构被截断", ErrSignature)
	}
	return n, nil
}

func readSSHString(r *bytes.Reader) ([]byte, error) {
	n, err := readU32(r)
	if err != nil {
		return nil, err
	}
	if n > maxSSHString {
		return nil, fmt.Errorf("%w: 字段长度 %d 超限", ErrSignature, n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("%w: 结构被截断", ErrSignature)
	}
	return b, nil
}

func containsKey(allowed []SSHPublicKey, pub ed25519.PublicKey) bool {
	for _, k := range allowed {
		if bytes.Equal(k.Key, pub) {
			return true
		}
	}
	return false
}

// ParseAllowedSigners 解析 ssh-keygen 的 allowed_signers 文本：
//
//	principal[,principal] [options] ssh-ed25519 <base64> [comment]
//
// 只收 ssh-ed25519 行；空行与 # 注释跳过。
func ParseAllowedSigners(text string) []SSHPublicKey {
	var out []SSHPublicKey
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		for k := 1; k+1 < len(fields); k++ {
			if fields[k] != ed25519Name {
				continue
			}
			// allowed_signers 里的公钥是 SSH wire blob（"ssh-ed25519" + 32B），不是裸公钥
			raw, err := base64.StdEncoding.DecodeString(fields[k+1])
			if err != nil {
				break
			}
			key, err := parseEd25519Pub(raw)
			if err != nil {
				break
			}
			out = append(out, SSHPublicKey{Principal: fields[0], Key: key})
			break
		}
	}
	return out
}
