# pk-desktop —— packnest 的可视化补充界面（0.13.0，1.0 前的收口版）

> **定位**：GUI 不是独立端。**必须先装 CLI**（`pk`），这里只把 `pk <cmd> --json` 的结果投影成界面。
> 不 import 主模块任何 `internal/*`，业务真相永远在 CLI 与 `PK_ROOT`（方案 §11 定案 2026-11-11）。
>
> **界面形态（2026-11-15 起）**：**mygo 原生 UI**（`github.com/egoist/mygo/ui`）——
> 纯 Go、`CGO_ENABLED=0`、无 webview、无 JS。Wails v3 + Vue3 的旧壳在 0.11.4 退役。

## 代码结构

| 文件 | 职责 |
|---|---|
| `main.go` | 入口：flag、单实例（占回环端口）、托盘、窗口；`--version` |
| `view.go` | 根视图：状态条 + 侧边栏 + 当前页 + 确认框；主题三态、中英切换、偏好落盘 |
| `page_*.go` | 一页一个文件：已装 / 安装队列（含版本选择）/ 镜像 / 环境 / 更新 / 体检 / 磁盘 / 设置 |
| `serve.go` | `--serve` 的 HTTP 层：**只做**"读参数 → 调 `internal/state` → 写 JSON" |
| `internal/state` | 装配层：18 个接口的数据逻辑，原生界面与 `--serve` 共用同一份 |
| `internal/pkcli` | 与 CLI 的胶水：定位 `pk`、跑 `--json`、投影结构体（契约测试在这） |
| `internal/update` | GUI 自更新：探测源、下载、校验、替换自身 |
| `internal/autostart` | 开机自启（只写用户级条目） |
| `frontend/` | **冻结**的旧 Vue 页面，只作 `--serve` 的兜底；不再随界面更新 |

行数：`cmd/pk-desktop` 2,174 + `pkcli` 651 + `update` 686 + `state` 475 + `autostart` 198（不含测试），
预算登记在 `tools/loc-budgets.txt`，`make loc` 会逐包比。

## 线程模型（改代码前先读这一段）

mygo 的视图是"状态的函数"，**只在主线程读状态**。所以：

- 任何后台取数都走 `g.load(key, work)`：`work` 在 goroutine 里跑、**只返回一个 `commit`**，
  `commit` 由主线程执行（`g.onMain`）后才改字段并重画。
- 不要在 `work` 里写 `g.xxx = ...` —— `go test -race` 会立刻红（第一版就是这么红的）。
- 同一个 `key` 同时在跑的只有一个（用户连点刷新不会起 10 个 `pk` 子进程）。

## 跑起来

```bash
cd cmd/pk-desktop
go build -o ../../bin/pk-desktop .

# ① 原生界面（不需要任何 WebView 运行时）
../../bin/pk-desktop

# ② 浏览器形态（冻结静态页 + 同一套 /api/*，无窗口环境/远程排障用）
../../bin/pk-desktop --serve --addr 127.0.0.1:7799
```

没装 CLI 时状态条会给安装命令（带复制）；GUI 自己**不会**下载任何东西。

## 构建与测试

```bash
make gui          # 本地构建 bin/pk-desktop
make gui-cross    # 交叉编 5 份：darwin-arm64 / windows-{amd64,arm64} / linux-{amd64,arm64}
make gui-test     # go test -race：state 层单测 + 视图验收（ui.Tester 无窗口）+ 契约测试
make gui-serve    # 浏览器形态
make check        # 12 项门禁（含 gui-test 与 GUI 的行数预算）
```

- 视图验收不需要显示器：`ui.NewTester(g.view, w, h)` + `Click`/`HasText`，
  见 `view_test.go`（`TestAcceptance_GUI3/4/6/7_*`）。
- `internal/pkcli/contract_test.go` 拿**真实的** `bin/pk` 跑 `version/list/doctor/mirror list/gc --dry-run`，
  断言结构体还能解析（契约漂移先在这里红；用临时 `PK_ROOT`，不碰用户状态）。

## 打包（macOS，Apple Silicon）

```bash
make gui-app            # → dist/gui/packnest.app（ad-hoc 签名，本机自用）
open dist/gui/packnest.app
```

- `LSMinimumSystemVersion = 13.0`；`CFBundleIdentifier = io.packnest.desktop`
- 图标由 `packaging/genicon`（纯标准库）生成 → `assets/icon.png`、`assets/tray-template.png`
- **签名/公证：拍板不做（2026-11-12）**，一律 ad-hoc（首次打开要手动放行）。机制保留：
  ```bash
  PK_SIGN_IDENTITY="Developer ID Application: 你的名字 (TEAMID)" \
  PK_NOTARY_PROFILE="notary" \
  make gui-app
  ```
- 运行时依赖：**无**（不依赖 WebView2 / WebKitGTK）。Linux 桌面只要 GTK3；
  **托盘**另需 `libayatana-appindicator3`（缺了会降级成"无托盘、关窗即退"，不会崩）。
  macOS 用 Metal、Windows 用 D3D11（无 GPU / RDP 时自带 CPU raster 后备）。

## 从发布页安装（用户视角）

release 里有独立产物（需要先装 CLI），挂在 **`gui-v<版本>`** 这个 tag 下：

```
https://gitee.com/null_593_5354/packnest/releases/download/gui-v0.13.0/<文件名>
https://github.com/nekoleamo/packnest/releases/download/gui-v0.13.0/<文件名>
```

| 平台 | 文件 | 用法 |
|---|---|---|
| macOS | `packnest-desktop_<ver>_darwin_arm64.app.zip` | 解压后 `open packnest.app`（ad-hoc 签名：首次启动需在"隐私与安全性"里放行）。**只发 Apple Silicon**（intel/amd64 2026-11-12 放弃；Intel Mac 自己 `make gui-app`） |
| Windows（amd64 / arm64） | `packnest-desktop_<ver>_windows_{amd64,arm64}.zip` | 解压得到 `pk-desktop.exe`，双击即可（**不需要 WebView2**） |
| Linux（amd64 / arm64） | `packnest-desktop_<ver>_linux_<arch>.tar.gz` | 解压得到 `pk-desktop`，运行即可（只要 GTK3；托盘见上） |

四份产物由 CI 的 `gui-build.yml` 在**一个 macOS job 上交叉编译**（纯 Go、无 cgo），
发布时用 `release-gui.sh --from-dir` 上传，字节与 CI 完全一致。

## 退出与首帧（0.12.1/0.12.2 修过的两个坑，别再犯）

- **退出**：`OnClose` 平时拦成"关窗收进托盘"，但任何退出入口（托盘「退出」/`OnBeforeQuit`）都会先置
  `quitting`，`OnClose` 见标记就放行；还有 3 秒 watchdog 兜底（正常流程走不完就硬退）。
  写窗口 hook 时别再无条件下 `PreventDefault()`。
- **首帧**：列表页（已装/安装/可选版本）**不套外层 Scroll**，`List` 自己占满并滚动（`pages[].fill`）。
  套进去会让 `Grow(1)` 拿不到高度，第一帧只画两三行。
- **Windows**：构建必须 `-ldflags "-H windowsgui"`，否则是控制台子系统（双击先弹黑窗）。
- **取数**：`load(key, work)` 的 `work` 跑在 goroutine 里，只返回 `commit`；panic 会被 `safeWork`
  转成错误（否则 `busy` 永久卡住 = 界面永远转圈）。

## 托盘与单实例

- 关窗口 = 收进托盘（菜单里"打开 packnest"/"退出"）；macOS 的 ⌘Q 仍然直接退出
- 单实例用"占住 `--addr` 的端口"实现（零依赖、跨平台）：第二个实例直接报
  "已经有一个 packnest 界面在运行"
- 桌面壳启动时**也会**在同一个地址提供浏览器形态（`http://127.0.0.1:7799/`）

## 安全边界

- `--serve` 默认只绑 `127.0.0.1`；
- 绑到非回环地址时**必须** `--token`，否则拒绝启动（这个接口能装卸软件）；
  带 token 后 `/api/*` 认 `?token=` 或 `Authorization: Bearer`；
- 会改用户机器/文件的动作（卸载、`gc --run`、`mirror write`、GUI 自更新、`doctor --fix`）
  一律先过统一确认框（`g.ask`），"取消"是默认选项。
