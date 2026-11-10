// M4：引用计数、全局默认切换、GC 与清扫（方案 §8.2 / §12.1 M4）。
package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/verify"
)

// Activate 把某工具某个已装版本设为全局默认：写 config.toml + 台账引用 + 重指 shim。
func (i *Installer) Activate(tool, version string) error {
	in := i.ledger.Find(tool, version)
	if in == nil {
		return fmt.Errorf("%s@%s 还没装（`pk list` 看已装版本，先 `pk install %s@%s`）", tool, version, tool, version)
	}
	p, err := i.opt.LookupProvider(tool)
	if err != nil {
		return err
	}

	// 1. 台账引用：清掉该工具其他版本的全局引用，再给这个版本加上。
	for idx := range i.ledger.Installs {
		if i.ledger.Installs[idx].Tool != tool {
			continue
		}
		i.ledger.Installs[idx].References = dropRefKind(i.ledger.Installs[idx].References, store.RefGlobal)
	}
	if err := i.ledger.AddRef(tool, version, store.Ref{Kind: store.RefGlobal}); err != nil {
		return err
	}
	// 2. 配置里的全局默认。
	if i.opt.Cfg.Tools == nil {
		i.opt.Cfg.Tools = map[string]string{}
	}
	i.opt.Cfg.Tools[tool] = version
	// 3. shim 重指到该版本。
	return i.installShims(p, in.Path)
}

// ActiveVersion 返回某工具当前生效的全局默认版本（空串表示没有）。
func (i *Installer) ActiveVersion(tool string) string {
	if v, ok := i.opt.Cfg.Tools[tool]; ok && v != "" {
		return v
	}
	return i.ledger.Active(tool)
}

// GCResult 是 gc 的结果。
type GCResult struct {
	// Removed 是被删掉的版本目录（"node@1.2.3"）。
	Removed []string
	// CacheRemoved 是被淘汰的缓存文件路径。
	CacheRemoved []string
	// KeptRef 是因为仍有引用而保留的版本。
	KeptRef []string
	// FreedBytes 是释放的字节数。
	FreedBytes int64
	DryRun     bool
}

// GC 回收零引用版本与超限缓存。
//
// 铁律（方案 §8.2 第 3/4 条）：只删台账里的记录；被引用（global/project/tool）的版本
// 必须有 --force 才删，`gc` 永远只清"零引用"的。
func (i *Installer) GC(ctx context.Context, cacheMax int64, dryRun, force bool) (GCResult, error) {
	res := GCResult{DryRun: dryRun}

	// 1. 零引用版本。
	var keep []store.Install
	for _, in := range i.ledger.Installs {
		if len(in.References) > 0 && !force {
			for _, ref := range in.References {
				res.KeptRef = append(res.KeptRef, fmt.Sprintf("%s@%s (%s)", in.Tool, in.Version, ref.Kind))
			}
			keep = append(keep, in)
			continue
		}
		res.Removed = append(res.Removed, in.Tool+"@"+in.Version)
		res.FreedBytes += store.DirSize(in.Path)
		if dryRun {
			keep = append(keep, in)
			continue
		}
		if err := removeDirWithRetry(in.Path, 3); err != nil {
			// 删不掉就留着（Windows 上进程占用是常态），台账不动。
			res.Removed = res.Removed[:len(res.Removed)-1]
			keep = append(keep, in)
			continue
		}
		pruneEmptyDirs(filepath.Dir(in.Path), i.opt.Paths.Root)
		if err := i.removeShimsFor(in.Path); err != nil {
			return res, err
		}
		if i.ActiveVersion(in.Tool) == in.Version {
			i.clearActive(in.Tool)
		}
	}
	if !dryRun {
		i.ledger.Installs = keep
	}

	// 2. 缓存上限。
	if cacheMax > 0 {
		prune, err := store.PruneCache(i.opt.Paths, cacheMax, i.now(), dryRun)
		if err != nil {
			return res, err
		}
		res.CacheRemoved = append(res.CacheRemoved, prune.Removed...)
		res.FreedBytes += prune.Bytes
	}
	sort.Strings(res.Removed)
	sort.Strings(res.CacheRemoved)
	sort.Strings(res.KeptRef)
	return res, nil
}

// pruneEmptyDirs 自下而上删空目录（遇到非空立即停，绝不强删）。
//
// 卸载/GC 之后 tools/<tool>/ 往往剩下一个空壳，用户看到的不是“零残留”。
func pruneEmptyDirs(dir, stopAt string) {
	for dir != "" && dir != stopAt && strings.HasPrefix(dir, stopAt+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// clearActive 清掉某工具的全局默认（配置与台账引用）。
func (i *Installer) clearActive(tool string) {
	delete(i.opt.Cfg.Tools, tool)
	for idx := range i.ledger.Installs {
		if i.ledger.Installs[idx].Tool == tool {
			i.ledger.Installs[idx].References = dropRefKind(i.ledger.Installs[idx].References, store.RefGlobal)
		}
	}
}

// SweepStale 清扫崩溃残留（install/gc 之前调用）。
func (i *Installer) SweepStale() (store.SweepResult, error) {
	return store.SweepStale(i.opt.Paths, i.now(), store.StaleMaxAge)
}

// ResolveProjectShims 在"当前默认版本被删掉"后，把 shim 指向剩余的最新版本。
func (i *Installer) repointAfterRemoval(tool string) error {
	if i.ActiveVersion(tool) != "" {
		return nil
	}
	versions := i.installedVersions(tool)
	if len(versions) == 0 {
		return nil
	}
	newest := spec.SortDesc(versions)[0]
	return i.Activate(tool, newest)
}

func dropRefKind(refs []store.Ref, kind string) []store.Ref {
	out := refs[:0]
	for _, r := range refs {
		if r.Kind != kind {
			out = append(out, r)
		}
	}
	return out
}

// resolvePair 供 --frozen 使用：直接取锁里的精确版本（不做索引解析）。
func (i *Installer) lockedVersion(lock *Lock, tool string) (string, string, error) {
	entry, ok := lock.Find(tool)
	if !ok {
		return "", "", fmt.Errorf("%s 不在 %s 里（--frozen 不做解析）", tool, LockFileName)
	}
	return entry.Version, entry.SHA256, nil
}

// recordCacheTouch 记录缓存使用时间（LRU 依据）。
func (i *Installer) recordCacheTouch(name string, now time.Time) {
	idx, err := store.LoadCacheIndex(i.opt.Paths.CacheIndex())
	if err != nil {
		return
	}
	path := filepath.Join(i.opt.Paths.CacheArchives(), name)
	st, statErr := os.Stat(path)
	if statErr != nil {
		return
	}
	idx.Touch(name, st.Size(), now)
	_ = idx.Save(i.opt.Paths.CacheIndex())
}

// verifyLockedSHA 校验产物是否与锁里的 sha256 一致。
func verifyLockedSHA(archivePath, want string) error {
	if want == "" {
		return nil
	}
	got, err := verify.SHA256File(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w: 产物 sha256 与 %s 不一致（锁 %s，实际 %s）", verify.ErrChecksum, LockFileName, want, got)
	}
	return nil
}
