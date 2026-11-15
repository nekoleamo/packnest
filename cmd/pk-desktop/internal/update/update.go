// Package update 让桌面端检查并升级**自己**（GUI），以及代为触发 CLI 的自更新。
//
// 边界（与 CLI 的分工，见 docs/gui.md）：
//   - **CLI 的版本检查与自更新完全交给 CLI**：这里只跑 `pk upgrade --self --check --json`
//     和 `pk upgrade --self --json`，不重复实现渠道判断、验签、原子替换；
//   - **GUI 自己的产物**由这里下载与替换（CLI 不管 GUI）：先验 `checksums.txt` 的 sshsig
//     再比 sha256，然后按平台替换（macOS 替换 .app 包、Windows 换 exe、Linux 换二进制）；
//   - **地址自动选**：CLI 的 `--check --json` 会带回它探测到的源与延迟（`sources[]`，
//     已按延迟排好），这里直接沿用那个顺序，只把 `releases/download` 换成各自的 raw 地址；
//   - **渠道分流**：scoop / brew / 包管理器装的 GUI 不自我替换，只给出该渠道的命令。
package update

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// 发布仓库（与 scripts/install-gui.sh 的默认值一致）。
const (
	GiteeRepo  = "null_593_5354/packnest"
	GitHubRepo = "nekoleamo/packnest"
)

//go:embed allowed_signers
var allowedSigners string

// RawLatestURLs 是各源的"最新版本"文本地址（由发布脚本写入 scripts/latest-gui）。
//
// 顺序不重要：真正的顺序由 CLI 的探测结果决定（见 pickOrder）。
var RawLatestURLs = map[string]string{
	"gitee":  "https://gitee.com/" + GiteeRepo + "/raw/main/scripts/latest-gui",
	"github": "https://raw.githubusercontent.com/" + GitHubRepo + "/main/scripts/latest-gui",
}

// ReleaseBases 是各源的 release 下载基地址。
var ReleaseBases = map[string]string{
	"gitee":  "https://gitee.com/" + GiteeRepo + "/releases/download",
	"github": "https://github.com/" + GitHubRepo + "/releases/download",
}

// Source 是 CLI 报回来的源（`pk upgrade --self --check --json` 的 sources[]）。
type Source struct {
	Name      string `json:"name"`
	Base      string `json:"base"`
	LatestURL string `json:"latest_url"`
	LatencyMS int64  `json:"latency_ms"`
}

// CLIInfo 是 CLI 自检的结果（字段与 CLI 的 Info 对齐，只取 GUI 需要的）。
type CLIInfo struct {
	Current         string   `json:"current"`
	Latest          string   `json:"latest"`
	UpdateAvailable bool     `json:"update_available"`
	Channel         string   `json:"channel"`
	ChannelHint     string   `json:"channel_hint"`
	Source          string   `json:"source"`
	Sources         []Source `json:"sources"`
}

// Result 是"检查更新"的整体结果（GUI 直接渲染它）。
type Result struct {
	GUI struct {
		Current         string `json:"current"`
		Latest          string `json:"latest"`
		UpdateAvailable bool   `json:"update_available"`
		Channel         string `json:"channel"`
		ChannelHint     string `json:"channel_hint,omitempty"`
		ArtifactURL     string `json:"artifact_url,omitempty"`
		// Restart 为 true 表示"已经替换完，重启后生效"。
		Restart bool `json:"restart"`
	} `json:"gui"`
	CLI CLIInfo `json:"cli"`
	// Source 是这次选中的源（由 CLI 的探测结果决定）。
	Source string `json:"source"`
	// Order 是候选源顺序（含探测延迟），给界面展示"从哪下、多快"。
	Order []Source `json:"order"`
}

// Check 汇总两个版本：GUI（本进程）与 CLI（子进程问）。
//
// cliCheck 是跑 `pk upgrade --self --check --json` 的函数（由调用方注入，便于测试）。
func Check(ctx context.Context, guiCurrent string, cliCheck func(context.Context) (CLIInfo, error), get func(context.Context, string) ([]byte, error)) (Result, error) {
	var res Result
	res.GUI.Current = guiCurrent
	cli, cliErr := cliCheck(ctx)
	res.CLI = cli
	if cliErr != nil && len(cli.Sources) == 0 {
		// CLI 连"源探测"都没跑成：GUI 自己的检查也没法挑源，直接报错
		return res, cliErr
	}
	res.Order = orderSources(cli.Sources)

	guiVer, srcName, err := latestGUI(ctx, res.Order, get)
	if err != nil {
		return res, err
	}
	res.Source = srcName
	res.GUI.Latest = guiVer
	res.GUI.UpdateAvailable = newer(guiVer, guiCurrent)
	ch := guiChannel()
	res.GUI.Channel, res.GUI.ChannelHint = ch.Name, ch.Hint
	if res.GUI.UpdateAvailable {
		res.GUI.ArtifactURL = artifactURL(srcName, guiVer, runtime.GOOS, runtime.GOARCH)
	}
	return res, nil
}

// orderSources 把 CLI 报的源整理成"最快优先"的顺序；CLI 没给就退回固定顺序（gitee → github）。
func orderSources(in []Source) []Source {
	out := make([]Source, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		if RawLatestURLs[s.Name] == "" {
			continue
		}
		seen[s.Name] = true
		out = append(out, s)
	}
	for _, name := range []string{"gitee", "github"} {
		if !seen[name] {
			out = append(out, Source{Name: name})
		}
	}
	return out
}

// latestGUI 按源顺序取最新 GUI 版本（第一个拿到的就用它）。
func latestGUI(ctx context.Context, order []Source, get func(context.Context, string) ([]byte, error)) (string, string, error) {
	var lastErr error
	for _, s := range order {
		u := RawLatestURLs[s.Name]
		if u == "" {
			continue
		}
		data, err := get(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "v"))
		if _, err := parseVersion(v); err != nil {
			lastErr = fmt.Errorf("%s 返回的版本号 %q 不合法", u, v)
			continue
		}
		return v, s.Name, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的更新源")
	}
	return "", "", lastErr
}

// artifactURL 是某个版本某个平台的 GUI 产物地址。
func artifactURL(source, ver, goos, goarch string) string {
	base := ReleaseBases[source]
	name := "packnest-desktop_" + strings.TrimPrefix(ver, "v") + "_" + goos + "_" + goarch
	switch goos {
	case "windows":
		name += ".zip"
	case "darwin":
		name += ".app.zip"
	default:
		name += ".tar.gz"
	}
	return strings.TrimSuffix(base, "/") + "/gui-v" + strings.TrimPrefix(ver, "v") + "/" + name
}

// guiChannel 判断当前 GUI 是不是包管理器装的（那些不该自我替换）。
func guiChannel() struct{ Name, Hint string } {
	exe, err := os.Executable()
	if err != nil {
		return struct{ Name, Hint string }{"unknown", ""}
	}
	p := strings.ToLower(strings.ReplaceAll(exe, `\`, "/"))
	switch {
	case strings.Contains(p, "/scoop/apps/packnest-desktop/"):
		return struct{ Name, Hint string }{"scoop", "scoop update packnest-desktop"}
	case strings.Contains(p, "/cellar/packnest-desktop/"):
		return struct{ Name, Hint string }{"brew", "brew upgrade packnest-desktop"}
	}
	return struct{ Name, Hint string }{"script", "sh scripts/install-gui.sh（或应用内“检查更新”）"}
}

// MarshalCLI 解析 CLI 的 `--json` 输出（只取需要的字段）。
func MarshalCLI(stdout []byte) (CLIInfo, error) {
	var out CLIInfo
	err := json.Unmarshal(stdout, &out)
	return out, err
}

// runtimeGOOS 抽出来只是为了测试能替换（生产就是 runtime.GOOS）。
func runtimeGOOS() string { return runtime.GOOS }
