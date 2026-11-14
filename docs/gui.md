# 桌面端 `pk-desktop`（GUI）

> **一句话**：CLI 的可视化补充。**必须先装 `pk`**，界面里每个动作都是子进程调 `pk <命令> --json`；
> 它没有自己的下载器、台账或更新器 —— 卸载/升级仍指回 CLI。

- 命令面与契约：`pk --json`（[docs/json-schema.md](json-schema.md)）
- 开发/打包/发布细节：[cmd/pk-desktop/README.md](../cmd/pk-desktop/README.md)

## 一、下载与安装

产物挂在**独立 tag** `gui-v<版本>` 下（GUI 与 CLI 版本解耦：CLI 是 `v1.5.1` 时 GUI 可能是 `0.11.0`），
gitee 与 GitHub 的 releases 页各一份：

```
https://gitee.com/null_593_5354/packnest/releases/tag/gui-v<版本>
https://github.com/nekoleamo/packnest/releases/tag/gui-v<版本>
```

| 平台 | 文件 | 怎么用 |
|---|---|---|
| macOS（arm64） | `packnest-desktop_<版本>_darwin_arm64.app.zip` | 解压得到 `packnest.app`，拖进「应用程序」；**首次打开**去「系统设置 → 隐私与安全性」点「仍要打开」（ad-hoc 签名，没有公证） |
| Windows（amd64） | `packnest-desktop_<版本>_windows_amd64.zip` | 解压后直接跑 `pk-desktop.exe`；需要系统自带的 WebView2 运行时（Win10/11 默认有） |
| Linux（amd64） | `packnest-desktop_<版本>_linux_amd64.tar.gz` | 解压跑 `pk-desktop`；需要 GTK4 + WebKitGTK 6.0（Debian/Ubuntu：`sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev`） |

macOS 只发 arm64（Apple Silicon）；Intel Mac 需要自己 `make gui-app`（2026-11-12 拍板放弃 intel 产物）。

同一个 release 里还有 **`checksums.txt` + `checksums.txt.sig`**（CLI 与 GUI 共用一把发布密钥）：
下载后先 `shasum -a 256 <文件>` 与清单比对，再
`ssh-keygen -Y verify -f <allowed_signers> -I release@packnest.dev -n pk-release -s checksums.txt.sig < checksums.txt`
确认清单本身没被镜像站换过。公钥在 `internal/verify/keys/allowed_signers`（与 `pk doctor` 内置的那份同源）。
核对现成的发布用 `sh scripts/verify-release.sh --gui`。

## 二、先装 CLI

GUI 启动时会按 `PATH` → 常见安装目录的顺序找 `pk`，并检查它的契约版本（`schema ≥ 1` 且版本够新）。

```bash
# 还没装 CLI 的话
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
# 或 brew install nekoleamo/packnest/packnest
```

找不到时界面顶部会直接给出**当前平台**的安装命令（Windows 是 `iwr … install.ps1 -UseB | iex`，其余是
`curl -fsSL … install.sh | sh`），旁边有「复制」按钮；主界面此时不显示。装完 CLI 重开窗口即可，
GUI 不会自己去下第二份运行时。

## 三、浏览器形态（不装桌面运行时也能用）

```bash
pk-desktop --serve                    # 默认 127.0.0.1:7799
pk-desktop --serve --addr 0.0.0.0:7799 --token <随机串>   # 绑非回环地址必须给 token
```

打开 `http://127.0.0.1:7799/` 就是同一套界面（前端是普通静态页 + `/api/*`，功能与桌面壳一致）。
远程使用时访问 `/api/*` 要带 `?token=` 或 `Authorization: Bearer <token>`。

## 三点五、界面语言与开机自启

- **语言**：顶栏 `中文 / EN` 切换，默认跟系统语言（`navigator.language`），选择记在本地，重开仍是上次那个。
  界面文案是 GUI 自己的；**CLI 的输出（体检问题、安装日志）仍然是中文** —— 这是刻意的：`--json` 才是机器接口，
  人类文案只维护一份，避免两边漂移。
- **开机自启**：设置里一个开关，**默认关**。打开后只写**用户级**条目：
  macOS `~/Library/LaunchAgents/packnest-desktop.plist`、Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`、
  Linux `~/.config/autostart/packnest-desktop.desktop`；关闭只删自己写的那一条，不碰别人的同类条目。

## 四、界面与 CLI 命令的对照

| 界面 | 实际调用 | 说明 |
|---|---|---|
| 已装工具表 | `pk list --json` | 版本、大小、`weak`/未校验/npm 徽标、`source`（内嵌/外部清单） |
| 可升级提示 | `pk list --json` 的 `outdated[]` | **只读 24h 索引缓存，不联网**；没缓存就不提示。点「升级」= `pk install <工具>@latest` |
| 设为默认 / 卸载 | `pk use` / `pk uninstall --yes` | 卸载前前端自己确认；退出码 5（另一个 pk 在跑）会提示稍后重试 |
| 可装版本 / 版本选择 | `pk list --available --json`、`pk list <工具> --json [--all]` | 全部版本 + 本地过滤 |
| 安装队列 | `pk install <spec> --json` | NDJSON 事件流；每行进度吃 `progress.bytes_done/bytes_total` |
| 镜像档 | `pk mirror list/use/probe --json` | 探测结果缓存 6h；写 `~/.npmrc` 的按钮背后是 `pk mirror write`（先备份、只增不改） |
| 体检 | `pk doctor --json [--globals] [--probe]` | `--probe` 用当前 node 的 `process.dlopen` 试加载 `.node`（只加载二进制、不跑包代码） |
| 一键修复 | `pk doctor --json --fix` | 只修 PATH 托管块与 pk/packnest 入口，其余问题只报不改；修完把 `fixed[]` 逐条显示 |
| 回收 | `pk gc --json [--keep N]` | GET = 预演、POST = 真删；`--keep N` = 每个工具保留最近 N 个版本 |
| `which` / `env` | `pk which --json`、`pk env --json [--project]` | 只读，用来回答"这个 node 是哪来的" |

## 五、故障排查

| 现象 | 原因 / 处置 |
|---|---|
| 顶部显示"找不到 pk" | 没装 CLI，或 `pk` 不在 `PATH`：`pk env --write` 后重开窗口；也可以把 GUI 和 `pk` 装在同一个 bin 目录 |
| "另一个 pk 正在运行" | CLI 退出码 5（`state/lock` 被占用）：等当前安装/卸载结束 |
| 404 / 401 | `--serve --addr` 绑了非回环地址却没给 `--token`：加 `--token <随机串>`，或在浏览器带上 token |
| macOS 提示"无法验证开发者" | ad-hoc 签名：系统设置 → 隐私与安全性 → 「仍要打开」一次即可 |
| Linux 启动报 WebKit 缺失 | 装 `libgtk-4-dev libwebkitgtk-6.0-dev`（各发行版包名略有差异） |
| 界面显示旧版本号 | 桌面壳自带前端静态文件：换版本要重装 GUI；CLI 版本看界面顶部 `CLI x.y.z` |

## 六、卸载

GUI 自己只是个可执行文件，删掉即可；它装的运行时都在 `~/.packnest` 里，统一走 CLI：

```bash
pk uninstall --all --yes            # 清运行时不删 pk
pk uninstall --all --yes --purge    # 连 ~/.packnest 一起删
```
