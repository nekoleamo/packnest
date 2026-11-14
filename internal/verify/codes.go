package verify

import "github.com/packnest/packnest/internal/coded"

// 校验相关的问题码（稳定，脚本与 GUI 可以拿来判断；文案在 cmd/pk/present 的码表里）。
//
// 命名：`sig.*` 是 sshsig 验签链路上的失败，`sum.*` 是摘要/校验和链路上的失败。
const (
	CodeSigBlobTooLarge      = "sig.blob_too_large"
	CodeSigNamespaceMismatch = "sig.namespace_mismatch"
	CodeSigHashAlg           = "sig.hash_alg"
	CodeSigKeyNotAllowed     = "sig.key_not_allowed"
	CodeSigDigestMismatch    = "sig.digest_mismatch"
	CodeSigMagicMissing      = "sig.magic_missing"
	CodeSigVersion           = "sig.version"
	CodeSigPubAlg            = "sig.pub_alg"
	CodeSigPubLen            = "sig.pub_len"
	CodeSigAlg               = "sig.sig_alg"
	CodeSigLen               = "sig.sig_len"
	CodeSigArmorMissing      = "sig.armor_missing"
	CodeSigBase64            = "sig.base64"
	CodeSigTruncated         = "sig.truncated"
	CodeSigFieldTooLarge     = "sig.field_too_large"
)

// sigErr 造一个"签名类"编码错误：Cause 固定为 ErrSignature，所以 errors.Is(err, ErrSignature) 照旧成立。
func sigErr(code string, args ...any) error { return coded.Wrap(ErrSignature, code, args...) }
