package state

// 装配层的测试：注入假 Commands，断言"界面看到的数据"与错误原样上行。
// 这些测试不起 HTTP、不起窗口，是 `make gui-test` 的主体。

import (
	"context"
	"errors"
	"testing"

	"github.com/packnest/packnest/cmd/pk-desktop/internal/autostart"
	"github.com/packnest/packnest/cmd/pk-desktop/internal/pkcli"
)

// fake 是 Commands 的假实现：每个方法都记一次调用，返回值由字段决定。
type fake struct {
	tools    pkcli.ListResult
	toolsErr error

	doctor    pkcli.DoctorResult
	doctorErr error

	mirror    pkcli.Mirror
	mirrorErr error

	probe []pkcli.ProbeEntry
	gc    pkcli.GCResult
	which pkcli.Which
	env   pkcli.EnvDoc
	avail []pkcli.AvailableTool
	vers  pkcli.Versions

	installedSpec string
	uninstalled   string
	used          string
	usedMirror    string
	wroteNPMRC    bool
}

func (f *fake) List(context.Context) (pkcli.ListResult, error) { return f.tools, f.toolsErr }

func (f *fake) Doctor(context.Context, bool, bool, bool) (pkcli.DoctorResult, error) {
	return f.doctor, f.doctorErr
}

func (f *fake) Install(_ context.Context, spec string, onEvent func(pkcli.Event)) error {
	f.installedSpec = spec
	if onEvent != nil {
		onEvent(pkcli.Event{Ev: "progress", Tool: "node", Version: "22.0.0", BytesDone: 5, BytesTotal: 10})
	}
	return nil
}

func (f *fake) Use(_ context.Context, tool, version string) error {
	f.used = tool + "@" + version
	return nil
}

func (f *fake) Mirrors(context.Context) (pkcli.Mirror, error) { return f.mirror, f.mirrorErr }

func (f *fake) UseMirror(_ context.Context, name string) error {
	f.usedMirror = name
	return nil
}

func (f *fake) GC(context.Context, bool, int) (pkcli.GCResult, error) { return f.gc, nil }

func (f *fake) UninstallTool(_ context.Context, tool string) error {
	f.uninstalled = tool
	return nil
}

func (f *fake) Which(context.Context, string) (pkcli.Which, error) { return f.which, nil }

func (f *fake) Environment(context.Context, bool) (pkcli.EnvDoc, error) { return f.env, nil }

func (f *fake) MirrorWrite(context.Context, string) (pkcli.MirrorWriteResult, error) {
	f.wroteNPMRC = true
	return pkcli.MirrorWriteResult{File: "/home/u/.npmrc", Registry: "https://registry.npmmirror.com"}, nil
}

func (f *fake) Available(context.Context) ([]pkcli.AvailableTool, error) { return f.avail, nil }

func (f *fake) ToolVersions(context.Context, string, int, bool) (pkcli.Versions, error) {
	return f.vers, nil
}

func (f *fake) Probe(context.Context, string) ([]pkcli.ProbeEntry, error) { return f.probe, nil }

func (f *fake) CheckSelfUpdate(context.Context) (pkcli.SelfUpdate, error) {
	return pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1"}, nil
}

func (f *fake) SelfUpdate(context.Context) (pkcli.SelfUpdate, error) {
	return pkcli.SelfUpdate{Current: "1.9.1", Latest: "1.9.1"}, nil
}

// svcWith 造一个注入了假 CLI 的 Service。
func svcWith(f *fake) *Service {
	return &Service{
		GUIVersion: "0.12.0",
		Locate: func() (Commands, pkcli.Info, error) {
			return f, pkcli.Info{Schema: 1, Name: "packnest", Version: "1.9.1", Path: "/usr/local/bin/pk"}, nil
		},
	}
}

func TestStatusReportsCLIPathAndPlatform(t *testing.T) {
	st := svcWith(&fake{}).Status(context.Background())
	if !st.OK {
		t.Fatal("状态应为 ok")
	}
	if st.Path != "/usr/local/bin/pk" {
		t.Fatalf("path = %q", st.Path)
	}
	if st.CLI == nil || st.CLI.Version != "1.9.1" {
		t.Fatalf("cli = %+v", st.CLI)
	}
	if st.Platform == "" {
		t.Fatal("platform 必须给（界面据此选安装命令）")
	}
}

func TestStatusWithoutCLIKeepsReason(t *testing.T) {
	svc := &Service{Locate: func() (Commands, pkcli.Info, error) { return nil, pkcli.Info{}, pkcli.ErrNotFound }}
	st := svc.Status(context.Background())
	if st.OK {
		t.Fatal("没装 CLI 时不该 ok")
	}
	if st.Reason != pkcli.ErrNotFound.Error() {
		t.Fatalf("reason = %q", st.Reason)
	}
	if st.CLI != nil {
		t.Fatal("没装 CLI 时不该有 cli 字段")
	}
}

func TestToolsNeverReturnsNilSlices(t *testing.T) {
	// GUI 的契约：数组永远不是 null（前端直接 v-for），装配层负责兜底。
	res, err := svcWith(&fake{}).Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Tools == nil || res.Outdated == nil {
		t.Fatalf("空结果也必须是空数组：%+v", res)
	}
}

func TestDoctorCarriesIssuesAndReload(t *testing.T) {
	f := &fake{doctor: pkcli.DoctorResult{
		Issues: []pkcli.Issue{{Code: "PATH_MISSING", Severity: "warn", Message: "PATH 里没有 pk bin"}},
		Fixed:  []string{"写入 ~/.zshrc 托管块"},
		Reload: &pkcli.ReloadAdvice{Needed: true, Command: "source ~/.zshrc"},
	}}
	got, err := svcWith(f).Doctor(context.Background(), true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Code != "PATH_MISSING" {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if got.Reload == nil || got.Reload.Command == "" {
		t.Fatal("reload 建议丢了")
	}
}

func TestErrorsPassThrough(t *testing.T) {
	// 退出码 5（锁被占用）必须原样上到界面：HTTP 层靠 errors.Is 映射 409。
	boom := pkcli.ErrBusy
	_, err := svcWith(&fake{toolsErr: boom}).Tools(context.Background())
	if !errors.Is(err, pkcli.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestWritePathsRequireExplicitCall(t *testing.T) {
	f := &fake{}
	svc := svcWith(f)

	// 读操作不该偷偷写
	if _, err := svc.Tools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.uninstalled != "" || f.used != "" || f.wroteNPMRC {
		t.Fatal("读操作触发了写：卸载/切换/写 npmrc")
	}

	if err := svc.Uninstall(context.Background(), "node"); err != nil {
		t.Fatal(err)
	}
	if f.uninstalled != "node" {
		t.Fatalf("uninstalled = %q", f.uninstalled)
	}
}

func TestMirrorUseOnlyWhenGiven(t *testing.T) {
	f := &fake{mirror: pkcli.Mirror{Current: "auto"}}
	svc := svcWith(f)
	if _, err := svc.Mirrors(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if f.usedMirror != "" {
		t.Fatal("没给 use 却切了档")
	}
	if _, err := svc.Mirrors(context.Background(), "npmmirror"); err != nil {
		t.Fatal(err)
	}
	if f.usedMirror != "npmmirror" {
		t.Fatalf("usedMirror = %q", f.usedMirror)
	}
}

func TestInstallStreamsEvents(t *testing.T) {
	f := &fake{}
	var got []string
	err := svcWith(f).Install(context.Background(), "node@22", func(ev pkcli.Event) {
		got = append(got, ev.Ev)
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.installedSpec != "node@22" || len(got) != 1 || got[0] != "progress" {
		t.Fatalf("spec=%q events=%v", f.installedSpec, got)
	}
}

func TestVersionsNeedsTool(t *testing.T) {
	if _, err := svcWith(&fake{}).Versions(context.Background(), "", false); err == nil {
		t.Fatal("空工具名应该报错（否则会跑 pk list ''）")
	}
}

func TestAutostartDefaultsOff(t *testing.T) {
	// 自启默认关：没显式打开过就是 false，且不需要 CLI。
	st := svcWith(&fake{}).Autostart()
	if st.Enabled {
		t.Fatal("默认应当是关闭状态")
	}
	_ = autostart.Label // 保持引用，防"未使用导入"
}
