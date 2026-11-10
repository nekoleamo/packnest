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

### 开源协议与网络自适应（2026-11-10 追加）

- **协议：MIT**（`LICENSE` 已加，`packaging/npm/package.json` 已是 `"license": "MIT"`）。
  理由：单二进制 CLI、要最大化采用率；Apache-2.0 的专利条款/NOTICE 义务对小工具是纯负担，
  Go 生态（cobra/viper/urfave）基本都是 MIT；我们只有 2 个依赖（semver、toml，均 MIT）。
- **网络自适应**：档位 `auto` 现在有真实语义 —— 装第一个工具时先探官方索引（500ms HEAD），
  可用即走官方（**国外环境不多花一次请求**），不可用回落到国内档；结果写进 `state/probe.json`。
  `pk mirror list` 会显示 auto 的当前判定；`pk install --dry-run` 也能直接看到判定与产物 URL。
  验收：`TestAcceptance_M5_AutoDetect`（官方可用→default 且不探 cn；官方挂→cn；两边挂→报错）。

### 打磨（2026-11-10 追加）

- `pk install --dry-run`：只解析并打印"版本 / 产物 URL / 校验方式"，不下载（CI 里做预检很方便）
- 安装失败追加"下一步"建议（mirror probe / doctor / --insecure）

## 0.7.0（2026-11-10，发布前全量复查）

全面 review + 自动化测试后修掉的问题（每条都补了会失败的用例，先红后绿）：

| # | 问题 | 后果 | 修法 / 用例 |
|---|---|---|---|
| 1 | 校验失败信息让你加 `--force-rehash`，但这个开关**根本不存在** | 用户按建议操作只会得到"flag provided but not defined" | 实现 `install --force-rehash`（忽略 `state/known.json` 的旧记录，重新向校验源求值并覆盖）：`TestVerifyRehashOverwritesTOFU`、`TestVerifyRehashStillRejectsMismatch`、`TestInstallForceRehash`、`TestCLIInstallForceRehashFlag` |
| 2 | `pk env --write` 反复写会**每次多一个空行**（托管块替换后没吃掉结尾换行） | rc 文件被慢慢撑大，"幂等"名不副实 | 替换/删除时吃掉一个换行：`TestWriteManagedBlockIdempotent`、`TestWriteManagedBlockReplacesMiddleBlock`、`TestRemoveManagedBlockLeavesNoBlankLine` |
| 3 | 归档里的符号链接目标**不校验**（先建 `../../x` 链接再写文件 → 写到解压根外） | tar-slip 的链接变体，安全红线 | `checkLinkTarget`：绝对路径/跑出解压根一律拒绝：`TestExtractRejectsEscapingSymlinkTar`、`TestExtractRejectsAbsoluteSymlink`、`TestCheckLinkTargetInTree` |
| 4 | 半截下载被清扫、只剩 `.part.meta` 时，旧的大小记录会让新下载假报"产物大小不符" | chunked（无 Content-Length）源上必现，用户被迫手清缓存 | meta 只在 `.part` 存在时才信；200 分支直接采信本次长度：`TestDownloadIgnoresStalePartMeta` |
| 5 | `gc` 淘汰缓存时，**删不掉的文件也算"已释放"**，且索引条目被删（下次永远不再试） | 报告的数字是假的，磁盘占用不会降 | 先删后记账，失败就保留条目：`TestPruneCacheRemovalFailureNotCounted` |
| 6 | 卸载/GC 之后留下空的 `tools/<tool>/` 空壳 | "卸载零残留"不成立 | `pruneEmptyDirs` 自下而上删空目录（非空即停）：`TestUninstallClean` 补断言 |

顺手清掉的债：`cmd_stub.go`（9 命令全实现后已是死代码）、`provider.SortVersionsDesc` 里的死排序、
`archive.applySingle` 的两个等价分支、`mirror_test` 里没人用的 `writeFile`、`tar.TypeRegA`（Go 1.11 起废弃）、
`verify.Known.Save` 不建父目录（首次写 `state/known.json` 会失败）。

真网实测（本机 macOS arm64，`/tmp/pk-smoke` 为全新 PK_ROOT）：node@22 + 伴生 npm、pnpm、bun、go、python
全部装成并可执行（`node -v` / `npm -v` / `pnpm -v` / `bun --version` / `go version` / `python3 --version`）；
`deno` 仍按设计硬失败（两源校验和不一致 → 提示 `--insecure`，属已知缺口"上游重新发布"）；
`env --write`/`--unset`、`list`、`list --available`、`which`、`use`、`gc --dry-run`、`uninstall`、
`--frozen` 不一致退出码、`doctor --json`、`--json` 事件流全部按契约工作。

## 官网（2026-11-10）

- `site/` 是**零依赖单页**：一个 `index.html`（内联 CSS，无 JS、无外部字体/图片/统计）+ 一个 `favicon.svg`，成品 7.9KB。
- `make site` → `site/dist/`（`scripts/build-site.sh` 只做一件事：把 `__PK_VERSION__` 换成 git tag，回退 `dist/latest`）——
  版本号不写死在 HTML 里，发版后不会静默腐化。
- 部署走 **GitHub Pages**（`.github/workflows/pages.yml`，官方 artifact 流程，不写 `gh-pages` 分支）；
  前置只需在仓库 Settings → Pages 把 Source 选成 **GitHub Actions**。
- 窄屏（≤34rem）表格自动拆成"标签 + 说明"，pre 里的长 URL 折行而不是横向裁切。
- Gitee Pages 已于 2024 年停服（官方口径"维护调整，暂停提供"），国内第二站暂缓；真要国内秒开再谈 EdgeOne Pages 或 OSS + 备案。
- 顺带修 `scripts/install.sh` 的版本探测：原来写死 `gitee.com/api/v5/repos/packnest/packnest/releases/latest`（404，且 gitee API 匿名 401），
  现改为先问 `registry.npmmirror.com/packnest/latest`（公开、无需凭据），再回退官方 registry，最后才试 gitee——`curl … | sh` 这条主路径现在真能用。

## 0.7.5（2026-11-10）

`pk list <工具>` 版本多时的排版（用户："--all 列表仍不够美观"）：

- **两种排版**：≤25 个版本仍然一版一行（右对齐 + `latest` / `lts: <代号>` / `已装` 标签，
  且同一 LTS 代号只标一次）；更多时自动切换成**按大版本分组 + 多列网格**——
  参考 `nvm ls-remote` / `pyenv install --list` 的"几百行没法看"困境，以及 `ls -C` /
  `proto versions` 的分组表格思路
- 网格列数按**终端宽度**算（`COLUMNS`，否则 ioctl `TIOCGWINSZ` / Windows
  `GetConsoleScreenBufferInfo`），窄窗口 4 列、宽窗口 8 列自动适配
- 网格里用单字符标记保持列对齐：`*` 最新 · `l` LTS · `+` 已装 · `@` 当前默认（带图例）
- 新增 `--major N` 只看某个大版本（0.x 按 major.minor，如 `--major 0.10`），默认列该大版本全部；
  Node 的 `0.1/0.10/0.12` 这种半代差异也分得开

## 0.7.4（2026-11-10）

Windows 上第三个连环坑：`Restricted` 执行策略拒绝加载未签名 profile → 每个新窗口都报
"无法加载文件 …Microsoft.PowerShell_profile.ps1，因为在此系统上禁止运行脚本"。

- Windows 的 PATH 改走**注册表用户 Path**（`HKCU\Environment\Path`，用 `syscall` 直接读写注册表，
  不引第三方库）：不受执行策略影响，cmd / PowerShell / Git Bash 都继承，`node` 立刻可用；
  写前把旧值备份到 `<PK_ROOT>\state\user-path.bak.txt`，`--unset` 可撤销，改动后广播
  `WM_SETTINGCHANGE` 让新窗口拿到新值
- `pk env --write` 先看 `Get-ExecutionPolicy`：`Restricted`/`AllSigned` 时**不写 profile**
  （写了只会每次启动报错），并清掉之前写进去的块；其余 registry/GOPROXY 等变量给出两条出路
  （放开 CurrentUser 策略 / 每次会话 `pk env --shell powershell | Invoke-Expression`）
- 新增 `internal/env/winpath_windows.go`（注册表读写，纯 `syscall`）与 `winpath.go`
  （`splitPathList`/`samePath` 纯函数，跨平台可测）

## 0.7.3（2026-11-10）

Windows 上「`pk env --write` 之后新开 shell 仍不认 node」的两个根因：

1. **写错了文件**：PowerShell 的 profile 有两套且互不相干 —— 系统自带的 5.1 读
   `Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1`，pwsh 7 读 `Documents\PowerShell\…`。
   旧代码只写 7 那一份（还写死 `Documents`，不认 OneDrive 重定向）。现在
   `env.WindowsProfilePaths` 按 `POWERSHELL_DISTRIBUTION_CHANNEL` / `PSModulePath` 判断版本，
   判不出来就**两份都写**，并在 OneDrive 重定向时写 OneDrive 那份。
2. **写错了语法**：Windows 上没有 `$SHELL`，旧 `detectShell()` 一律回落 `bash`，于是把
   `export PATH=…` 写进了 `~/.bashrc` —— Windows 上没有任何东西会读它。现在
   `detectShellFor` 在 Windows 且无 `$SHELL` 时返回 powershell（Git Bash 有 `$SHELL`，仍按 bash）。

`pk env --write` 现在会列出实际写入的每个 profile，并在 PowerShell 下提示用 `$PROFILE` 核对。
用例：`TestWindowsProfilePaths`、`TestWindowsProfilePathsOneDrive`（`internal/env/env_profile_test.go`，
注意不能叫 `*_windows_test.go` —— 那会被当成 GOOS 构建约束）与 `TestDetectShellFor`、`TestEnvWriteTargets`。

## 0.7.2（2026-11-10）

版本文本列表改成可扫读的形态（用户反馈"全部版本展示凌乱"）：

- `pk list <工具>` 的版本区改成**一版一行、升序、最新在最下面**（同 nvm ls-remote / pyenv
  install --list / fnm ls-remote 的约定），版本号右对齐成列，右侧是标签列：
  `latest` / `lts: <代号>` / `已装` / `已装 · 当前默认`
- 默认只列最近 20 个并在末尾提示"前面还有 N 个"；`--limit N` 自定义，`--all` 等于 `--limit 0`
- `--json` 同步带上 `installed` / `lts` 字段，GUI 与脚本不用再解析文本

## 0.7.1（2026-11-10）

Windows 实测反馈驱动的三条修复（详见下面「list 的三个问题」）：

- `pk list --available` 在 Windows 上一直不返回：索引拉取没有总超时，而 npm/pnpm 的 registry
  **完整元数据**是 25MB/16MB → 慢源上 `io.ReadAll` 挂死（Ctrl-C 后才吐出 `context canceled`）。
  现在索引请求带 45s 总超时，registry 改用 **abbreviated 元数据**（同一份 2.3MB，字段齐全），
  冷缓存实测 npm 1.2s、pnpm 0.35s、python 0.3s。
- `list --available` 不再在有/无已装工具时都喊"还没装任何工具，pk install node@22 开始"或打印空的「已装:」。
- 新增 `pk list <工具>`：已装版本 + 该工具**全部**可装版本（`✓` 已装、`*` 当前默认、括号里 LTS 代号、
  默认列最近 20 个、`--all` 不截断、`--json` 结构化）。

## 渠道审计（2026-11-10，0.7.0 发布后）

用户 `brew install` 报 `reports different checksum` 触发的一轮全面检查：

| 渠道 | 发现问题 | 处置 |
|---|---|---|
| Homebrew tap | formula 的 sha256 来自**另一次构建**（tar/gzip 写入 mtime，同版本重建就变），与 gitee 上的产物对不上 | 从 `dist/checksums.txt`（已与线上产物逐条核对）重推 tap；`brew install` 实测通过，装出的二进制与发布产物 sha256 一致 |
| Scoop bucket | 同样问题，两个架构的 hash 都不对；另外 `autoupdate` 的 URL 把 `v0.7.0` 钉死了（升版本后会 404） | 重推 manifest（hash 已对）；`gen-manifests.sh` 模板改成 `v$version` |
| winget | 清单 hash 与线上 zip 一致 | 无需改（仍是草稿，等提交到 winget-pkgs） |
| npm（官方 + npmmirror） | 7 个包的 `dist.integrity`/`dist.shasum` 两侧完全一致；`npm i -g` 两个源各实测一次 | 无需改 |
| curl 一行（gitee） | **完全不校验哈希**就装 | 安装前拉 `checksums.txt` 对 sha256，不符退出码 4；`--skip-verify` 才能放行 |
| `--from npm` / `--mirror cn` | 同样不校验 | 对 registry 的 `dist.shasum` 校验 sha1 |
| 自托管 `--base` | 同样不校验 | 对 `<base>/v<ver>/checksums.txt` 校验 sha256 |
| install.ps1 | ① 版本探测写死 `gitee.com/api/v5/repos/packnest/packnest`（404，且匿名 401）→ 默认通道直接装不上；② `-Mirror cn` 指向 `npmmirror /-/binary/packnest/...`（该路径不存在，404）；③ 不校验哈希；④ **zip 解到临时根目录 → 与"待安装 exe"同路径 → 最后一步变成自我复制**（用户实测 Windows ARM64：`Copy-Item : 无法使用项 … 其自身覆盖该项`）；⑤ 中文消息经 `iwr \| iex` 按控制台代码页解码 → GBK 控制台下全是 `?` | 版本改为 npm registry 优先；`-Mirror cn` 改走 npm 平台包（`@packnest/win32-<arch>` + registry shasum 校验）；gitee/base 通道校验 `checksums.txt` sha256；解压改到独立子目录（zip → `$tmp\unz`，npm → `$tmp\npmpkg`）；**面向用户的输出全部改成 ASCII**（注释仍是中文）；新增 `-SkipVerify` 与 `PK_SKIP_VERIFY` / `PK_VERSION` / `PK_INSTALL_DIR` 环境变量；`LOCALAPPDATA`/`TEMP` 缺失时有兜底 |

install.ps1 的验证方式：`brew install --cask powershell` 需要 sudo，改为直接下载 PowerShell 7.6.9 的
osx-arm64 自包含包，用真 `pwsh` 跑五个分支（gitee / `-Mirror cn` / 篡改产物 / `-SkipVerify` / `-DryRun`），
全部符合预期——其中篡改分支实测抛 `SHA256 mismatch` 且不落任何文件。

根因与硬化（防止再犯）：
- `make release` 现在**可重复**：打包前 `touch -t $(SOURCE_DATE)`，tar 走 `tar -cf - | gzip -n`，zip 用 `-X`；
  连跑两次构建 checksums 完全一致（已验证）。重发同版本不会再让清单与产物错位。
- `scripts/verify-release.sh` + `make verify-release`：把已发布产物拉回来逐条对 `checksums.txt`。
- `make publish-manifests` = `verify-release` → `gen-manifests.sh` → `release-taps.sh`，
  "清单不能指向没验证过的 sha256" 变成 make 的依赖关系。
- 顺手修了安装脚本里 `$VAR` 紧贴中文的写法（C locale 下 bash 会把多字节字符当成变量名的一部分，
  报 `unbound variable`），全部改成 `${VAR}`；安装脚本已在 `LC_ALL=C` 下实测。

## 行数预算现状

`cmd/` + `internal/`（不含 `*_test.go`、不含 `internal/testkit` 夹具）**8465 / 8500 行**，余量 35 行。
注意：这个数字现在靠人肉看，`make check` 的 7 项门禁里**没有行数门禁**（AGENTS.md 里的说法超前于实现）。
下个版本要么加一条 `loc` 门禁，要么先还砍一刀（`internal/install` 1281 行、`cmd/pk` 2189 行是两个大头）。

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
