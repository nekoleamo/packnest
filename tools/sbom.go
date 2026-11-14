//go:build ignore

// sbom 生成本次发布的最小 CycloneDX 1.5 清单（方案 §SBOM）。
//
//	go run tools/sbom.go -out dist/v1.7.0/sbom.cdx.json -version 1.7.0
//
// 为什么自己造而不引工具：依赖只有 2 个模块，跑一遍 `go list -m all` 就能给全；
// 引入 syft / cyclonedx-gomod 会破坏"第三方依赖 2 个"和"单二进制"的纪律，
// 而这个文件的合同作用只有一个 —— 让 `trivy sbom` / 供应链审计能解析出一份组件表。
//
// 不含时间戳与随机 UUID：重跑一次发布必须字节一致（见 tools/provenance.go 同理）。
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
	"strings"
)

const modulePath = "github.com/packnest/packnest"

type bom struct {
	BOMFormat    string      `json:"bomFormat"`
	SpecVersion  string      `json:"specVersion"`
	SerialNumber string      `json:"serialNumber"`
	Version      int         `json:"version"`
	Metadata     bomMetadata `json:"metadata"`
	Components   []component `json:"components"`
}

type bomMetadata struct {
	Tools     bomTools   `json:"tools"`
	Component *component `json:"component,omitempty"`
}

type bomTools struct {
	Components []component `json:"components"`
}

type component struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	PURL    string `json:"purl,omitempty"`
}

func main() {
	out := flag.String("out", "", "输出文件（默认 stdout）")
	version := flag.String("version", "", "packnest 版本（不带 v）")
	flag.Parse()
	if *version == "" {
		fmt.Fprintln(os.Stderr, "sbom: 需要 -version")
		os.Exit(2)
	}
	ver := strings.TrimPrefix(*version, "v")

	comps := moduleComponents()
	b := bom{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: bomMetadata{
			Tools:     bomTools{Components: []component{{Type: "application", Name: "tools/sbom.go", Version: "1"}}},
			Component: &component{Type: "application", Name: "packnest", Version: ver, PURL: purl(modulePath, ver)},
		},
		Components: comps,
	}
	// serialNumber 由内容派生：合法的 urn:uuid 形状 + 可重复（重跑发布字节一致）
	b.SerialNumber = "urn:uuid:" + serialFrom(ver, comps)

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sbom:", err)
		os.Exit(1)
	}
	data = append(data, '\n')
	if *out == "" {
		os.Stdout.Write(data)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sbom:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sbom:", err)
		os.Exit(1)
	}
	fmt.Printf("已生成 %s（%d 个组件）\n", *out, len(comps))
}

// moduleComponents 把 `go list -m all` 的全部模块变成 components（主模块单列在 metadata 里）。
func moduleComponents() []component {
	out, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sbom: go list -m all 失败:", err)
		os.Exit(1)
	}
	var comps []component
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 被 replace 的模块形如 `path v1 => other v2`：取箭头右边（实际构建进去的代码）
		if i := strings.Index(line, "=>"); i >= 0 {
			line = strings.TrimSpace(line[i+2:])
		}
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == modulePath {
			continue // 主模块没有版本号 / 就是自己
		}
		comps = append(comps, component{Type: "library", Name: f[0], Version: f[1], PURL: purl(f[0], f[1])})
	}
	return comps
}

// purl 拼 golang 类型的 package URL（purl-spec：namespace/name 原样保留，只做最小转义）。
func purl(path, version string) string {
	p := strings.TrimSuffix(path, ".git")
	// purl 的 golang 类型里 @ 与空格必须转义；模块路径本身不含这两个字符，这里只防意外
	p = strings.ReplaceAll(p, "@", "%40")
	p = strings.ReplaceAll(p, " ", "%20")
	return fmt.Sprintf("pkg:golang/%s@%s", p, version)
}

// serialFrom 由版本 + 组件表派生 uuid 形状的串（8-4-4-4-12，版本位 4、变体位 8）。
func serialFrom(ver string, comps []component) string {
	var sb strings.Builder
	sb.WriteString(ver)
	for _, c := range comps {
		sb.WriteString("|" + c.Name + "@" + c.Version)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	h := hex.EncodeToString(sum[:])
	// RFC 4122 v4 形状：只借形状，不声称随机
	return fmt.Sprintf("%s-%s-4%s-8%s-%s", h[0:8], h[8:12], h[13:16], h[17:20], h[20:32])
}
