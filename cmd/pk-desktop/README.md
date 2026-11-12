# pk-desktop —— packnest 的可视化补充界面

> **定位**：GUI 不是独立端。**必须先装 CLI**（`pk`），这里只把 `pk <cmd> --json` 的结果投影成界面。
> 不 import 主模块任何 `internal/*`，业务真相永远在 CLI 与 `PK_ROOT`（方案 §11 定案 2026-11-11）。

## 现在能做什么（spike 版）

| 界面 | 背后调用 |
|---|---|
| 已装工具列表 / 设为默认 / 卸载 | `pk list --json`、`pk use <tool>@<version> --json`、`pk uninstall <tool> --yes` |
| 可装版本 / 版本选择 | `pk list --available --json`、`pk list <tool> --json` |
| 安装 + 进度条 | `pk install <spec> --json`（NDJSON 流，`progress` 事件驱动进度条） |
| 镜像档切换 | `pk mirror list --json`、`pk mirror use <name>` |
| 磁盘回收 | `pk gc --json`（GET = 预演，POST = 真删） |
| 体检 | `pk doctor --json [--globals]` |

## 跑起来

```bash
cd cmd/pk-desktop
go build -o ../../bin/pk-desktop .

# ① 浏览器形态（前端是普通静态页 + /api/*，无需 Wails 运行时）
../../bin/pk-desktop --serve --addr 127.0.0.1:7799

# ② 桌面壳（Wails v3.1.0；macOS/Windows/Linux 各自需要 WebView 运行时）
../../bin/pk-desktop
```

没装 CLI 时页面顶部会直接给出安装命令；GUI 自己**不会**下载任何东西。

## 为什么不用 Wails 绑定

前端走 `/api/*`（普通 HTTP + NDJSON 流）而不是 Wails 的 JS 绑定，好处是：

- 桌面壳与 `--serve` 共用一套代码，浏览器里也能验收；
- 契约就是 `docs/json-schema.md`，不需要生成前端绑定；
- 前端可以用任何框架（现在是零依赖的 vanilla，后续换 Vue3 时只换 `assets/`）。

## 打包（macOS）

```bash
make gui-app            # → dist/gui/packnest.app（ad-hoc 签名，本地自用）
open dist/gui/packnest.app
```

- `LSMinimumSystemVersion = 13.0`（Wails v3 的目标）；`CFBundleIdentifier = io.packnest.desktop`
- 分发要额外做 Developer ID 签名 + notarize；Linux 需要 GTK4/WebKitGTK 6.0，Windows 需要 WebView2（都还没接构建）

## 从发布页安装（用户视角）

release 里有独立产物（需要先装 CLI）：

| 平台 | 文件 | 用法 |
|---|---|---|
| macOS | `packnest-desktop_<ver>_darwin_{arm64,amd64}.app.zip` | 解压后 `open packnest.app`（ad-hoc 签名：首次启动需在"隐私与安全性"里放行） |
| Windows | `packnest-desktop_<ver>_windows_amd64.zip` | 解压得到 `pk-desktop.exe`，双击即可（需要 WebView2 运行时） |
| Linux | `packnest-desktop_<ver>_linux_amd64.tar.gz` | 解压得到 `pk-desktop`，运行即可（需要 GTK4 + WebKitGTK 6.0） |

Windows/Linux 的产物由 CI 的 `gui-build.yml` 构建（本机只有 macOS），发布时用
`release-gui.sh --from-dir` 上传，字节与 CI 完全一致。

## 安全边界

- `--serve` 默认只绑 `127.0.0.1`；
- 绑到非回环地址时**必须** `--token`，否则拒绝启动（这个接口能装卸软件）；
  带 token 后 `/api/*` 认 `?token=` 或 `Authorization: Bearer`。

## 测试

`internal/pkcli` 有单测：

- `pkcli_test.go`：假 `pk` 脚本覆盖定位 / schema 与版本门禁 / NDJSON 事件流 / 退出码 5；
- `contract_test.go`：拿**真实的** `bin/pk` 跑 `version/list/doctor/mirror list/gc --dry-run`，
  断言 GUI 侧结构体还能解析（契约漂移先在这里红；用临时 `PK_ROOT`，不碰用户状态）。
`cmd/pk-desktop` 不参与 `make loc`（GUI 单列预算）。
