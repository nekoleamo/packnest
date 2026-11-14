package shim

// shim 创建/解析的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeEmptyArgs     = "shim.empty_args"
	CodeMkdir         = "shim.mkdir"
	CodeUnparsable    = "shim.unparsable"
	CodeReadFailed    = "shim.read"
	CodeSymlinkFailed = "shim.symlink"
	CodeReplaceFailed = "shim.replace"
	CodeWriteFailed   = "shim.write"
)
