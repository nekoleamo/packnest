package present

import (
	"fmt"

	"github.com/packnest/packnest/internal/coded"
)

// codeText 是一个问题码的两种语言模板（%s/%d 顺序必须严格一致，见 §八 踩坑 1）。
type codeText struct{ zh, en string }

// codeTable 是"内部包只给 code、CLI 负责说人话"的唯一文案表（E1 的 L2）。
//
// 加新码的规矩：
//  1. 先在该包定义 `Code*` 常量（稳定、只增不改）；
//  2. 在这里补两条模板，**中文必须与改造前的文案逐字节一致**（否则 `testdata/human` 快照会红）；
//  3. 英文模板的占位符顺序必须与中文一致。
var codeTable = map[string]codeText{
	// ── internal/verify：sshsig 验签链路 ──
	"sig.blob_too_large":     {zh: "签名校验失败: 签名块 %d 字节，超出上限", en: "signature check failed: signature block is %d bytes, over the limit"},
	"sig.namespace_mismatch": {zh: "签名校验失败: namespace 是 %q，期望 %q", en: "signature check failed: namespace is %q, want %q"},
	"sig.hash_alg":           {zh: "签名校验失败: 哈希算法是 %q，只接受 sha512", en: "signature check failed: hash algorithm is %q, only sha512 is accepted"},
	"sig.key_not_allowed":    {zh: "签名校验失败: 签名公钥不在内置白名单（可能换了发布密钥，先升级 pk）", en: "signature check failed: the signing key is not in the built-in allowlist (the release key may have rotated; upgrade pk)"},
	"sig.digest_mismatch":    {zh: "签名校验失败: 摘要不匹配（清单被改过？）", en: "signature check failed: digest mismatch (was the manifest modified?)"},
	"sig.magic_missing":      {zh: "签名校验失败: 缺少 SSHSIG 魔数", en: "signature check failed: SSHSIG magic missing"},
	"sig.version":            {zh: "签名校验失败: 版本号不是 1", en: "signature check failed: version is not 1"},
	"sig.pub_alg":            {zh: "签名校验失败: 公钥算法只接受 %s", en: "signature check failed: public key algorithm must be %s"},
	"sig.pub_len":            {zh: "签名校验失败: ed25519 公钥长度不对", en: "signature check failed: wrong ed25519 public key length"},
	"sig.sig_alg":            {zh: "签名校验失败: 签名算法只接受 %s", en: "signature check failed: signature algorithm must be %s"},
	"sig.sig_len":            {zh: "签名校验失败: ed25519 签名长度不对", en: "signature check failed: wrong ed25519 signature length"},
	"sig.armor_missing":      {zh: "签名校验失败: 缺少 %s 外壳", en: "signature check failed: missing the %s armor"},
	"sig.base64":             {zh: "签名校验失败: base64 解码失败: %v", en: "signature check failed: base64 decode error: %v"},
	"sig.truncated":          {zh: "签名校验失败: 结构被截断", en: "signature check failed: structure is truncated"},
	"sig.field_too_large":    {zh: "签名校验失败: 字段长度 %d 超限", en: "signature check failed: field length %d is over the limit"},
}

// Text 把任意错误渲染成"给人看的一句话"（当前语言）。
//
// 优先级：编码错误查码表 → 没有码/没登记就退回 err.Error()（不丢信息，只是没翻译）。
// 这条链路是 E1 的核心：`internal/*` 只给 code + 参数，文案全在这里。
func Text(err error) string {
	if err == nil {
		return ""
	}
	if ce, ok := coded.Of(err); ok {
		if entry, ok := codeTable[ce.Code]; ok {
			tpl := entry.zh
			if LangIsEN() {
				tpl = entry.en
			}
			// 参数个数不匹配时不要 panic（模板与调用点会漂），退回带 code 的紧凑形式
			if rendered, ok := safeSprintf(tpl, ce.Args); ok {
				return rendered
			}
		}
	}
	return err.Error()
}

// safeSprintf 用 recover 兜住"模板与参数个数不符"的格式化 panic。
func safeSprintf(tpl string, args []any) (out string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			out, ok = "", false
		}
	}()
	return fmt.Sprintf(tpl, args...), true
}

// Codes 返回已登记的问题码（测试用：断言"定义过的码都有文案"）。
func Codes() []string {
	out := make([]string, 0, len(codeTable))
	for code := range codeTable {
		out = append(out, code)
	}
	return out
}
