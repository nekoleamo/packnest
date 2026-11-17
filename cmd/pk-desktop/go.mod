module github.com/packnest/packnest/cmd/pk-desktop

// GUI 是 CLI 的"可视化补充"：必须先装 pk，通过子进程 + `pk --json` 驱动。
// 因此这个模块**不 import** 主模块的任何 internal/*；界面用 mygo 的原生 ui（纯 Go、无 cgo、无 webview）。
go 1.27.3

require (
	github.com/egoist/mygo v0.10.0
	golang.org/x/sys v0.50.0
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)
