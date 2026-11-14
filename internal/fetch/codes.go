package fetch

import "github.com/packnest/packnest/internal/coded"

// 网络/落盘的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeOffline           = "fetch.offline"
	CodeTooManyRedirects  = "fetch.too_many_redirects"
	CodeDowngradeRedirect = "fetch.downgrade_redirect"
	CodeRequest           = "fetch.request"
	CodeBodyRead          = "fetch.body_read"
	CodeIndexTimeout      = "fetch.index_timeout"
	CodeMkdir             = "fetch.mkdir"
	CodePersist           = "fetch.persist"
	CodeResumeRange       = "fetch.resume_range"
	CodeResumeInvalid     = "fetch.resume_invalid"
	CodeHTTPStatus        = "fetch.http_status"
	CodePartOpen          = "fetch.part_open"
	CodeInterrupted       = "fetch.interrupted"
	CodeWrite             = "fetch.write"
	CodeSizeMismatch      = "fetch.size_mismatch"
	CodeNetwork           = "fetch.network"
	CodeShardShort        = "fetch.shard_short"
)

// offlineErr 把"离线禁止联网"包成编码错误（errors.Is 仍能命中 ErrOffline）。
func offlineErr() error { return coded.Wrap(ErrOffline, CodeOffline) }
