# packnest

**国内网络开箱可用的多语言运行时安装器 —— 与 NestJS 无关。**

`pk`（发布名 `packnest`）用 Go 写成，单二进制、零 CGO，装 node / npm / pnpm / bun / deno / go / python，
内置国内镜像档与自动探测，node 装完自动配对「最新兼容的 npm」，卸载可回滚到零残留。

> 总体方案（含实测出处）：`~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md`

## 为什么不是又一个 nvm/fnm/proto

| proto / mise 的做法 | 我们的做法 |
|---|---|
| WASM 插件，首次使用从 GitHub/OCI 下 | provider 是**内嵌 TOML**，零下载；外部 provider 也只读 TOML，不执行代码 |
| 镜像靠用户写正则 | 预置 `cn`/`tuna`/`aliyun`/`ustc` 档 + `auto` 探测 + 回退链 |
| node 与 npm 完全解耦 | **npm 是 node 的伴生包**，按 `engines.node` 反查最新兼容版 |
| 卸载靠删目录 | **台账驱动**：原子写 + 引用计数 + GC + 只删自己记录的 |
| 装完不管 registry | 按档位生成 npm/pnpm/bun/pip/go 的 registry 环境（默认不碰 `~/.npmrc`） |

## 状态

**M4 已完成**（详见 `docs/progress.md`）：台账引用计数、`pk use` 切版本、`pk gc`（零引用 + 缓存 LRU）、
`pk.lock` + `--frozen`（CI 用）、启动清扫、全局包 L1/L2/L3 体检，全部真网验证过。

**M3 已完成**：7 个工具在真网全部装得通并跑得起来 ——
node 22.23.4 / npm 12.2.2（伴生）/ pnpm 12.18.0 / bun 1.4.4 / deno 2.9.8 / go 1.27.3 / python 3.13.18。

**M2 已完成**：pnpm / bun / deno（索引补齐 `dirlist` 与 `github-releases` 两种形态）。

**M1 已完成**：`pk install node@22` 在真网跑通 —— node 从 npmmirror 下载并
多源校验、npm 按 `engines.node` 反查后替换进 node prefix（npmmirror tarball 返回 500 时自动退回
registry.npmjs.org）、卸载后离线重装 0 网络走 TOFU。90 格故障矩阵、8 条 M1 验收用例、`make check` 7 项门禁全绿。

CLI-DONE 的 10 条（M5：`--json` 冻结、错误码定型、6 平台产物、npm/brew/gitee 分发）尚未收口，命令会**明确报出里程碑**并以 1 退出。

## 快速开始

```bash
make build            # → ./bin/pk
./bin/pk --version
./bin/pk list         # 已装工具（初期为空）
./bin/pk mirror list  # 镜像档
./bin/pk doctor       # 环境体检
eval "$(./bin/pk env --shell bash)"   # 把 bin/ 与 registry 环境塞进当前 shell
```

命令面固定 9 个：`install` · `uninstall` · `use` · `list` · `which` · `mirror`（list/probe/use）· `doctor` · `env` · `gc`。
全局开关：`--json`（GUI 与脚本唯一契约）· `--offline` · `--yes` · `--root`（调试）。
退出码：0 成功 / 1 通用失败 / 2 用法 / 3 网络 / 4 校验 / 5 锁被占用。

## 开发

```bash
make test     # 零外网单测（默认 go test ./... 也零外网）
make accept   # 只跑验收用例 TestAcceptance_*
make check    # 7 项门禁：gofmt+vet → race → 覆盖率 → 6 平台交叉编译 → fuzz → 性能 → --json golden
make cross    # 6 平台产物到 dist/
```

约定与硬约束（依赖预算 2 个、代码 ≤8500 行、9 命令、CGO=0、零外网测试……）见 `AGENTS.md`。

## 目录

```
cmd/pk/              CLI（9 命令 + 全局开关）
internal/paths/      PK_ROOT 单根布局（测试用 t.TempDir() 完全隔离）
internal/spec/       版本说明符、engines 匹配、预发布过滤
internal/archive/    tar.gz/tgz/zip 解压、strip、单文件布局、原子替换
internal/verify/     SHASUMS/registry integrity 校验 + TOFU 表
internal/companion/  node↔npm 伴生反查（engines 精确匹配）
internal/toolchain/  go 的 GOTOOLCHAIN 检查 + python 的 pbs 两级目录列举
internal/install/    安装编排：索引→下载→校验→解压→伴生→shim→台账
internal/provider/   provider TOML 模型 + URL 模板渲染 + 版本索引加载
internal/mirror/     镜像档、回退链、探测（6h TTL）
internal/fetch/      可注入 HTTP 层：Range 续传、ETag、超时、重试
internal/store/      台账 installs.json + config.toml + 全局锁
internal/shim/       bin/ 入口生成与解析（pk / packnest 双名同源）
internal/env/        PATH 三段 + registry 环境片段
internal/jsonout/    --json / NDJSON 契约（schema 1）
internal/testkit/    假镜像 + 合成发行包 + 故障注入（测试专用）
providers/*.toml     7 个内嵌 provider（//go:embed）
acceptance/          验收用例（TestAcceptance_*）
```

## 许可证

待定（M5 定稿）。
