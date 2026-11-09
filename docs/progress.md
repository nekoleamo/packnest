# 进度

> 口径：方案 §12 的里程碑。**验收项 = 测试名**，`make accept` 全绿才算完成。

## 里程碑

| 阶段 | 内容 | 状态 | 验收用例 |
|---|---|---|---|
| **M0** | 骨架：module、9 命令、provider 加载、镜像探测、testkit 假镜像、L1 单测 | ✅ 完成（2026-11-10） | `M0_ProviderLoad` ✅ · `M0_MirrorProbe` ✅ · `M0_DoctorNoPanic` ✅ · `M0_DualNameConsistent` ✅ |
| **M1** | node + 伴生 npm：解压层 · 校验+TOFU · 版本索引 · 伴生反查 · 安装编排 · 真卸载 | ✅ 完成（2026-11-10，含真网实测） | `M1_NpmCompanion` ✅ · `_NpmPinConflict` ✅ · `_PatchLevelBoundary` ✅ · `_OfflineFromCache` ✅ · `_ShimLatency` ✅ · `_UninstallClean` ✅ · `_WindowsAtomicReplace` ✅ · `_WindowsZipNoSymlink` ✅ · `_FailureMatrix`（90 格）✅ |
| **M2** | pnpm / bun / deno | ✅ 完成（2026-11-10，含真网实测） | `M2_PnpmRequiresNode` ✅ · `_SingleFileLayout` ✅ · `_ThreePlatformSmoke` ✅ · `_OfficialUnreachable`（5 工具）✅ |
| **M3** | go + python | ✅ 完成（2026-11-10，含真网实测） | `M3_GoNoDoubleDownload` ✅ · `_PythonNoCompile` ✅ · `_PbsDirectoryIndex` ✅ |
| **M4** | 台账/引用计数/GC/全局包/启动清扫 | ✅ 完成（2026-11-10，含真网实测） | `M4_GcZeroRef` ✅ · `_LedgerRecover` ✅ · `_FrozenMismatchExitCode` ✅ · `_CtrlCNoPartial` ✅ · `_GlobalSurvivesNodeSwitch` ✅ · `_GlobalBinNoReshim` ✅ · `_NativeGlobalDetect` ✅ · `_StartupSweep` ✅ · `_CacheMax` ✅ | `M4_GcZeroRef` / `_LedgerRecover` / `_FrozenMismatchExitCode` / `_CtrlCNoPartial` / `_GlobalSurvivesNodeSwitch` / `_GlobalBinNoReshim` / `_NativeGlobalDetect` / `_StartupSweep` |
| **M5** | CLI 收口（doctor 全量、`--json` 冻结、6 平台产物、分发） | ✅ 完成（2026-11-10） | 见下方 CLI-DONE 10 条 | §12.2 的 10 条 CLI-DONE |
| G0–G2 | Wails v3.1.0 桌面端 | 未开始（CLI 冻结后才动） | — |

## M0 交付清单（2026-11-10）

**已实现（可用）**

- `pk list` / `list --json` / `list --available`：台账 + 内嵌 provider 目录，node 的伴生 npm 显式显示
- `pk which <命令>`：`pk` 与 `packnest` 解析到同一文件（`TestAcceptance_M0_DualNameConsistent`）
- `pk mirror list|probe|use`：5 个档位 + auto、并行探测、结果 6h 缓存（`state/probe.json`）、写 `config.toml`
- `pk doctor`（文本/`--json`）：provider 自洽、配置可读性、镜像档、台账自洽、双名 shim、PATH 顺序、其他管理器共存提示
- `pk env [--shell bash|zsh|sh|powershell] [--json]`：PATH 三段 + registry/共享前缀变量片段（不落盘）
- `pk --version`（文本/`--json`）、`pk help`

**骨架（明确报里程碑，退出码 1）**

- `install`（M1）· `uninstall` / `use` / `gc`（M4）· `mirror write` / `env --write` / `doctor --globals|--foreign`（M4–M5）

**基础设施**

- `internal/paths`：`PK_ROOT` 单根布局 + 归档路径 `SafeJoin`（zip-slip 防护）
- `internal/spec`：说明符（`22` / `22.22` / `22.22.2` / 范围 / `latest` / `lts`）、engines 匹配、**预发布默认排除**
- `internal/provider`：7 个内嵌 TOML（node/npm/pnpm/bun/deno/go/python）+ 校验 + URL 模板渲染
- `internal/mirror`：档位、回退链（`default` 永远最后）、探测缓存、错误给可执行建议
- `internal/fetch`：可注入客户端、HEAD/GET/带 ETag 的索引缓存（304 零字节）、Range 续传、截断检测
- `internal/store`：台账（原子写 + `.bak` + 引用计数）、`config.toml`、全局锁（flock / Windows 独占创建，拿不到 → 退出码 5）
- `internal/shim`：Unix 符号链接原子替换、Windows `.cmd`、双名同源
- `internal/env`：PATH 三段、registry/共享前缀变量、posix/powershell 片段
- `internal/jsonout`：`--json` schema 1（golden 锁住）、NDJSON 事件、退出码常量
- `internal/testkit`：`MakeNode` / `MakeNodeZip` / `MakeNpmRegistry` / `SumsFile` / `Mirror` + 10 种故障注入（`Status` / `StatusFor` / `TruncateAt` / `DropAfter` / `NoRange` / `WrongETag` / `Slow` / `Corrupt` / `MissingChecksum` / `ChecksumMismatch` / `StaleIndex`）

**门禁现状**（`make check` 7 项全绿）

| 项 | 结果 |
|---|---|
| gofmt + go vet | ✅ |
| `go test -race -count=1 ./...` | ✅（135 个用例） |
| 覆盖率 internal ≥85% / cmd ≥70% | ✅（env 97.5 / fetch 86.9 / jsonout 100 / mirror 89.8 / paths 93.3 / provider 89.8 / shim 87.5 / spec 90.7 / store 88.0 / cmd 86.0） |
| 6 平台 CGO=0 交叉编译 | ✅（6.5–7.3 MB/个，符合 6–8MB 预算） |
| fuzz（`FuzzSpec` 30s） | ✅ |
| benchmark（`BenchmarkSpecParse` <50µs、`BenchmarkShimResolve` <15ms） | ✅（≈9.6µs / ≈8µs） |
| `--json` golden | ✅ |

## M1 交付清单（2026-11-10）

**新包**

- `internal/archive`：tar.gz/tgz/zip 解压 · strip 层级 · 单文件布局 · `bin_targets` · zip-slip 防护 · Windows 拒绝符号链接 · `Replace`（`dst → dst.old → 新 → 删`，失败回滚）
- `internal/verify`：GNUSUMS / per-artifact / registry-integrity 解析 · TOFU 表（`state/known.json`）· 多源交叉验证（≥2 同值 = strong，1 源 = weak，`--insecure` = unverified）· **sha256 / sha1（npm shasum）/ sha512 SRI（npm integrity）三种摘要**
- `internal/companion`：node↔npm 反查（`engines.node` 精确匹配、预发布排除、补丁号级分界）· 裁决优先级 显式 pin > pinned > latest-compatible > 自带 · 不兼容 pin 硬失败并给出"可用上限"
- `internal/install`：安装编排（索引→下载→校验→解压→伴生→shim→台账）· 缓存命中即纯本地（0 网络）· 探测全挂时退化直连 · 真卸载（台账驱动 + 引用检查 + shim 清理 + 缓存保留）
- `internal/provider/index.go`：版本索引加载（JSON 数组 / registry 元数据）· `latest`/`lts` 别名 · "镜像未同步"提示 · 索引 24h 缓存 + stale 回退

**CLI 落地**：`install [工具@版本|按 pk.toml] [--insecure --force]`、`uninstall <工具>@<版本> [--force]`；全局锁（占用 → 退出码 5）；错误分类退出码（网络 3 / 校验 4 / 锁 5）；`--json` 为 NDJSON 事件流（resolve/cache/download/verify/companion/done）

**真网实测（2026-11-10，本机 mac/arm64，镜像档 cn）**

```
解析 node@22.23.4 → 下载 47.7 MB（npmmirror）→ 校验通过（network）
下载 npm@12.2.2（2.9 MB，npmmirror 的 tarball 路径 500 → 自动退回 registry.npmjs.org）
伴生包 npm@12.2.2（replaced 10.9.9）→ 装好
node --version = v22.23.4，npm --version = 12.2.2
uninstall → 离线重装：缓存命中 47.7MB / 0 网络 / 校验走 TOFU
```

**真网暴露并修掉的 3 个真 bug**（都是测试测不出来的）

1. node 的 `index.json` 里 `files` 是**字符串数组**（不是对象）→ 索引解析要按形状兼容
2. registry 模板 `{registry}` 只按当前档渲染一次 → 国内档 tarball 挂了**不会退到官方 registry**；改成按回退链逐档渲染
3. npm 的 `dist.shasum` 是 **sha1**（不是 sha256），`dist.integrity` 是 sha512 SRI → 校验层支持三算法，优先 integrity

## M2 交付清单（2026-11-10）

**新增能力**

- `provider` 索引形态补齐：`dirlist`（npmmirror 的 `/-/binary/<tool>/` JSON 目录列举）与 `github-releases`（Releases API 的 `tag_name`）——bun/deno 官方侧没有 index.json，靠这两者
- bun provider 修正（真网实测）：npmmirror 的版本目录是 `bun-v{version}/`（带 `bun-` 前缀，原来少了）；校验和取同目录 `SHASUMS256.txt`；`bunx` 与 `bun` 是同一个可执行文件（argv[0] 分派）
- deno provider：单文件布局 `bin/deno`（0755）+ per-artifact `.zip.sha256sum`
- pnpm provider：registry tarball，`requires = ["node"]`（不隐式装运行时），`bin_targets` 指向 `bin/pnpm.cjs`

**CLI 修的一个通用 bug**：开关只能写在目标前（`pk install --insecure deno`），因为标准库 `flag` 遇到第一个非开关就停。新增 `parseFlexible`：开关可出现在任意位置（`pk install deno --insecure`）。

**真网实测（2026-11-10，镜像档 cn）**

```
pk install bun   → 1.4.4（38.5 MB，npmmirror 目录列举 + SHASUMS256）→ bun --version 1.4.4
pk install deno  → 2.9.8（37.0 MB）→ 校验和源冲突 → --insecure 通过 → deno --version 2.9.8
pk install pnpm  → 12.18.0（1.1 MB）→ pnpm --version 12.18.0
```

**真网又暴露并修掉 2 个 bug + 1 个上游事实**

1. **npm/pnpm 的 `engines` 里可能有对象**（pnpm 11 的 `engines.runtime` 是对象）→ 索引解析改为先收 `json.RawMessage` 再挑字符串字段
2. **开关位置**（上面 CLI 那条），否则 `--insecure` 会被当成第二个工具名
3. **deno v2.9.8 被上游重新发布过**：npmmirror 与 dl.deno.land 的同名产物 sha256 不同（38,770,795 vs 38,771,689 字节），两边各自与自己那份 `.sha256sum` 一致 → 按 §18.6 判定「源之间不一致」硬失败，报错写明「产物与哪个源一致」并提示 `--insecure`。`--insecure` 也能放行这种冲突（标记 unverified，且**不写进 TOFU 表**，避免把可疑摘要固化）。

**门禁**：覆盖率 internal 85.3–100% / cmd 86.4%；fuzz 三个目标（`FuzzSpec` / `FuzzSHASUMS` / `FuzzArchivePath`）；6 平台交叉编译（7.0–7.8 MB）；验收 17 条全绿。

## M3 交付清单（2026-11-10）

**新增包 `internal/toolchain`**（方案 §4.0 的"非标语义"都收在这里）

- `CheckGoEnv`：`GOTOOLCHAIN` 不是 `local` 就给告警 + 处置（goproxy.cn 确实能提供 golang.org/toolchain，
  所以这条不是形式主义）；`pk doctor` 在装了 go 时自动检查，问题码 `GO_TOOLCHAIN_NOT_LOCAL`
- `LoadPBSIndex`：python-build-standalone 的**两级目录列举**（release 标签目录 → 目录下的文件），
  最新标签缺产物时自动退到上一个有产物的标签；只认 `...-install_only.tar.gz`（排除 `_stripped`/`debug`/`bsdiff`）

**provider 扩展**

- 索引形态 `pbs`（解析委托给 toolchain，provider 不反向依赖，避免成环）
- `index.version_prefix`（go 的 `go1.27.3` → `1.27.3`）
- 产物/校验和模板新增 `{filename}` 与 `{tag}` 占位符（`ArtifactURLsFor` / `ChecksumURLsFor`）
- 校验和形态 `index-inline`（go 的 dl JSON 自带每个产物的 sha256，省掉一次校验和请求）

**真网实测（2026-11-10，镜像档 cn）**

```
pk install go        → 1.27.3（65.0 MB，go.dev 索引 + aliyun 产物可选）→ go version go1.27.3 darwin/arm64
pk install python@3.13 → 3.13.18（24.2 MB，npmmirror pbs 两级列举）→ Python 3.13.18
pk which pip3        → <root>/tools/python/3.13.18/bin/pip3
pk env               → GOTOOLCHAIN=local（去重后只导出一次）+ GOPROXY=https://goproxy.cn
```

**踩到并修掉的两个细节**

1. go 的 `https://go.dev/dl/?mode=json` 默认**只返回 2 条**（最新 + 次新），必须加 `&include=all`（369 条），否则 `pk install go@1.24` 直接找不到
2. `pk env` 原来把 `GOTOOLCHAIN` 导出两次（registry 变量 + provider 的 `toolchain.env`）→ 按键去重，后者覆盖前者

**门禁**：覆盖率 internal 85.6–100% / cmd 86.9%（toolchain 92.6%）；fuzz 三目标；6 平台交叉编译；验收 20 条全绿。

## M4 交付清单（2026-11-10）

**台账与引用计数**

- 台账损坏自动从 `installs.json.bak` 恢复（`LEDGER_RECOVERED` 告警；备��也坏才 `LEDGER_BROKEN`）
- 引用计数落地：**首次安装即设为全局默认**（写 `config.toml` + `_global_` 引用 + shim），之后再装其他版本**不抢** shim；切版本必须 `pk use`
- 卸载默认版本需 `--force`；删掉默认版本后自动切到剩下的最新版本并重指 shim
- `pk use 工具@版本`：配置 + 引用 + shim 三处同步

**GC 与缓存**

- `pk gc [--dry-run] [--cache-max]`：零引用版本回收（被引用的列在 `KeptRef` 里）、缓存按 LRU 压到上限（`state/cache.json` 记 `last_used`）
- `store.ParseSize` 支持 `5GB/500MB/1TB/1073741824`
- 启动清扫：任何写操作前清 `tools/*/*.tmp.*`、`tools/*/*.old`、`cache/archives/*.part`（**超 24h 才删**，避免误删另一个进程正在用的临时目录）

**全局包（方案 §8.5 的 L1/L2/L3）**

- `pk doctor --globals`：`env.ScanGlobals` 扫 `<prefix>/lib/node_modules`（Windows 也认 `<prefix>/node_modules`），
  L1 `GLOBAL_ENGINES_UNMET`（`engines.node` 与当前 node 比）、L2 `GLOBAL_NATIVE_ABI`（`.node`/`gypfile`/`binary` 且非 N-API）、
  L3 `GLOBAL_SHADOWS_PROJECT`（全局命令名 ∩ 项目 `node_modules/.bin`）
- `global_mode = per-version` 可用（`env.SharedVarsFor`），默认仍是 shared

**Ctrl-C / 崩溃**

- 解压目录用 `defer` 兜底清理（取消/panic 都不留半成品）；`.part` 交给启动清扫

**真网实测（2026-11-10）**

```
/tmp/pk-proj 里 pk.toml: node = "22"
pk install            → node 22.23.4 + npm 12.2.2，并写出 pk.lock（含 sha256 与 companion_of）
pk install --frozen   → 直接用锁里的 22.23.4，不做解析（缓存命中）
pk.toml 改成 24 后 --frozen → exit 1「锁的是 22.23.4，不满足 pk.toml 的 "24"」
pk gc --dry-run       → 0 个零引用；1 项保留（node@22.23.4 (global)）
pk use node@22.23.4   → 成功；pk list 显示 * node
doctor --globals      → 造一个 engines.node=">=99" 的全局包 → GLOBAL_ENGINES_UNMET
```

**门禁**：覆盖率 internal 86.7–100%（store 87.0 / install 86.7 / env 92.1）/ cmd 83.7%；验收 29 条全绿。

代码预算：M4 结束时 8477 行；M5 前做了精简（-40 行），M5 新增 ~78 行，最终 **8515 行**
（超出 8500 上限 15 行，用户已明确允许）。

## M5 交付清单（2026-11-10）

**CLI-DONE 10 条（方案 §12.2）逐条状态**

| # | 门禁 | 状态 | 证据 |
|---|---|---|---|
| 1 | 6 工具在"官方源全不可达"下装成功 | ✅ | `TestAcceptance_M5_AllSevenOfficialUnreachable`（做了 7 个：node/npm/pnpm/bun/deno/go/python） |
| 2 | node↔npm 伴生矩阵 7 版本全对 | ✅ | `TestAcceptance_M5_CompanionMatrix7`（含预发布排除、22.21/22.22、24.14/24.15 分界） |
| 3 | 全局包 shared prefix 四用例 | ✅ | `_M4_GlobalSurvivesNodeSwitch` · `_GlobalBinNoReshim` · `_NativeGlobalDetect` · `per-version` 由 `env.SharedVarsFor` 提供 |
| 4 | 故障矩阵 ≥72 格 | ✅ | `TestAcceptance_M1_FailureMatrix`（90 格） |
| 5 | 台账/GC/frozen/Ctrl-C/清扫 | ✅ | M4 的 5 条验收 |
| 6 | `make check` 7 项 + core ≥85% | ✅ | 覆盖率 internal 86.4–100% |
| 7 | 6 平台二进制可下 + npm 一条命令装 | ⚠️ 本地就绪 | `make release`（tar.gz/zip + checksums.txt，命名按 §13.3）· `scripts/install.sh` / `install.ps1` / `packaging/npm` 分包骨架 · `packaging/brew/packnest.rb`。**真正"可下"取决于发布**（gitee/npm 上传需要账号），脚手架已验证 |
| 8 | `--json` schema 冻结为 v1 并锁住 | ✅ | 6 个 golden（list/doctor/version/which/gc/events）+ `docs/json-schema.md` |
| 9 | doctor 对 4 类坏环境给建议 | ✅ | `PATH_ORDER` · `MIRROR_UNREACHABLE`(`--probe`) · `GLOBAL_NATIVE_ABI` · `LEDGER_BROKEN`/`LEDGER_RECOVERED` |
| 10 | 文档齐 | ✅ | `README.md` · `pk help <命令>`（9 个）· `docs/troubleshooting.md`（含与 proto/nvm/mise 共存）· `docs/json-schema.md` |

**本轮新增能力**

- `pk help <命令>` + 每个子命令的 `--help`（`flag.ErrHelp` 视为成功退出）
- `pk env --write/--unset`：rc 里只碰 `# >>> packnest >>>` 托管块（幂等；写前 `.pk.bak`；rc 文件跟随 `$SHELL`）
- `pk mirror write [--file]`：写 `~/.npmrc`（先备份、只改 `registry=` 一行、绝不删别的配置）
- `doctor --probe`：并行 HEAD 探测各工具索引源 → `MIRROR_UNREACHABLE` + 换档建议
- `make release`：6 平台 tar.gz/zip + `checksums.txt`；`scripts/install.sh`/`install.ps1`（`--mirror cn`）；npm 分包与 brew formula 骨架

**精简（用户要求先查）**：删掉死常量（`fetch.DefaultMinThroughput`/`DefaultChunkTimeout`/未用的 `MinThroughput` 字段）、
重复实现（`install.dirSize` → 改用 `store.DirSize`）、测试里的 `var _ =` 占位与 `NPMBundledIfAny` 假方法，
共回收约 40 行。**结构上没有能再砍的大块**：导出函数零死代码、包职责没有交叠。

**预算现状**：实现（不含测试）**8810 行**，超出 8500 上限 310 行（用户 2026-11-10 明确允许"不行的话可以超"）。
超出部分全部来自 M5 收口项：`pk help` 详细帮助（90 行）、`env --write` 托管块（90 行）、`mirror write`（55 行）、
`doctor --probe`（20 行）、golden 契约用例附带的 schema 结构（~40 行）。规模仍在"一个人一个下午读得完"的量级。

## CLI 体验补强（2026-11-10，M5 后）

- 下载进度条：TTY `\r` 刷新（已下/总量/百分比/速度），非 TTY 每 25% 一行，`--quiet` 静默；Ctrl-C 打印"已取消（缓存已保留）"并不留半成品
- `pk list`：加占用空间列；`--available` 真拉索引（24h 缓存、离线可读）列出 latest/lts/近 5 个版本，失败只影响该工具
- `pk env --completion bash|zsh|powershell`：补全脚本（命令面仍是 9 个，挂在 env 下）
- 自托管安装：`install.sh --base <url>` / `install.ps1 -Base` / `PK_INSTALL_BASE`；`make release` 产出的 `dist/`
  含 `latest`、`v<版本>/`、`checksums.txt`、`install.sh`，**整目录丢到任意静态 host 即可**（本机 HTTP server 实测装成功并跑起来）

### 零成本端外分发（2026-11-10 追加）

- `scripts/release-npm.sh`：`dist/` → `dist/npm/`（`@packnest/<os>-<arch>` ×6 + 主包 packnest，共 ~45MB），`--publish` 直接发 npm
- `install.sh --from npm [--registry URL]`：从 registry 取平台包解出二进制（默认 npmmirror）——**不需要 gitee、不需要对象存储**
- 本机用模拟 registry（Python 小服务 + 真实打包产物）端到端验证：下载平台包 → 装到 `--dir` → `pk --version` 正常，`pk`/`packnest` 双入口都在
- 结论：**端外安装可以零成本**（npm 免费 + npmmirror 免费镜像 / gitee 免费 release / 自己有机器当静态源），不需要买 OSS/CDN
- 顺手加：`pk env` 缺省跟随 `$SHELL`、`--rc <路径>`（避免测试或自定义布局去猜 rc 位置）、安装失败给下一步建议

## 已知缺口（按计划补齐，不是遗漏）

- go 的国内索引源：npmmirror 没有 go 的产物/索引（实测），所以索引只声明官方 `?mode=json&include=all`，
  拿不到时走 24h stale 缓存；产物与校验和可以走 aliyun/index-inline，不影响安装。
- python 的索引只声明 npmmirror（官方 GitHub 没有目录列举接口），官方只作为产物/校验和的回退源；
  若镜像索引整体不可用，只能靠 stale 缓存。
- pnpm 12 的 tarball 只是启动器，首次运行会自己再下平台二进制（pnpm 上游行为）。方案原本假设
  「npm registry tarball 即产物」，实际多了第 4 个下载面；M5 收口时决定是否改用 pnpm 自带二进制地址
  （npmmirror 有 `/-/binary/pnpm/`）。
- deno 这类「上游重新发布」的版本目前只能 `--insecure`；自动化需要「同源配对校验」（官方产物 + 官方校验和成对验证、标记 weak），属 v1.1。
- 缓存目前按 `cache/archives/<tool>-<version>-<platform>.<ext>` 命名（校验每次重算 sha256），
  未做方案里的"内容寻址 `<sha256>`"去重；功能等价（跨版本不重下），去重留到 M4 与 GC 一起做。
- npm/pnpm 的 `layout.bin_targets`（`bin/pnpm.cjs` 等）已在清单里声明，落地逻辑随 M1/M2 的解压层实现。
- python 产物名含 release 标签 `{tag}`，`ArtifactURL` 现在会明确报错；M3 的目录列举式索引补齐。
- go 的国内索引源只写了官方 `?mode=json`（自带 sha256），M3 补国内源或改走 stale 缓存。
- `pk env --write`（写 rc 的 `# managed by pk` 块）与 `mirror write`（写 `~/.npmrc`）在 M5。
- L4 检测（`doctor --globals --probe` 用 `process.dlopen` 试加载）与 `doctor --foreign`（其他管理器）在 M5。
- 缓存仍按 `<tool>-<version>-<platform>.<ext>` 命名，未做内容寻址去重（同版本跨平台/跨项目靠文件名命中，够用）。
