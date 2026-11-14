package env

// 环境/rc 文件的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	CodeShellUnsupported = "env.shell_unsupported"
	CodeRead             = "env.read"
	CodeBackup           = "env.backup"
	CodeMkdir            = "env.mkdir"
	CodeWrite            = "env.write"
	CodeRegOpen          = "env.registry_open"
	CodeRegWrite         = "env.registry_write"
)
