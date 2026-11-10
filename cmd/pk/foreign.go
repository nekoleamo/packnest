package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 其他版本管理器的只读探测（方案 §18.10）。
//
// 铁律：**只读、只提示、不删不改**别人的目录。用户当前很可能同时在用 proto/nvm，
// 我们的价值是"告诉他 PATH 上谁在生效"，而不是"帮他清理"。

type foreignManager struct {
	Name     string
	Path     string
	Versions []string
}

// foreignCandidates 是各管理器放 node 版本的常见路径（相对 $HOME）。
var foreignCandidates = []struct {
	name   string
	dir    string
	prefix string // 版本目录前缀（nvm/fnm 带 v）
}{
	{"proto", ".proto/tools/node", ""},
	{"nvm", ".nvm/versions/node", "v"},
	{"volta", ".volta/tools/image/node", ""},
	{"asdf", ".asdf/installs/nodejs", ""},
	{"mise", ".mise/installs/node", ""},
	{"fnm", ".fnm/node-versions", "v"},
	{"nodenv", ".nodenv/versions", ""},
}

// scanForeignManagers 扫描其他管理器的 node 版本（只读）。
func scanForeignManagers(home string) []foreignManager {
	out := make([]foreignManager, 0, len(foreignCandidates))
	for _, c := range foreignCandidates {
		dir := filepath.Join(home, filepath.FromSlash(c.dir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var versions []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := strings.TrimPrefix(e.Name(), c.prefix)
			if name == "" || name[0] < '0' || name[0] > '9' {
				continue
			}
			versions = append(versions, name)
		}
		if len(versions) == 0 {
			continue
		}
		sort.Strings(versions)
		out = append(out, foreignManager{Name: c.name, Path: dir, Versions: versions})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// foreignPathConflicts 找出 PATH 里排在 pk 前面的"别人的 bin"（会导致 pk 的 shim 不生效）。
func foreignPathConflicts(pathEnv string, pkBin string) []string {
	segs := splitPath(pathEnv)
	var conflicts []string
	for _, seg := range segs {
		if seg == pkBin {
			break // pk 之前的才算冲突
		}
		lower := strings.ToLower(filepath.ToSlash(seg))
		for _, marker := range []string{"/.proto/shims", "/.nvm/", "/.volta/bin", "/.asdf/shims", "/.mise/shims", "/.fnm/", "/.nodenv/shims", "/.local/share/mise/shims"} {
			if strings.Contains(lower, strings.TrimSuffix(marker, "/")) {
				conflicts = append(conflicts, seg)
				break
			}
		}
	}
	return conflicts
}
