package update

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 检查更新：源顺序、回落、产物地址。这三件事错一个，用户就会看到"永远转圈"或者下载 404。

func TestOrderSources(t *testing.T) {
	// CLI 按延迟排好 → 原样保留；重复的去掉；不认识的源丢掉；缺的补在最后
	in := []Source{
		{Name: "gitee", LatencyMS: 40},
		{Name: "gitee", LatencyMS: 99},
		{Name: "some-mirror", LatencyMS: 10},
		{Name: "github", LatencyMS: 300},
	}
	got := orderSources(in)
	if len(got) != 2 || got[0].Name != "gitee" || got[1].Name != "github" {
		t.Fatalf("orderSources = %+v，期望 [gitee github]", got)
	}
	if got[0].LatencyMS != 40 || got[1].LatencyMS != 300 {
		t.Fatalf("延迟信息丢了: %+v", got)
	}

	// CLI 什么都没报 → 退回固定顺序
	got = orderSources(nil)
	if len(got) != 2 || got[0].Name != "gitee" || got[1].Name != "github" {
		t.Fatalf("空输入应退回 gitee→github，实际 %+v", got)
	}
}

func TestLatestGUI(t *testing.T) {
	order := []Source{{Name: "gitee"}, {Name: "github"}}
	fails := func(ctx context.Context, u string) ([]byte, error) { return nil, errors.New("连不上") }

	// 第一个源挂了 → 用第二个
	got, src, err := latestGUI(context.Background(), order, func(ctx context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "gitee") {
			return nil, errors.New("446")
		}
		return []byte("v0.13.0\n"), nil
	})
	if err != nil || got != "0.13.0" || src != "github" {
		t.Fatalf("回落失败: ver=%q src=%q err=%v", got, src, err)
	}

	// 第一个源返回垃圾版本 → 继续用第二个（别把垃圾当版本）
	got, src, err = latestGUI(context.Background(), order, func(ctx context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "gitee") {
			return []byte("<html>404</html>"), nil
		}
		return []byte("0.13.1"), nil
	})
	if err != nil || got != "0.13.1" || src != "github" {
		t.Fatalf("垃圾版本没被跳过: ver=%q src=%q err=%v", got, src, err)
	}

	// 全挂 → 报错（界面要显示"这次请求没完成"，不是永远转圈）
	if _, _, err := latestGUI(context.Background(), order, fails); err == nil {
		t.Fatal("所有源都挂了却没报错")
	}

	// 全是垃圾 → 也要报错，并说清是哪来的版本号不合法
	if _, _, err := latestGUI(context.Background(), []Source{{Name: "gitee"}}, func(context.Context, string) ([]byte, error) {
		return []byte("not-a-version"), nil
	}); err == nil || !strings.Contains(err.Error(), "不合法") {
		t.Fatalf("垃圾版本应报「不合法」，实际 %v", err)
	}

	// 顺序里全是未知源（理论上 orderSources 已过滤，这里兜底"没有可用的更新源"）
	if _, _, err := latestGUI(context.Background(), []Source{{Name: "nope"}}, fails); err == nil {
		t.Fatal("没有可用源时应当报错")
	}
}

func TestArtifactURL(t *testing.T) {
	cases := []struct{ source, ver, goos, arch, want string }{
		{"gitee", "0.13.0", "darwin", "arm64",
			"https://gitee.com/" + GiteeRepo + "/releases/download/gui-v0.13.0/packnest-desktop_0.13.0_darwin_arm64.app.zip"},
		{"gitee", "v0.13.0", "windows", "amd64",
			"https://gitee.com/" + GiteeRepo + "/releases/download/gui-v0.13.0/packnest-desktop_0.13.0_windows_amd64.zip"},
		{"github", "1.0.0", "linux", "arm64",
			"https://github.com/" + GitHubRepo + "/releases/download/gui-v1.0.0/packnest-desktop_1.0.0_linux_arm64.tar.gz"},
	}
	for _, c := range cases {
		if got := artifactURL(c.source, c.ver, c.goos, c.arch); got != c.want {
			t.Errorf("artifactURL(%s,%s,%s,%s):\n got %s\nwant %s", c.source, c.ver, c.goos, c.arch, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	sources := []Source{{Name: "gitee", LatencyMS: 30}, {Name: "github", LatencyMS: 200}}
	get := func(ctx context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "gitee") {
			return []byte("0.13.0"), nil
		}
		return nil, errors.New("连不上")
	}

	// 正常：GUI 落后 → 给出产物地址 + 源 + 顺序
	res, err := Check(ctx, "0.12.3", func(context.Context) (CLIInfo, error) {
		return CLIInfo{Current: "1.9.1", Latest: "1.9.1", Sources: sources}, nil
	}, get)
	if err != nil {
		t.Fatalf("Check 报错: %v", err)
	}
	if !res.GUI.UpdateAvailable || res.GUI.Latest != "0.13.0" || res.Source != "gitee" {
		t.Fatalf("结果不对: %+v", res.GUI)
	}
	if res.GUI.ArtifactURL == "" || len(res.Order) != 2 {
		t.Fatalf("产物地址/源顺序缺失: %+v", res)
	}
	if res.GUI.Channel == "" {
		t.Fatal("渠道没判断出来")
	}

	// 已是最新 → 不给产物地址（界面就不显示"升级"按钮）
	res, err = Check(ctx, "0.13.0", func(context.Context) (CLIInfo, error) {
		return CLIInfo{Sources: sources}, nil
	}, get)
	if err != nil || res.GUI.UpdateAvailable || res.GUI.ArtifactURL != "" {
		t.Fatalf("已是最新却仍提示升级: %+v err=%v", res.GUI, err)
	}

	// CLI 探测失败但没给源 → 直接失败（GUI 挑不了源）
	if _, err := Check(ctx, "0.12.3", func(context.Context) (CLIInfo, error) {
		return CLIInfo{}, errors.New("没有 pk")
	}, get); err == nil {
		t.Fatal("CLI 连源都没报却成功了")
	}

	// CLI 报错但给了源 → 继续检查（CLI 升级那栏显示错误，GUI 照常查）
	res, err = Check(ctx, "0.12.3", func(context.Context) (CLIInfo, error) {
		return CLIInfo{Sources: sources}, errors.New("pk upgrade 挂了")
	}, get)
	if err != nil || !res.GUI.UpdateAvailable {
		t.Fatalf("CLI 报错不该影响 GUI 检查: %+v err=%v", res.GUI, err)
	}

	// 所有源都拿不到 → 报错
	if _, err := Check(ctx, "0.12.3", func(context.Context) (CLIInfo, error) {
		return CLIInfo{Sources: sources}, nil
	}, func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("超时")
	}); err == nil {
		t.Fatal("所有源都挂了却成功了")
	}
}

func TestGuiChannel(t *testing.T) {
	ch := guiChannel()
	switch ch.Name {
	case "script", "scoop", "brew", "unknown":
	default:
		t.Fatalf("渠道取值不认识: %+v", ch)
	}
	// 除了无法判断的 unknown，其余都要给人话提示
	if ch.Name != "unknown" && ch.Hint == "" {
		t.Fatalf("%s 渠道没有升级提示", ch.Name)
	}

	// 路径 → 渠道（穷举：这三条直接决定要不要自替换）
	cases := []struct{ exe, want, hintHas string }{
		{`C:\Users\me\scoop\apps\packnest-desktop\current\pk-desktop.exe`, "scoop", "scoop update"},
		{"/usr/local/Cellar/packnest-desktop/0.12.3/pk-desktop", "brew", "brew upgrade"},
		{"/Applications/packnest.app/Contents/MacOS/pk-desktop", "script", "install-gui"},
		{"/home/me/.local/bin/pk-desktop", "script", "install-gui"},
	}
	for _, c := range cases {
		got := channelOf(c.exe)
		if got.Name != c.want || !strings.Contains(got.Hint, c.hintHas) {
			t.Errorf("channelOf(%q) = %+v，期望 %s 含 %q", c.exe, got, c.want, c.hintHas)
		}
	}
}

func TestMarshalCLI(t *testing.T) {
	in := []byte(`{"current":"1.9.1","latest":"1.9.1","update_available":false,` +
		`"channel":"script","sources":[{"name":"gitee","latency_ms":42}]}`)
	got, err := MarshalCLI(in)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Current != "1.9.1" || len(got.Sources) != 1 || got.Sources[0].LatencyMS != 42 {
		t.Fatalf("字段没对上: %+v", got)
	}
	if _, err := MarshalCLI([]byte("not json")); err == nil {
		t.Fatal("垃圾输入应当报错")
	}
}

// TestSelfUpdatePlatform：只有发布矩阵里的 5 个平台才提示"有新版"，
// 其它平台（Intel mac 等）点了必然 404 —— 要诚实地引导去下载安装包。
func TestSelfUpdatePlatform(t *testing.T) {
	for _, ok := range [][2]string{{"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}} {
		if !selfUpdatePlatform(ok[0], ok[1]) {
			t.Fatalf("%s/%s 应有自更新产物", ok[0], ok[1])
		}
	}
	for _, bad := range [][2]string{{"darwin", "amd64"}, {"linux", "386"}, {"plan9", "amd64"}} {
		if selfUpdatePlatform(bad[0], bad[1]) {
			t.Fatalf("%s/%s 不该被当成有自更新产物", bad[0], bad[1])
		}
	}
}
