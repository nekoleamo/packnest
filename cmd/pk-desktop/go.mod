module github.com/packnest/packnest/cmd/pk-desktop

// GUI 是 CLI 的"可视化补充"：必须先装 pk，通过子进程 + `pk --json` 驱动。
// 因此这个模块**不 import** 主模块的任何 internal/*，只依赖 Wails。
go 1.26.0

require github.com/wailsapp/wails/v3 v3.1.0

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
)
