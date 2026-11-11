package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/shim"
	"github.com/packnest/packnest/internal/spec"
	"github.com/packnest/packnest/internal/store"
	"github.com/packnest/packnest/internal/toolchain"
)

// cmdList 列出已装工具（--available 时附带内嵌 provider 目录）。
func (a *app) cmdList(args []string) int {
	fs := a.newFlagSet("list")
	available := fs.Bool("available", false, "列出各工具的最新可用版本（索引默认 24h 缓存）")
	refresh := fs.Bool("refresh", false, "忽略索引缓存，强制重新拉取（配合 --available / 工具名）")
	all := fs.Bool("all", false, "配合工具名：列出全部可装版本（等于 --limit 0）")
	limit := fs.Int("limit", 20, "配合工具名：只列最近 N 个版本（0 = 全部）")
	major := fs.String("major", "", "配合工具名：只看某个大版本（如 --major 22；0.x 可写 0.10）")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() > 1 {
		return a.failUsage(fmt.Errorf("用法: pk list [工具] [--available] [--refresh] [--all]"))
	}
	if tool := fs.Arg(0); tool != "" {
		n := *limit
		if *all || *major != "" {
			n = 0
		}
		if n < 0 {
			return a.failUsage(fmt.Errorf("--limit 不能为负数"))
		}
		return a.listToolVersions(tool, *refresh, n, strings.TrimPrefix(*major, "v"))
	}

	ledger, recovered, err := store.LoadLedgerWithRecovery(a.paths.Ledger())
	if err != nil {
		return a.fail(err)
	}
	if recovered && !a.json {
		fmt.Fprintf(a.errOut, "pk: 台账损坏，已从 %s.bak 恢复（`pk doctor` 查看详情）\n", a.paths.Ledger())
	}

	entries := make([]jsonout.ToolEntry, 0, len(ledger.Installs))
	active := a.activeVersions(ledger)
	for _, in := range ledger.Installs {
		p, _ := provider.Get(in.Tool)
		entry := jsonout.ToolEntry{
			Name:     in.Tool,
			Version:  in.Version,
			Active:   active[in.Tool] == in.Version,
			Path:     in.Path,
			Requires: requiresOf(p),
			Verified: in.Verified,
			Source:   sourceOf(p),
		}
		if in.Tool == "node" {
			entry.NPM = &jsonout.ToolNPM{Version: in.NpmVersion, Replaced: in.NpmBundled}
		}
		if in.Path != "" {
			entry.SizeBytes = store.DirSize(in.Path)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})

	if a.json {
		if *available {
			a.refreshIndex = *refresh
			rows := a.availableRows()
			return a.emit(struct {
				Schema    int                 `json:"schema"`
				Mirror    string              `json:"mirror"`
				Tools     []jsonout.ToolEntry `json:"tools"`
				Available []availableEntry    `json:"available"`
			}{jsonout.SchemaVersion, a.cfg.Mirror, entries, rows})
		}
		return a.emit(jsonout.NewList(entries))
	}

	switch {
	case len(entries) == 0 && !*available:
		fmt.Fprintln(a.out, "还没装任何工具。`pk install node@22` 开始。")
	case len(entries) > 0:
		fmt.Fprintln(a.out, "已装:")
		for _, e := range entries {
			mark := " "
			if e.Active {
				mark = "*"
			}
			line := fmt.Sprintf("  %s %-6s %-10s %8s", mark, e.Name, e.Version, humanBytes(e.SizeBytes))
			if e.NPM != nil && e.NPM.Version != "" {
				line += fmt.Sprintf(" (npm %s", e.NPM.Version)
				if e.NPM.Replaced != "" && e.NPM.Replaced != e.NPM.Version {
					line += fmt.Sprintf(" ← 自带 %s", e.NPM.Replaced)
				}
				line += ")"
			}
			if e.Verified == "unverified" {
				line += " [未校验]"
			}
			if e.Source == provider.SourceExternal {
				line += " *"
			}
			fmt.Fprintln(a.out, line)
		}
	}

	if *available {
		a.refreshIndex = *refresh
		return a.printAvailable()
	}
	for _, e := range entries {
		if e.Source == provider.SourceExternal {
			fmt.Fprintln(a.out, "* 来自外部清单（"+strings.Join(provider.ExternalDirs(), string(os.PathListSeparator))+"）")
			break
		}
	}
	return jsonout.ExitOK
}

// sourceOf 标出清单来源（GUI 与脚本据此判断"官方内嵌"还是"用户自带"）。
func sourceOf(p *provider.Provider) string {
	if p == nil {
		return ""
	}
	return p.Source
}

// availableEntry 是 `list --available` 的一项。
type availableEntry struct {
	Name     string   `json:"name"`
	Latest   string   `json:"latest,omitempty"`
	LTS      string   `json:"lts,omitempty"`
	Recent   []string `json:"recent,omitempty"`
	Requires []string `json:"requires"`
	Stale    bool     `json:"stale,omitempty"`
	Source   string   `json:"source,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// printAvailable 打印各工具的最新可装版本（索引走缓存；离线时读 24h 内缓存）。
func (a *app) printAvailable() int {
	rows := a.availableRows()
	if a.json {
		return a.emit(struct {
			Schema int              `json:"schema"`
			Mirror string           `json:"mirror"`
			Tools  []availableEntry `json:"tools"`
		}{jsonout.SchemaVersion, a.cfg.Mirror, rows})
	}
	fmt.Fprintf(a.out, "可装版本（档位 %s；缓存 24h，离线读缓存）:\n", a.cfg.Mirror)
	for _, row := range rows {
		if row.Err != "" {
			fmt.Fprintf(a.out, "  %-7s 取不到索引: %s\n", row.Name, firstLine(row.Err))
			continue
		}
		extra := ""
		if row.LTS != "" {
			extra = " (lts " + row.LTS + ")"
		}
		if row.Stale {
			extra += " [索引来自过期缓存]"
		}
		name := row.Name
		if row.Source == provider.SourceExternal {
			name += "*"
		}
		fmt.Fprintf(a.out, "  %-7s %-10s%s  近几个: %s\n",
			name, row.Latest, extra, strings.Join(row.Recent, ", "))
	}
	return jsonout.ExitOK
}

// indexLoader 造版本索引加载器（refreshIndex 为真时 0 = 每次回源，仍写缓存供 --offline 用）。
func (a *app) indexLoader() *provider.IndexLoader {
	ttl := provider.DefaultIndexTTL
	if a.refreshIndex {
		ttl = 0
	}
	return &provider.IndexLoader{Client: a.client, CacheDir: a.paths.CacheMeta(), TTL: ttl, Custom: toolchain.LoadPBSIndex}
}

// availableRows 拉取各工具的可用版本（失败只记录，不影响其他工具）。
func (a *app) availableRows() []availableEntry {
	all, err := provider.All()
	if err != nil {
		return []availableEntry{{Name: "-", Err: err.Error()}}
	}
	loader := a.indexLoader()
	rows := make([]availableEntry, 0, len(all))
	for _, p := range all {
		row := availableEntry{Name: p.ID, Requires: requiresOf(p), Source: p.Source}
		idx, err := loader.Load(a.context(), p, a.cfg.Mirror)
		if err != nil {
			row.Err = err.Error()
			rows = append(rows, row)
			continue
		}
		row.Stale = idx.Stale
		if latest, ok := idx.Latest(); ok {
			row.Latest = latest.Version
		}
		if lts, ok := idx.Resolve(mustAlias("lts")); ok == nil {
			row.LTS = lts.Version
		}
		sorted := spec.SortDesc(idx.VersionList())
		if len(sorted) > 5 {
			sorted = sorted[:5]
		}
		row.Recent = sorted
		rows = append(rows, row)
	}
	return rows
}

// listToolVersions 打印某一个工具的全部可装版本 + 本机已装（`pk list node`）。
//
// 单工具视图和 `--available` 的"每个工具一行"是两件事：这里要能看见完整版本集，
// 而不是只有最新版和"近几个"。
func (a *app) listToolVersions(name string, refresh bool, limit int, major string) int {
	p, err := provider.Get(name)
	if err != nil {
		return a.fail(err) // provider.Get 的报错里已经列了可用工具
	}
	ledger, _, lerr := store.LoadLedgerWithRecovery(a.paths.Ledger())
	if lerr != nil {
		return a.fail(lerr)
	}
	active := a.activeVersions(ledger)
	installed := make([]string, 0, 4)
	for _, in := range ledger.Installs {
		if in.Tool == name {
			installed = append(installed, in.Version)
		}
	}
	installed = spec.SortDesc(installed)

	_ = refresh // 缓存 TTL 由 a.refreshIndex 统一决定
	idx, err := a.indexLoader().Load(a.context(), p, a.cfg.Mirror)
	if err != nil {
		fmt.Fprintf(a.errOut, "pk: %s 的版本索引取不到: %v\n", name, err)
		fmt.Fprintln(a.errOut, "试一试：pk list "+name+" --refresh（强制回源）· pk mirror probe（看哪个源通）· pk mirror use cn（换档）")
		return jsonout.ExitNetwork
	}

	ltsOf := make(map[string]string, len(idx.Versions))
	for _, v := range idx.Versions {
		if v.LTS != "" {
			ltsOf[v.Version] = v.LTS
		}
	}
	versions := spec.SortDesc(idx.VersionList()) // 降序、去预发布
	if major != "" {
		kept := make([]string, 0, len(versions))
		for _, v := range versions {
			if majorOf(v) == major {
				kept = append(kept, v)
			}
		}
		if len(kept) == 0 {
			fmt.Fprintf(a.errOut, "pk: %s 没有 %s.x 的版本（pk list %s 看有哪些大版本）\n", name, major, name)
			return jsonout.ExitFail
		}
		versions = kept
	}
	latest, hasLatest := idx.Latest()
	ltsVer, hasLTS := idx.LatestLTS()
	ltsLatest := ""
	if hasLTS {
		ltsLatest = ltsVer.Version
	}

	if a.json {
		type instEntry struct {
			Version string `json:"version"`
			Active  bool   `json:"active"`
		}
		type availEntry struct {
			Version   string `json:"version"`
			LTS       string `json:"lts,omitempty"`
			Installed bool   `json:"installed,omitempty"`
		}
		inst := make([]instEntry, 0, len(installed))
		for _, v := range installed {
			inst = append(inst, instEntry{Version: v, Active: active[name] == v})
		}
		avail := make([]availEntry, 0, len(versions))
		for _, v := range versions {
			avail = append(avail, availEntry{Version: v, LTS: ltsOf[v], Installed: containsStr(installed, v)})
		}
		out := struct {
			Schema    int          `json:"schema"`
			Tool      string       `json:"tool"`
			Latest    string       `json:"latest,omitempty"`
			LTS       string       `json:"lts,omitempty"`
			Total     int          `json:"total"`
			Installed []instEntry  `json:"installed"`
			Available []availEntry `json:"available"`
			Stale     bool         `json:"stale,omitempty"`
		}{jsonout.SchemaVersion, name, latest.Version, ltsLatest, len(versions), inst, avail, idx.Stale}
		return a.emit(out)
	}

	head := name + "：可装 " + fmt.Sprint(len(versions)) + " 个版本"
	if major != "" {
		head = name + " " + major + ".x：" + fmt.Sprint(len(versions)) + " 个版本"
	}
	if hasLatest {
		head += "（索引最新 " + latest.Version
		if hasLTS && ltsLatest != "" {
			head += " · lts " + ltsLatest
		}
		head += "）"
	}
	if idx.Stale {
		head += " [索引来自过期缓存]"
	}
	fmt.Fprintln(a.out, head)
	if len(installed) == 0 {
		fmt.Fprintln(a.out, "  已装   （本机还没装）")
	} else {
		for i, v := range installed {
			label := "       "
			if i == 0 {
				label = "  已装 "
			}
			mark := " "
			if active[name] == v {
				mark = "*"
			}
			fmt.Fprintf(a.out, "%s%s %s\n", label, mark, v)
		}
	}
	if len(versions) == 0 {
		fmt.Fprintln(a.out, "  可装   （索引里没有稳定版本）")
		return jsonout.ExitOK
	}
	// versions 是降序；展示用升序（最新在最下面），窗口取升序序列的尾部 = 最近 N 个。
	asc := make([]string, 0, len(versions))
	for i := len(versions) - 1; i >= 0; i-- {
		asc = append(asc, versions[i])
	}
	shown := asc
	truncated := 0
	if limit > 0 && len(asc) > limit {
		truncated = len(asc) - limit
		shown = asc[truncated:]
	}
	if truncated > 0 {
		fmt.Fprintf(a.out, "  可装   （显示最近 %d 个，旧 → 新，最新在最下面）\n", len(shown))
	} else {
		fmt.Fprintf(a.out, "  可装   （全部 %d 个，旧 → 新，最新在最下面）\n", len(shown))
	}

	// 行多的时候改成"按大版本分组 + 多列网格"：877 行逐行铺开没法看（参考 nvm/pyenv 用户
	// 都靠 grep 也是因为如此）。少量版本仍用一版一行 + 文字标签，读起来更清楚。
	mark := func(v string) string {
		m := ""
		if v == latest.Version && hasLatest {
			m = "*"
		}
		if lts := ltsOf[v]; lts != "" {
			if m == "" {
				m = "l"
			}
		}
		if containsStr(installed, v) {
			if active[name] == v {
				m = "@"
			} else if m == "" {
				m = "+"
			}
		}
		return m
	}
	if len(shown) > gridThreshold {
		a.printVersionGrid(shown, ltsOf, mark)
		fmt.Fprintln(a.out, "         图例：* 最新 · l 长期支持(LTS) · + 已装 · @ 当前默认")
	} else {
		width := 0
		for _, v := range shown {
			if len(v) > width {
				width = len(v)
			}
		}
		lastLTS := ""
		for _, v := range shown {
			tags := make([]string, 0, 3)
			if v == latest.Version && hasLatest {
				tags = append(tags, "latest")
			}
			if lts := ltsOf[v]; lts != "" {
				// 同一个代号连续出现时只标一次，否则整列都是 "lts: Jod"，噪声太大
				if strings.EqualFold(lts, "lts") || strings.EqualFold(lts, lastLTS) {
					tags = append(tags, "lts")
				} else {
					tags = append(tags, "lts: "+lts)
					lastLTS = lts
				}
			}
			if containsStr(installed, v) {
				if active[name] == v {
					tags = append(tags, "已装 · 当前默认")
				} else {
					tags = append(tags, "已装")
				}
			}
			if len(tags) == 0 {
				fmt.Fprintf(a.out, "         %*s\n", width, v)
				continue
			}
			fmt.Fprintf(a.out, "         %*s  %s\n", width, v, strings.Join(tags, " · "))
		}
	}
	if truncated > 0 {
		fmt.Fprintf(a.out, "         …还有前面 %d 个更旧的版本（pk list %s --all 看全部，或 --limit N）\n", truncated, name)
	}
	fmt.Fprintf(a.out, "  想装某个版本：pk install %s@<版本>\n", name)
	return jsonout.ExitOK
}

// containsStr 判断切片里有没有某个值（列表展示到处要用）。
func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// mustAlias 把 "lts" 包成 spec（别名由索引层解析）。
func mustAlias(name string) spec.Spec { return spec.MustParse(name) }

// firstLine 取多行错误的第一行（列表里不刷屏）。
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}

// activeVersions 汇总"当前生效版本"：config 的全局默认优先，台账引用兜底。
func (a *app) activeVersions(ledger *store.Ledger) map[string]string {
	active := make(map[string]string, len(ledger.Installs))
	for tool, version := range a.cfg.Tools {
		active[tool] = version
	}
	for _, in := range ledger.Installs {
		if _, ok := active[in.Tool]; ok {
			continue
		}
		for _, ref := range in.References {
			if ref.Kind == store.RefGlobal {
				active[in.Tool] = in.Version
				break
			}
		}
	}
	return active
}

func requiresOf(p *provider.Provider) []string {
	if p == nil || len(p.Requires) == 0 {
		return []string{}
	}
	return append([]string{}, p.Requires...)
}

// cmdWhich 解析 shim 到真实二进制路径（调试 shim / 双名一致性的唯一手段）。
func (a *app) cmdWhich(args []string) int {
	fs := a.newFlagSet("which")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}
	if fs.NArg() != 1 {
		return a.failUsage(fmt.Errorf("用法: pk which <命令>"))
	}
	name := fs.Arg(0)
	path, isShim, err := shim.Resolve(a.paths.Bin(), name, goos())
	if err != nil {
		return a.fail(err)
	}
	if path == "" {
		return a.fail(fmt.Errorf("命令 %q 不在 %s（`pk list` 看已装工具，`pk env` 检查 PATH）", name, a.paths.Bin()))
	}
	if a.json {
		return a.emit(struct {
			Schema int    `json:"schema"`
			Name   string `json:"name"`
			Path   string `json:"path"`
			Shim   bool   `json:"shim"`
		}{jsonout.SchemaVersion, name, path, isShim})
	}
	fmt.Fprintln(a.out, path)
	return jsonout.ExitOK
}

// gridThreshold 是"改用网格"的行数阈值：少量版本一版一行更好读，多了就得压缩。
const gridThreshold = 25

// majorOf 取版本的"大版本"分组键：常规是 major，0.x 用 major.minor（node 的 0.10/0.12 区别很大）。
func majorOf(v string) string {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) < 2 {
		return v
	}
	if parts[0] == "0" {
		return parts[0] + "." + parts[1]
	}
	return parts[0]
}

// printVersionGrid 按大版本分组、组内升序、多列铺开（列数按终端宽度算）。
//
// 参考 nvm ls-remote / pyenv install --list：这类列表动辄几百上千行，逐行铺开无法扫读；
// 分组 + 网格让人一眼看到"有哪些大版本、每个大版本有哪些补丁"。
func (a *app) printVersionGrid(shown []string, ltsOf map[string]string, mark func(string) string) {
	// 按 majorOf 分组，保持 shown 的顺序（升序），组也按首次出现顺序（旧 → 新）
	order := make([]string, 0, 8)
	groups := make(map[string][]string, 8)
	for _, v := range shown {
		k := majorOf(v)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], v)
	}
	// 单元格：版本号左对齐占 verWidth，标记前留一个空格，再留两格间隙
	verWidth := 0
	for _, v := range shown {
		if len(v) > verWidth {
			verWidth = len(v)
		}
	}
	cell := verWidth + 4
	labelWidth := 0
	for _, k := range order {
		if len(k)+2 > labelWidth { // "26.x"
			labelWidth = len(k) + 2
		}
	}
	const indent = 2
	width := termWidth()
	if width <= 0 {
		width = 100
	}
	cols := (width - indent - labelWidth - 2) / cell
	if cols < 1 {
		cols = 1
	}

	for _, k := range order {
		label := k + ".x"
		vs := groups[k]
		for row := 0; row*cols < len(vs); row++ {
			head := strings.Repeat(" ", indent+labelWidth+2)
			if row == 0 {
				head = fmt.Sprintf("%*s  ", indent+labelWidth, label)
			}
			var b strings.Builder
			b.WriteString(head)
			for col := 0; col < cols; col++ {
				idx := row*cols + col
				if idx >= len(vs) {
					break
				}
				v := vs[idx]
				cellText := v + " "
				if m := mark(v); m != "" {
					cellText = v + " " + m
				}
				b.WriteString(fmt.Sprintf("%-*s", cell, cellText))
			}
			fmt.Fprintln(a.out, strings.TrimRight(b.String(), " "))
		}
	}
}
