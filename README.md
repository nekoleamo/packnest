# packnest

**中文** · [English](README.en.md)

**国内网络开箱可用的多语言运行时安装器。**

命令行叫 `pk`（也提供 `packnest` 别名）。Go 单二进制、`CGO_ENABLED=0`、6 平台、只依赖 2 个第三方库，
装 **node / npm / pnpm / bun / go / python**，内置国内镜像档与真实网络探测，
node 装完自动配对「最新兼容的 npm」，卸载可回滚到零残留。

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh && pk install node@22
```

> 官网（单页极简版）：<https://nekoleamo.github.io/packnest/> · 源码 [GitHub](https://github.com/nekoleamo/packnest) / 镜像 [gitee](https://gitee.com/null_593_5354/packnest)
>
> 完整设计见 `~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md`；进度与踩坑见 [docs/progress.md](docs/progress.md)。

## 安装

```bash
# ① curl 一行（推荐；从 gitee 直取，不访问 GitHub）
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh

# ② Windows PowerShell 一行
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

# ③ Homebrew（自有 tap；formula 名 packnest，命令名 pk）
brew install nekoleamo/packnest/packnest

# ④ Scoop
scoop bucket add nekoleamo https://github.com/nekoleamo/scoop-bucket
scoop install packnest

# ⑤ 自托管（把 make release 的 dist/ 丢到任意静态 host，零成本）
curl -fsSL https://你的域名/install.sh | sh -s -- --base https://你的域名/packnest
```

海外用户可直接从 GitHub Releases 取同一批产物（sha256 与 gitee 完全一致）：
`curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh -s -- --base https://github.com/nekoleamo/packnest/releases/download`

安装脚本开关：`--from gitee|base` · `--base URL` · `--version` · `--dir` · `--skip-verify` · `--dry-run`
（`--mirror cn|default` 保留兼容，两者现在都走 gitee）。下载后**校验 sha256**（对 `checksums.txt`），
对不上退出码 4。PowerShell 版参数名换成 `-Base` / `-Version` / `-Dir` / `-SkipVerify` / `-DryRun`。

> npm 分发渠道已于 2026-11-11 停用：npm 的 stage/approve 要 2FA，而且云端与本地构建出的归档字节不同，
> 容易出现两套 sha256。已经用 `npm i -g packnest` 装过的旧版本照常能用，`pk uninstall --all` 仍会识别这个渠道。

## 快速开始

```bash
pk install node@22          # 装 node，并自动替换成最新兼容的 npm
pk list                     # 已装工具（* 是当前全局默认，带占用空间）
pk list node                # 只看 node：已装 + 可装版本（版本多时自动按大版本分组网格）
pk list node --major 22     # 只看 22.x 的全部版本；--all 不截断
pk list --available         # 各工具最新可装版本（索引 24h 缓存，离线可读；--refresh 强制回源）
pk which node               # shim 指到哪个真实二进制
pk use node@22.23.4         # 切换全局默认版本
pk use node@22.22.2 --project   # 固定到当前项目（写 pk.toml + pk.lock，不动全局）
pk env --shell zsh          # 输出 PATH/registry 片段；--write 写进 rc 托管块
pk doctor                   # 环境体检（PATH / 台账 / 镜像 / 全局包 / 其他管理器）
pk gc --dry-run             # 零引用版本与超限缓存的回收预演
```

命令面固定 9 个：`install` · `uninstall`（`<工具>[@<版本>]` / `--all`）· `use` · `list`（`[工具]` / `--available`）·
`which` · `mirror`（list/probe/use/write）· `doctor` · `env` · `gc`，另加 `version` / `help`
（`pk help install` 有逐命令详解）。

全局开关：`--json`（GUI 与脚本的唯一契约）· `--offline` · `--yes` · `--root`（调试）。
退出码固定：`0` 成功 / `1` 通用 / `2` 用法 / `3` 网络 / `4` 校验 / `5` 锁被占用。

## 与 proto / mise / nvm 的差别

| 它们 | packnest |
|---|---|
| WASM 插件，首次使用从 GitHub/OCI 下 | provider 是**内嵌 TOML**，零下载；外部 provider 也只读 TOML，不执行代码 |
| 镜像要用户写正则重写 URL | 预置 `cn`/`tuna`/`aliyun`/`ustc` 档 + `auto` **真实探测**（国外走官方不多花请求，国内回落镜像，结果缓存 6h） |
| node 与 npm 完全解耦 | **npm 是 node 的伴生包**，按 `engines.node` 反查最新兼容版（含预发布排除与补丁号级分界） |
| 卸载靠删目录 | **台账驱动**：原子写 + `.bak` 自动恢复 + 引用计数 + GC + 只删自己记录的 |
| 装完不管 registry | 按档位生成 npm/pnpm/bun/pip/go 的 registry 环境，默认**不动** `~/.npmrc` |

## 安装具体版本

```bash
pk install node@22.22.2   # 精确版本（跳过索引解析，仍取校验和）
pk install node@22        # 22.x 最新补丁
pk install node@latest    # 最新稳定
pk install node@lts       # 最新 LTS
pk install node@22 --dry-run   # 只打印版本/产物 URL/校验方式，不下载
```

## 全局 vs 项目级

`install` 的**作用域由当前目录决定**；`use` 用开关显式指定作用域：

| 动作 | 行为 |
|---|---|
| `pk install`（目录**没有** `pk.toml`） | 装进共享目录 `~/.packnest/tools/<tool>/<ver>`；该工具还没有全局默认时**自动设为默认** |
| `pk install`（目录**有** `pk.toml`） | 按声明安装，并生成/更新 `pk.lock`（项目级锁定） |
| `pk use node@22.22.2` | 固定**全局**默认（config.toml + 台账引用 + 重指 shim 三处同步） |
| `pk use node@22.22.2 --project` | 固定**项目**：写当前目录 `pk.toml` + 重新生成 `pk.lock`，不动全局默认 |
| `pk use --lock` | 只按现有 `pk.toml` 刷新 `pk.lock`（不切版本、不装东西；用 `latest`/`lts` 后用它对冲别名漂移） |
| `pk env --project` | 在项目内把锁定版本前置到 PATH（配合 direnv，实现"项目级覆盖全局默认"） |

磁盘上永远只有一份，项目级只体现「锁定 + PATH 顺序」，不复制副本。
`pk.toml` 的写入是**文本级合并**：保留你手写的注释、分段与顺序。

## 工程约束（写死为门禁）

- 第三方依赖 **2 个**：`Masterminds/semver/v3`、`BurntSushi/toml`
- 归档只支持 **tar.gz / zip**，解压零第三方（`archive/tar` + `compress/gzip` + `archive/zip`）
- 单根 `PK_ROOT`（默认 `~/.packnest`），**永不 sudo、永不写 shell rc**（`pk env --write` 的托管块除外）
- 默认**不动**用户已有配置：不写 `~/.npmrc`（要写用 `pk mirror write`，写前备份、只增不改）
- 默认 `go test ./...` **零外网**；真网测试一律 `//go:build nettest`，不进门禁

## 下载与校验

- 下载：Range 续传（`.part` + `.part.meta`）、ETag/304、超时分档（连接 5s / 首字节 8s）、单源最多重试 2 次；**单连接默认**（国内 CDN 分片常更慢，`--jobs` 目前仅预留）
- 校验：`SHASUMS256.txt` / 每产物 `.sha256sum` / registry integrity（sha1 或 sha512 SRI）/ go 索引内联 sha256
- **TOFU + 多源交叉验证**：首次装要求 ≥2 个源给出相同摘要（否则标 `weak`），写进 `state/known.json`；此后同版本**完全离线可验证**
- 源之间矛盾 → 硬失败并说明"产物与哪个源一致"，`--insecure` 可放行（标 unverified，且不写入 TOFU 表）

## 外部 provider 清单（长尾工具不用等发版）

内嵌 TOML 只有 6 个工具；想装 zig / uv / jdk 这类长尾，自己写一份清单放进去就行 —— **清单是纯数据，不执行任何代码**。

```bash
~/.packnest/providers/zig.toml     # 文件名必须等于 id（zig.toml ↔ id = "zig"）
PK_PROVIDERS_DIR=/opt/corp/pk:/srv/more   # 追加目录（可多个，冒号分隔；不支持指向 http(s)://）
```

加载顺序：内嵌 → `$PK_ROOT/providers` → `PK_PROVIDERS_DIR`。**内嵌永远赢**（同 id 时外部被忽略，
`pk doctor` 报 `PROVIDER_EXTERNAL_CONFLICT`），外部之间按文件名排序、先加载的赢。

外部清单的安全边界（不满足就拒绝加载该文件，`pk doctor` 报 `PROVIDER_EXTERNAL_INVALID` 并带文件名与原因）：

- URL 必须是 `https://`；内网 http 源要 `PK_ALLOW_HTTP=1` 显式放行（`pk doctor` 报 `PROVIDER_HTTP_URL`）
- `layout.single` / `bin_targets` 必须是相对路径且不含 `..`；`strip` 不许为负
- 禁用 `index.kind = "pbs"`（python 的目录列举解析在内嵌逻辑里）
- URL 模板只能用 `{version} {platform} {ext} {registry} {tool} {filename} {tag}`
- `[companions]` 的 `registry` 必须指向已存在的工具 id
- 单个清单 ≤ 256KB（防止误把二进制当清单）

**校验强度按校验源算，不按产物**：产物与校验和都走 http 时最多判 `weak`（摘要可以跟着一起被换掉）；
产物走 http 但校验和走 https 且 ≥2 源一致，仍然是 `strong`。台账里会留 `transport: "http"` 的痕。

`pk list` 里外部工具带 `*`，`--json` 每条工具多一个 `"source": "embedded" | "external"`（只增字段）。
外部清单**任何一个坏文件都不会让 CLI 起不来**：跳过它、记一条 issue，其余照常。

## 工具链自带的那些东西（corepack / pip / `go install`）

- **corepack**（node ≤24 自带）：`pk install node@22 --corepack` 会在 `~/.packnest/global/corepack/bin`
  （PATH 第 2 段）装 `yarn`/`yarnpkg`/`pnpm` 的 shim，`pk install node@22 --no-corepack` 关闭（写在 `config.toml`）。
  provider 装的 `pnpm` 永远优先。node 25+ 已移除 corepack，敲 `--corepack` 会**明确报错**并建议 `pk install pnpm`。
  首次使用会联网拉包，走 `COREPACK_NPM_REGISTRY`（= 当前镜像档），缓存进 `~/.packnest/cache/corepack`。
- **pip**：`pip3` 是 python 自带的，`PIP_INDEX_URL` 已由 `pk env` 导出；**用户级安装（`~/.local/lib/pythonX`）不归 pk 管**
  （`pk doctor` 用 `PIP_USER_SITE_UNMANAGED` 说明），要隔离请用 venv。
- **`go install`**：产物落到 `GOBIN`（默认 `~/go/bin`），**我们不替你设 GOBIN、也不加 PATH 段**；
  它不在 PATH 时 `pk doctor` 报 `GO_INSTALL_NOT_IN_PATH`。

## 全局包（`npm i -g`）与切版本

- 共享前缀 `~/.packnest/global`（`npm_config_prefix`），`globalconfig` 也引出来 → **切 node 版本不丢全局包、不丢 `npm config set -g`**
- 包缓存在 `~/.npm`（不迁移不复制）；`global/bin` 是固定目录 → 新装全局包**无需 reshim**
- `pk doctor --globals` 三级检测：`GLOBAL_ENGINES_UNMET`（engines 不满足）、`GLOBAL_NATIVE_ABI`（原生模块，N-API 预编译不告警）、`GLOBAL_SHADOWS_PROJECT`（命令遮蔽项目 bin）
- 三种包管理器的全局目录都扫（`detail.manager` 标来源）：npm（`global`）、pnpm（`global/pnpm/global/<N>`）、bun（`global/bun/install/global`）
- 想回 nvm 式行为：`global_mode = "per-version"`

## 与已有管理器共存

`pk` 只写 `~/.packnest`，**绝不改别人的目录**。`pk doctor --foreign` 会只读列出 proto/nvm/volta/asdf/mise/fnm 的 node 版本，
并在别人的 shim 抢在 `~/.packnest/bin` 前面时告警（`FOREIGN_SHIM_PRIORITY`）。

推荐：先并存 → `pk install node@22 && pk which node` 验证 → 再决定是否从 rc 里删掉对方的初始化行（这一步由你手动做）。

## 项目锁与 CI

```toml
# pk.toml（入库）
[tools]
node = "22"
npm  = "auto"
```

`pk install` 会生成 `pk.lock`（只写 version/sha256/source，不写 URL → 换镜像不影响锁）。
日常改版本用 `pk use node@X --project`（一条命令同时改清单和锁），只改锁用 `pk use --lock`。
CI 里 `pk install --frozen` 严格按锁安装，清单与锁不一致即非 0 退出（别名也会被拒，必须先钉住）。

## 开发

```bash
make build      # CGO_ENABLED=0 本地构建 ./bin/pk
make test       # 零外网单测
make accept     # 只跑验收用例 TestAcceptance_*
make check      # 7 项门禁（提交前必跑）
make release    # 6 平台发布包 + checksums.txt → dist/
make npm-dry    # 只打 npm 包并校验 manifest（不发、不需要凭据）
```

## 文档

| 文档 | 内容 |
|---|---|
| [docs/cli.md](docs/cli.md) | **操作手册**：9 个命令全量示例（可直接用于网站发布） |
| [README.en.md](README.en.md) · [docs/en/](docs/en/README.md) | 英文版介绍与文档（CLI 参考、故障排查） |
| [docs/progress.md](docs/progress.md) | 里程碑进度、每轮交付与踩过的坑 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 故障排查（含与 proto/nvm/mise 共存） |
| [docs/json-schema.md](docs/json-schema.md) | `--json` v1 契约（字段与问题码） |
| [docs/publishing.md](docs/publishing.md) | 发布渠道（gitee / brew / scoop）、凭据、唯一发布者规则 |
| [docs/verify-1.0.md](docs/verify-1.0.md) | 1.0 前实装验证记录（6 平台矩阵、发现的问题与修复） |

## 彻底卸载

分两层：**pk 装过的东西**用 pk 自己清，**pk 自己**要用当初安装它的渠道删。

```bash
pk uninstall --all --yes            # 所有运行时 + shim + rc/Path 托管块（保留缓存，可离线重装）
pk uninstall --all --yes --purge    # 连 ~/.packnest 一起删（工具、缓存、台账、配置）
pk uninstall --all --yes --purge --keep-cache   # 只留 cache/，下次装同版本不联网
```

`--all` 会依次：① 删掉台账里每个运行时目录；② 删 `~/.packnest/bin` 里的 shim；
③ 清掉 shell rc 里的 `# >>> packnest >>>` 托管块（Windows 还有**注册表用户 Path** 里的五段目录）；
④ 按"你当初怎么装的 pk"给出删二进制的确切命令（brew / npm / scoop / winget / 脚本）。
不带 `--yes` 只打印计划，什么都不动。

pk 二进制本身（按渠道）：

| 渠道 | 命令 |
|---|---|
| Homebrew | `brew uninstall packnest && brew untap nekoleamo/packnest` |
| npm | `npm uninstall -g packnest` |
| Scoop | `scoop uninstall packnest && scoop bucket rm nekoleamo` |
| winget | `winget uninstall nekoleamo.packnest` |
| curl 脚本（macOS/Linux） | `rm -f ~/.local/bin/pk ~/.local/bin/packnest`（默认 `~/.local/bin`，装了 `--dir` 就用那个目录） |
| iwr 脚本（Windows） | `Remove-Item -Recurse -Force "$env:LOCALAPPDATA\packnest\bin"` |
| 自托管 / 手动 | 删掉那个文件即可 |

脚本装的这两个渠道，`pk` 自己知道装到哪了（安装时写了 `state/install.json`），所以可以一条命令连自己一起删：

```bash
pk uninstall --all --yes --purge --self     # 删运行时 + 缓存 + 配置 + pk 二进制本身
```

`--self` 在 macOS/Linux 上直接删掉进程自己（unlink 正在运行的文件没问题）并收掉空目录；
Windows 上系统不允许删除正在运行的 exe，会打印出要你执行的那条 `Remove-Item`。

**pk 只写 `~/.packnest` 和它自己的托管块**，所以上面的步骤之后不会剩什么。特别地：
`~/.npm` 缓存、`~/.npmrc`、别的版本管理器目录**从来没被动过**，也不需要清理。

## 已知限制

- `--jobs` 目前仅预留（单连接下载）
- python 的索引只声明 npmmirror（官方 GitHub 无目录列举接口）；go 的索引只声明官方 `?mode=json&include=all`，拉不到时用 24h stale 缓存
- pnpm 12 的 registry tarball 只是启动器，首次运行会自己再下平台二进制（pnpm 上游行为）
- 缓存按内容寻址：`cache/archives/<sha256>.<ext>`（TOFU 表里已知摘要时直接按内容找，同内容跨版本只存一份）
- 项目内自动覆盖全局版本需要 direnv（`pk env --project`）；未装 direnv 时 `node` 仍解析到全局默认
- GUI（Wails v3.1.0）未开工，按方案排在 CLI 冻结之后

## 许可证

MIT，见 [LICENSE](LICENSE)。
