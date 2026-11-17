package present

import (
	"fmt"
	"strings"

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
	// verify 事件的 msg（1.8：内部只给码，CLI 渲染）
	"sum.republish_note":   {zh: "上游重新发布了同一版本的产物（同源 %s 已更新，其它源还是旧摘要），按 weak 接受", en: "upstream republished the same version (same-origin %s has the new digest, others still hold the old one); accepted as weak"},
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
	"archive.too_large":          {zh: "归档解压后超过 %s 上限（%s）：可能是投毒或异常归档，已拒绝写入", en: "the archive expands past the %s limit (%s): refusing to write (poisoned or broken archive?)"},
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

	// ── internal/fetch：网络与落盘 ──
	"fetch.offline":            {zh: "离线模式下禁止访问网络", en: "network access is disabled in offline mode"},
	"fetch.too_many_redirects": {zh: "重定向次数过多", en: "too many redirects"},
	"fetch.downgrade_redirect": {zh: "拒绝 https → %s 的降级重定向（%s）", en: "refusing an https -> %s downgrade redirect (%s)"},
	"fetch.request":            {zh: "构造请求失败: %v", en: "cannot build the request: %v"},
	"fetch.body_read":          {zh: "读取响应体失败: %v", en: "cannot read the response body: %v"},
	"fetch.index_timeout":      {zh: "索引拉取超时或被取消（%s，上限 %s）: %v", en: "index fetch timed out or was cancelled (%s, limit %s): %v"},
	"fetch.mkdir":              {zh: "创建下载目录失败: %v", en: "cannot create the download directory: %v"},
	"fetch.persist":            {zh: "落盘 %q 失败: %v", en: "cannot persist %q: %v"},
	"fetch.resume_range":       {zh: "续传校验失败：Content-Range %q 与本地记录不一致，已作废重下", en: "resume check failed: Content-Range %q does not match the local record, discarding and restarting"},
	"fetch.resume_invalid":     {zh: "续传位置无效（HTTP 416），已作废", en: "invalid resume offset (HTTP 416), discarded"},
	"fetch.http_status":        {zh: "下载 %q 失败：HTTP %d", en: "download %q failed: HTTP %d"},
	"fetch.part_open":          {zh: "打开临时文件失败: %v", en: "cannot open the temporary file: %v"},
	"fetch.interrupted":        {zh: "下载中断（已保留 %d 字节待续传）: %v", en: "download interrupted (%d bytes kept for resume): %v"},
	"fetch.write":              {zh: "写入失败: %v", en: "write failed: %v"},
	"fetch.size_mismatch":      {zh: "产物大小不符：期望 %d 字节，实得 %d", en: "artifact size mismatch: want %d bytes, got %d"},
	"fetch.network":            {zh: "网络请求失败: %v", en: "network request failed: %v"},
	"fetch.shard_short":        {zh: "第 %d-%d 字节只拿到 %d 字节", en: "shard %d-%d returned only %d bytes"},

	// ── internal/store：台账/配置/锁 ──
	"store.ledger_load":      {zh: "读取台账失败: %v", en: "cannot read the ledger: %v"},
	"store.ledger_corrupt":   {zh: "台账 %q 损坏（%v）；可执行 `pk doctor` 查看恢复建议", en: "ledger %q is corrupt (%v); run `pk doctor` for recovery advice"},
	"store.not_in_ledger":    {zh: "%s@%s 不在台账里", en: "%s@%s is not in the ledger"},
	"store.ledger_encode":    {zh: "序列化台账失败: %v", en: "cannot encode the ledger: %v"},
	"store.config_load":      {zh: "读取配置失败: %v", en: "cannot read the config: %v"},
	"store.config_parse":     {zh: "配置 %q 解析失败: %v", en: "cannot parse config %q: %v"},
	"store.config_encode":    {zh: "序列化配置失败: %v", en: "cannot encode the config: %v"},
	"store.backup":           {zh: "备份 %q 失败: %v", en: "cannot back up %q: %v"},
	"store.install_record":   {zh: "序列化安装记录失败: %v", en: "cannot encode the install record: %v"},
	"store.cache_index_load": {zh: "读取缓存索引失败: %v", en: "cannot read the cache index: %v"},
	"store.capacity_empty":   {zh: "容量为空", en: "capacity is empty"},
	"store.capacity_parse":   {zh: "容量 %q 无法解析（示例：5GB / 500MB / 1073741824）", en: "cannot parse capacity %q (examples: 5GB / 500MB / 1073741824)"},
	"store.lock_mkdir":       {zh: "创建锁目录失败: %v", en: "cannot create the lock directory: %v"},
	"store.lock_open":        {zh: "打开锁文件失败: %v", en: "cannot open the lock file: %v"},
	"store.lock_path":        {zh: "锁路径非法: %v", en: "illegal lock path: %v"},
	"store.lock_acquire":     {zh: "取全局锁失败: %v", en: "cannot acquire the global lock: %v"},

	// ── internal/env：rc 文件与注册表 ──
	"env.shell_unsupported": {zh: "不支持的 shell %q（支持 bash/zsh/sh/powershell）", en: "unsupported shell %q (supported: bash/zsh/sh/powershell)"},
	"env.read":              {zh: "读取 %q 失败: %v", en: "cannot read %q: %v"},
	"env.backup":            {zh: "备份 %q 失败: %v", en: "cannot back up %q: %v"},
	"env.mkdir":             {zh: "创建目录 %q 失败: %v", en: "cannot create directory %q: %v"},
	"env.write":             {zh: "写入 %q 失败: %v", en: "cannot write %q: %v"},
	"env.registry_open":     {zh: "打开注册表 HKCU\\%s 失败（错误码 %d）", en: "cannot open the registry key HKCU\\%s (error %d)"},
	"env.registry_write":    {zh: "写注册表 HKCU\\%s\\%s 失败（错误码 %d）", en: "cannot write the registry key HKCU\\%s\\%s (error %d)"},

	// ── internal/install：编排、卸载、锁、项目文件 ──
	"install.network":              {zh: "网络失败: %v", en: "network failure: %v"},
	"install.network_probe":        {zh: "网络失败: 探测与直连都失败：%v；%v", en: "network failure: probe and direct download both failed: %v; %v"},
	"install.platform_missing":     {zh: "%s@%s 没有对应的平台包（版本号须与本体一致）", en: "%s@%s has no matching platform package (the version must match the main artifact)"},
	"install.min_version":          {zh: "%s 只支持 %s 及以上（产物地址换了形态）；更老的版本请用 corepack 或 `npm i -g %s@%s`", en: "%s only supports %s and newer (the artifact layout changed); for older versions use corepack or `npm i -g %s@%s`"},
	"install.extract_failed":       {zh: "解压失败（产物可能被截断或损坏）: %v", en: "extraction failed (the artifact may be truncated or corrupt): %v"},
	"install.spec_invalid":         {zh: "版本说明符 %q 非法: %v", en: "illegal version spec %q: %v"},
	"install.requires_missing":     {zh: "缺少前置工具：%s 需要 %s，先 `pk install %s` 再装 %s（我们不隐式安装运行时）", en: "missing prerequisite: %s needs %s; run `pk install %s` before installing %s (we never install runtimes implicitly)"},
	"install.not_installed":        {zh: "还没装：%s@%s（`pk list` 看已装版本，先 `pk install %s@%s`）", en: "not installed: %s@%s (see `pk list`; run `pk install %s@%s` first)"},
	"install.npm_provider_missing": {zh: "找不到 npm 的 provider 清单，无法替换伴生包", en: "cannot find the npm manifest, so the bundled package cannot be replaced"},
	"install.corepack_absent":      {zh: "该 node 不自带 corepack（node 25+ 已移除）：pnpm/yarn 请用 `pk install pnpm`", en: "this node does not ship corepack (removed in node 25+): use `pk install pnpm` for pnpm/yarn"},
	"install.corepack_output":      {zh: "%s: %v（%s）", en: "%s: %v (%s)"},
	"install.corepack_enable":      {zh: "corepack enable 失败: %v", en: "corepack enable failed: %v"},
	"install.companion_lookup":     {zh: "%s 的伴生包 %s 找不到清单 %q: %v", en: "companion %s of %s has no manifest %q: %v"},
	"install.companion_index":      {zh: "伴生包 %s 的版本索引不可用: %v", en: "version index for companion %s is unavailable: %v"},
	"install.companion_no_version": {zh: "伴生包 %s 的索引里没有可用版本", en: "no usable version in the index for companion %s"},
	"install.companion_place":      {zh: "伴生包 %s 落地失败: %v", en: "cannot place companion %s: %v"},
	// ── install：事件 msg 的结构化来源（1.8）──
	"install.mirror_probe":        {zh: "%s (%dms)", en: "%s (%dms)"},
	"install.mirror_probe_cached": {zh: "%s (%dms, 6h 内探过)", en: "%s (%dms, probed within 6h)"},
	"install.abort_batch":         {zh: "本批已放弃（%s）", en: "batch abandoned (%s)"},
	"install.use_not_confirmed":   {zh: "没有确认安装 %s@%s（加 `--yes` 或在交互终端里确认后重试）", en: "%s@%s was not confirmed for install (add `--yes`, or confirm in an interactive terminal)"},

	// ── internal/selfupdate：`pk upgrade --self` ──
	"selfupdate.no_source":           {zh: "没有可用的发布源（gitee / GitHub 都试过了）", en: "no usable release source (tried both gitee and GitHub)"},
	"selfupdate.no_latest":           {zh: "取不到最新版本号: %v", en: "cannot read the latest version: %v"},
	"selfupdate.bad_version":         {zh: "版本号格式不对（%s 返回了 %q）", en: "unexpected version format (%s returned %q)"},
	"selfupdate.download":            {zh: "下载失败: %s: %v", en: "download failed: %s: %v"},
	"selfupdate.read_sums":           {zh: "读校验清单失败: %s: %v", en: "cannot read the checksum manifest: %s: %v"},
	"selfupdate.no_sum_for_artifact": {zh: "校验清单里没有 %s 的 sha256", en: "the checksum manifest has no sha256 for %s"},
	"selfupdate.read_artifact":       {zh: "读产物失败: %s: %v", en: "cannot read the artifact: %s: %v"},
	"selfupdate.sum_mismatch":        {zh: "sha256 不符：%s 期望 %s，实际 %s（可能被篡改或截断）", en: "sha256 mismatch for %s: expected %s, got %s (tampered or truncated)"},
	"selfupdate.signature":           {zh: "校验清单的发布签名验不过（可能被换过，或本版 pk 的公钥已过期）: %v", en: "the release signature of the checksum manifest does not verify (it may have been swapped, or this pk build trusts an older key): %v"},
	"selfupdate.no_trust_key":        {zh: "本版 pk 没有内置发布公钥（自己从源码构建的正常），不能安全自更新；请用安装脚本或包管理器升级", en: "this pk build embeds no release key (normal for local builds), so it refuses to self-update; use the install script or a package manager"},
	"selfupdate.archive_format":      {zh: "认不出产物格式: %s: %v", en: "unknown artifact format: %s: %v"},
	"selfupdate.extract":             {zh: "解压失败: %s: %v", en: "cannot extract %s: %v"},
	"selfupdate.extract_layout":      {zh: "解压后没找到 %s（发布包结构变了？）", en: "%s not found after extraction (did the release layout change?)"},
	"selfupdate.write_new":           {zh: "写新二进制失败: %s: %v", en: "cannot write the new binary: %s: %v"},
	"selfupdate.replace":             {zh: "替换 %s 失败: %v（用安装脚本或包管理器升级）", en: "cannot replace %s: %v (upgrade with the install script or a package manager)"},
	"selfupdate.temp_dir":            {zh: "建临时目录失败: %v", en: "cannot create a temp dir: %v"},
	"selfupdate.channel_managed":     {zh: "这个 pk 是 %s 装的，自更新会让包管理器台账失真；请用：%s（确认要强刷可加 `--force`）", en: "this pk was installed by %s; self-updating would desync the package manager ledger. Use: %s (add `--force` to override)"},
	"install.not_in_ledger":          {zh: "%s@%s 不在台账里；不在台账里的目录只提示不删除（确认后可手动清理）", en: "%s@%s is not in the ledger; directories that are not in the ledger are only reported, never deleted (clean them up manually)"},
	"install.still_referenced":       {zh: "%s@%s 仍被引用（%s），确需删除加 --force", en: "%s@%s is still referenced (%s); add --force to delete anyway"},
	"install.remove_failed":          {zh: "删除 %q 失败（可能仍有进程占用，关掉后重试或用 --force 留残）: %v", en: "cannot delete %q (still in use?); retry after closing it, or use --force and leave the leftovers: %v"},
	"install.no_versions":            {zh: "%s 没有已装版本（`pk list` 看已装工具）", en: "%s has no installed version (see `pk list`)"},
	"install.versions_refused":       {zh: "%s 里这些版本仍被引用，未删除（确需删除加 --force）: %s", en: "some versions of %s are still referenced and were kept (add --force to delete anyway): %s"},
	"install.shim_remove":            {zh: "删除 shim %q 失败: %v", en: "cannot delete shim %q: %v"},
	"lock.load":                      {zh: "读取 pk.lock 失败: %v", en: "cannot read pk.lock: %v"},
	"lock.parse":                     {zh: "解析 pk.lock 失败: %v", en: "cannot parse pk.lock: %v"},
	"lock.encode":                    {zh: "序列化 pk.lock 失败: %v", en: "cannot encode pk.lock: %v"},
	"lock.missing":                   {zh: "--frozen 需要 pk.lock，但项目里没有（先跑一次 `pk install` 生成并入库）", en: "--frozen needs pk.lock, but the project has none (run `pk install` once, then commit the lock)"},
	"lock.mismatch":                  {zh: "锁与清单不一致：清单与 pk.lock 不一致（--frozen 不做解析）：\n  %s\n建议：本地 `pk install` 更新锁并提交", en: "lock mismatch: pk.toml and pk.lock disagree (--frozen never resolves):\n  %s\nsuggestion: run `pk install` locally, then commit the lock"},
	"lock.tool_missing":              {zh: "%s 不在 pk.lock 里（--frozen 不做解析）", en: "%s is not in pk.lock (--frozen never resolves)"},
	"lock.no_install":                {zh: "%s 还没装，无法生成锁（先 `pk install %s`）", en: "%s is not installed, cannot write the lock (run `pk install %s` first)"},
	"lock.sha_mismatch":              {zh: "校验失败: 产物 sha256 与 pk.lock 不一致（锁 %s，实际 %s）", en: "verification failed: artifact sha256 differs from pk.lock (lock %s, actual %s)"},
	"project.read":                   {zh: "读取 %q 失败: %v", en: "cannot read %q: %v"},
	"project.parse":                  {zh: "解析 %q 失败: %v", en: "cannot parse %q: %v"},
	"project.engines_unparsable":     {zh: "package.json 的 engines.node %q 无法解析: %v（改用 pk install node@<版本>，或写进 pk.toml）", en: "cannot parse engines.node %q in package.json: %v (use `pk install node@<version>` or put it in pk.toml)"},

	// ── internal/provider：清单加载校验与 URL 模板 ──
	"provider.unknown_tool":             {zh: "未知工具 %q，可用: %s", en: "unknown tool %q, available: %s"},
	"provider.embed_scan":               {zh: "扫描内嵌 provider 失败: %v", en: "cannot scan embedded providers: %v"},
	"provider.read_manifest":            {zh: "读取 %s 失败: %v", en: "cannot read %s: %v"},
	"provider.parse_manifest":           {zh: "%s: %v", en: "%s: %v"},
	"provider.id_mismatch":              {zh: "%s: id = %q 与文件名不一致", en: "%s: id = %q does not match the file name"},
	"provider.none_embedded":            {zh: "没有内嵌任何 provider", en: "no embedded provider found"},
	"provider.toml":                     {zh: "TOML 解析失败: %v", en: "cannot parse TOML: %v"},
	"provider.missing_id":               {zh: "缺少 id", en: "missing id"},
	"provider.missing_display":          {zh: "%s: 缺少 display", en: "%s: missing display"},
	"provider.index_kind":               {zh: "%s: index.kind = %q 非法（json/registry/dirlist/github-releases/pbs）", en: "%s: illegal index.kind = %q (json/registry/dirlist/github-releases/pbs)"},
	"provider.missing_index_urls":       {zh: "%s: 缺少 index.urls", en: "%s: missing index.urls"},
	"provider.empty_index_url":          {zh: "%s: index.urls.%s 为空", en: "%s: index.urls.%s is empty"},
	"provider.missing_artifact_urls":    {zh: "%s: 缺少 artifact.urls", en: "%s: missing artifact.urls"},
	"provider.ext_missing":              {zh: "%s: artifact.ext 缺少 %s（只允许 tar.gz/zip/tgz）", en: "%s: artifact.ext is missing %s (only tar.gz/zip/tgz)"},
	"provider.ext_invalid":              {zh: "%s: artifact.ext.%s = %q 非法（只允许 tar.gz/zip/tgz）", en: "%s: artifact.ext.%s = %q is illegal (only tar.gz/zip/tgz)"},
	"provider.platform_missing":         {zh: "%s: artifact.platform 缺少 %s", en: "%s: artifact.platform is missing %s"},
	"provider.empty_bins":               {zh: "%s: layout.bins 不能为空", en: "%s: layout.bins must not be empty"},
	"provider.checksum_format":          {zh: "%s: checksum.format = %q 非法", en: "%s: illegal checksum.format = %q"},
	"provider.checksum_mode":            {zh: "%s: checksum.mode = %q 非法（tofu-then-verify/verify/none）", en: "%s: illegal checksum.mode = %q (tofu-then-verify/verify/none)"},
	"provider.missing_checksum_urls":    {zh: "%s: 缺少 checksum.urls（校验源必须与产物源解耦）", en: "%s: missing checksum.urls (the checksum source must be decoupled from the artifact source)"},
	"provider.companion_mode":           {zh: "%s: companions.%s.mode = %q 非法", en: "%s: illegal companions.%s.mode = %q"},
	"provider.companion_fields":         {zh: "%s: companions.%s 缺少 registry/target", en: "%s: companions.%s needs both registry and target"},
	"provider.companion_pinned_version": {zh: "%s: companions.%s 是 pinned，需同时给 version", en: "%s: companions.%s is pinned and needs a version"},
	"provider.no_url_profile":           {zh: "%s: artifact.urls 缺少可用档位", en: "%s: artifact.urls has no usable profile"},
	"provider.artifact_render":          {zh: "%s 产物 URL 渲染失败: %v", en: "cannot render the artifact URL for %s: %v"},
	"provider.no_artifact_source":       {zh: "%s: 没有可用的产物源", en: "%s: no usable artifact source"},
	"provider.checksum_render":          {zh: "%s 校验和 URL 渲染失败: %v", en: "cannot render the checksum URL for %s: %v"},
	"provider.platform_unsupported":     {zh: "%s 不支持平台 %s（可支持: %s）", en: "%s does not support platform %s (supported: %s)"},
	"provider.artifact_platform":        {zh: "%s: 不支持平台 %s", en: "%s: unsupported platform %s"},
	"provider.template_unclosed":        {zh: "URL 模板 %q 有大括号未闭合", en: "URL template %q has an unclosed brace"},
	"provider.template_placeholder":     {zh: "URL 模板 %q 含未知占位符 {%s}", en: "URL template %q contains an unknown placeholder {%s}"},
	"provider.ext_id_mismatch":          {zh: "id = %q 与文件名不一致", en: "id = %q does not match the file name"},
	"provider.ext_pbs":                  {zh: "pbs 索引只支持内嵌清单（解析在 provider 自己的 pbs.go）", en: "the pbs index kind is only supported by embedded manifests"},
	"provider.ext_http":                 {zh: "URL 必须是 https：%s（内网 http 源需 %s=1 放行，且按弱校验处理）", en: "URL must be https: %s (set %s=1 to allow intranet http sources; they are treated as weak verification)"},
	"provider.ext_negative_strip":       {zh: "layout.strip 不能为负数", en: "layout.strip must not be negative"},
	"provider.ext_relative":             {zh: "layout 路径必须是相对路径且不含 ..：%q", en: "layout paths must be relative and must not contain ..: %q"},
	"provider.ext_companion_registry":   {zh: "companions.%s.registry = %q 不是已存在的工具 id", en: "companions.%s.registry = %q is not an existing tool id"},
	"provider.ext_too_large":            {zh: "清单 %d 字节，超过上限 %d（不是 TOML 清单？）", en: "manifest is %d bytes, over the %d limit (is this really a TOML manifest?)"},

	// ── internal/provider：索引解析 ──
	"index.no_source":               {zh: "%s: 没有可用的索引源", en: "%s: no usable index source"},
	"index.all_failed":              {zh: "%s 的版本索引全部不可用：\n  %s\n建议：`pk mirror use cn` 换档，或检查代理设置", en: "every version index for %s is unavailable:\n  %s\nsuggestion: switch profile with `pk mirror use cn`, or check the proxy settings"},
	"index.empty":                   {zh: "%s 的索引是空的（镜像异常？）", en: "the index for %s is empty (mirror anomaly?)"},
	"index.no_lts":                  {zh: "%s 没有标记 LTS 的版本（镜像可能未同步）", en: "%s has no LTS-tagged version (the mirror may be out of sync)"},
	"index.no_stable":               {zh: "%s 索引里没有稳定版本", en: "%s has no stable version in the index"},
	"index.no_match":                {zh: "%s 没有满足 %q 的版本%s", en: "%s has no version matching %q%s"},
	"index.kind_unknown":            {zh: "未知索引形态 %q", en: "unknown index kind %q"},
	"index.not_array":               {zh: "%s 不是 JSON 数组（%v）", en: "%s is not a JSON array (%v)"},
	"index.no_version_dirs":         {zh: "目录列举里没有版本目录", en: "the directory listing has no version directories"},
	"index.registry_parse":          {zh: "registry 元数据解析失败（%v）", en: "cannot parse the registry metadata (%v)"},
	"index.registry_no_versions":    {zh: "registry 元数据里没有 versions", en: "the registry metadata has no versions"},
	"index.pbs_tag_list":            {zh: "%s 的 release 标签列表拉不到: %v", en: "cannot fetch the release tag list for %s: %v"},
	"index.pbs_tag_empty":           {zh: "目录列举里没有 release 标签目录", en: "the directory listing has no release tag directories"},
	"index.pbs_tag_no_artifact":     {zh: "没有可用的 release 标签", en: "no usable release tag"},
	"index.pbs_no_artifact":         {zh: "%s: 目录列举里没找到可用产物: %v", en: "%s: no usable artifact in the directory listing: %v"},
	"index.pbs_no_cpython":          {zh: "没有解析出任何 cpython 版本", en: "no cpython version could be parsed"},
	"index.pbs_tag_no_install_only": {zh: "tag %s 里没有 install_only 产物", en: "tag %s has no install_only artifact"},

	// ── internal/shim 与 internal/companion 的收尾 ──
	"shim.empty_args":                {zh: "shim 名称与目标不能为空", en: "shim name and target must not be empty"},
	"shim.mkdir":                     {zh: "创建 shim 目录失败: %v", en: "cannot create the shim directory: %v"},
	"shim.unparsable":                {zh: "shim %q 内容无法解析（缺少目标路径）", en: "cannot parse shim %q (target path missing)"},
	"shim.read":                      {zh: "读取 shim %q 失败: %v", en: "cannot read shim %q: %v"},
	"shim.symlink":                   {zh: "创建符号链接 %q 失败: %v", en: "cannot create symlink %q: %v"},
	"shim.replace":                   {zh: "替换 shim %q 失败: %v", en: "cannot replace shim %q: %v"},
	"shim.write":                     {zh: "写 shim %q 失败: %v", en: "cannot write shim %q: %v"},
	"companion.pinned_needs_version": {zh: `companion.mode=pinned 需要显式 npm 版本（pk.toml 里写 npm = "<版本>"）`, en: `companion.mode=pinned needs an explicit npm version (write npm = "<version>" in pk.toml)`},
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
				// Cause 本身也是编码错误时递归渲染（否则外层模板里会漏出 `store.config_parse(...)` 这种机器串）
				var tail any = ce.Cause
				if _, nested := coded.Of(ce.Cause); nested {
					tail = Text(ce.Cause)
				}
				args = append(append([]any{}, args...), tail)
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

// safeSprintf 兜住"模板与参数个数不符"：panic 与 `%!s(MISSING)` 都算失败，
// 调用方会退回紧凑形式（宁可露出 `code(args)`，也不要把 `%!s(MISSING)` 打给用户）。
func safeSprintf(tpl string, args []any) (out string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			out, ok = "", false
		}
	}()
	out = fmt.Sprintf(tpl, args...)
	if strings.Contains(out, "%!") {
		return "", false
	}
	return out, true
}

// Code 按码表渲染一句话（当前语言）。
//
// 给 jsonout.Event 的 MsgCode/MsgArgs 用：`internal/*` 只给码与参数，人话在这里生成，
// 这样 `--lang en` 下 msg 也跟着变英文，且文案只有一份（不散在内部包里）。
// 码没登记或参数对不上时返回空串：调用方应保留自己的兑底，不要把机器串打给用户。
func Code(code string, args ...any) string {
	entry, ok := codeTable[code]
	if !ok {
		return ""
	}
	tpl := entry.zh
	if LangIsEN() {
		tpl = entry.en
	}
	out, ok := safeSprintf(tpl, args)
	if !ok {
		return ""
	}
	return out
}

// Codes 返回已登记的问题码（测试用：断言"定义过的码都有文案"）。
func Codes() []string {
	out := make([]string, 0, len(codeTable))
	for code := range codeTable {
		out = append(out, code)
	}
	return out
}
