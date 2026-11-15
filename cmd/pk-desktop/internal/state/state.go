// Package state 是 GUI 的数据装配层：把「用户想做什么」翻译成 `pk <cmd> --json` 的一次调用，
// 并把结果整理成**与传输方式无关**的结构体。
//
// 为什么要有这一层（2026-11-15 mygo 重写）：
//   - 原生界面与 `--serve` 浏览器形态必须永远拿同一份数据，"浏览器对、桌面错"是最贵的 bug；
//   - 视图测试不需要起 HTTP：注入假 Commands 就能断言页面看到的内容；
//   - HTTP 层退化成"读参数 → 调这里 → 写 JSON"，纯 Go 改造也不会碰到业务真相。
//
// 纪律不变：不 import 主模块的 internal/*，业务真相永远在 CLI 与 PK_ROOT 里。
package state

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/autostart"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/update"
)

// Commands 是装配层需要的 CLI 能力。生产实现是 *pkcli.Client，测试注入假实现。
//
// 只列 GUI 真正用到的 9 个命令面 + 自更新：加了新方法就要在 state 里有调用点，
// 否则说明这个接口比实际需要的宽。
type Commands interface {
	List(ctx context.Context) (pkcli.ListResult, error)
	Doctor(ctx context.Context, globals, probe, fix bool) (pkcli.DoctorResult, error)
	Install(ctx context.Context, spec string, onEvent func(pkcli.Event)) error
	Use(ctx context.Context, tool, version string) error
	Mirrors(ctx context.Context) (pkcli.Mirror, error)
	UseMirror(ctx context.Context, name string) error
	GC(ctx context.Context, dryRun bool, keep int) (pkcli.GCResult, error)
	UninstallTool(ctx context.Context, tool string) error
	Which(ctx context.Context, name string) (pkcli.Which, error)
	Environment(ctx context.Context, project bool) (pkcli.EnvDoc, error)
	MirrorWrite(ctx context.Context, file string) (pkcli.MirrorWriteResult, error)
	Available(ctx context.Context) ([]pkcli.AvailableTool, error)
	ToolVersions(ctx context.Context, tool string, limit int, all bool) (pkcli.Versions, error)
	Probe(ctx context.Context, tool string) ([]pkcli.ProbeEntry, error)
	CheckSelfUpdate(ctx context.Context) (pkcli.SelfUpdate, error)
	SelfUpdate(ctx context.Context) (pkcli.SelfUpdate, error)
}

// Locator 找到已安装的 pk，并返回它的版本信息。测试用假实现替换。
type Locator func() (Commands, pkcli.Info, error)

// Service 是 GUI 的全部数据入口。
type Service struct {
	// Locate 找 CLI；生产传 pkcli.Locate 的包装（见 Locate）
	Locate Locator
	// GUIVersion 是 GUI 自己的版本（检查更新用）
	GUIVersion string
	// Exe 返回自己的可执行文件路径（GUI 自更新要替换它）
	Exe func() (string, error)
}

// Locate 把 pkcli.Locate 包成 Locator（生产用）。
func Locate() (Commands, pkcli.Info, error) {
	cl, info, err := pkcli.Locate()
	if err != nil {
		return nil, pkcli.Info{}, err
	}
	return cl, info, nil
}

// Status 是首屏状态条（`/api/status`）。
type Status struct {
	OK     bool        `json:"ok"`
	Reason string      `json:"reason,omitempty"`
	Path   string      `json:"path,omitempty"`
	CLI    *pkcli.Info `json:"cli,omitempty"`
	// Platform 让界面给"对应平台"的安装命令（Windows 用户不该被塞 sh 命令）
	Platform string `json:"platform"`
}

// Status 读"CLI 在不在、什么版本"。
func (s *Service) Status(ctx context.Context) Status {
	out := Status{Platform: runtime.GOOS}
	cmds, info, err := s.Locate()
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	_ = cmds
	out.OK = true
	out.CLI = &info
	out.Path = info.Path
	return out
}

// client 取可用的 CLI；没装就给 ErrNotFound（HTTP 层映射 412，界面映射"先装 CLI"）。
func (s *Service) client() (Commands, error) {
	cmds, _, err := s.Locate()
	if err != nil {
		return nil, err
	}
	return cmds, nil
}

// Tools 是已装工具 + 可升级提示（`/api/tools`）。
type Tools struct {
	Tools    []pkcli.Tool         `json:"tools"`
	Outdated []pkcli.OutdatedTool `json:"outdated"`
}

// Tools 读已装清单（首屏 + 刷新按钮）。
func (s *Service) Tools(ctx context.Context) (Tools, error) {
	cmds, err := s.client()
	if err != nil {
		return Tools{}, err
	}
	ctx, cancel := timeout(ctx, 60*time.Second)
	defer cancel()
	res, err := cmds.List(ctx)
	if err != nil {
		return Tools{}, err
	}
	if res.Tools == nil {
		res.Tools = []pkcli.Tool{}
	}
	if res.Outdated == nil {
		res.Outdated = []pkcli.OutdatedTool{}
	}
	return Tools{Tools: res.Tools, Outdated: res.Outdated}, nil
}

// Doctor 是体检结果（issues 永远不是 null，前端直接 v-for）。
type Doctor struct {
	Issues []pkcli.Issue       `json:"issues"`
	Fixed  []string            `json:"fixed"`
	Reload *pkcli.ReloadAdvice `json:"reload,omitempty"`
}

// Doctor 跑体检；globals 附带全局包检查，probe 实测原生模块，fix 修 PATH 托管块与入口。
func (s *Service) Doctor(ctx context.Context, globals, probe, fix bool) (Doctor, error) {
	cmds, err := s.client()
	if err != nil {
		return Doctor{}, err
	}
	ctx, cancel := timeout(ctx, 90*time.Second)
	defer cancel()
	res, err := cmds.Doctor(ctx, globals, probe, fix)
	if err != nil {
		return Doctor{}, err
	}
	if res.Issues == nil {
		res.Issues = []pkcli.Issue{}
	}
	return Doctor{Issues: res.Issues, Fixed: res.Fixed, Reload: res.Reload}, nil
}

// Install 装一个 spec，事件逐条回调（NDJSON 的来源）。
func (s *Service) Install(ctx context.Context, spec string, onEvent func(pkcli.Event)) error {
	cmds, err := s.client()
	if err != nil {
		return err
	}
	return cmds.Install(ctx, spec, onEvent)
}

// Use 切换全局默认版本。
func (s *Service) Use(ctx context.Context, tool, version string) error {
	cmds, err := s.client()
	if err != nil {
		return err
	}
	ctx, cancel := timeout(ctx, 60*time.Second)
	defer cancel()
	return cmds.Use(ctx, tool, version)
}

// Uninstall 卸载某工具的全部版本（调用方必须先拿到用户的显式确认）。
func (s *Service) Uninstall(ctx context.Context, tool string) error {
	cmds, err := s.client()
	if err != nil {
		return err
	}
	ctx, cancel := timeout(ctx, 300*time.Second)
	defer cancel()
	return cmds.UninstallTool(ctx, tool)
}

// Mirrors 读镜像档列表（变更后返回新列表，界面一次拿到"改完的样子"）。
func (s *Service) Mirrors(ctx context.Context, use string) (pkcli.Mirror, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.Mirror{}, err
	}
	ctx, cancel := timeout(ctx, 30*time.Second)
	defer cancel()
	if use != "" {
		if err := cmds.UseMirror(ctx, use); err != nil {
			return pkcli.Mirror{}, err
		}
	}
	return cmds.Mirrors(ctx)
}

// Probe 并行探测各镜像源（慢，界面上单独一个按钮）。
type Probe struct {
	Results []pkcli.ProbeEntry `json:"results"`
}

// Probe 探测源。
func (s *Service) Probe(ctx context.Context, tool string) (Probe, error) {
	cmds, err := s.client()
	if err != nil {
		return Probe{}, err
	}
	ctx, cancel := timeout(ctx, 120*time.Second)
	defer cancel()
	rows, err := cmds.Probe(ctx, tool)
	if err != nil {
		return Probe{}, err
	}
	if rows == nil {
		rows = []pkcli.ProbeEntry{}
	}
	return Probe{Results: rows}, nil
}

// GC 回收磁盘。run=false 是 dry-run，keep>0 表示每个工具保留最新 N 个版本。
func (s *Service) GC(ctx context.Context, run bool, keep int) (pkcli.GCResult, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.GCResult{}, err
	}
	ctx, cancel := timeout(ctx, 120*time.Second)
	defer cancel()
	return cmds.GC(ctx, !run, keep)
}

// Available 是"每个工具的最新可装版本"。
type Available struct {
	Available []pkcli.AvailableTool `json:"available"`
}

// Available 读索引（24h 缓存，不刷新）。
func (s *Service) Available(ctx context.Context) (Available, error) {
	cmds, err := s.client()
	if err != nil {
		return Available{}, err
	}
	ctx, cancel := timeout(ctx, 120*time.Second)
	defer cancel()
	rows, err := cmds.Available(ctx)
	if err != nil {
		return Available{}, err
	}
	if rows == nil {
		rows = []pkcli.AvailableTool{}
	}
	return Available{Available: rows}, nil
}

// Versions 是某工具的可装/已装版本。
func (s *Service) Versions(ctx context.Context, tool string, all bool) (pkcli.Versions, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.Versions{}, err
	}
	if tool == "" {
		return pkcli.Versions{}, errors.New("需要工具名")
	}
	ctx, cancel := timeout(ctx, 120*time.Second)
	defer cancel()
	return cmds.ToolVersions(ctx, tool, 40, all)
}

// Which 把命令名解析回真实二进制。
func (s *Service) Which(ctx context.Context, name string) (pkcli.Which, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.Which{}, err
	}
	if name == "" {
		return pkcli.Which{}, errors.New("需要命令名")
	}
	ctx, cancel := timeout(ctx, 30*time.Second)
	defer cancel()
	return cmds.Which(ctx, name)
}

// Env 只读地看当前生效的 PATH / 环境变量。
func (s *Service) Env(ctx context.Context, project bool) (pkcli.EnvDoc, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.EnvDoc{}, err
	}
	ctx, cancel := timeout(ctx, 30*time.Second)
	defer cancel()
	return cmds.Environment(ctx, project)
}

// MirrorWrite 把当前档位的 registry 写进 npmrc（调用方必须先弹确认）。
func (s *Service) MirrorWrite(ctx context.Context, file string) (pkcli.MirrorWriteResult, error) {
	cmds, err := s.client()
	if err != nil {
		return pkcli.MirrorWriteResult{}, err
	}
	ctx, cancel := timeout(ctx, 60*time.Second)
	defer cancel()
	return cmds.MirrorWrite(ctx, file)
}

// Autostart 的读写不需要 CLI（只动用户级启动项）。
func (s *Service) Autostart() autostart.Status { return autostart.Get() }

// SetAutostart 打开/关闭开机自启。
func (s *Service) SetAutostart(enable bool) (autostart.Status, error) { return autostart.Set(enable) }

// UpdateCheck 是"检查更新"的整体结果（GUI 与 CLI 两个版本）。
func (s *Service) UpdateCheck(ctx context.Context) (update.Result, error) {
	cmds, err := s.client()
	if err != nil {
		return update.Result{}, err
	}
	ctx, cancel := timeout(ctx, 120*time.Second)
	defer cancel()
	return update.Check(ctx, s.GUIVersion, func(c context.Context) (update.CLIInfo, error) {
		info, cerr := cmds.CheckSelfUpdate(c)
		return update.CLIInfo{
			Current: info.Current, Latest: info.Latest, UpdateAvailable: info.UpdateAvailable,
			Channel: info.Channel, ChannelHint: info.ChannelHint, Source: info.Source,
			Sources: convertSources(info.Sources),
		}, cerr
	}, fetchBytes())
}

// UpdateApply 是升级动作的结果：Target 指明升的是谁，Result 是检查结果，Error 给界面显示。
type UpdateApply struct {
	Target string `json:"target"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error"`
	OK     bool   `json:"ok"`
}

// UpdateApply 执行升级：target=gui 替换自身；target=cli 全部交给 `pk upgrade --self`。
func (s *Service) UpdateApply(ctx context.Context, target string) (UpdateApply, error) {
	cmds, err := s.client()
	if err != nil {
		return UpdateApply{}, err
	}
	ctx, cancel := timeout(ctx, 300*time.Second)
	defer cancel()

	switch target {
	case "cli":
		res, serr := cmds.SelfUpdate(ctx)
		return UpdateApply{Target: "cli", Result: res, Error: errString(serr), OK: serr == nil}, nil
	case "gui", "":
		up, cerr := s.UpdateCheck(ctx)
		if cerr != nil {
			return UpdateApply{}, cerr
		}
		if !up.GUI.UpdateAvailable {
			return UpdateApply{Target: "gui", Result: up, OK: true}, nil
		}
		if up.GUI.Channel == "scoop" || up.GUI.Channel == "brew" {
			return UpdateApply{Target: "gui", Result: up,
				Error: "这个 GUI 是 " + up.GUI.Channel + " 装的，请用：" + up.GUI.ChannelHint}, nil
		}
		exe, eerr := s.exePath()
		if eerr != nil {
			return UpdateApply{}, eerr
		}
		_, aerr := update.ApplyGUI(ctx, update.ApplyOptions{
			Current:     up.GUI.Latest,
			ArtifactURL: up.GUI.ArtifactURL,
			Exe:         exe,
			Get:         fetchBytes(),
			Download:    fetchToFile(),
		})
		up.GUI.Restart = aerr == nil
		return UpdateApply{Target: "gui", Result: up, Error: errString(aerr), OK: aerr == nil}, nil
	}
	return UpdateApply{}, errors.New("未知的升级目标 " + target)
}

// exePath 返回自己（GUI 二进制）的路径。
func (s *Service) exePath() (string, error) {
	if s.Exe != nil {
		return s.Exe()
	}
	return exe()
}

// timeout 给一次子进程调用套超时（界面上"取消"要能真的杀掉 pk）。
func timeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// convertSources 把 pkcli 里的匿名结构转成 update.Source（两个包各留一份类型，避免互相依赖）。
func convertSources(in []struct {
	Name      string `json:"name"`
	Base      string `json:"base"`
	LatestURL string `json:"latest_url"`
	LatencyMS int64  `json:"latency_ms"`
}) []update.Source {
	out := make([]update.Source, 0, len(in))
	for _, s := range in {
		out = append(out, update.Source{Name: s.Name, Base: s.Base, LatestURL: s.LatestURL, LatencyMS: s.LatencyMS})
	}
	return out
}
