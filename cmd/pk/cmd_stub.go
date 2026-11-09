package main

import (
	"fmt"

	"github.com/packnest/packnest/internal/jsonout"
)

// notImplemented 明确告知尚未实现的命令与里程碑，绝不伪装成功。
func (a *app) notImplemented(name, milestone string) int {
	fmt.Fprintf(a.errOut, "pk: %s 还没实现（计划 %s，见 ~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md）\n", name, milestone)
	return jsonout.ExitFail
}
