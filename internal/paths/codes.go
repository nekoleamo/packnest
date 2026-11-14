package paths

// 路径/落盘的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeHomeUnresolved  = "path.home_unresolved"
	CodeCacheUnresolved = "path.cache_unresolved"
	CodeRootEmpty       = "path.root_empty"
	CodeRootUnresolved  = "path.root_unresolved"
	CodeArchiveAbs      = "path.archive_absolute"
	CodeArchiveEscape   = "path.archive_escape"
	CodeMkdir           = "path.mkdir"
	CodeWrite           = "path.write"
	CodeRename          = "path.rename"
)
