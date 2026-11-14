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

	// ── internal/mirror：镜像档与探测 ──
	"mirror.unknown_profile":    {zh: "未知镜像档 %q，可选: %s", en: "unknown mirror profile %q, available: %s"},
	"mirror.no_source":          {zh: "没有可用的下载源（检查 providers/*.toml 与镜像档）", en: "no usable download source (check providers/*.toml and the mirror profile)"},
	"mirror.all_sources_failed": {zh: "所有下载源均不可用：\n  %s\n建议：`pk mirror use cn` 换档，或用 PK_MIRROR 指向内网源", en: "all download sources failed:\n  %s\nsuggestion: switch profile with `pk mirror use cn`, or point PK_MIRROR at an intranet source"},
	"mirror.both_unreachable":   {zh: "国外与国内镜像都不可达（%s / %s）：用 PK_MIRROR 指向内网源，或检查代理", en: "neither the overseas nor the domestic mirror is reachable (%s / %s): point PK_MIRROR at an intranet source, or check the proxy"},
	"mirror.no_candidate":       {zh: "没有可探测的候选", en: "no candidate to probe"},

	// ── internal/archive：解压与原子落盘 ──
	"archive.format_unsupported": {zh: "不支持的归档格式 %q（只支持 tar.gz / tgz / zip）", en: "unsupported archive format %q (only tar.gz / tgz / zip)"},
	"archive.tmpdir":             {zh: "创建临时安装目录失败: %v", en: "cannot create the temporary install directory: %v"},
	"archive.open":               {zh: "打开归档失败: %v", en: "cannot open the archive: %v"},
	"archive.gzip":               {zh: "解 gzip 失败（归档损坏？）: %v", en: "gzip decompression failed (corrupt archive?): %v"},
	"archive.tar_read":           {zh: "读 tar 失败（归档截断？）: %v", en: "reading the tar failed (truncated archive?): %v"},
	"archive.mkdir":              {zh: "创建目录失败: %v", en: "cannot create a directory: %v"},
	"archive.link_unsupported":   {zh: "不支持归档里的符号链接（%s：%s）", en: "symbolic links inside archives are not supported (%s: %s)"},
	"archive.link_create":        {zh: "创建符号链接 %q 失败: %v", en: "cannot create symlink %q: %v"},
	"archive.link_read":          {zh: "读取符号链接失败: %v", en: "cannot read the symlink: %v"},
	"archive.entry_open":         {zh: "打开 %q 失败: %v", en: "cannot open %q: %v"},
	"archive.create":             {zh: "创建文件 %q 失败: %v", en: "cannot create file %q: %v"},
	"archive.write_truncated":    {zh: "写入 %q 失败（归档截断？）: %v", en: "writing %q failed (truncated archive?): %v"},
	"archive.write":              {zh: "写入 %q 失败: %v", en: "writing %q failed: %v"},
	"archive.link_empty":         {zh: "符号链接目标为空: %q", en: "symlink target is empty: %q"},
	"archive.link_absolute":      {zh: "符号链接 %q 指向绝对路径 %q，拒绝", en: "symlink %q points at the absolute path %q, refusing"},
	"archive.link_escape":        {zh: "符号链接 %q 指向解压目录之外（%q），拒绝", en: "symlink %q escapes the extraction directory (%q), refusing"},
	"archive.single_count":       {zh: "单文件布局的归档里应有且只有一个文件（实际 %d 个）", en: "a single-file archive must contain exactly one file (found %d)"},
	"archive.single_move":        {zh: "移动到单文件布局位置失败: %v", en: "cannot move into the single-file layout: %v"},
	"archive.chmod":              {zh: "设置 %s 可执行位失败: %v", en: "cannot set the executable bit on %s: %v"},
	"archive.bin_not_found":      {zh: "解压后的树里找不到命令 %q（查找过: %s）", en: "command %q not found in the extracted tree (looked for: %s)"},
	"archive.place_tmp":          {zh: "落盘到 %q 失败: %v", en: "cannot place the tree at %q: %v"},
	"archive.old_rename":         {zh: "旧目录改名失败（进程还占用？）: %v", en: "cannot rename the old directory (still in use?): %v"},
	"archive.place":              {zh: "新目录落盘失败（已回滚旧版本）: %v", en: "cannot place the new directory (old version rolled back): %v"},
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
			// 模板比参数多一个 %v 时，把 Cause 补在最后：`sum.known_corrupt` / `path.mkdir` 这类
			// 模板的最后一个占位符就是"底层错误"，调用点不必再手动塞一遍。
			args := ce.Args
			if need := verbCount(tpl); need > len(args) && ce.Cause != nil {
				args = append(append([]any{}, args...), ce.Cause)
			}
			// 参数仍然不够时不要 panic（模板与调用点会漂），退回带 code 的紧凑形式
			if rendered, ok := safeSprintf(tpl, args); ok {
				return rendered
			}
		}
	}
	return err.Error()
}

// verbCount 数模板里有多少个格式化占位符（%% 不算）。
func verbCount(tpl string) int {
	n := 0
	for i := 0; i < len(tpl); i++ {
		if tpl[i] != '%' {
			continue
		}
		if i+1 < len(tpl) && tpl[i+1] == '%' {
			i++
			continue
		}
		n++
		if i+1 < len(tpl) {
			i++ // 跳过动词本身（%q/%v/%d/%s…），不处理宽度等修饰符的极端情况
		}
	}
	return n
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
