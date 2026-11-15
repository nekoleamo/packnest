package selfupdate

// `pk upgrade --self` 的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeNoSource         = "selfupdate.no_source"
	CodeNoLatest         = "selfupdate.no_latest"
	CodeBadVersion       = "selfupdate.bad_version"
	CodeDownload         = "selfupdate.download"
	CodeReadSums         = "selfupdate.read_sums"
	CodeNoSumForArtifact = "selfupdate.no_sum_for_artifact"
	CodeReadArtifact     = "selfupdate.read_artifact"
	CodeSumMismatch      = "selfupdate.sum_mismatch"
	CodeSignature        = "selfupdate.signature"
	CodeNoTrustKey       = "selfupdate.no_trust_key"
	CodeArchiveFormat    = "selfupdate.archive_format"
	CodeExtract          = "selfupdate.extract"
	CodeExtractLayout    = "selfupdate.extract_layout"
	CodeWriteNew         = "selfupdate.write_new"
	CodeReplace          = "selfupdate.replace"
	CodeTempDir          = "selfupdate.temp_dir"
	CodeChannelManaged   = "selfupdate.channel_managed"
)
