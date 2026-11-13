// Package verify 处理校验和：解析、TOFU 表、多源交叉验证（方案 §6.4 + §18.6）。
//
// 为什么不能"每次取校验和"：npmmirror 的 node 目录没有 SHASUMS256.txt（实测 404），
// 校验源必须与产物源解耦；而每次装同版本都去求校验和，等于把网络依赖翻倍。
// 所以：首次装用 N 个源交叉验证并写进 state/known.json，之后同版本**完全离线可验证**。
package verify

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/paths"
)

// 校验强度标记（写进台账与 `pk list`）。
const (
	VerifiedStrong     = "strong"     // ≥2 个源给出相同 sha256
	VerifiedWeak       = "weak"       // 只有 1 个源可用
	VerifiedUnverified = "unverified" // --insecure 放行
)

// ErrChecksum 表示校验失败（CLI 映射退出码 4）。
var ErrChecksum = errors.New("校验失败")

// KnownSchema 是 TOFU 表格式版本。
const KnownSchema = 1

// DefaultMinSources 是首次安装要求的最小校验源数（方案 §18.6）。
const DefaultMinSources = 2

// 支持的摘要算法。registry 的 dist.shasum 是 sha1，dist.integrity 是 sha512 SRI，
// 产物校验和文件是 sha256 —— TOFU 表必须记住用的是哪一种，否则没法比对。
const (
	AlgoSHA256 = "sha256"
	AlgoSHA1   = "sha1"
	AlgoSHA512 = "sha512"
)

// Known 是 state/known.json。
type Known struct {
	Schema  int              `json:"schema"`
	Entries map[string]Entry `json:"entries"`
}

// Entry 是 TOFU 表里的一条。
type Entry struct {
	Digest    string    `json:"digest"`
	Algo      string    `json:"algo"`
	Verified  string    `json:"verified"`
	Sources   int       `json:"sources"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Key 生成 TOFU 表的键：tool@version/platform。
func Key(tool, version, platform string) string {
	return tool + "@" + version + "/" + platform
}

// LoadKnown 读取 TOFU 表；不存在返回空表。
func LoadKnown(path string) (*Known, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Known{Schema: KnownSchema, Entries: map[string]Entry{}}, nil
		}
		return nil, fmt.Errorf("读取校验表失败: %w", err)
	}
	var k Known
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("校验表 %q 损坏: %w", path, err)
	}
	if k.Entries == nil {
		k.Entries = map[string]Entry{}
	}
	k.Schema = KnownSchema
	return &k, nil
}

// Get 查一条。
func (k *Known) Get(key string) (Entry, bool) {
	e, ok := k.Entries[key]
	return e, ok
}

// Save 原子写 TOFU 表。
func (k *Known) Save(path string) error {
	k.Schema = KnownSchema
	if k.Entries == nil {
		k.Entries = map[string]Entry{}
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteAtomic(path, data)
}

// SHA256File 计算文件 sha256（十六进制小写）。
func SHA256File(path string) (string, error) {
	return DigestFile(path, AlgoSHA256)
}

// DigestFile 按算法计算摘要：sha256/sha1 返回十六进制，sha512 返回 SRI（"sha512-<base64>"）。
func DigestFile(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开产物失败: %w", err)
	}
	defer f.Close()

	var h hash.Hash
	switch algo {
	case AlgoSHA1:
		h = sha1.New()
	case AlgoSHA512:
		h = sha512.New()
	default:
		h = sha256.New()
	}
	buf := make([]byte, 128*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("读取产物失败: %w", err)
		}
	}
	if algo == AlgoSHA512 {
		return "sha512-" + base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ParseGNUSums 解析 "SHASUMS256.txt" 风格：每行 "<sha256>  <文件名>"。
//
// 容忍 "*文件名"（sha256sum -b 的输出）与前后的空行。
func ParseGNUSums(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 || !isHex(sum) {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		out[name] = sum
	}
	return out
}

// ParsePerArtifact 解析单文件校验和（`<产物>.sha256sum` 里只有一个哈希）。
func ParsePerArtifact(data []byte) (string, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) == 64 && isHex(sum) {
			return sum, true
		}
	}
	return "", false
}

// NormalizeRegistryShasum 归一化 npm registry 的 dist.shasum。
func NormalizeRegistryShasum(shasum string) string {
	return strings.ToLower(strings.TrimSpace(shasum))
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Request 是一次校验请求。
type Request struct {
	Tool     string
	Version  string
	Platform string // 如 darwin-arm64
	// Artifact 是校验和文件里对应的文件名（如 node-v22.22.2-darwin-arm64.tar.gz）。
	Artifact string
	// ArtifactURL 是产物的实际下载地址（同源配对校验用：与产物同 host 的校验源可信度更高）。
	ArtifactURL string
	// Digest 是实际产物的摘要（算法见 Algo）。
	Digest string
	// Algo 是摘要算法：sha256（默认）/ sha1（registry shasum）/ sha512（registry integrity）。
	Algo string
	// SumsURLs 是校验和来源（已按镜像回退链排序）。
	SumsURLs []string
	// Format 来自 provider：gnu-sums | per-artifact | registry-integrity。
	Format string
	// InlineShasum 是 registry 元数据里自带的值（registry-integrity 用）。
	InlineShasum string
	// Insecure 允许无校验安装（标记 unverified）。
	Insecure bool
	// InsecureTransport 表示产物清单里有被放行的 http:// 源：校验源也走 http 时最多只能 weak。
	InsecureTransport bool
	// Rehash 表示忽略本地 TOFU 记录、重新向校验源求值并覆盖记录（`--force-rehash`）。
	// 用于上游重新发布了同一版本、或本地表被写脏的场景。
	Rehash bool
}

// Result 是校验结果。
type Result struct {
	// Mode 是本次判定所依据的来源：tofu | network | insecure。
	Mode     string
	Verified string
	Digest   string
	Algo     string
	Sources  int
	// Note 是欣得提醒用户的人类可读说明（如"上游重新发布，按同源配对接受"）。
	Note string
}

// Verifier 执行校验。Fetch 与 Now 可注入，测试零外网。
type Verifier struct {
	Known      *Known
	Fetch      func(ctx context.Context, url string) ([]byte, error)
	Now        func() time.Time
	MinSources int
}

// New 构造 Verifier。
func New(known *Known) *Verifier {
	if known == nil {
		known = &Known{Schema: KnownSchema, Entries: map[string]Entry{}}
	}
	return &Verifier{Known: known, Now: time.Now, MinSources: DefaultMinSources}
}

// Verify 按"TOFU 优先、首次交叉验证"的顺序判定。
func (v *Verifier) Verify(ctx context.Context, req Request) (Result, error) {
	algo := req.Algo
	if algo == "" {
		algo = AlgoSHA256
	}
	if req.Digest == "" {
		return Result{}, fmt.Errorf("%w: 缺少产物摘要", ErrChecksum)
	}
	key := Key(req.Tool, req.Version, req.Platform)

	// 1. 已知版本：只比本地表，零网络（离线可验证）。--force-rehash 跳过这一步。
	if entry, ok := v.Known.Get(key); ok && !req.Rehash {
		if entryAlgo := entry.Algo; entryAlgo != "" && entryAlgo != algo {
			return Result{}, fmt.Errorf("%w: %s 的摘要算法变了（记录 %s，现在 %s）；确需覆盖用 --force-rehash",
				ErrChecksum, key, entryAlgo, algo)
		}
		if !strings.EqualFold(entry.Digest, req.Digest) {
			// 本地记录与实际不符：可能是上游重新发布了同一版本。只要与产物同源的校验源也同意新摘要，
			// 就按 weak 接受并改写记录（同源配对）；否则保留原来那条"要求 --force-rehash"的报错。
			if res, ok := v.sameOriginRepublish(ctx, req, algo, key); ok {
				return res, nil
			}
			return Result{}, fmt.Errorf("%w: %s 的 %s 与本地记录不一致（记录 %s，实际 %s）；确需覆盖用 --force-rehash",
				ErrChecksum, key, algo, entry.Digest, req.Digest)
		}
		return Result{Mode: "tofu", Verified: entry.Verified, Digest: req.Digest, Algo: algo, Sources: entry.Sources}, nil
	}

	// 2. 首次安装：收集所有源的期望值。
	expected, sources, httpSums, err := v.collect(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if sources == 0 {
		if req.Insecure {
			v.record(key, Entry{Digest: req.Digest, Algo: algo, Verified: VerifiedUnverified, Sources: 0})
			return Result{Mode: "insecure", Verified: VerifiedUnverified, Digest: req.Digest, Algo: algo}, nil
		}
		return Result{}, fmt.Errorf("%w: 拿不到 %s 的校验和（所有源都不可用）；确认风险后可加 --insecure", ErrChecksum, key)
	}
	if len(expected) > 1 {
		// 源之间不一致有两种可能：镜像被投毒，或上游重新发布过同一版本。
		// 同源配对：与产物同 host 的校验源也同意"当前产物" 时，按 weak 接受（并改写 TOFU）。
		if res, ok := v.sameOriginRepublish(ctx, req, algo, key); ok {
			return res, nil
		}
		// 按方案 §18.6 一律硬失败，但把"产物与哪个源一致"写清楚，让用户能判断该不该 --insecure。
		matched := "与任何源都不一致"
		for sum := range expected {
			if strings.EqualFold(sum, req.Digest) {
				matched = "产物与其中 " + sum[:12] + "… 一致"
			}
		}
		if !req.Insecure {
			return Result{}, fmt.Errorf("%w: %s 的校验和源之间不一致（%s；%s）——可能是镜像被污染，也可能是上游重新发布了产物；确认来源可信后可加 --insecure",
				ErrChecksum, key, describeMismatch(expected), matched)
		}
		// --insecure：放行但标记未校验，且不写进 TOFU 表（否则会把可疑摘要固化下来）。
		return Result{Mode: "insecure", Verified: VerifiedUnverified, Digest: req.Digest, Algo: algo, Sources: sources}, nil
	}
	want := ""
	for _, sum := range expected {
		want = sum
	}
	if want != "" && !strings.EqualFold(want, req.Digest) {
		return Result{}, fmt.Errorf("%w: %s 的产物 %s 不符（期望 %s，实际 %s）；产物可能被篡改或损坏", ErrChecksum, key, algo, want, req.Digest)
	}

	verified := VerifiedWeak
	if sources >= v.minSources() && !httpSums {
		verified = VerifiedStrong
	}
	v.record(key, Entry{Digest: req.Digest, Algo: algo, Verified: verified, Sources: sources})
	return Result{Mode: "network", Verified: verified, Digest: req.Digest, Algo: algo, Sources: sources}, nil
}

// sameOriginRepublish 处理"上游重新发布了同一版本"（产物变了，同源校验和也跟着变）。
//
// 放行条件必须同时满足两条，否则就是"镜像把产物和校验和一起换了"（那只能靠 --insecure）：
//  1. 与产物**同 host** 的校验源认可当前产物摘要；
//  2. 另一个 host 的校验源还在给**别的**摘要（典型是旧摘要，说明它是没同步的镜像而不是攻击者）。
//
// 这种"跨源分裂"才是上游重发的特征；只有同源一家之言时保持硬失败。
func (v *Verifier) sameOriginRepublish(ctx context.Context, req Request, algo, key string) (Result, bool) {
	host := hostOf(req.ArtifactURL)
	if host == "" || v.Fetch == nil {
		return Result{}, false
	}
	sameOriginAgrees, otherOriginDiffers := false, false
	for _, u := range req.SumsURLs {
		data, err := v.Fetch(ctx, u)
		if err != nil {
			continue
		}
		sum, ok := pickSum(req.Format, data, req.Artifact)
		if !ok {
			continue
		}
		if strings.EqualFold(sum, req.Digest) {
			sameOriginAgrees = sameOriginAgrees || hostOf(u) == host
			continue
		}
		otherOriginDiffers = otherOriginDiffers || hostOf(u) != host
	}
	if !sameOriginAgrees || !otherOriginDiffers {
		return Result{}, false
	}
	v.record(key, Entry{Digest: req.Digest, Algo: algo, Verified: VerifiedWeak, Sources: 1})
	return Result{Mode: "network", Verified: VerifiedWeak, Digest: req.Digest, Algo: algo, Sources: 1,
		Note: "上游重新发布了同一版本的产物（同源 " + host + " 已更新，其它源还是旧摘要），按 weak 接受"}, true
}

// pickSum 从校验和文件里取出本产物的摘要（per-artifact 单文件 / gnu-sums 多行）。
func pickSum(format string, data []byte, artifact string) (string, bool) {
	if format == "per-artifact" {
		return ParsePerArtifact(data)
	}
	sum, ok := ParseGNUSums(data)[artifact]
	return sum, ok
}

// hostOf 取 URL 的 host（解析失败返回空串：调用方按"不同源"处理）。
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// collect 拉取全部校验和源，返回期望哈希集合与成功源数量。
func (v *Verifier) collect(ctx context.Context, req Request) (map[string]string, int, bool, error) {
	expected := map[string]string{}
	sources := 0
	algo := req.Algo
	if algo == "" {
		algo = AlgoSHA256
	}

	if req.Format == "index-inline" {
		// 校验和直接来自版本索引（go 的 dl JSON），sha256 十六进制。
		if sum := strings.ToLower(strings.TrimSpace(req.InlineShasum)); len(sum) == 64 && isHex(sum) {
			if algo == AlgoSHA256 {
				expected[sum] = sum
				sources++
			}
		} else if sum != "" {
			return nil, 0, false, fmt.Errorf("%w: 索引里的 sha256 格式非法: %q", ErrChecksum, req.InlineShasum)
		}
	}
	if req.Format == "registry-integrity" {
		// registry 给的可能是 sha1（dist.shasum，40 位）或 sha512 SRI（dist.integrity）。
		if sum := strings.TrimSpace(req.InlineShasum); sum != "" {
			switch {
			case len(sum) == 40 && isHex(sum):
				if algo == AlgoSHA1 {
					expected[strings.ToLower(sum)] = strings.ToLower(sum)
					sources++
				}
			case strings.HasPrefix(sum, "sha512-"):
				if algo == AlgoSHA512 {
					expected[sum] = sum
					sources++
				}
			case len(sum) == 64 && isHex(sum):
				if algo == AlgoSHA256 {
					expected[strings.ToLower(sum)] = strings.ToLower(sum)
					sources++
				}
			default:
				return nil, 0, false, fmt.Errorf("%w: registry 提供的 shasum 格式非法: %q", ErrChecksum, sum)
			}
		}
	}

	httpSums := false
	fetchFn := v.Fetch
	if fetchFn == nil {
		return expected, sources, false, nil
	}
	minSources := v.minSources()
	for _, url := range req.SumsURLs {
		// 已经拿到足够多且互相一致的源时停手：省下的是真实用户的一次小文件请求。
		if sources >= minSources && len(expected) <= 1 {
			break
		}
		data, err := fetchFn(ctx, url)
		if err != nil {
			continue // 单源失败不影响其他源（镜像缺校验和是常态）
		}
		if req.InsecureTransport && !strings.HasPrefix(url, "https://") {
			httpSums = true // http 上的校验源不可信：产物与摘要可以一起被换掉（方案 §A.4.1）
		}
		if algo != AlgoSHA256 {
			continue // 校验和文件都是 sha256；算法不一致时不用它
		}
		switch req.Format {
		case "per-artifact":
			if sum, ok := ParsePerArtifact(data); ok {
				expected[sum] = sum
				sources++
			}
		default:
			sums := ParseGNUSums(data)
			if sum, ok := sums[req.Artifact]; ok {
				expected[sum] = sum
				sources++
			}
		}
	}
	return expected, sources, httpSums, nil
}

func (v *Verifier) minSources() int {
	if v.MinSources <= 0 {
		return DefaultMinSources
	}
	return v.MinSources
}

func (v *Verifier) record(key string, entry Entry) {
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = v.now()
	}
	if v.Known == nil {
		v.Known = &Known{Schema: KnownSchema, Entries: map[string]Entry{}}
	}
	if v.Known.Entries == nil {
		v.Known.Entries = map[string]Entry{}
	}
	v.Known.Entries[key] = entry
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func describeMismatch(expected map[string]string) string {
	sums := make([]string, 0, len(expected))
	for sum := range expected {
		sums = append(sums, sum)
	}
	return strings.Join(sums, " / ")
}
