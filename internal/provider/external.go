package provider

import (
	"fmt"
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
	Code    string
	File    string
	Message string
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
			p, msg := readExternal(path, name)
			switch {
			case msg == "" && taken[p.ID]:
				issues = append(issues, Issue{IssueExternalConflict, path,
					fmt.Sprintf("%s 已由先加载的清单定义，外部清单被忽略", p.ID)})
			case msg != "":
				issues = append(issues, Issue{IssueExternalInvalid, path, msg})
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
			issues = append(issues, Issue{IssueExternalInvalid, f.path, err.Error()})
			continue
		}
		f.p.Source, f.p.Dir = SourceExternal, f.dir
		out = append(out, f.p)
	}
	return out
}

// readExternal 读并校验一个外部清单；msg 非空表示不可用。
func readExternal(path, name string) (*Provider, string) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err.Error()
	}
	if st.Size() > MaxManifestBytes {
		return nil, fmt.Sprintf("清单 %d 字节，超过上限 %d（不是 TOML 清单？）", st.Size(), MaxManifestBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err.Error()
	}
	p, err := Parse(data)
	if err == nil && p.ID != strings.TrimSuffix(name, ".toml") {
		err = fmt.Errorf("id = %q 与文件名不一致", p.ID)
	}
	if err != nil {
		return nil, err.Error()
	}
	return p, ""
}

// validateExternal 只查"外部清单特有"的安全边界（方案 A.4）。
func validateExternal(p *Provider, known map[string]bool) error {
	if p.Index.Kind == IndexPBS {
		return fmt.Errorf("pbs 索引只支持内嵌清单（解析在 internal/toolchain）")
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
				return fmt.Errorf("URL 必须是 https：%s（内网 http 源需 %s=1 放行，且按弱校验处理）", u, AllowHTTPEnv)
			}
			if _, err := render(u, dummy); err != nil {
				return err
			}
		}
	}
	if p.Layout.Strip < 0 {
		return fmt.Errorf("layout.strip 不能为负数")
	}
	paths := []string{p.Layout.Single}
	for _, bin := range p.Layout.BinTargets {
		paths = append(paths, bin)
	}
	for _, v := range paths {
		s := filepath.ToSlash(v)
		if filepath.IsAbs(v) || s == ".." || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") {
			return fmt.Errorf("layout 路径必须是相对路径且不含 ..：%q", v)
		}
	}
	for name, c := range p.Companions {
		if !known[c.Registry] {
			return fmt.Errorf("companions.%s.registry = %q 不是已存在的工具 id", name, c.Registry)
		}
	}
	return nil
}
