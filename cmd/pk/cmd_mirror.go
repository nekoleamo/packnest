package main

import (
	"flag"
	"fmt"
	"runtime"
	"strings"

	"github.com/packnest/packnest/internal/jsonout"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
)

func goos() string { return runtime.GOOS }

// cmdMirror 是镜像档的门面命令：list / probe / use（write 属 M5）。
func (a *app) cmdMirror(args []string) int {
	if len(args) == 0 {
		return a.failUsage(fmt.Errorf("用法: pk mirror list|probe|use <档位>"))
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return a.mirrorList()
	case "probe":
		return a.mirrorProbe(rest)
	case "use":
		return a.mirrorUse(rest)
	case "write":
		return a.notImplemented("mirror write", "M5")
	}
	return a.failUsage(fmt.Errorf("未知子命令 pk mirror %s", sub))
}

func (a *app) mirrorList() int {
	current := a.cfg.Mirror
	if a.json {
		type row struct {
			Name     string `json:"name"`
			Display  string `json:"display"`
			Registry string `json:"registry"`
			Current  bool   `json:"current"`
		}
		rows := make([]row, 0, len(mirror.Profiles()))
		for _, p := range mirror.Profiles() {
			rows = append(rows, row{p.Name, p.Display, p.Registry, p.Name == current})
		}
		return a.emit(struct {
			Schema  int    `json:"schema"`
			Current string `json:"current"`
			Mirror  []row  `json:"mirrors"`
		}{jsonout.SchemaVersion, current, rows})
	}
	fmt.Fprintf(a.out, "当前档位: %s\n", current)
	for _, p := range mirror.Profiles() {
		mark := " "
		if p.Name == current {
			mark = "*"
		}
		fmt.Fprintf(a.out, "  %s %-8s %-16s %s\n      %s\n", mark, p.Name, p.Display, p.Registry, p.Note)
	}
	fmt.Fprintln(a.out, "  (auto 表示自动探测：普通命令只探首选 + 上轮最优，结果缓存 6h)")
	return jsonout.ExitOK
}

func (a *app) mirrorProbe(args []string) int {
	fs := flag.NewFlagSet("mirror probe", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	only := fs.String("tool", "", "只探测某个工具的索引源")
	if err := parseFlexible(fs, args); err != nil {
		return a.failUsage(err)
	}

	all, err := provider.All()
	if err != nil {
		return a.fail(err)
	}
	prober := &mirror.Prober{Client: a.client, Path: a.paths.Probe(), TTL: mirror.DefaultTTL}

	var urls []string
	for _, p := range all {
		if *only != "" && p.ID != *only {
			continue
		}
		urls = append(urls, p.IndexURLs(a.cfg.Mirror)...)
	}
	if len(urls) == 0 {
		return a.fail(fmt.Errorf("没有可探测的源（检查 --tool 与 provider 清单）"))
	}
	results := prober.Probe(a.context(), urls)

	if a.json {
		return a.emit(struct {
			Schema  int             `json:"schema"`
			Results []mirror.Result `json:"results"`
		}{jsonout.SchemaVersion, results})
	}
	ok := 0
	for _, r := range results {
		status := "OK  "
		if !r.OK {
			status = "FAIL"
		} else {
			ok++
		}
		fmt.Fprintf(a.out, "  %s %5dms %s\n", status, r.LatencyMS, r.URL)
		if !r.OK && r.Err != "" {
			fmt.Fprintf(a.out, "       └ %s\n", r.Err)
		}
	}
	fmt.Fprintf(a.out, "\n%d/%d 可用（结果缓存 6h 于 %s）\n", ok, len(results), a.paths.Probe())
	if ok == 0 {
		return jsonout.ExitNetwork
	}
	return jsonout.ExitOK
}

func (a *app) mirrorUse(args []string) int {
	if len(args) != 1 {
		return a.failUsage(fmt.Errorf("用法: pk mirror use <%s>", strings.Join(mirror.Names(), "|")))
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if _, err := mirror.Get(name); err != nil {
		return a.failUsage(err)
	}
	a.cfg.Mirror = name
	if err := a.cfg.Save(a.paths.Config()); err != nil {
		return a.fail(err)
	}
	if a.json {
		return a.emit(struct {
			Schema int    `json:"schema"`
			Mirror string `json:"mirror"`
			Config string `json:"config"`
		}{jsonout.SchemaVersion, name, a.paths.Config()})
	}
	fmt.Fprintf(a.out, "镜像档已切到 %s（写入 %s）\n", name, a.paths.Config())
	fmt.Fprintln(a.out, "下次安装即生效；`pk env --shell bash` 可查看随之变化的 registry 变量")
	return jsonout.ExitOK
}
