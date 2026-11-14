package mirror

// 镜像档/探测的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeUnknownProfile   = "mirror.unknown_profile"
	CodeNoSource         = "mirror.no_source"
	CodeAllSourcesFailed = "mirror.all_sources_failed"
	CodeBothUnreachable  = "mirror.both_unreachable"
	CodeNoCandidate      = "mirror.no_candidate"
)
