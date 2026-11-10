# packnest

**国内网络开箱可用的多语言运行时安装器 —— 与 NestJS 无关。**

命令行叫 `pk`（也提供 `packnest` 别名）。Go 单二进制、`CGO_ENABLED=0`、6 平台、只依赖 2 个第三方库，
装 **node / npm / pnpm / bun / deno / go / python**，内置国内镜像档与真实网络探测，
node 装完自动配对「最新兼容的 npm」，卸载可回滚到零残留。

```bash
npm i -g packnest && pk install node@22
```

> 完整设计见 `~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md`；进度见 [docs/progress.md](docs/progress.md)。

## 当前状态（v0.6.6）

| 项 | 状态 |
|---|---|
| 7 个工具 | ✅ 真网全部装通并跑得起来（node 22.23.4 / npm 12.2.2 / pnpm 12.18 / bun 1.4.4 / deno 2.9.8 / go 1.27.3 / python 3.13.18） |
| CLI-DONE（方案 §12.2 的 10 条） | ✅ 9/10 达标；第 7 条"渠道可下"已全部上线（见下） |
| 分发渠道 | ✅ npm · gitee release · curl/iwr 脚本 · 自托管 `--base` · Homebrew tap · Scoop bucket（winget manifest 已备） |
| 发版 | ✅ GitHub Actions + **Trusted Publishing（OIDC）**：`git tag` 即发版，无 token、无 2FA |
| 门禁 | ✅ `make check` 7 项（gofmt/vet · race · 覆盖率 · 6 平台交叉编译 · fuzz · benchmark · `--json` golden） |
| GUI（Wails v3.1.0） | ⏳ 未开工（按方案排在 CLI 冻结之后） |

## 安装

```bash
# ① npm（国内自动走 npmmirror，秒装）
npm i -g packnest
# 国内想强制镜像：
npm i -g packnest --registry=https://registry.npmmirror.com

# ② curl 一行（gitee，不访问 GitHub）
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh -s -- --mirror cn

# ③ PowerShell 一行
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex

# ④ Homebrew（自有 tap；命令名是 pk，formula 名是 packnest）
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
pk list --available         # 各工具最新可装版本（索引 24h 缓存，离线可读）
pk which node               # shim 指到哪个真实二进制
pk use node@22.23.4         # 切换全局默认版本
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

## 工程约束（写死为门禁）

- 第三方依赖 **2 个**：`Masterminds/semver/v3`、`BurntSushi/toml`
- 归档只支持 **tar.gz / zip**，解压零第三方（`archive/tar` + `compress/gzip` + `archive/zip`）
- 单根 `PK_ROOT`（默认 `~/.packnest`），**永不 sudo、永不写 shell rc**（`pk env --write` 的托管块除外）
- 默认**不动**用户已有配置：不写 `~/.npmrc`（要写用 `pk mirror write`，写前备份、只增不改）
- 默认 `go test ./...` **零外网**；真网测试一律 `//go:build nettest`，不进门禁

## 下载与校验

- 下载：Range 续传（`.part` + `.part.meta`）、ETag/304、超时分档（连接 5s / 首字节 8s）、单源最多重试 2 次；**单连接默认**（国内 CDN 分片常更慢，`--jobs` 目前仅预留）
- 校验：`SHASUMS256.txt` / 每产物 `.sha256sum` / registry integrity（sha1 或 sha512 SRI）/ go 索引内联 sha256
- **TOFU + 多源交叉验证**：首次装要求 ≥2 个源给出相同摘要（否则 `weak`），写进 `state/known.json`；此后同版本**完全离线可验证**
- 源之间矛盾 → 硬失败并说明"产物与哪个源一致"，`--insecure` 可放行（标 unverified，且不写入 TOFU 表）

## 全局包（`npm i -g`）与切版本

- 共享前缀 `~/.packnest/global`（`npm_config_prefix`），`globalconfig` 也引出来 → **切 node 版本不丢全局包、不丢 `npm config set -g`**
- 包缓存在 `~/.npm`（不迁移不复制）；`global/bin` 是固定目录 → 新装全局包**无需 reshim**
- `pk doctor --globals` 做三级检测：`GLOBAL_ENGINES_UNMET`（engines 不满足）、`GLOBAL_NATIVE_ABI`（原生模块，N-API 预编译不告警）、`GLOBAL_SHADOWS_PROJECT`（命令遮蔽项目 bin）
- 想回 nvm 式行为：`global_mode = "per-version"`

## 项目锁与 CI

```toml
# pk.toml（入库）
[tools]
node = "22"
npm  = "auto"
```

`pk install` 会生成 `pk.lock`（只写 version/sha256/source，不写 URL → 换镜像不影响锁）。
CI 里 `pk install --frozen` 严格按锁安装，清单与锁不一致即非 0 退出。

## 开发

```bash
make build      # CGO_ENABLED=0 本地构建 ./bin/pk
make test       # 零外网单测
make accept     # 只跑验收用例 TestAcceptance_*
make check      # 7 项门禁（提交前必跑）
make release    # 6 平台发布包 + checksums.txt → dist/
make gitee-release   # 传到 gitee release（凭据只在本机）
make npm-dry    # 只打 npm 包并校验 manifest（不发、不需要凭据）
```

当前规模：实现约 **9.4k 行**（`cmd/` + `internal/`，不含测试），测试约 7.5k 行，17 个包。

## 目录

```
cmd/pk/                CLI（9 命令 + version/help）
internal/paths/        PK_ROOT 单根布局、归档路径防护
internal/spec/         版本说明符、engines 匹配、预发布过滤
internal/provider/     provider TOML 模型、URL 模板、版本索引加载
internal/mirror/       镜像档、回退链、探测与 auto 判定
internal/fetch/        可注入 HTTP 层（Range/ETag/进度回调）
internal/archive/      tar.gz/zip 解压、strip、单文件布局、原子替换
internal/verify/       SHASUMS/registry 校验 + TOFU 多源交叉验证
internal/companion/    node↔npm 伴生反查
internal/install/      安装编排、台账、锁文件、GC、use
internal/store/        台账/配置/锁、缓存 LRU、启动清扫
internal/shim/         shim 生成与解析（pk / packnest 双名同源）
internal/env/          PATH/registry 片段、全局包扫描、rc 托管块
internal/jsonout/      `--json` v1 契约（golden 锁住）
internal/toolchain/    go 的 GOTOOLCHAIN、python 的 pbs 两级目录列举
internal/testkit/      假镜像与合成发行包（测试专用）
providers/*.toml       7 个内嵌 provider（//go:embed）
.github/workflows/     发版流水线（OIDC 发布 + gitee + brew/scoop + 镜像）
acceptance/            验收用例（TestAcceptance_*）
```

## 发布（维护者）

```bash
# 用户侧：npm / gitee / brew / scoop 见上文
# 维护者：改版本 → 打 tag → CI 全自动
gh variable set SKIP_NPM_PUBLISH -b false -R nekoleamo/packnest
git tag -a v0.6.7 -m "packnest 0.6.7" && git push origin v0.6.7
```

CI 用 **Trusted Publishing（OIDC）** 发布 npm（无 token、无 2FA），随后上传 gitee release、
刷新 brew/scoop 清单、同步 gitee 代码镜像。细节见 [docs/publishing.md](docs/publishing.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [docs/progress.md](docs/progress.md) | 里程碑进度、每轮交付与踩过的坑 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 故障排查（含与 proto/nvm/mise 共存） |
| [docs/json-schema.md](docs/json-schema.md) | `--json` v1 契约（字段与问题码） |
| [docs/publishing.md](docs/publishing.md) | 渠道、凭据、staged/trusted 发布流程 |

## 已知限制

- `--jobs` 目前仅预留（单连接下载）
- python 的索引只声明 npmmirror（官方 GitHub 无目录列举接口）；go 的索引只声明官方 `?mode=json&include=all`，拉不到时用 24h stale 缓存
- pnpm 12 的 registry tarball 只是启动器，首次运行会自己再下平台二进制（pnpm 上游行为）
- 缓存按 `<tool>-<version>-<platform>.<ext>` 命名（跨版本不重下，但未做内容寻址去重）
- scheduler/GUI 未开始：`cmd/pk-desktop` 计划在 CLI 冻结后按方案 M6 接入

## 许可证

MIT，见 [LICENSE](LICENSE)。
