package provider

import (
	"github.com/packnest/packnest/internal/coded"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/packnest/packnest/internal/paths"
)

// 外部清单（方案 A）：内嵌 → $PK_ROOT/providers → PK_PROVIDERS_DIR（追加、可多目录）。
// 三条硬边界：内嵌永远赢（外部只能补新 id）；外部清单的任何问题只记录 issue、不让 CLI 起不来；
// 清单是数据，不执行任何来自清单的代码。
const (
	// ProvidersDirEnv 追加外部清单目录（os.PathListSeparator 分隔，支持多个）。
	ProvidersDirEnv = "PK_PROVIDERS_DIR"
	// AllowHTTPEnv 放行外部清单里的 http:// 源（方案 §0.1 决策 3：默认拒绝）。
	AllowHTTPEnv = "PK_ALLOW_HTTP"
	// MaxManifestBytes 是单个外部清单的大小上限：清单是几十行 TOML，256KB 已经很宽松。
	MaxManifestBytes = 256 << 10

	// IssueExternalInvalid / IssueExternalConflict 是 doctor 呈现的两个问题码。
	IssueExternalInvalid  = "PROVIDER_EXTERNAL_INVALID"
	IssueExternalConflict = "PROVIDER_EXTERNAL_CONFLICT"
)

// Issue 是外部清单的一个加载问题（由 doctor / list 呈现，不影响内嵌清单）。
type Issue struct {
	Code string
	File string
	// Message 是给人看的说明（CLI 会优先用 Err 渲染成本地语言；保留它是为了
	// `pk doctor --json` 的 detail 与旧消费者不为空）。
	Message string
	// Err 是结构化的原因（编码错误）：文案在 cmd/pk/present，provider 只给码 + 参数。
	Err error
}

var (
	issues  []Issue
	extRoot string
)

// SetRoot 让外部清单目录跟随 CLI 的 --root（不依赖 PK_ROOT）；须在首次 All() 之前调用。
func SetRoot(root string) { extRoot, once, issues = root, sync.Once{}, nil }

// ExternalIssues 返回外部清单的问题（会确保清单已加载）。
func ExternalIssues() []Issue { _, _ = All(); return issues }

// ExternalDirs 返回生效的外部清单目录（$PK_ROOT/providers + PK_PROVIDERS_DIR）。
func ExternalDirs() []string {
	dirs := make([]string, 0, 2)
	if extRoot != "" {
		dirs = append(dirs, filepath.Join(extRoot, "providers"))
	} else if p, err := paths.Resolve(); err == nil {
		dirs = append(dirs, p.Providers())
	}
	for _, d := range filepath.SplitList(os.Getenv(ProvidersDirEnv)) {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// loadExternal 读全部外部目录：内嵌已占用的 id 跳过并记 conflict，坏文件记 invalid。
func loadExternal(dirs []string, embedded []*Provider) []*Provider {
	taken := make(map[string]bool, len(embedded)+4)
	for _, p := range embedded {
		taken[p.ID] = true
	}
	type pending struct {
		path, dir string
		p         *Provider
	}
	// 第一遍：解析 + 查重（不校验 companions —— 它可能指向还没加载的另一个外部文件）
	var files []pending
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录不存在 = 没配置外部清单，正常
		}
		for _, e := range entries { // os.ReadDir 已按文件名排序（先加载的赢）
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".toml") {
				continue
			}
			path := filepath.Join(dir, name)
			p, rerr := readExternal(path, name)
			switch {
			case rerr == nil && taken[p.ID]:
				issues = append(issues, Issue{Code: IssueExternalConflict, File: path,
					Message: p.ID + " 已由先加载的清单定义，外部清单被忽略"})
			case rerr != nil:
				issues = append(issues, Issue{Code: IssueExternalInvalid, File: path, Message: rerr.Error(), Err: rerr})
			default:
				taken[p.ID] = true
				files = append(files, pending{path: path, dir: dir, p: p})
			}
		}
	}
	// 第二遍：此时 taken 含全部可用 id，再校验 companions 与安全边界
	out := make([]*Provider, 0, len(files))
	for _, f := range files {
		if err := validateExternal(f.p, taken); err != nil {
			issues = append(issues, Issue{Code: IssueExternalInvalid, File: f.path, Message: err.Error(), Err: err})
			continue
		}
		f.p.Source, f.p.Dir = SourceExternal, f.dir
		out = append(out, f.p)
	}
	return out
}

// readExternal 读并校验一个外部清单；err 非空表示不可用（编码错误，文案在 present）。
func readExternal(path, name string) (*Provider, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, coded.Wrap(err, CodeReadManifest, name)
	}
	if st.Size() > MaxManifestBytes {
		return nil, coded.New(CodeExtTooLarge, st.Size(), MaxManifestBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, coded.Wrap(err, CodeReadManifest, name)
	}
	p, err := Parse(data)
	if err == nil && p.ID != strings.TrimSuffix(name, ".toml") {
		err = coded.New(CodeExtIDMismatch, p.ID)
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// validateExternal 只查"外部清单特有"的安全边界（方案 A.4）。
func validateExternal(p *Provider, known map[string]bool) error {
	if p.Index.Kind == IndexPBS {
		return coded.New(CodeExtPBS)
	}
	httpOK := os.Getenv(AllowHTTPEnv) == "1"
	// 占位符校验：render 自带"未知占位符"报错，这里用假值提前跑一遍。
	dummy := map[string]string{"version": "0", "platform": "p", "ext": "x", "registry": "r", "tool": p.ID, "filename": "f", "tag": "t"}
	for _, m := range []map[string]string{p.Index.URLs, p.Artifact.URLs, p.Checksum.URLs} {
		for _, u := range m {
			switch {
			case strings.HasPrefix(u, "https://"):
			case strings.HasPrefix(u, "http://") && httpOK:
				p.InsecureTransport = true // 校验强度按方案 §A.4.1 降级
			default:
				return coded.New(CodeExtHTTP, u, AllowHTTPEnv)
			}
			if _, err := render(u, dummy); err != nil {
				return err
			}
		}
	}
	if p.Layout.Strip < 0 {
		return coded.New(CodeExtNegativeStrip)
	}
	paths := []string{p.Layout.Single}
	for _, bin := range p.Layout.BinTargets {
		paths = append(paths, bin)
	}
	for _, v := range paths {
		s := filepath.ToSlash(v)
		if filepath.IsAbs(v) || s == ".." || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") {
			return coded.New(CodeExtRelative, v)
		}
	}
	for name, c := range p.Companions {
		if !known[c.Registry] {
			return coded.New(CodeExtCompanionReg, name, c.Registry)
		}
	}
	return nil
}
