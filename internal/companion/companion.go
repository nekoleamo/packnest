// Package companion 实现 node ↔ npm 的伴生关系（方案 §7）。
//
// 语义：npm 不是独立工具，而是 node 的伴生包；装 node 时按 node 的**精确版本**
// 反查"可安装的最新 npm"，落进该 node 的 prefix。所以切 node 时 npm 天然跟着切，
// 不存在第二个 shim，也不存在错配。
//
// 裁决优先级（方案 §18.2）：显式 pin > companion.mode=pinned > latest-compatible > node 自带。
package companion

import (
	"fmt"
	"strings"

	"github.com/packnest/packnest/internal/spec"
)

// 裁决模式。
const (
	ModeBundled          = "bundled"
	ModeLatestCompatible = "latest-compatible"
	ModePinned           = "pinned"
)

// Candidate 是 registry 里的一个 npm 版本。
type Candidate struct {
	Version string
	Engines string // engines.node
}

// Request 是一次裁决请求。
type Request struct {
	// NodeVersion 是已经装好/将要装的 node 精确版本。
	NodeVersion string
	// Bundled 是 node 发行版自带的 npm 版本。
	Bundled string
	// Explicit 是 pk.toml 里显式写的 npm 版本（"auto" 视为未指定）。
	Explicit string
	// Mode 来自 provider 的 companion.mode（或用户配置覆盖）。
	Mode string
	// Candidates 是 registry 里的全部 npm 版本（含预发布，内部会排除）。
	Candidates []Candidate
	// IndexAvailable 为 false 表示拿不到 registry（离线/镜像挂），此时沿用自带版本。
	IndexAvailable bool
}

// Decision 是裁决结果。
type Decision struct {
	Version string
	// Replaced 表示需要下载并替换自带 npm。
	Replaced bool
	// Mode 是实际生效的裁决依据。
	Mode string
	// Fallback 表示本想反查但索引不可用，退回了自带版本。
	Fallback bool
}

// Resolve 按优先级裁决 npm 版本。
func Resolve(req Request) (Decision, error) {
	bundled := normalize(req.Bundled)
	mode := req.Mode
	if mode == "" {
		mode = ModeLatestCompatible
	}
	explicit := normalizeExplicit(req.Explicit)

	// 1. 显式 pin 最优先（方案 §18.2）。
	if explicit != "" {
		return decidePinned(req, explicit, "explicit")
	}
	// 2. companion.mode = pinned 需要显式版本，否则是配置错误。
	if mode == ModePinned {
		return Decision{}, fmt.Errorf("companion.mode=pinned 需要显式 npm 版本（pk.toml 里写 npm = \"<版本>\"）")
	}
	// 3. bundled。
	if mode == ModeBundled {
		return Decision{Version: bundled, Mode: ModeBundled}, nil
	}
	// 4. latest-compatible。
	if !req.IndexAvailable || len(req.Candidates) == 0 {
		return Decision{Version: bundled, Mode: ModeLatestCompatible, Fallback: true}, nil
	}
	best, ok := MaxCompatible(req.NodeVersion, req.Candidates)
	if !ok {
		// 没有兼容版本（极端情况）：用自带版本，不阻塞安装。
		return Decision{Version: bundled, Mode: ModeLatestCompatible, Fallback: true}, nil
	}
	if bundled != "" && !isNewer(best, bundled) {
		return Decision{Version: bundled, Mode: ModeLatestCompatible}, nil
	}
	return Decision{Version: best, Replaced: best != bundled, Mode: ModeLatestCompatible}, nil
}

func decidePinned(req Request, version, mode string) (Decision, error) {
	cand, ok := findCandidate(req.Candidates, version)
	if !ok {
		if !req.IndexAvailable {
			// 离线时无法判断是否存在，直接按用户指定的版本走（下载时会再校验）。
			bundled := normalize(req.Bundled)
			return Decision{Version: version, Replaced: version != bundled, Mode: mode, Fallback: true}, nil
		}
		return Decision{}, fmt.Errorf("registry 里没有 npm@%s（`pk list --available` 可看可用版本）", version)
	}
	if cand.Engines != "" {
		ok, err := spec.EnginesSatisfied(cand.Engines, req.NodeVersion)
		if err != nil {
			return Decision{}, fmt.Errorf("npm@%s 的 engines.node 无法解析: %w", version, err)
		}
		if !ok {
			capped := ""
			if best, ok := MaxCompatible(req.NodeVersion, req.Candidates); ok {
				capped = fmt.Sprintf("；该 node 可用的 npm 上限 = %s", best)
			}
			return Decision{}, fmt.Errorf("npm@%s 要求 node %s，与当前 node %s 不兼容%s",
				version, cand.Engines, req.NodeVersion, capped)
		}
	}
	bundled := normalize(req.Bundled)
	return Decision{Version: version, Replaced: version != bundled, Mode: mode}, nil
}

// MaxCompatible 反查满足 engines.node 的最新 npm 版本（排除预发布）。
//
// ⭐ 两个必须守住的点（方案 §7）：
//  1. 排除预发布：12.0.0-pre.0.0 的 engines 满足 node 20 且排序高于 11.21.2；
//  2. 按精确版本比 engines，不能按大版本：22.21.1 → 11.x 而 22.22.2 → 12.x。
func MaxCompatible(nodeVersion string, candidates []Candidate) (string, bool) {
	best := ""
	for _, c := range candidates {
		version := normalize(c.Version)
		if version == "" || spec.IsPrerelease(version) {
			continue
		}
		if c.Engines != "" {
			ok, err := spec.EnginesSatisfied(c.Engines, nodeVersion)
			if err != nil || !ok {
				continue
			}
		}
		if best == "" || isNewer(version, best) {
			best = version
		}
	}
	return best, best != ""
}

func findCandidate(candidates []Candidate, version string) (Candidate, bool) {
	for _, c := range candidates {
		if normalize(c.Version) == version {
			return c, true
		}
	}
	return Candidate{}, false
}

// isNewer 比较两个语义化版本（都是稳定版，格式已校验）。
func isNewer(a, b string) bool {
	va, err := spec.ParseVersion(a)
	if err != nil {
		return false
	}
	vb, err := spec.ParseVersion(b)
	if err != nil {
		return true
	}
	return va.GreaterThan(vb)
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if normalized, err := spec.Normalize(v); err == nil {
		return normalized
	}
	return strings.TrimPrefix(v, "v")
}

func normalizeExplicit(v string) string {
	v = normalize(v)
	if strings.EqualFold(v, "auto") {
		return ""
	}
	return v
}
