# 桌面端 `pk-desktop`（GUI）

> **一句话**：CLI 的可视化补充。**必须先装 `pk`**，界面里每个动作都是子进程调 `pk <命令> --json`；
> 它没有自己的台账，也不重复实现升级逻辑：CLI 的升级交给 `pk upgrade --self`，桌面端只替换自己的产物。

- 命令面与契约：`pk --json`（[docs/json-schema.md](json-schema.md)）
- 体检页的「一键修复」：修完会显示"**环境已更新，但当前窗口还没生效**"的提示块，
  按 shell 给出要执行的命令（zsh/bash 的 `source <rc>`、PowerShell 的 `. <profile>`、
  Windows 注册表 Path 则提示重开窗口）并带复制按钮 —— 文案来自 CLI `--json` 的 `reload` 字段（1.8.2）
- 开发/打包/发布细节：[cmd/pk-desktop/README.md](../cmd/pk-desktop/README.md)

## 一、下载与安装

产物挂在**独立 tag** `gui-v<版本>` 下（GUI 与 CLI 版本解耦：CLI 是 `v1.8.1` 时 GUI 可能是 `0.11.2`），
gitee 与 GitHub 的 releases 页各一份：

```
https://gitee.com/null_593_5354/packnest/releases/tag/gui-v<版本>
https://github.com/nekoleamo/packnest/releases/tag/gui-v<版本>
```

| 平台 | 文件 | 怎么用 |
|---|---|---|
| macOS（arm64） | `packnest-desktop_<版本>_darwin_arm64.app.zip` | 解压得到 `packnest.app`，拖进「应用程序」；**首次打开**去「系统设置 → 隐私与安全性」点「仍要打开」（ad-hoc 签名，没有公证） |
| Windows（amd64 / **arm64**） | `packnest-desktop_<版本>_windows_{amd64,arm64}.zip` | 解压后直接跑 `pk-desktop.exe`；**不需要 WebView2**（0.12 起原生界面）。ARM64 机器用 `arm64` 那份 |
| Linux（amd64 / **arm64**） | `packnest-desktop_<版本>_linux_<arch>.tar.gz` | 解压跑 `pk-desktop`；只要 GTK3（桌面自带），**托盘**另需 `libayatana-appindicator3` |

macOS 只发 arm64（Apple Silicon）；Intel Mac 需要自己 `make gui-app`（2026-11-12 拍板放弃 intel 产物）。
首次放行这一步**已实测通过**（2026-11-15，GUI 0.11.2 + CLI 1.8.1：被拦 → 隐私与安全性点「仍要打开」→ 正常起窗口并读到 CLI），
详见 `docs/verify-manual.md` §3。

### 用脚本装（推荐，和 CLI 一样的校验强度）

```bash
# macOS：验 checksums.txt 的 sshsig → 比 sha256 → 解压到 ~/Applications（只写用户目录，永不 sudo）
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.sh | sh

# Windows（PowerShell）
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install-gui.ps1 -UseB | iex
```

新版脚本会先检查 `pk` 在不在 PATH 上：**桌面端只是 CLI 的可视化壳，没装 CLI 装了也打不开东西**。

### 渠道现状（需求 R-GUI-1）

| 渠道 | 状态 | 说明 |
|---|---|---|
| 安装脚本（curl / iwr） | ✅ 已提供 | 与 CLI 同一把发布密钥、同一套清单校验；`--skip-verify` 只在拿不到清单时用 |
| 手动下载 zip | ✅ | 见上面表格；先 `shasum -a 256` 对清单 |
| Scoop | ✅ 已发布（0.11.4，amd64 + arm64） | `scoop install packnest-desktop`（`depends: packnest`，会先装 CLI）；manifest 带 `shortcuts`（开始菜单），GUI 不进 PATH；未签名 exe 首次启动有 SmartScreen 提示。**清单按 `checksums.txt` 里实有的架构声明**（0.11.2 起含 ARM64），不写空 hash 的架构 |
| **Homebrew cask** | ❌ **不做** | 产物是 ad-hoc 签名（未公证），过不了 Gatekeeper；而 Homebrew 已移除 `--no-quarantine` 并会禁用过不了检查的 cask。要做 cask 的前提是先买 **Developer ID + 公证（$99/年）**，2026-11-14 拍板**不排期** |
| winget | ❌ 不做 | 2026-11-11 已放弃 |

分析全文（含 gitee 1GB 配额测算与拆仓判据）：`~/Documents/Plan/17-包管理器/pn-GUI分发渠道与仓库形态-调研.md`。

同一个 release 里还有 **`checksums.txt` + `checksums.txt.sig`**（CLI 与 GUI 共用一把发布密钥）：
下载后先 `shasum -a 256 <文件>` 与清单比对，再
`ssh-keygen -Y verify -f <allowed_signers> -I release@packnest.dev -n pk-release -s checksums.txt.sig < checksums.txt`
确认清单本身没被镜像站换过。公钥在 `internal/verify/keys/allowed_signers`（与 `pk doctor` 内置的那份同源）。
核对现成的发布用 `sh scripts/verify-release.sh --gui`（版本取自 `dist/gui/latest`，也可 `--version 0.11.4` 指定）。

## 一点五、检查更新（1.9 起）

界面里的「更新」区块同时看 **桌面端** 与 **CLI** 两个版本（它们版本号解耦）：

- **地址自动选**：先跑 `pk upgrade --self --check --json`，CLI 会把探测到的源与延迟带回来
  （`sources[]` 已按延迟排好），GUI 直接沿用这个顺序 —— 国内通常落到 gitee，海外落到 GitHub；
- **升级 CLI**：完全交给 `pk upgrade --self`（渠道判断、验签、原子替换都只有一份实现）；
- **升级桌面端**：GUI 自己下载 → `ssh-keygen` 验 `checksums.txt.sig` → 比 sha256 → 替换
  （macOS 换整个 `packnest.app`，Windows/Linux 换可执行文件），然后提示「退出并重新打开」；
- **渠道分流**：`scoop` 装的 GUI 只提示 `scoop update packnest-desktop`，不自替换；
- 没有 `ssh-keygen` 时（Windows 需安装「OpenSSH 客户端」功能）**拒绝自更新**并提示用安装脚本 ——
  不做"只比 sha256"的降级。

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
