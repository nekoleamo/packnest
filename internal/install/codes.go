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
