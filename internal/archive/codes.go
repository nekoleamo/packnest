package archive

// 解压/落盘的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeFormatUnsupported = "archive.format_unsupported"
	CodeTmpDir            = "archive.tmpdir"
	CodeOpen              = "archive.open"
	CodeGzip              = "archive.gzip"
	CodeTarRead           = "archive.tar_read"
	CodeMkdir             = "archive.mkdir"
	CodeLinkUnsupported   = "archive.link_unsupported"
	CodeLinkCreate        = "archive.link_create"
	CodeLinkRead          = "archive.link_read"
	CodeEntryOpen         = "archive.entry_open"
	CodeCreate            = "archive.create"
	CodeWriteTruncated    = "archive.write_truncated"
	CodeWrite             = "archive.write"
	CodeLinkEmpty         = "archive.link_empty"
	CodeLinkAbsolute      = "archive.link_absolute"
	CodeLinkEscape        = "archive.link_escape"
	CodeSingleCount       = "archive.single_count"
	CodeSingleMove        = "archive.single_move"
	CodeChmod             = "archive.chmod"
	CodeBinNotFound       = "archive.bin_not_found"
	CodePlaceTmp          = "archive.place_tmp"
	CodeOldRename         = "archive.old_rename"
	CodePlace             = "archive.place"
)
