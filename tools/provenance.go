//go:build ignore

// provenance 生成本次构建的 provenance.json（方案 §provenance A：本机生成，纳入签名覆盖范围）。
//
//	go run tools/provenance.go -out dist/v1.7.0/provenance.json -version 1.7.0 \
//	    -ldflags "-s -w -X main.Version=1.7.0" -source-date 202001010000
//
// 只记"这个二进制是怎么来的"：commit / tag / Go 版本 / 平台 / ldflags / 可重复构建的 mtime 基线 /
// 谁构建的 / 模块数。**不记时间戳** —— 重跑一次发布必须得到同一份字节，否则 checksums.txt 会变，
// 已发布过一次的版本就再也对不上了。
//
// 它不是证明（没有第三方签名背书"这就是从那个 commit 构建的"），但能让用户与审计方
// 自己重建一遍并比对 sha256；发布链刻意不搬到 CI（决策 3），provenance 由构建者自己声明。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type provenance struct {
	Schema      int      `json:"schema"`
	Commit      string   `json:"commit"`
	Tag         string   `json:"tag"`
	GoVersion   string   `json:"go_version"`
	Platforms   []string `json:"platforms"`
	LDFlags     string   `json:"ldflags"`
	SourceDate  string   `json:"source_date"`
	BuiltBy     string   `json:"built_by"`
	Dirty       bool     `json:"dirty"`
	ModuleCount int      `json:"module_count"`
	// BuildInput 是上面所有事实的 sha256，方便一眼比对两份 provenance
	BuildInput string `json:"build_input_sha256"`
}

func main() {
	out := flag.String("out", "", "输出文件（默认 stdout）")
	version := flag.String("version", "", "发布版本（不带 v）")
	ldflags := flag.String("ldflags", "", "构建用的 -ldflags")
	sourceDate := flag.String("source-date", "", "归档 mtime 基准（可重复构建）")
	platforms := flag.String("platforms", "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64,windows/amd64,windows/arm64", "发布的平台列表")
	flag.Parse()

	if *version == "" {
		fmt.Fprintln(os.Stderr, "provenance: 需要 -version")
		os.Exit(2)
	}

	commit := gitOutput("rev-parse", "HEAD")
	dirty := strings.TrimSpace(gitOutput("status", "--porcelain")) != ""
	modules := moduleCount()

	p := provenance{
		Schema:      1,
		Commit:      commit,
		Tag:         "v" + strings.TrimPrefix(*version, "v"),
		GoVersion:   runtime.Version(),
		Platforms:   strings.Split(*platforms, ","),
		LDFlags:     *ldflags,
		SourceDate:  *sourceDate,
		BuiltBy:     builtBy(),
		Dirty:       dirty,
		ModuleCount: modules,
	}
	p.BuildInput = digestOf(p)

	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "provenance:", err)
		os.Exit(1)
	}
	b = append(b, '\n')
	if *out == "" {
		os.Stdout.Write(b)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "provenance:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "provenance:", err)
		os.Exit(1)
	}
	fmt.Printf("已生成 %s（commit %s，%d 个模块）\n", *out, shortCommit(commit), modules)
}

// digestOf 对"除 build_input_sha256 之外的全部事实"取 sha256。
func digestOf(p provenance) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d|%s|%s|%s|%s|%s|%s|%s|%t|%d",
		p.Schema, p.Commit, p.Tag, p.GoVersion, strings.Join(p.Platforms, ","),
		p.LDFlags, p.SourceDate, p.BuiltBy, p.Dirty, p.ModuleCount)
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func shortCommit(c string) string {
	if len(c) > 8 {
		return c[:8]
	}
	return c
}

// builtBy 记"谁构建的"：默认本机 os/arch，可用 PK_BUILT_BY 覆盖（例如 CI 的 job 名）。
func builtBy() string {
	if v := os.Getenv("PK_BUILT_BY"); v != "" {
		return v
	}
	// 不带主机名：provenance 会随发布物公开，没必要把维护者的机器名写进去
	return fmt.Sprintf("local:%s/%s", runtime.GOOS, runtime.GOARCH)
}

// moduleCount 数 go.mod 里除主模块外的模块数（与 SBOM 的组件数对得上）。
func moduleCount() int {
	out, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 { // 主模块没有版本号
			continue
		}
		n++
	}
	return n
}

func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
