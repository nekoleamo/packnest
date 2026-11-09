// Package providers 只做一件事：把 providers/*.toml 编进二进制。
//
// 这是与 proto 最大的结构差异（方案 §2 第 1 条）：provider 是内嵌 TOML，
// **首次使用零下载**，也没有 WASM/OCI 插件面。
package providers

import "embed"

// FS 是内嵌的 provider 清单。
//
//go:embed *.toml
var FS embed.FS
