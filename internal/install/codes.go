package install

// 编排层的问题码（稳定；文案在 cmd/pk/present 的码表里）。
//
// 命名：`install.*` 是安装/卸载/编排，`lock.*` 是 pk.lock，`project.*` 是 pk.toml / package.json。
const (
	// 安装与产物流转
	CodeNetwork         = "install.network"
	CodeNetworkProbe    = "install.network_probe"
	CodePlatformMissing = "install.platform_missing"
	CodeMinVersion      = "install.min_version"
	CodeExtractFailed   = "install.extract_failed"
	CodeSpecInvalid     = "install.spec_invalid"
	CodeRequiresMissing = "install.requires_missing"
	CodeNotInstalled    = "install.not_installed"
	CodeNpmProviderGone = "install.npm_provider_missing"
	CodeCorepackAbsent  = "install.corepack_absent"
	CodeCorepackOutput  = "install.corepack_output"
	CodeCorepackEnable  = "install.corepack_enable"
	CodeCompanionLookup = "install.companion_lookup"
	CodeCompanionIndex  = "install.companion_index"
	CodeCompanionNoVer  = "install.companion_no_version"
	CodeCompanionPlace  = "install.companion_place"
	// mirror/abort 事件的 msg 片段（1.8：内部只给码，CLI 渲染成当前语言）。
	CodeMirrorProbe       = "install.mirror_probe"
	CodeMirrorProbeCached = "install.mirror_probe_cached"
	CodeAbortBatch        = "install.abort_batch"
	// CodeUseNotConfirmed 是 `pk use` 发现目标版本没装、用户（或非交互环境）没确认时给的码。
	CodeUseNotConfirmed = "install.use_not_confirmed"
	// 卸载
	CodeNotInLedger      = "install.not_in_ledger"
	CodeStillReferenced  = "install.still_referenced"
	CodeRemoveFailed     = "install.remove_failed"
	CodeNoVersions       = "install.no_versions"
	CodeVersionsRefused  = "install.versions_refused"
	CodeShimRemoveFailed = "install.shim_remove"
	// pk.lock
	CodeLockLoad        = "lock.load"
	CodeLockParse       = "lock.parse"
	CodeLockEncode      = "lock.encode"
	CodeLockMissing     = "lock.missing"
	CodeLockMismatch    = "lock.mismatch"
	CodeLockToolMissing = "lock.tool_missing"
	CodeLockNoInstall   = "lock.no_install"
	CodeLockSHAMismatch = "lock.sha_mismatch"
	// 项目文件
	CodeProjectRead       = "project.read"
	CodeProjectParse      = "project.parse"
	CodeEnginesUnparsable = "project.engines_unparsable"
)
