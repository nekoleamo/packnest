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

	// 摘要/校验和链路（sum.*）
	CodeSumKnownLoad       = "sum.known_load"
	CodeSumKnownCorrupt    = "sum.known_corrupt"
	CodeSumOpenArtifact    = "sum.open_artifact"
	CodeSumReadArtifact    = "sum.read_artifact"
	CodeSumDigestMissing   = "sum.digest_missing"
	CodeSumAlgoChanged     = "sum.algo_changed"
	CodeSumTOFUMismatch    = "sum.tofu_mismatch"
	CodeSumNoSource        = "sum.no_source"
	CodeSumSourcesConflict = "sum.sources_conflict"
	CodeSumMismatch        = "sum.mismatch"
	CodeSumInlineInvalid   = "sum.inline_invalid"
	CodeSumRegistryInvalid = "sum.registry_invalid"
)

// sigErr 造一个"签名类"编码错误：Cause 固定为 ErrSignature，所以 errors.Is(err, ErrSignature) 照旧成立。
func sigErr(code string, args ...any) error { return coded.Wrap(ErrSignature, code, args...) }

// sumErr 造一个"校验和类"编码错误（Cause 固定 ErrChecksum）。
func sumErr(code string, args ...any) error { return coded.Wrap(ErrChecksum, code, args...) }

// ioErr 造一个"底层 I/O 出错"的编码错误：Cause 是原始错误（errors.Is 能穿过去）。
func ioErr(cause error, code string, args ...any) error { return coded.Wrap(cause, code, args...) }
