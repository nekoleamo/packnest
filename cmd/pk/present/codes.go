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

	// ── internal/verify：摘要/校验和链路 ──
	"sum.known_load":       {zh: "读取校验表失败: %v", en: "cannot read the verification table: %v"},
	"sum.known_corrupt":    {zh: "校验表 %q 损坏: %v", en: "verification table %q is corrupt: %v"},
	"sum.open_artifact":    {zh: "打开产物失败: %v", en: "cannot open the artifact: %v"},
	"sum.read_artifact":    {zh: "读取产物失败: %v", en: "cannot read the artifact: %v"},
	"sum.digest_missing":   {zh: "校验失败: 缺少产物摘要", en: "verification failed: artifact digest missing"},
	"sum.algo_changed":     {zh: "校验失败: %s 的摘要算法变了（记录 %s，现在 %s）；确需覆盖用 --force-rehash", en: "verification failed: digest algorithm for %s changed (recorded %s, now %s); use --force-rehash to override"},
	"sum.tofu_mismatch":    {zh: "校验失败: %s 的 %s 与本地记录不一致（记录 %s，实际 %s）；确需覆盖用 --force-rehash", en: "verification failed: %s %s differs from the local record (recorded %s, actual %s); use --force-rehash to override"},
	"sum.no_source":        {zh: "校验失败: 拿不到 %s 的校验和（所有源都不可用）；确认风险后可加 --insecure", en: "verification failed: no checksum source for %s is reachable; add --insecure only after accepting the risk"},
	"sum.sources_conflict": {zh: "校验失败: %s 的校验和源之间不一致（%s；%s）——可能是镜像被污染，也可能是上游重新发布了产物；确认来源可信后可加 --insecure", en: "verification failed: checksum sources disagree for %s (%s; %s) -- either a poisoned mirror or an upstream re-release; add --insecure only if you trust the source"},
	"sum.mismatch":         {zh: "校验失败: %s 的产物 %s 不符（期望 %s，实际 %s）；产物可能被篡改或损坏", en: "verification failed: %s artifact %s mismatch (want %s, got %s); the artifact may be tampered with or corrupted"},
	"sum.inline_invalid":   {zh: "校验失败: 索引里的 sha256 格式非法: %q", en: "verification failed: malformed sha256 in the index: %q"},
	"sum.registry_invalid": {zh: "校验失败: registry 提供的 shasum 格式非法: %q", en: "verification failed: malformed shasum from the registry: %q"},

	// ── internal/spec：版本说明符 ──
	"spec.empty":           {zh: "版本说明符不能为空", en: "version spec must not be empty"},
	"spec.version_invalid": {zh: "版本 %q 非法: %v", en: "invalid version %q: %v"},
	"spec.unparsable":      {zh: "版本说明符 %q 无法解析: %v", en: "cannot parse version spec %q: %v"},
	"spec.version_empty":   {zh: "版本号为空", en: "version string is empty"},
	"spec.engines_invalid": {zh: "engines 范围 %q 无法解析: %v", en: "cannot parse engines range %q: %v"},

	// ── internal/companion：npm 伴生包 ──
	"companion.not_in_registry":      {zh: "registry 里没有 npm@%s（`pk list --available` 可看可用版本）", en: "npm@%s is not in the registry (see `pk list --available`)"},
	"companion.engines_incompatible": {zh: "npm@%s 要求 node %s，与当前 node %s 不兼容%s", en: "npm@%s requires node %s, incompatible with the current node %s%s"},
	"companion.engines_unparsable":   {zh: "npm@%s 的 engines.node 无法解析: %v", en: "cannot parse engines.node for npm@%s: %v"},

	// ── internal/paths：根目录与原子落盘 ──
	"path.home_unresolved":  {zh: "解析用户主目录失败: %v", en: "cannot resolve the home directory: %v"},
	"path.cache_unresolved": {zh: "解析 %s 失败: %v", en: "cannot resolve %s: %v"},
	"path.root_empty":       {zh: "根目录不能为空", en: "root directory must not be empty"},
	"path.root_unresolved":  {zh: "解析根目录 %q 失败: %v", en: "cannot resolve root %q: %v"},
	"path.archive_absolute": {zh: "归档内路径非法（绝对路径）: %q", en: "illegal path inside the archive (absolute): %q"},
	"path.archive_escape":   {zh: "归档内路径非法（越界）: %q", en: "illegal path inside the archive (escapes the root): %q"},
	"path.mkdir":            {zh: "创建目录 %q 失败: %v", en: "cannot create directory %q: %v"},
	"path.write":            {zh: "写 %q 失败: %v", en: "cannot write %q: %v"},
	"path.rename":           {zh: "替换 %q 失败: %v", en: "cannot replace %q: %v"},
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
