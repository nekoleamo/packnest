# pk-desktop —— packnest 的可视化补充界面（0.10.3）

> **定位**：GUI 不是独立端。**必须先装 CLI**（`pk`），这里只把 `pk <cmd> --json` 的结果投影成界面。
> 不 import 主模块任何 `internal/*`，业务真相永远在 CLI 与 `PK_ROOT`（方案 §11 定案 2026-11-11）。

## 现在能做什么（spike 版）

| 界面 | 背后调用 |
|---|---|
| 已装工具列表 / 设为默认 / 卸载（含 `weak`/未校验/npm 徽标、复制安装路径） | `pk list --json`、`pk use …`、`pk uninstall … --yes` |
| **可升级提示**：落后索引最新时顶部给一行 `node 22.23.4 → 26.13.1`，点「升级」= `pk install node@latest`（只读索引缓存，不联网） | `pk list --json` 的 `outdated[]`（1.3） |
| 可装版本 / 版本选择（全部版本 + 本地过滤） | `pk list --available --json`、`pk list <tool> --json [--all]` |
| 安装队列（多条 spec 依次装，每行独立进度）+ 深浅色切换（跟随系统/浅色/深色） | `pk install <spec> --json`（逐条复用同一条 NDJSON 流） |
| 镜像档切换 + **各源探测** | `pk mirror list --json`、`pk mirror use <name>`、`pk mirror probe --json`（延迟/可达性表格） |
| 磁盘回收（统计块 + 分组清单，预演"将删"/真删"已删"；**每工具保留 N 个版本**输入框 = `--keep N`） | `pk gc --json [--keep N]`（GET = 预演，POST = 真删） |
| 体检（含"实测原生模块"= `--probe`；全局包问题单独一张表；**一键修复**按钮） | `pk doctor --json [--globals] [--probe] [--fix]`（`--fix` 只修 PATH 托管块与 pk/packnest 入口） |
| 环境页：`which`（命令→真实路径）、`env`（PATH/变量只读）、`mirror write`（写 npmrc，先确认） | `pk which <命令> --json`、`pk env --json [--project]`、`pk mirror write --json` |

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

## 前端

Vue 3 + TypeScript + Vite（`frontend/`）：

```bash
make gui-frontend     # npm ci + vite build → frontend/dist（已随库提交，clone 后可直接 go build）
make gui              # 构建桌面壳（embed frontend/dist）
```

- `src/api.ts` 是**唯一的契约视图**：接口形状与 `internal/pkcli` 的结构体一一对应；
- `src/App.vue` 只做"调 /api/* + 渲染"，不含业务判断（装卸/校验语义都在 CLI）；
- 前端产物 `frontend/dist` 提交进库，是为了让 `go build` 零依赖（不需要 npm 也能构建 GUI）。

## 为什么不用 Wails 绑定

前端走 `/api/*`（普通 HTTP + NDJSON 流）而不是 Wails 的 JS 绑定，好处是：

- 桌面壳与 `--serve` 共用一套代码，浏览器里也能验收；
- 契约就是 `docs/json-schema.md`，不需要生成前端绑定；
- 前端可以用任何框架（现在是 Vue 3 + TS，换框架只动 `frontend/` 与重建 `dist/`）。

## 打包（macOS，Apple Silicon）

```bash
make gui-app            # → dist/gui/packnest.app（ad-hoc 签名，本机自用）
open dist/gui/packnest.app
```

- 默认按本机架构（`uname -m`）交叉编译；**发布只发 arm64**（intel 2026-11-12 放弃）。
  Intel Mac 想要 GUI 就自己在 Intel 机器上 `make gui-app`（脚本仍支持 `build-macos.sh <ver> amd64`）。

- `LSMinimumSystemVersion = 13.0`（Wails v3 的目标）；`CFBundleIdentifier = io.packnest.desktop`
- 图标由 `packaging/genicon`（纯标准库）生成 → `assets/icon.png` 与 `assets/tray-template.png`，
  打包时用 `sips`/`iconutil` 转成 `icon.icns`
- **签名/公证：拍板不做（2026-11-12）**。产物一律 ad-hoc（别人首次打开要手动放行）。
  机制保留，将来想发 Developer ID 只需设两个环境变量，不改代码：
  ```bash
  PK_SIGN_IDENTITY="Developer ID Application: 你的名字 (TEAMID)" \
  PK_NOTARY_PROFILE="notary" \                      # xcrun notarytool store-credentials 建一次
  make gui-app
  ```
  有 Developer ID 就带 hardened runtime 签名；设了 profile 会自动 `notarytool submit --wait` + `stapler staple`
- 运行时依赖：Linux 需要 **GTK4 + WebKitGTK 6.0**，Windows 需要 WebView2（Win10+ 一般自带）。
  各发行版包名（2026-11-12 实测对应发行版的包索引）：

  | 发行版 | 安装命令 |
  |---|---|
  | Debian 12+ / Ubuntu 22.04+ | `sudo apt install libgtk-4-1 libwebkitgtk-6.0-4` |
  | Fedora | `sudo dnf install gtk4 webkitgtk6.0` |
  | Arch / Manjaro | `sudo pacman -S gtk4 webkitgtk-6.0` |
  | Alpine（edge/community） | `sudo apk add gtk4.0 webkit2gtk-6.0` |

  注意区分 **WebKitGTK 6.0**（GTK4，包名 `libwebkitgtk-6.0-4` / `webkitgtk6.0` / `webkitgtk-6.0`）
  与旧的 4.1（`libwebkit2gtk-4.1-0`，GTK3）：**4.1 不够**，Wails v3 要 6.0。
  缺 6.0 的老发行版（Debian 11、Ubuntu 20.04、CentOS 7）不用装依赖，改用浏览器形态：
  `pk-desktop --serve --addr 127.0.0.1:7799` 然后开浏览器访问同一个地址（同一套 `/api/*`，功能一致）。

## 从发布页安装（用户视角）

release 里有独立产物（需要先装 CLI），挂在 **`gui-v<版本>`** 这个 tag 下：

```
https://gitee.com/null_593_5354/packnest/releases/download/gui-v0.10.3/<文件名>
https://github.com/nekoleamo/packnest/releases/download/gui-v0.10.3/<文件名>
```

| 平台 | 文件 | 用法 |
|---|---|---|
| macOS | `packnest-desktop_<ver>_darwin_arm64.app.zip` | 解压后 `open packnest.app`（ad-hoc 签名：首次启动需在"隐私与安全性"里放行）。**只发 Apple Silicon**（intel/amd64 2026-11-12 放弃；Intel Mac 自己 `make gui-app` 出本机构建） |
| Windows | `packnest-desktop_<ver>_windows_amd64.zip` | 解压得到 `pk-desktop.exe`，双击即可（需要 WebView2 运行时） |
| Linux | `packnest-desktop_<ver>_linux_amd64.tar.gz` | 解压得到 `pk-desktop`，运行即可（需要 GTK4 + WebKitGTK 6.0，包名见上表；没有就 `--serve` 走浏览器） |

Windows/Linux 的产物由 CI 的 `gui-build.yml` 构建（本机只有 macOS），发布时用
`release-gui.sh --from-dir` 上传，字节与 CI 完全一致。

## 托盘与单实例

- 关窗口 = 收进托盘（菜单里"打开 packnest"/"退出"）；⌘Q 仍然直接退出
- 单实例用"占住 `--addr` 的端口"实现（零依赖、跨平台）：第二个实例会直接报
  "已经有一个 packnest 界面在运行"
- 桌面壳启动时**也会**在同一个地址提供浏览器形态（`http://127.0.0.1:7799/`），
  所以桌面和浏览器可以并用；不想暴露就绑到回环地址（默认就是）

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
