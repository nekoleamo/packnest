// Package mirror 管理镜像档位、回退链与探测（方案 §2、§6.3）。
//
// 与 proto 的差别：档位是**预置的**（不需要用户写正则重写 URL），且探测结果
// 有 6 小时 TTL、普通命令只探 2 个候选（不为了"最优"把命令拖成 60 秒）。
package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/paths"
)

// AutoProfile 表示"自动选择档位"。
const AutoProfile = "auto"

// DefaultTTL 是探测结果缓存时长（方案 §6.3 第 4 条）。
const DefaultTTL = 6 * time.Hour

// DefaultProbeTimeout 是单次 HEAD 探测超时（普通命令的快速探测）。
const DefaultProbeTimeout = 500 * time.Millisecond

// Profile 是一个镜像档位。
type Profile struct {
	Name     string `json:"name"`
	Display  string `json:"display"`
	Registry string `json:"registry"`  // npm registry 基地址
	GoProxy  string `json:"go_proxy"`  // GOPROXY
	PipIndex string `json:"pip_index"` // PIP_INDEX_URL
	Note     string `json:"note,omitempty"`
}

// builtins 是内嵌档位表，顺序即回退优先级。
func builtins() []Profile {
	return []Profile{
		{
			Name:     "default",
			Display:  "官方源",
			Registry: "https://registry.npmjs.org",
			GoProxy:  "https://proxy.golang.org",
			PipIndex: "https://pypi.org/simple",
			Note:     "境外直连，无代理时可能不可达",
		},
		{
			Name:     "cn",
			Display:  "npmmirror（阿里）",
			Registry: "https://registry.npmmirror.com",
			GoProxy:  "https://goproxy.cn",
			PipIndex: "https://mirrors.aliyun.com/pypi/simple",
			Note:     "国内默认；npm 生态唯一完整镜像",
		},
		{
			Name:     "tuna",
			Display:  "清华 TUNA",
			Registry: "https://registry.npmmirror.com",
			GoProxy:  "https://mirrors.tuna.tsinghua.edu.cn/goproxy",
			PipIndex: "https://pypi.tuna.tsinghua.edu.cn/simple",
			Note:     "node 产物与校验和齐全；npm registry 借用 npmmirror",
		},
		{
			Name:     "aliyun",
			Display:  "阿里云",
			Registry: "https://registry.npmmirror.com",
			GoProxy:  "https://mirrors.aliyun.com/goproxy",
			PipIndex: "https://mirrors.aliyun.com/pypi/simple",
			Note:     "go 与 node 产物均可用；npm registry 借用 npmmirror",
		},
		{
			Name:     "ustc",
			Display:  "中科大 USTC",
			Registry: "https://registry.npmmirror.com",
			GoProxy:  "https://goproxy.cn",
			PipIndex: "https://mirrors.ustc.edu.cn/pypi/simple",
			Note:     "node 校验和可用；npm registry 借用 npmmirror",
		},
	}
}

// Profiles 返回全部档位（不含 auto）。
func Profiles() []Profile { return builtins() }

// Names 返回可选档位名（含 auto，auto 排在最前）。
func Names() []string {
	names := []string{AutoProfile}
	for _, p := range builtins() {
		names = append(names, p.Name)
	}
	return names
}

// Get 按名字取档位；auto 返回占位档位（Registry 为空，须先探测）。
func Get(name string) (Profile, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		name = AutoProfile
	}
	if name == AutoProfile {
		return Profile{Name: AutoProfile, Display: "自动探测"}, nil
	}
	for _, p := range builtins() {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("未知镜像档 %q，可选: %s", name, strings.Join(Names(), " / "))
}

// RegistryFor 返回档位对应的 npm registry 基地址（auto / 未知档回落到 cn）。
func RegistryFor(profile string) string {
	p, err := Get(profile)
	if err != nil || p.Registry == "" {
		p, _ = Get("cn")
	}
	return strings.TrimRight(p.Registry, "/")
}

// Chain 返回回退链：首选档 → 其余国内档 → 官方档。
func Chain(profile string) []string {
	profile = strings.TrimSpace(strings.ToLower(profile))
	if profile == "" || profile == AutoProfile {
		profile = "cn"
	}
	var head []string
	var rest []string
	for _, p := range builtins() {
		if p.Name == profile {
			head = append(head, p.Name)
		} else {
			rest = append(rest, p.Name)
		}
	}
	// 回退顺序：同档 → 其他国内档 → 官方档（官方源放最后，境外可达才有用）。
	sort.SliceStable(rest, func(i, j int) bool { return rest[j] == "default" && rest[i] != "default" })
	return append(head, rest...)
}

// PickURLs 按回退链把 {档位: URL 模板} 挑成有序 URL 列表（去重、跳过缺失档）。
func PickURLs(templates map[string]string, chain []string) []string {
	seen := make(map[string]bool, len(templates))
	out := make([]string, 0, len(templates))
	for _, name := range chain {
		u, ok := templates[name]
		if !ok || u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	// 链里没列到的档位（自定义内网等）兜底追加，保持 map 稳定顺序。
	extra := make([]string, 0, len(templates))
	for name := range templates {
		if !slices.Contains(chain, name) {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		u := templates[name]
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// Result 是一次探测结果。
type Result struct {
	URL       string    `json:"url"`
	OK        bool      `json:"ok"`
	Status    int       `json:"status,omitempty"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Err       string    `json:"err,omitempty"`
	// Cached 表示这次没发请求、直接用 6h 内的缓存结论（不落盘）。
	Cached bool `json:"-"`
}

// CacheFile 是探测结果缓存（state/probe.json）。
type CacheFile struct {
	Schema  int               `json:"schema"`
	Results map[string]Result `json:"results"`
}

// Prober 负责探测与缓存。Client/Now 均可注入，测试零外网。
type Prober struct {
	Client  *fetch.Client
	Now     func() time.Time
	TTL     time.Duration
	Timeout time.Duration
	Path    string // 缓存文件路径；为空则不落盘
}

// Load 读取探测缓存（文件缺失视为空缓存）。
func (p *Prober) Load() CacheFile {
	cache := CacheFile{Schema: 1, Results: map[string]Result{}}
	if p.Path == "" {
		return cache
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, &cache); err != nil || cache.Results == nil {
		return CacheFile{Schema: 1, Results: map[string]Result{}}
	}
	return cache
}

// Save 原子写探测缓存。
func (p *Prober) Save(cache CacheFile) error {
	if p.Path == "" {
		return nil
	}
	cache.Schema = 1
	if cache.Results == nil {
		cache.Results = map[string]Result{}
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteAtomic(p.Path, data)
}

// ProbeOne 对单个 URL 发 HEAD，永不返回 error（失败信息在 Result 里）。
func (p *Prober) ProbeOne(ctx context.Context, url string) Result {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := p.Client
	if client == nil {
		client = fetch.New()
	}
	res, err := client.Head(probeCtx, url)
	out := Result{URL: url, CheckedAt: p.now()}
	if err != nil {
		out.Err = err.Error()
		if res.Status > 0 {
			out.Status = res.Status
		}
		if shortcut := describeFailure(url, err); shortcut != "" {
			out.Err = shortcut
		}
		return out
	}
	out.OK = true
	out.Status = res.Status
	out.LatencyMS = res.Elapsed.Milliseconds()
	return out
}

// describeFailure 给失败探测附上可执行建议（不静默降级）。
func describeFailure(url string, err error) string {
	msg := err.Error()
	if strings.Contains(msg, ": network is unreachable") || strings.Contains(msg, "no such host") {
		return msg + "（建议：`pk mirror use cn` 或检查代理 HTTPS_PROXY）"
	}
	return msg
}

// Probe 全量并行探测（仅 `pk mirror probe` 用）。
func (p *Prober) Probe(ctx context.Context, urls []string) []Result {
	results := make([]Result, len(urls))
	done := make(chan int, len(urls))
	for i, u := range urls {
		go func(i int, u string) {
			results[i] = p.ProbeOne(ctx, u)
			done <- i
		}(i, u)
	}
	for range urls {
		<-done
	}
	cache := p.Load()
	for _, r := range results {
		cache.Results[r.URL] = r
	}
	_ = p.Save(cache)
	return results
}

// Select 在候选 URL（已按优先级排序）里挑第一个可用的。
//
// 规则（方案 §6.3 第 4 条）：普通命令只探 2 个候选 —— 首选 + 上轮最优；
// 命中缓存（TTL 内）不探测。全部失败时返回明确错误。
func (p *Prober) Select(ctx context.Context, urls []string) (Result, error) {
	if len(urls) == 0 {
		return Result{}, fmt.Errorf("没有可用的下载源（检查 providers/*.toml 与镜像档）")
	}
	cache := p.Load()
	ttl := p.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	now := p.now()

	fresh := func(u string) (Result, bool) {
		r, ok := cache.Results[u]
		if !ok || now.Sub(r.CheckedAt) >= ttl {
			return Result{}, false
		}
		return r, true
	}

	// 候选：首选 + 上轮最优（TTL 内的 OK 结果），最多 2 个；
	// 都失败后才按回退链继续，快速失败优先。
	order := []string{urls[0]}
	for _, u := range urls[1:] {
		if r, ok := fresh(u); ok && r.OK {
			order = append(order, u)
			break
		}
	}
	for _, u := range urls {
		if !slices.Contains(order, u) {
			order = append(order, u)
		}
	}

	var attempts []string
	for _, u := range order {
		if r, ok := fresh(u); ok {
			if r.OK {
				return r, nil
			}
			attempts = append(attempts, fmt.Sprintf("%s: %s", r.URL, r.Err))
			continue
		}
		r := p.ProbeOne(ctx, u)
		cache.Results[u] = r
		if r.OK {
			_ = p.Save(cache)
			return r, nil
		}
		attempts = append(attempts, fmt.Sprintf("%s: %s", u, r.Err))
	}
	_ = p.Save(cache)
	return Result{}, fmt.Errorf("所有下载源均不可用：\n  %s\n建议：`pk mirror use cn` 换档，或用 PK_MIRROR 指向内网源", strings.Join(attempts, "\n  "))
}

func (p *Prober) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Detect 探测"用哪个档位"，给 auto 用（方案 §6.3 第 4 条：普通命令只探 2 个候选）。
//
// 规则：先探候选 A（官方），再探候选 B（国内）；谁先成功用谁，A 成功就不再探 B ——
// 国外环境多花 0 次请求，国内环境多花一次 500ms 以内的 HEAD。
func Detect(ctx context.Context, prober *Prober, officialURL, cnURL string) (string, Result, error) {
	cache := prober.Load()
	// 6h 内探过就照缓存：auto 是默认档位，每次 install/upgrade 都走这里，
	// 不该为一个早已知道的结论每次再花 150~500ms 现探。
	fresh := func(url string) (Result, bool) {
		if url == "" {
			return Result{}, false
		}
		res, ok := cache.Results[url]
		if !ok || !res.OK || prober.now().Sub(res.CheckedAt) >= prober.ttl() {
			return Result{}, false
		}
		res.Cached = true
		return res, true
	}
	if res, ok := fresh(officialURL); ok {
		return "default", res, nil
	}
	if res, ok := fresh(cnURL); ok {
		return "cn", res, nil
	}
	remember := func(res Result) {
		cache.Results[res.URL] = res
		_ = prober.Save(cache)
	}
	if officialURL != "" {
		res := prober.ProbeOne(ctx, officialURL)
		remember(res)
		if res.OK {
			return "default", res, nil
		}
	}
	if cnURL != "" {
		res := prober.ProbeOne(ctx, cnURL)
		remember(res)
		if res.OK {
			return "cn", res, nil
		}
		return "", res, fmt.Errorf("国外与国内镜像都不可达（%s / %s）：用 PK_MIRROR 指向内网源，或检查代理", officialURL, cnURL)
	}
	return "", Result{}, fmt.Errorf("没有可探测的候选")
}

// ProfileOf 把档位名归一：auto 交给 Detect，其他原样返回。
func ProfileOf(profile string) string {
	if profile == "" || profile == AutoProfile {
		return ""
	}
	return profile
}

// ttl 是探测结论的有效期（默认 6h）。
func (p *Prober) ttl() time.Duration {
	if p.TTL > 0 {
		return p.TTL
	}
	return DefaultTTL
}

// CachedProfile 从探测缓存里挑一个已知可用的档位（不发网络请求）。
//
// 用于 `pk mirror list` 与 install 的前置判断：缓存里官方源 OK 就用 default，
// 否则回落到 cn。缓存 TTL 由调用方保证（Prober.Load 只读文件）。
func CachedProfile(prober *Prober, officialURL, cnURL string, now time.Time, ttl time.Duration) string {
	cache := prober.Load()
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	fresh := func(url string) bool {
		res, ok := cache.Results[url]
		return ok && res.OK && now.Sub(res.CheckedAt) < ttl
	}
	if officialURL != "" && fresh(officialURL) {
		return "default"
	}
	if cnURL != "" && fresh(cnURL) {
		return "cn"
	}
	return ""
}
