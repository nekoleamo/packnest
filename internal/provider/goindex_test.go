package provider

import (
	"strings"
	"testing"
)

// TestGoIndexHasCNMirror：go 的 cn 档必须先用官方 CN 镜像，同时保留 go.dev 作回退。
func TestGoIndexHasCNMirror(t *testing.T) {
	p, err := Get("go")
	if err != nil {
		t.Fatal(err)
	}
	cn := p.IndexURLs("cn")
	if len(cn) != 2 {
		t.Fatalf("go 的 cn 档应有 2 个索引源（CN 镜像 + go.dev），得到 %v", cn)
	}
	if !strings.Contains(cn[0], "golang.google.cn") {
		t.Fatalf("cn 档应优先 golang.google.cn，得到 %v", cn)
	}
	if !strings.Contains(cn[1], "go.dev") {
		t.Fatalf("回退源应是 go.dev，得到 %v", cn)
	}
	// default 档：go.dev 在前；回退链会把国内档排在官方档之前（mirror.Chain 的既有语义）
	got := p.IndexURLs("default")
	if len(got) == 0 || !strings.Contains(got[0], "go.dev") {
		t.Fatalf("default 档应先用 go.dev，得到 %v", got)
	}
	if len(got) != 2 || !strings.Contains(got[1], "golang.google.cn") {
		t.Fatalf("default 档的第二个源应是 CN 镜像（回退），得到 %v", got)
	}
}
