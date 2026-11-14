package spec

// 版本说明符解析的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeSpecEmpty      = "spec.empty"
	CodeVersionInvalid = "spec.version_invalid"
	CodeSpecUnparsable = "spec.unparsable"
	CodeVersionEmpty   = "spec.version_empty"
	CodeEnginesInvalid = "spec.engines_invalid"
)
