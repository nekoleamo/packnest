package provider

// 清单加载/校验与 URL 模板的问题码（稳定；文案在 cmd/pk/present 的码表里）。
const (
	// 内嵌清单加载
	CodeUnknownTool   = "provider.unknown_tool"
	CodeEmbedScan     = "provider.embed_scan"
	CodeReadManifest  = "provider.read_manifest"
	CodeParseManifest = "provider.parse_manifest"
	CodeIDMismatch    = "provider.id_mismatch"
	CodeNoneEmbedded  = "provider.none_embedded"
	CodeTOML          = "provider.toml"
	// 清单校验
	CodeMissingID          = "provider.missing_id"
	CodeMissingDisplay     = "provider.missing_display"
	CodeIndexKind          = "provider.index_kind"
	CodeMissingIndexURLs   = "provider.missing_index_urls"
	CodeEmptyIndexURL      = "provider.empty_index_url"
	CodeMissingArtifactURL = "provider.missing_artifact_urls"
	CodeExtMissing         = "provider.ext_missing"
	CodeExtInvalid         = "provider.ext_invalid"
	CodePlatformMissing    = "provider.platform_missing"
	CodeEmptyBins          = "provider.empty_bins"
	CodeChecksumFormat     = "provider.checksum_format"
	CodeChecksumMode       = "provider.checksum_mode"
	CodeMissingChecksum    = "provider.missing_checksum_urls"
	CodeCompanionMode      = "provider.companion_mode"
	CodeCompanionFields    = "provider.companion_fields"
	CodeCompanionPinned    = "provider.companion_pinned_version"
	// URL 模板
	CodeNoURLProfile      = "provider.no_url_profile"
	CodeArtifactRender    = "provider.artifact_render"
	CodeNoArtifactSource  = "provider.no_artifact_source"
	CodeChecksumRender    = "provider.checksum_render"
	CodePlatformUnsupport = "provider.platform_unsupported"
	CodeArtifactPlatform  = "provider.artifact_platform"
	CodeTemplateUnclosed  = "provider.template_unclosed"
	CodeTemplateKey       = "provider.template_placeholder"
	// 外部清单（external.go）
	CodeExtIDMismatch    = "provider.ext_id_mismatch"
	CodeExtPBS           = "provider.ext_pbs"
	CodeExtHTTP          = "provider.ext_http"
	CodeExtNegativeStrip = "provider.ext_negative_strip"
	CodeExtRelative      = "provider.ext_relative"
	CodeExtCompanionReg  = "provider.ext_companion_registry"
	CodeExtTooLarge      = "provider.ext_too_large"
)

// 索引解析的问题码（index.go / pbs.go）。
const (
	CodeNoIndexSource   = "index.no_source"
	CodeIndexAllFailed  = "index.all_failed"
	CodeIndexEmpty      = "index.empty"
	CodeNoLTS           = "index.no_lts"
	CodeNoStable        = "index.no_stable"
	CodeNoMatch         = "index.no_match"
	CodeKindUnknown     = "index.kind_unknown"
	CodeNotArray        = "index.not_array"
	CodeNoVersionDirs   = "index.no_version_dirs"
	CodeRegistryParse   = "index.registry_parse"
	CodeRegistryNoVers  = "index.registry_no_versions"
	CodePBSTagList      = "index.pbs_tag_list"
	CodePBSTagEmpty     = "index.pbs_tag_empty"
	CodePBSTagMissProd  = "index.pbs_tag_no_artifact"
	CodePBSNoArtifact   = "index.pbs_no_artifact"
	CodePBSCPython      = "index.pbs_no_cpython"
	CodePBSTagNoProduct = "index.pbs_tag_no_install_only"
)
