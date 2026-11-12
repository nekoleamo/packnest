package install

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/packnest/packnest/internal/archive"
	"github.com/packnest/packnest/internal/companion"
	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/verify"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 伴生包与工具链自带工具：node → npm 的反查与替换、corepack 的 shim 落位。

func (i *Installer) applyCompanion(ctx context.Context, p *provider.Provider, nodeVersion, treeDir string, nodeIdx *provider.Index) (string, string, error) {
	c, ok := p.Companions["npm"]
	if !ok {
		return "", "", nil
	}
	npmDir := filepath.Join(treeDir, filepath.FromSlash(companionTarget(c.Target, i.opt.GOOS)))
	bundled := readNpmVersion(filepath.Join(npmDir, "package.json"))
	if bundled == "" {
		// 拿不到自带版本（zip 布局差异）也不该让安装失败。
		return "", "", nil
	}

	mode := c.Mode
	if override, ok := i.opt.Cfg.CompanionModes["npm"]; ok && override != "" {
		mode = override
	}
	explicit := ""
	if v, ok := i.opt.Project["npm"]; ok && !strings.EqualFold(v, "auto") {
		explicit = v
	}

	var candidates []companion.Candidate
	indexAvailable := false
	npmP, err := i.opt.LookupProvider(c.Registry)
	var npmIdx *provider.Index // 一次索引加载复用两处（候选版本 + shasum）
	if err == nil {
		if idx, err := i.loadIndex(ctx, npmP); err == nil {
			npmIdx, indexAvailable = idx, true
			for _, v := range idx.Versions {
				candidates = append(candidates, companion.Candidate{Version: v.Version, Engines: v.Engines["node"]})
			}
		}
	}

	dec, err := companion.Resolve(companion.Request{
		NodeVersion:    nodeVersion,
		Bundled:        bundled,
		Explicit:       explicit,
		Mode:           mode,
		Candidates:     candidates,
		IndexAvailable: indexAvailable,
	})
	if err != nil {
		return "", "", err
	}
	if !dec.Replaced {
		return bundled, bundled, nil
	}

	npmVersion, shasum := dec.Version, ""
	if npmIdx != nil {
		if v, ok := npmIdx.Find(npmVersion); ok {
			shasum = v.Shasum
		}
	}
	if err := i.installNpmPackage(ctx, npmP, npmVersion, shasum, npmDir); err != nil {
		return "", "", err
	}
	i.emit(jsonout.Event{Ev: "companion", Tool: "npm", Version: npmVersion, Msg: "replaced " + bundled})
	return npmVersion, bundled, nil
}

func (i *Installer) installNpmPackage(ctx context.Context, npmP *provider.Provider, version, shasum, npmDir string) error {
	if npmP == nil {
		return fmt.Errorf("找不到 npm 的 provider 清单，无法替换伴生包")
	}
	// 缓存/下载走与主产物同一条路径（fetchArtifact 已含缓存命中与回退链）。
	cachePath, _, _, err := i.fetchArtifact(ctx, npmP, provider.Version{Version: version}, nil)
	if err != nil {
		return err
	}
	sha, err := verify.SHA256File(cachePath)
	if err != nil {
		return err
	}
	algo, digest := verify.AlgoSHA256, sha
	if shasum != "" {
		if d, err := verify.DigestFile(cachePath, verify.AlgoSHA1); err == nil {
			algo, digest = verify.AlgoSHA1, d
		}
	}
	if _, err := i.newVerifier().Verify(ctx, verify.Request{
		Tool: "npm", Version: version, Platform: i.opt.GOOS + "-" + i.opt.GOARCH,
		Digest: digest, Algo: algo, Format: "registry-integrity", InlineShasum: shasum,
		Insecure: i.opt.Insecure, Rehash: i.opt.Rehash,
	}); err != nil {
		_ = os.Remove(cachePath)
		return err
	}

	tmp := npmDir + ".tmp." + randomSuffix()
	format, err := archive.FormatFor(extOrDefault(npmP, i.opt.GOOS))
	if err != nil {
		return err
	}
	if err := archive.Extract(cachePath, tmp, format, archive.Layout{Strip: npmP.Layout.Strip, Bins: npmP.Layout.Bins, BinTargets: npmP.Layout.BinTargets}, i.opt.GOOS); err != nil {
		_ = os.RemoveAll(tmp)
		_ = os.Remove(cachePath)
		return err
	}
	if err := archive.Replace(npmDir, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// enableCorepack 把该 node 自带的 corepack 的 shim 指到独立目录（方案 B2-2）。
//
// node 25+ 已移除 corepack —— 这种情况必须明确报错，而不是静默跳过：
// 用户敲了 --corepack 就得知道"这个版本没有"，并被告知 pnpm/yarn 该怎么装。

func (i *Installer) enableCorepack(ctx context.Context, toolDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(toolDir, filepath.FromSlash("lib/node_modules/corepack"))); err != nil {
		return false, fmt.Errorf("该 node 不自带 corepack（node 25+ 已移除）：pnpm/yarn 请用 `pk install pnpm`")
	}
	bin := i.opt.CorepackBin
	if bin == "" {
		bin = i.opt.Paths.GlobalCorepackBin()
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return false, err
	}
	pruneDanglingShims(bin) // gc 回收过 node 版本后旧 shim 会悬空，corepack enable 会因 realpath 失败中断
	run := i.opt.RunCommand
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w（%s）", name, err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	node := filepath.Join(toolDir, "bin", "node")
	if _, err := os.Stat(node); err != nil {
		node = filepath.Join(toolDir, "node") // 单文件布局兜底
	}
	if err := run(ctx, node, filepath.Join(toolDir, filepath.FromSlash("lib/node_modules/corepack/dist/corepack.js")),
		"enable", "--install-directory", bin); err != nil {
		return false, fmt.Errorf("corepack enable 失败: %w", err)
	}
	i.emit(jsonout.Event{Ev: "corepack", Tool: "node", Version: filepath.Base(filepath.Dir(toolDir)), Source: bin})
	return true, nil
}

// pruneDanglingShims 删掉 shim 目录里指向已不存在文件的软链（Windows 的 .cmd 不是软链，自然跳过）。
func pruneDanglingShims(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if p := filepath.Join(dir, e.Name()); e.Type()&os.ModeSymlink != 0 {
			if _, err := os.Stat(p); err != nil { // Stat 跟随软链：悬空即报错
				_ = os.Remove(p)
			}
		}
	}
}

// transportOf 记录清单走哪种传输：内网 http 源在台账里留痕（只增字段）。

// companionTarget 处理 Windows 与 Unix 的目录差异：
// Unix 是 lib/node_modules/npm，Windows 官方 zip 是 node_modules/npm。
func companionTarget(target, goos string) string {
	if goos == "windows" {
		return strings.TrimPrefix(target, "lib/")
	}
	return target
}

func readNpmVersion(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return pkg.Version
}
