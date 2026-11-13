package verify

import (
	_ "embed"
)

// allowedSigners 是编译进二进制的发布签名公钥（只是公钥，私钥永远离线）。
//
//go:embed keys/allowed_signers
var allowedSigners string

// TrustedKeys 返回内置的发布签名公钥；空切片表示这一版没配密钥
// （自己从源码构建时正常，官方发布版应当有 1-2 把）。
func TrustedKeys() []SSHPublicKey { return ParseAllowedSigners(allowedSigners) }
