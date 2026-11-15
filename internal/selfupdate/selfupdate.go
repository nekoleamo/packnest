// Package selfupdate 让 `pk` 自己检查并升级（`pk upgrade --self`）。
//
// 设计约束（来自方案与既有拍板）：
//   - **命令面不扩**：走 `pk upgrade --self`（一个开关），不新增第 11 个命令；
//   - **渠道自知**：brew / scoop / npm 装的 pk 不自我替换 —— 那会让包管理器的 hash 台账失真，
//     只给出该渠道的升级命令；脚本装 / 手动放的才自己动手（`--force` 可强行覆盖判断）；
//   - **地址自动选**：候选源（gitee / GitHub）先用 `mirror.Prober` 排序（带 6h 缓存），
//     最快的优先；探测全挂就按默认顺序逐个试 —— 国内直连 gitee、海外 GitHub 更快；
//   - **校验不打折**：`checksums.txt` 必须验出 sshsig（内置公钥）+ 产物 sha256，任一失败就停，
//     绝不"装一个没验过的 pk"。验签用 Go 实现（`internal/verify`），不依赖 ssh-keygen；
//   - **原子替换**：先写同目录的临时文件再 rename；Windows 上 rename 不能覆盖正在运行的文件，
//     退化成 `exe → exe.old` + `新文件 → exe`，`.old` 下次启动清理（见 CleanupOld）。
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/coded"
	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/install"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/paths"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/verify"
)

// 发布仓库（与 scripts/install.sh 的默认值保持一致）。
const (
	GiteeRepo  = "null_593_5354/packnest"
	GitHubRepo = "nekoleamo/packnest"
)

// Option 是检查/升级所需的全部依赖（都可注入，测试零外网）。
type Option struct {
	// Current 是当前版本（由 cmd 注入 main.Version）。
	Current string
	// Exe 是当前可执行文件路径（默认 os.Executable）。
	Exe string
	// GOOS / GOARCH 默认取 runtime。
	GOOS, GOARCH string
	Client       *fetch.Client
	Paths        paths.Paths
	// ProbeFile 是源探测缓存（默认 Paths.Probe()）。
	ProbeFile string
	Now       func() time.Time
	// Log 收进度行（CLI 的 --json 模式下不设）。
	Log func(string)
	// Sources 覆盖候选源（测试用）；默认 gitee → GitHub。
	Sources []Source
	// TrustedKeys 覆盖内置发布公钥（测试用；为空则用 verify.TrustedKeys()）。
	TrustedKeys []verify.SSHPublicKey
}

// Source 是一个发布源：下载基地址 + "最新版本"文本地址 + 探测到的延迟。
type Source struct {
	Name      string `json:"name"`
	Base      string `json:"base"`
	LatestURL string `json:"latest_url"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// Info 是检查/升级结果（`--json` 直接输出它）。
type Info struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	Channel         string `json:"channel"`
	ChannelHint     string `json:"channel_hint,omitempty"`
	Source          string `json:"source,omitempty"`
	// DirURL 是这个版本在发布源上的目录（含 checksums.txt / .sig）；ArchiveURL 是其中一个产物。
	DirURL     string   `json:"dir_url,omitempty"`
	ArchiveURL string   `json:"archive_url,omitempty"`
	Sources    []Source `json:"sources,omitempty"`
	// Applied 表示这次真的替换了二进制（--check / 已是最新时为 false）。
	Applied bool `json:"applied"`
}

// DefaultSources 返回内置的两个发布源（gitee 国内主源 / GitHub 海外主源）。
func DefaultSources() []Source {
	return []Source{
		{
			Name:      "gitee",
			Base:      "https://gitee.com/" + GiteeRepo + "/releases/download",
			LatestURL: "https://gitee.com/" + GiteeRepo + "/raw/main/scripts/latest",
		},
		{
			Name:      "github",
			Base:      "https://github.com/" + GitHubRepo + "/releases/download",
			LatestURL: "https://raw.githubusercontent.com/" + GitHubRepo + "/main/scripts/latest",
		},
	}
}

// Check 只查不装：挑源 → 取最新版本 → 比 semver → 查渠道能不能自更新。
func Check(ctx context.Context, opt Option) (Info, error) {
	opt = opt.withDefaults()
	src, latency, err := pickSource(ctx, opt)
	if err != nil {
		return Info{}, err
	}
	opt.log(fmt.Sprintf("源：%s（%s）", src.Name, latencyText(latency)))

	latest, err := latestVersion(ctx, opt, src, opt.Current)
	if err != nil {
		return Info{}, err
	}
	info := Info{Current: opt.Current, Latest: latest, Source: src.Name}
	info.Sources = rankedSources(opt, src, latency)
	info.UpdateAvailable = newer(latest, opt.Current)

	ch := install.DetectInstallChannel(opt.Exe, opt.GOOS)
	info.Channel, info.ChannelHint = ch.Name, ch.Hint
	if info.UpdateAvailable {
		info.DirURL = releaseURL(src, latest)
		info.ArchiveURL = info.DirURL + "/" + artifactName(latest, opt.GOOS, opt.GOARCH)
	}
	return info, nil
}

// Apply 在 Check 之后真正替换自身；渠道受管（brew/scoop/npm）时拒绝并给出该渠道的命令。
func Apply(ctx context.Context, opt Option) (Info, error) { return apply(ctx, opt, false) }

// ApplyForce 是 `pk upgrade --self --force`：忽略渠道判断强行自替换
// （典型场景：brew 装过、后来手动换成脚本版，路径仍像 brew）。
func ApplyForce(ctx context.Context, opt Option) (Info, error) { return apply(ctx, opt, true) }

func apply(ctx context.Context, opt Option, force bool) (Info, error) {
	opt = opt.withDefaults()
	info, err := Check(ctx, opt)
	if err != nil {
		return Info{}, err
	}
	if !info.UpdateAvailable {
		return info, nil
	}
	if !force && !selfInstallable(info.Channel) {
		return info, coded.New(CodeChannelManaged, info.Channel, info.ChannelHint)
	}

	tmp, err := os.MkdirTemp("", "pk-selfupdate-")
	if err != nil {
		return info, coded.Wrap(err, CodeTempDir)
	}
	defer os.RemoveAll(tmp)

	verDir := info.DirURL
	artifact := artifactName(info.Latest, opt.GOOS, opt.GOARCH)
	opt.log(fmt.Sprintf("下载 %s（%s）", artifact, info.Source))

	archivePath := filepath.Join(tmp, artifact)
	if _, err := opt.Client.Download(ctx, info.ArchiveURL, archivePath); err != nil {
		return info, coded.Wrap(err, CodeDownload, info.ArchiveURL)
	}
	sumsPath := filepath.Join(tmp, "checksums.txt")
	if _, err := opt.Client.Download(ctx, verDir+"/checksums.txt", sumsPath); err != nil {
		return info, coded.Wrap(err, CodeDownload, verDir+"/checksums.txt")
	}
	sigPath := filepath.Join(tmp, "checksums.txt.sig")
	if _, err := opt.Client.Download(ctx, verDir+"/checksums.txt.sig", sigPath); err != nil {
		return info, coded.Wrap(err, CodeDownload, verDir+"/checksums.txt.sig")
	}
	if err := verifyReleaseSums(sumsPath, sigPath, opt.TrustedKeys); err != nil {
		return info, err
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return info, coded.Wrap(err, CodeReadSums, sumsPath)
	}
	wantSum := sumFor(sums, artifact)
	if wantSum == "" {
		return info, coded.New(CodeNoSumForArtifact, artifact)
	}
	gotSum, err := sha256File(archivePath)
	if err != nil {
		return info, coded.Wrap(err, CodeReadArtifact, archivePath)
	}
	if gotSum != wantSum {
		return info, coded.New(CodeSumMismatch, artifact, wantSum, gotSum)
	}

	// 解压：发布包就是"一个 packnest 可执行文件"（tar.gz 单文件 / zip 单文件）
	extractDir := filepath.Join(tmp, "unpacked")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return info, coded.Wrap(err, CodeTempDir)
	}
	// 格式从完整文件名判断：`.tar.gz` 用 filepath.Ext 只会拿到 `.gz`
	format, err := archive.FormatFor(strings.TrimPrefix(artifact, "packnest_"+strings.TrimPrefix(info.Latest, "v")+"_"+opt.GOOS+"_"+opt.GOARCH+"."))
	if err != nil {
		return info, coded.Wrap(err, CodeArchiveFormat, artifact)
	}
	if err := archive.Extract(archivePath, extractDir, format, archive.Layout{}, opt.GOOS); err != nil {
		return info, coded.Wrap(err, CodeExtract, artifact)
	}
	binName := "packnest"
	if opt.GOOS == "windows" {
		binName = "packnest.exe"
	}
	newBin, err := findBin(extractDir, binName)
	if err != nil {
		return info, coded.Wrap(err, CodeExtractLayout, binName)
	}
	if err := replaceSelf(opt.Exe, newBin, opt.GOOS); err != nil {
		return info, err
	}
	info.Applied = true
	opt.log("已升级到 " + info.Latest + "：" + opt.Exe)
	return info, nil
}

// CleanupOld 清掉上次 Windows 替换留下的 `<exe>.old*`（进程启动时调用，失败无妨）。
func CleanupOld(exe, goos string) {
	if exe == "" || goos != "windows" {
		return
	}
	dir := filepath.Dir(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	base := filepath.Base(exe)
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), base+".old") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ── 内部实现 ───────────────────────────────────────────────────────────

func (o Option) withDefaults() Option {
	if o.Exe == "" {
		if exe, err := os.Executable(); err == nil {
			o.Exe = exe
		}
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Client == nil {
		o.Client = fetch.New()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if len(o.Sources) == 0 {
		o.Sources = DefaultSources()
	}
	return o
}

func (o Option) log(msg string) {
	if o.Log != nil {
		o.Log(msg)
	}
}

func (o Option) probeFile() string {
	if o.ProbeFile != "" {
		return o.ProbeFile
	}
	return o.Paths.Probe()
}

// pickSource 用 pk 自己的探测缓存给候选源排序：最快的优先；探测全挂就按默认顺序。
func pickSource(ctx context.Context, opt Option) (Source, int64, error) {
	urls := make([]string, 0, len(opt.Sources))
	for _, s := range opt.Sources {
		urls = append(urls, s.LatestURL)
	}
	prober := &mirror.Prober{Client: opt.Client, Path: opt.probeFile(), TTL: mirror.DefaultTTL, Now: opt.Now}
	res, err := prober.Select(ctx, urls)
	if err != nil {
		if len(opt.Sources) == 0 {
			return Source{}, 0, coded.Wrap(err, CodeNoSource)
		}
		return opt.Sources[0], 0, nil // 探不到不是错误：latestVersion 会逐个回退
	}
	for _, s := range opt.Sources {
		if s.LatestURL == res.URL {
			return s, res.LatencyMS, nil
		}
	}
	return opt.Sources[0], res.LatencyMS, nil
}

// rankedSources 把"选中源 + 延迟"整理给调用方（GUI 展示"从哪下、多快"）。
func rankedSources(opt Option, chosen Source, chosenLatency int64) []Source {
	out := make([]Source, 0, len(opt.Sources))
	for _, s := range opt.Sources {
		if s.Name == chosen.Name {
			s.LatencyMS = chosenLatency
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == chosen.Name {
			return true
		}
		if out[j].Name == chosen.Name {
			return false
		}
		return out[i].LatencyMS < out[j].LatencyMS
	})
	return out
}

// latestVersion 取最新版本号：先问选中源，失败再按顺序问其余源；结果缓存 6h。
// latestVersion 取最新版本号：先问选中源，失败再按顺序问其余源。
//
// 缓存规则（6h）里有个坑：刚发完版时缓存还是"上一个版本"，于是会报"已是最新"。
// 所以只在本机版本**不比缓存旧**时才信任缓存（`current >= cached`）；
// 一旦发现本机比缓存还新（说明发版了），就绕过缓存重新问一次网络。
func latestVersion(ctx context.Context, opt Option, src Source, current string) (string, error) {
	cacheDir := ""
	if opt.Paths.Root != "" {
		cacheDir = opt.Paths.CacheDir("selfupdate")
	}
	cacheFile := ""
	if cacheDir != "" {
		cacheFile = filepath.Join(cacheDir, "latest-"+src.Name)
		if v, ok := cachedVersion(cacheFile, opt.Now()); ok && !newer(current, v) {
			return v, nil
		}
	}
	tried := []string{src.LatestURL}
	for _, s := range opt.Sources {
		if s.LatestURL != src.LatestURL {
			tried = append(tried, s.LatestURL)
		}
	}
	var lastErr error
	for _, u := range tried {
		data, err := opt.Client.Get(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		v := strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
		if !validVersion(v) {
			lastErr = coded.New(CodeBadVersion, u, strings.TrimSpace(string(data)))
			continue
		}
		if cacheFile != "" {
			_ = os.MkdirAll(cacheDir, 0o755)
			_ = os.WriteFile(cacheFile, []byte(v), 0o644)
		}
		return v, nil
	}
	if lastErr == nil {
		lastErr = coded.New(CodeNoSource)
	}
	return "", coded.Wrap(lastErr, CodeNoLatest)
}

// cachedVersion 读 6h 内的版本缓存（重复检查不至于每次打网络）。
func cachedVersion(path string, now time.Time) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || now.Sub(st.ModTime()) > mirror.DefaultTTL {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(data))
	if !validVersion(v) {
		return "", false
	}
	return v, true
}

// verifyReleaseSums 验 checksums.txt 的 sshsig（内置公钥）。
//
// 用 Go 实现而不是调 ssh-keygen：Windows 上 ssh-keygen 是可选功能，
// 而"升级自身"这一步不能因为缺个工具就降级成只比 sha256。
func verifyReleaseSums(sumsPath, sigPath string, override []verify.SSHPublicKey) error {
	keys := override
	if len(keys) == 0 {
		keys = verify.TrustedKeys()
	}
	if len(keys) == 0 {
		return coded.New(CodeNoTrustKey)
	}
	msg, err := os.ReadFile(sumsPath)
	if err != nil {
		return coded.Wrap(err, CodeReadSums, sumsPath)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return coded.Wrap(err, CodeReadSums, sigPath)
	}
	if err := verify.VerifySSHSIG(msg, sig, keys); err != nil {
		return coded.Wrap(err, CodeSignature)
	}
	return nil
}

// replaceSelf 原子替换可执行文件，并同步同目录里内容相同的 `pk` / `packnest` 副本
// （Windows 安装脚本复制两份；posix 脚本是软链，替换本体即可）。
func replaceSelf(exe, newBin, goos string) error {
	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	oldSum, _ := sha256File(exe)

	tmpNew := filepath.Join(dir, "."+base+".new."+randomSuffix())
	if err := copyFile(newBin, tmpNew, 0o755); err != nil {
		return coded.Wrap(err, CodeWriteNew, tmpNew)
	}
	if goos != "windows" {
		if err := os.Rename(tmpNew, exe); err != nil {
			_ = os.Remove(tmpNew)
			return coded.Wrap(err, CodeReplace, exe)
		}
	} else {
		oldPath := exe + ".old." + randomSuffix()
		if err := os.Rename(exe, oldPath); err != nil {
			_ = os.Remove(tmpNew)
			return coded.Wrap(err, CodeReplace, exe)
		}
		if err := os.Rename(tmpNew, exe); err != nil {
			_ = os.Rename(oldPath, exe) // 还原，别让用户没有 pk
			_ = os.Remove(tmpNew)
			return coded.Wrap(err, CodeReplace, exe)
		}
		_ = os.Remove(oldPath)
	}

	if goos == "windows" && oldSum != "" {
		for _, name := range []string{"pk.exe", "packnest.exe"} {
			p := filepath.Join(dir, name)
			if filepath.Clean(p) == filepath.Clean(exe) {
				continue
			}
			if sum, err := sha256File(p); err == nil && sum == oldSum {
				_ = copyFile(exe, p, 0o755)
			}
		}
	}
	return nil
}

// sumFor 在 checksums.txt 里找某个文件的 sha256（容忍 `*文件名` 的二进制模式）。
func sumFor(sums []byte, name string) string {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

// releaseURL 返回某个源的某个版本的目录 URL（不带尾部 /）。
func releaseURL(src Source, ver string) string {
	return strings.TrimSuffix(src.Base, "/") + "/v" + strings.TrimPrefix(strings.TrimPrefix(ver, "v"), "v")
}

func findBin(root, name string) (string, error) {
	var found string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), name) && found == "" {
			found = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("not found: " + name)
	}
	return found, nil
}

func artifactName(ver, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("packnest_%s_%s_%s.%s", strings.TrimPrefix(ver, "v"), goos, goarch, ext)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func randomSuffix() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

func validVersion(v string) bool {
	_, err := spec.ParseVersion(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	return err == nil
}

// newer 判断 latest 是否比 current 新（解析不了就不动，宁可少提示）。
func newer(latest, current string) bool {
	lv, err := spec.ParseVersion(strings.TrimPrefix(latest, "v"))
	if err != nil {
		return false
	}
	cv, err := spec.ParseVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return true // 当前是 dev / 空：远端有正式版本就提示
	}
	return lv.GreaterThan(cv)
}

func latencyText(ms int64) string {
	if ms <= 0 {
		return "未知延迟"
	}
	return fmt.Sprintf("%dms", ms)
}

// selfInstallable 判断这个渠道能不能安全地自我替换。
func selfInstallable(channel string) bool {
	switch channel {
	case "brew", "scoop", "npm":
		return false
	}
	return true
}
