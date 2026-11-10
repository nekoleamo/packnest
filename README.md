# packnest

**国内网络开箱可用的多语言运行时安装器 —— 与 NestJS 无关。**

命令行叫 `pk`（也提供 `packnest` 别名）。Go 单二进制、`CGO_ENABLED=0`、6 平台、只依赖 2 个第三方库，
装 **node / npm / pnpm / bun / deno / go / python**，内置国内镜像档与真实网络探测，
node 装完自动配对「最新兼容的 npm」，卸载可回滚到零残留。

```bash
npm i -g packnest && pk install node@22
```

> 完整设计见 `~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md`；进度与踩坑见 [docs/progress.md](docs/progress.md)。

## 安装

```bash
# ① npm（国内自动走 npmmirror，秒装）
npm i -g packnest
npm i -g packnest --registry=https://registry.npmmirror.com   # 强制镜像

# ② curl 一行（gitee，不访问 GitHub）
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh -s -- --mirror cn

# ③ PowerShell 一行
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

# ④ Homebrew（自有 tap；formula 名 packnest，命令名 pk）
brew install nekoleamo/packnest/packnest

# ⑤ Scoop
scoop bucket add nekoleamo https://github.com/nekoleamo/scoop-bucket
scoop install packnest

# ⑥ 自托管（把 make release 的 dist/ 丢到任意静态 host，零成本）
curl -fsSL https://你的域名/install.sh | sh -s -- --base https://你的域名/packnest
```

安装脚本开关：`--mirror cn|default` · `--from npm|gitee|base` · `--base URL` · `--scope` · `--version` · `--dir` · `--dry-run`
（PowerShell 同名）。

## 快速开始

```bash
pk install node@22          # 装 node，并自动替换成最新兼容的 npm
pk list                     # 已装工具（* 是当前全局默认，带占用空间）
pk list --available         # 各工具最新可装版本（索引 24h 缓存，离线可读；--refresh 强制回源）
pk which node               # shim 指到哪个真实二进制
pk use node@22.23.4         # 切换全局默认版本
pk use node@22.22.2 --project   # 固定到当前项目（写 pk.toml + pk.lock，不动全局）
pk env --shell zsh          # 输出 PATH/registry 片段；--write 写进 rc 托管块
pk doctor                   # 环境体检（PATH / 台账 / 镜像 / 全局包 / 其他管理器）
pk gc --dry-run             # 零引用版本与超限缓存的回收预演
```

命令面固定 9 个：`install` · `uninstall` · `use` · `list` · `which` · `mirror`（list/probe/use/write）·
`doctor` · `env` · `gc`，另加 `version` / `help`（`pk help install` 有逐命令详解）。

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

## 全局包（`npm i -g`）与切版本

- 共享前缀 `~/.packnest/global`（`npm_config_prefix`），`globalconfig` 也引出来 → **切 node 版本不丢全局包、不丢 `npm config set -g`**
- 包缓存在 `~/.npm`（不迁移不复制）；`global/bin` 是固定目录 → 新装全局包**无需 reshim**
- `pk doctor --globals` 三级检测：`GLOBAL_ENGINES_UNMET`（engines 不满足）、`GLOBAL_NATIVE_ABI`（原生模块，N-API 预编译不告警）、`GLOBAL_SHADOWS_PROJECT`（命令遮蔽项目 bin）
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
| [docs/progress.md](docs/progress.md) | 里程碑进度、每轮交付与踩过的坑 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 故障排查（含与 proto/nvm/mise 共存） |
| [docs/json-schema.md](docs/json-schema.md) | `--json` v1 契约（字段与问题码） |
| [docs/publishing.md](docs/publishing.md) | 渠道、凭据、staged/trusted 发布流程 |

## 已知限制

- `--jobs` 目前仅预留（单连接下载）
- python 的索引只声明 npmmirror（官方 GitHub 无目录列举接口）；go 的索引只声明官方 `?mode=json&include=all`，拉不到时用 24h stale 缓存
- pnpm 12 的 registry tarball 只是启动器，首次运行会自己再下平台二进制（pnpm 上游行为）
- 缓存按 `<tool>-<version>-<platform>.<ext>` 命名（跨版本不重下，但未做内容寻址去重）
- 项目内自动覆盖全局版本需要 direnv（`pk env --project`）；未装 direnv 时 `node` 仍解析到全局默认
- GUI（Wails v3.1.0）未开工，按方案排在 CLI 冻结之后

## 许可证

MIT，见 [LICENSE](LICENSE)。
