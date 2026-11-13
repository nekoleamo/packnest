package env

import (
	"strings"
	"testing"
)

func TestCheckGoToolchain(t *testing.T) {
	// 没设 → 告警并给处置
	msg := CheckGoToolchain(func(string) string { return "" })
	if !strings.Contains(msg, "GOTOOLCHAIN") || !strings.Contains(msg, "pk env") {
		t.Fatalf("应告警并给建议: %q", msg)
	}
	if msg := CheckGoToolchain(func(k string) string { return "local" }); msg != "" {
		t.Fatalf("local 时不该告警: %q", msg)
	}
	if msg := CheckGoToolchain(func(k string) string { return "auto" }); msg == "" {
		t.Fatal("auto 也应告警（会多下一次工具链）")
	}
	if CheckGoToolchain(nil) != "" {
		t.Fatal("nil lookup 应安全返回空")
	}
	if GoToolchainEnv != "GOTOOLCHAIN=local" {
		t.Fatalf("常量必须与方案一致: %q", GoToolchainEnv)
	}
}
