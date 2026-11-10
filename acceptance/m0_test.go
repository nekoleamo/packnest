// Package acceptance 存放"验收项 = 测试名"（方案 §9.6）。
//
// 每个里程碑的验收项先在这里登记名字，再填实现；`make accept` 只跑本包。
// M0 验收（方案 §12.1 / §13.1）：
//
//	TestAcceptance_M0_ProviderLoad
//	TestAcceptance_M0_MirrorProbe
//	TestAcceptance_M0_DoctorNoPanic
//	TestAcceptance_M0_DualNameConsistent
package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/packnest/packnest/internal/fetch"
	"github.com/packnest/packnest/internal/mirror"
	"github.com/packnest/packnest/internal/provider"
	"github.com/packnest/packnest/internal/testkit"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
	tempRoot  string
)

// TestMain 只构建一次 pk 二进制，供 L3 端到端用例复用。
func TestMain(m *testing.M) {
	code := m.Run()
	if tempRoot != "" {
		_ = os.RemoveAll(tempRoot)
	}
	os.Exit(code)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod（用例应在仓库内运行）")
		}
		dir = parent
	}
}

// pkBinary 构建（或复用）真实 CLI 二进制。
func pkBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root := repoRoot(t)
		tempRoot, buildErr = os.MkdirTemp("", "packnest-accept-*")
		if buildErr != nil {
			return
		}
		binPath = filepath.Join(tempRoot, "pk")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/pk")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			t.Logf("构建输出: %s", out)
		}
	})
	if buildErr != nil {
		t.Fatalf("构建 pk 失败: %v", buildErr)
	}
	return binPath
}

// TestAcceptance_M0_ProviderLoad：内嵌 provider 全部可加载、自洽、无网络依赖。
func TestAcceptance_M0_ProviderLoad(t *testing.T) {
	all, err := provider.All()
	if err != nil {
		t.Fatalf("provider 加载失败: %v", err)
	}
	if len(all) != 6 {
		t.Fatalf("应有 6 个 provider（node/npm/pnpm/bun/go/python），得到 %d", len(all))
	}
	for _, p := range all {
		if err := p.Validate(); err != nil {
			t.Errorf("%s 校验失败: %v", p.ID, err)
		}
		// 每个 provider 都必须能渲染出四平台的产物 URL（模板自洽）。
		for _, plat := range [][2]string{{"darwin", "arm64"}, {"linux", "amd64"}, {"windows", "amd64"}} {
			if p.ID == "python" {
				continue // {tag} 由 M3 的目录列举式索引补齐
			}
			url, err := p.ArtifactURL("1.2.3", plat[0], plat[1], "cn")
			if err != nil {
				t.Errorf("%s/%s-%s 渲染失败: %v", p.ID, plat[0], plat[1], err)
				continue
			}
			if !strings.HasPrefix(url, "https://") {
				t.Errorf("%s 产物 URL 异常: %q", p.ID, url)
			}
		}
	}
	// 首次使用只有一个下载面：产物本身（provider 内嵌，不需要任何插件下载）。
	for _, p := range all {
		if len(p.Index.URLs) == 0 {
			t.Errorf("%s 缺少索引源", p.ID)
		}
	}
}

// TestAcceptance_M0_MirrorProbe：探测走假镜像（零外网），并能回退到可用源。
func TestAcceptance_M0_MirrorProbe(t *testing.T) {
	nodeIndex := []byte(`{"versions":[{"version":"v22.22.2","lts":"Jod"}]}`)
	srv := testkit.Mirror(t, map[string][]byte{
		"/-/binary/node/index.json": nodeIndex,
	})

	prober := &mirror.Prober{
		Client: fetch.New(),
		Path:   filepath.Join(t.TempDir(), "probe.json"),
		TTL:    mirror.DefaultTTL,
	}
	dead := srv.URL + "/dead/index.json"
	alive := srv.URL + "/-/binary/node/index.json"

	res, err := prober.Select(context.Background(), []string{dead, alive})
	if err != nil {
		t.Fatalf("应回退到可用源: %v", err)
	}
	if res.URL != alive || !res.OK {
		t.Fatalf("选中了错误的源: %+v", res)
	}
	cache := prober.Load()
	if len(cache.Results) < 2 {
		t.Fatalf("探测结果应落盘: %+v", cache)
	}
	if !cache.Results[dead].OK == false {
		t.Fatalf("失败结果也应记录: %+v", cache.Results[dead])
	}

	// 全部不可用 → 明确报错并给建议（不静默降级）。
	if _, err := prober.Select(context.Background(), []string{dead, srv.URL + "/nope"}); err == nil {
		t.Fatal("全部失败应报错")
	} else if !strings.Contains(err.Error(), "pk mirror") {
		t.Fatalf("错误应给可执行建议: %v", err)
	}
}

// TestAcceptance_M0_DoctorNoPanic：空环境（无根目录、PATH 为空、台账损坏）都不许 panic。
func TestAcceptance_M0_DoctorNoPanic(t *testing.T) {
	bin := pkBinary(t)

	cases := []struct {
		name    string
		prepare func(root string)
	}{
		{"空白根目录", func(string) {}},
		{"台账损坏", func(root string) {
			state := filepath.Join(root, "state")
			if err := os.MkdirAll(state, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(state, "installs.json"), []byte("{broken"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"配置损坏", func(root string) {
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("mirror = ["), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			c.prepare(root)
			cmd := exec.Command(bin, "doctor", "--json")
			cmd.Env = append(os.Environ(), "PK_ROOT="+root, "PATH=")
			out, err := cmd.Output()
			exitCode := 0
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("执行失败: %v", err)
			}
			if exitCode != 0 && exitCode != 1 {
				t.Fatalf("doctor 退出码应是 0 或 1，得到 %d", exitCode)
			}
			var doc struct {
				Schema int `json:"schema"`
				OK     bool
				Issues []struct {
					Code     string
					Severity string
				}
			}
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatalf("doctor --json 输出不是合法 JSON: %v\n%s", err, out)
			}
			if doc.Schema != 1 {
				t.Fatalf("schema = %d，必须是 1", doc.Schema)
			}
			if c.name == "台账损坏" {
				found := false
				for _, is := range doc.Issues {
					if is.Code == "LEDGER_BROKEN" && is.Severity == "error" {
						found = true
					}
				}
				if !found {
					t.Fatalf("台账损坏必须报 LEDGER_BROKEN/error: %+v", doc.Issues)
				}
				if doc.OK {
					t.Fatal("有 error 时 ok 必须为 false")
				}
			}
		})
	}
}

// TestAcceptance_M0_DualNameConsistent：pk 与 packnest 解析到同一文件、版本一致。
func TestAcceptance_M0_DualNameConsistent(t *testing.T) {
	bin := pkBinary(t)
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(binDir, "pk")
	if err := os.WriteFile(self, []byte("placeholder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(binDir, "packnest")); err != nil {
		t.Fatalf("创建别名链接: %v", err)
	}

	runWith := func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "PK_ROOT="+root)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("执行 pk %v 失败: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}

	a := runWith("which", "pk")
	b := runWith("which", "packnest")
	if a != b {
		t.Fatalf("pk which pk = %q，pk which packnest = %q，必须是同一个文件", a, b)
	}
	if !strings.HasSuffix(a, "pk") {
		t.Fatalf("解析结果异常: %q", a)
	}

	ja := runWith("--json", "which", "pk")
	jb := runWith("--json", "which", "packnest")
	var ra, rb struct {
		Schema int    `json:"schema"`
		Name   string `json:"name"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(ja), &ra); err != nil {
		t.Fatalf("which --json 非法: %v", err)
	}
	if err := json.Unmarshal([]byte(jb), &rb); err != nil {
		t.Fatalf("which --json 非法: %v", err)
	}
	if ra.Path != rb.Path {
		t.Fatalf("--json 下两个名字解析不一致: %q vs %q", ra.Path, rb.Path)
	}

	// 版本输出一致（否则会出现"两个名字两个版本"的诡异状态）。
	if v1, v2 := runWith("--version"), runWith("version"); v1 != v2 {
		t.Fatalf("--version 与 version 输出不一致: %q vs %q", v1, v2)
	}
	if !strings.Contains(runWith("--version"), "packnest v"+versionOf(t, bin)) {
		t.Fatalf("版本行格式应为 `packnest vX.Y.Z (...)`: %q", runWith("--version"))
	}
}

func versionOf(t *testing.T, bin string) string {
	t.Helper()
	out := exec.Command(bin, "--version")
	line, err := out.Output()
	if err != nil {
		t.Fatalf("--version 失败: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(line)))
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		t.Fatalf("版本行无法解析: %q", line)
	}
	return strings.TrimPrefix(fields[1], "v")
}
