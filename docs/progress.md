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

## 2026-11-11 · npm 分发渠道停用

- 触发原因：`npm stage approve` 需要 2FA，`www.npmjs.com` 的浏览器批准页大陆直连打不开
  （实测经代理也被 Cloudflare 拦 curl → 403；`registry.npmjs.org` 才通），而 stage 的
  `--otp` 又需要本地 TOTP 密钥。
- 更根本的原因：云端与本机构建出的归档字节不同，两边都发布会出现两套 sha256（0.8.0 / 0.9.0 各踩一次）。
- 处置：删 `scripts/release-npm.sh`、`scripts/npm-approve.sh`、`scripts/set-npm-token.sh`、
  `.github/workflows/npm-publish.yml`（换成一个只做代码镜像的 `gitee-mirror.yml`）、Makefile 的
  `npm-packages / npm-dry / publish / publish-stage / promote / publish-direct` 目标；
  `install.sh` / `install.ps1` 去掉 npm 通道（`--from npm` 报错指向 gitee/base，`-Mirror cn` 退化为 gitee，
  gitee 下载失败不再自动退 npm 平台包）；README / docs / 官网同步（渠道矩阵 6 → 5）。
- 保留：已经用 `npm i -g packnest` 装过的旧版本照常可用，`pk uninstall --all` 仍识别 npm 渠道
  （`cmd/pk/uninstall_all.go` 那段不许删）；命令行工具 npm 本身（node 伴生）完全不受影响。
- 0.9.0 的 7 个 staged 包会在 npm 侧自然过期（~7 天），不需要清理。

## 0.9.0（2026-11-11）

- **外部 provider 清单**：`$PK_ROOT/providers/*.toml` + `PK_PROVIDERS_DIR`（追加、多目录）；内嵌永远赢；
  六条安全边界（https 默认、路径不许绝对/`..`、禁 pbs、未知占位符、伴生 registry 必须存在、≤256KB）；
  坏清单只记 issue，不影响 CLI；`pk list` 标 `*` + `--json` 的 `source`；doctor 新码
  `PROVIDER_EXTERNAL_INVALID` / `_CONFLICT` / `PROVIDER_HTTP_URL` / `PROVIDERS_DIRS`
- **corepack（PATH 4 → 5 段）**：`global/corepack/bin` 独立一段（provider 的 pnpm 永远优先）；
  `COREPACK_HOME` + `COREPACK_NPM_REGISTRY`；`pk install node --corepack|--no-corepack`；
  node 25+ 明确报错
- **doctor**：`GO_INSTALL_NOT_IN_PATH`（不设 GOBIN）、`PIP_USER_SITE_UNMANAGED`、
  `--globals` 覆盖 pnpm/bun 的全局目录（`detail.manager` 标来源）
- **门禁**：`make check` 8 项（`make loc` 行数门禁上线，红线 8,500 → 10,000 → 10,200 → 10,300）
- **发布事故与规则**：本机与 GitHub Actions 同时发布会产出两套 sha256（构建字节随机器不同），
  0.9.0 已按"唯一发布者（本机）"修复，CI 的 publish/taps job 改为 `workflow_dispatch` 才跑

## 0.8.0（2026-11-10）

**移除 deno**（上游进入生命末期，不再值得为一个单文件产物维护一整条 provider 链路）。

删掉的东西：
- `providers/deno.toml`（内嵌清单从 7 个降到 6 个：node / npm / pnpm / bun / go / python）
- `DENO_INSTALL_ROOT` / `DENO_DIR` 两个环境变量（`pk env` 输出里不再有）
- 文档/站点/清单里的 deno 行与"7 种运行时"（改成 6）

**保留的东西**（有意为之，不是漏删）：
- `layout.single` 这条解压能力 + 它的测试。真实 provider 已经没人用，但代码只有几十行、
  逻辑独立，以后加 zig/nushell 这类"单文件产物"能直接复用。回归改由合成清单
  `singlefile`（testkit）守着：`TestAcceptance_M2_SingleFileLayout` 名字不变，主体换成它。
- `per-artifact` 校验和格式（`<产物>.sha256sum`）同样保留，同样是通用能力。
- `docs/progress.md` 里的历史记录（0.6–0.7 的 deno 交付与踩坑）**不改**：那是当时的真实记录。

验收用例调整（按"验收项 = 测试名"的约定，改名需留痕）：
- `M5_AllSevenOfficialUnreachable` → **`M5_AllProvidersOfficialUnreachable`**（工具数从 7 变 6，
  名字里的 "Seven" 会变成假话；断言改为"所有 provider"）
- `M2_ThreePlatformSmoke` / `M2_OfficialUnreachable` / `M0_ProviderLoad`：去掉 deno 断言，工具数同步

旧用户不受影响：台账里如果还有 deno 记录，`pk list` 照常显示（provider 查不到就少两列），
`pk uninstall deno` / `pk uninstall --all` 仍然能把它删干净。

## 0.7.9（2026-11-10）

Windows `--self` 仍然删不掉（0.7.8 实测 `where.exe pk` 还在）。根因是 **Go 给 cmd.exe 拼参数**：
把 `ping … & del "路径" & rd "路径"` 整串当一个参数传给 `exec.Command("cmd.exe", "/c", …)`，
Go 按 Windows 规则加引号并把内部引号转义成 `\"…\"`，而 cmd.exe 不认反斜杠转义 —— 命令行被拼坏，
cmd.exe 什么也没执行（错误还被 `>nul` 吞掉）。

- 改为把要执行的命令写进 `%TEMP%\packnest-self-remove-*.cmd`，只把**文件路径**交给 cmd.exe
- 脚本内容抽成纯函数 `selfRemoveScript`（可在任意平台单测）：只 `del` 自己的两个 exe，
  再 `rd`（不带 /s）尝试删空目录，最后自删脚本，并往
  `%TEMP%\packnest-self-remove.log` 写一行 REMOVED/FAILED 便于事后核对
- 用 `CREATE_NO_WINDOW` 起子进程（比 DETACHED_PROCESS 更稳，cmd.exe 需要控制台）

## 0.7.8（2026-11-10）

Windows 实测 `pk uninstall --all --yes --purge --self` 之后的两个残留：

1. **`pk.exe` 还在**：Windows 不允许删运行中的程序，旧实现的 `--self` 只是打印一句话（等于没删）。
   现在改为**安排分离进程延时删除**：隐藏的 `cmd.exe` 等约 3 秒后删 `pk.exe` / `packnest.exe`，
   目录空了再移除；**只删这两个文件，不 `rd /s` 整个目录**（`-Dir` 可能指到共享目录）。
2. **用户 Path 里留死项**：`install.ps1` 加的是"安装目录"，而 `pk env --write` 加的是
   `~/.packnest` 的四段目录，两者不是一回事；旧版 `--unset` 只清后者。现在 `uninstall --all`
   会把 `state/install.json` 里记录的安装目录也一并从注册表用户 Path 摘掉。

顺带修 `install.sh --from npm --version X` 一直装 latest 的老毛病（按版本取 tarball，取不到明确报错）。

## 0.7.7（2026-11-10）

卸载体验补齐两件事：

**① `pk uninstall <工具>`（不带版本）= 删该工具的全部版本**
原来必须写精确版本（`pk uninstall go@1.27.3`），删一个装了多版本的工具得反复敲。
现在先**逐条列出会被删掉的版本**（`*` 标当前全局默认）再二次确认：
`--yes` 直接过；交互终端问 y/N；非交互（管道/CI）没有 `--yes` 一律不动 —— 用 ioctl/GetConsoleMode
判断 stdin 是不是终端（`/dev/null` 也是字符设备，只看 `ModeCharDevice` 会误判）。
`npm` 单独卸载会提示"它是 node 的伴生包"。`--all` 也换成同一套确认逻辑。

**② curl / iwr 装的 `pk` 自己也能一条命令删掉**
两个安装脚本现在会写 `state/install.json`（channel / platform / version / dir），
`pk uninstall --all --yes --purge --self` 就能连二进制一起删：unix 上直接 unlink 正在运行的文件
（合法）并收掉空目录；Windows 不允许删运行中的 exe，改为打印那条 `Remove-Item`。
`--all` 的收尾提示也会用记录下来的真实目录，而不是猜 `~/.local/bin`。

用例：`TestUninstallToolAllVersions`（引用拦截 / force / 空集合）、`TestUninstallAllSelf`
（用 install.json 的目录）、`TestInstallInfoRoundTrip`，覆盖率回到 install 87.0% / store 87.7%。

## 0.7.6（2026-11-10）

「怎么彻底卸载」原来只有零散知识（`pk uninstall <工具>@<版本>` + `pk env --unset` + 各渠道各自的删法），
没有一个动作能把 pk 装过的东西一次清掉。补齐：

| 层 | 原来 | 现在 |
|---|---|---|
| 运行时 | `pk uninstall <工具>@<版本>` 逐个删 | `pk uninstall --all`（引用关系不再拦路，显式全清） |
| shim | 随单个卸载清理 | 一起清，连 `pk`/`packnest` 自入口 |
| shell rc / Windows 用户 Path | `pk env --unset` | `--all` 里自动带上（含注册表 Path 四段） |
| 缓存/台账/配置 | 无 | `--purge`（连根删）、`--keep-cache`（只留缓存） |
| pk 二进制本身 | 无提示 | 按可执行文件路径**识别渠道**（brew / npm / scoop / 脚本 / 手动；winget 渠道已于 2026-11-11 放弃），
给出确切命令，如 `brew uninstall packnest && brew untap nekoleamo/packnest` |

- 不带 `--yes` 只打印计划（删了什么、保留什么、pk 自己怎么删），不动磁盘；
- `--json` 输出 `{removed[], purged, root, channel, self_hint, failures[]}`，且**不再混入逐条 uninstall 事件**；
- 渠道判断是纯函数 `detectInstallChannel`，跨平台（手工归一化 `\\`，因为 `filepath.ToSlash` 在非 Windows 上是空操作）；
- 用例：`TestDetectInstallChannel`（10 条路径）、`TestUninstallAll`（rc 块被清、台账清空、缓存保留）、
  `TestUninstallAllPurge`（`--purge` 与 `--purge --keep-cache` 两种）

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

## 下一步（新会话从这里接）

### 已完成（截至 2026-11-13，v1.5.0）

| 线 | 状态 |
|---|---|
| CLI | **v1.5.0 已发（2026-11-13）**：gitee + GitHub Releases + brew tap + scoop；6 平台产物、`--json` 契约冻结（schema 1）、**10 个命令**；1.5 新增 `PK_CACHE` + 读 `package.json` 的 `engines.node`，并把 `auto` 探测、索引传输、`list` 大小三处延迟修掉 |
| 门禁 | `make check` 8 项全绿（含 `make loc`），**并已上云**（`.github/workflows/ci.yml`：同 8 项 + GUI 契约测试）；`make accept` 37 项；smoke CI 5/5；staticcheck/vet 干净 |
| 行数 | **11,584 / 11,600**（余量 16）；`cmd/pk` 3,169 / 3,200；单文件无 >600 行（`cmd_list.go` 495） |
| GUI | `cmd/pk-desktop`（Wails v3.1.0 + Vue3/TS/Vite，**独立模块、不 import internal/***），子进程调 `pk --json`；托盘 + 单实例 + `--serve` 浏览器形态；mac `.app` 只发 arm64（intel 2026-11-12 放弃；图标就绪，ad-hoc 签名，公证不做） |
| GUI 发布 | 独立 tag **`gui-v0.11.0`**（gitee + GitHub 各 3 产物：mac arm64 `.app.zip`、win amd64 `.zip`、linux amd64 `.tar.gz`）；mac intel 已放弃，`gui-v0.10.1` 是补差异页之前那版 |
| 发布流程 | 唯一清单在 `docs/publishing.md`（「三、发布流程」第 ⑦ 步 + 「五点五、GUI 的产物」） |

### 接下来可做的（1.3 已落地，待发版）

1. **2 项人工确认**（唯一挡在「全部自动化跑通」前面的）：Windows ARM64 运行时、macOS Gatekeeper 首启 ——
   步骤与通过标准在 `docs/verify-manual.md`，缺的是对应硬件/真人操作（`.cmd` shim 已在 1.4 自动化进 smoke）
2. **已无阻塞项**：1.1/1.2/1.3 的可选增强与偿债全部完成；`v1.3.1` 已发（2026-11-13）
3. **下一轮候选**在 `~/Documents/Plan/17-包管理器/pn-1.3-候选清单.md` 的“不做”一栏（`pk upgrade` 独立命令、自更新器、daemon）

### 1.2 已完成（2026-11-12，v1.2.0）

- **多连接下载**：`pk install --jobs N` / `PK_JOBS=N`，分片走**独立 HTTP/1.1 连接**（HTTP/2 下所有分片共用一条 TCP、
  受同一套流控，实测反而更慢 —— 这是第一版没收益的原因）；只在「冷启动 + 源支持 Range + ≥8MB」时启用，
  任一异常整体退回单流；有 `.part` 续传现场时不走分片。实测 48MB node 包：单流 1.46s → 4 段 0.93s → 8 段 0.84s。
  用例：`TestParallelDownloadSplits` / `TestParallelFallsBackWithoutRange` / `TestParallelSkipsWhenResuming`；
  真网基准 `TestParallelDownloadRealWorld`（`-tags nettest`，不进门禁）。
- **doctor 三个新问题码**：`ROOT_ON_SYNC_DIR`（PK_ROOT 在 OneDrive/Dropbox/iCloud/坚果云 → 锁与台账不可靠）、
  `LEDGER_SCHEMA_NEWER`（旧版 pk 打开新台账）、`SHIM_ALIAS_MISMATCH`（pk 与 packnest 指向不同文件）。
  用例：`TestOnSyncDir`、`TestDoctorNewCodes_1_2`。
- **外部清单字段级文档**：新增 `docs/providers.md`（每个字段的合法值、四种索引形态、校验格式、伴生包、占位符、排错），
  README 与官网都指向它。
- 行数红线 10,700 → **11,100**（本轮实测驱动）；落地后 10,993 / 11,100。
- 发布：tag **v1.2.0**（gitee + GitHub + brew tap + scoop 同步，verify-release 6 产物 sha256 全对，
  smoke 5/5 装的都是真 1.2.0，`install.sh --dir` 端到端装出 1.2.0 并跑通 install/doctor/uninstall 零残留）。
- 顺手修：`smoke.yml` 的版本号改成先查**线上最新 release**再回退 `scripts/latest` —— 否则"刚发完版但还没推
  scripts/latest"时会装到上一个版本（本轮连踩两次，第一次 smoke 装的是 1.1.0）。

### 1.4 已完成（2026-11-13，v1.4.0 已发 + GUI gui-v0.11.0）

- **索引并行拉取**：`internal/provider` 新增 `IndexLoader.LoadAll`（并发加载多工具索引、结果顺序与入参一致、
  单个失败只写自己的 `Err`），`pk list --available` 改用它。实测 6 工具冷缓存 `--refresh`：
  串行 4.8s → 并行 **2.9s**（1.6×；瓶颈是 go 索引 `go.dev` 单次 2.0s，其余 0.15~0.52s）。
  单测 `TestLoadAllConcurrent`（6×80ms 慢源，断言最大并发 ≥2、总耗时 <400ms、顺序稳定、单项失败不影响其他）。
- **Windows `.cmd` shim 自动化**：`smoke.yml` 加 Windows 专属 `pwsh` 步骤，真 PowerShell 调
  `pk.cmd` / `packnest.cmd` / `node.cmd` / `npm.cmd` 并断言输出，再零残留卸载。
  首次运行即通过（`packnest v1.3.1 (windows/amd64)`、`node.cmd -v -> v22.23.5`、`npm.cmd -v -> 12.2.2`），
  人工确认 3 项 → **2 项**（`docs/verify-manual.md` 同步）。
- **`pk upgrade`（第 10 个命令）**：`pk upgrade [工具…] [--dry-run] [--json]` —— 把已装工具升到索引最新、
  把全局默认切过去、**旧版本保留**（清理交给 `pk gc --keep N`）；npm 明确跳过（伴生包按 node 精确版本配对，
  单独升会配错 `engines`）；未安装的工具只给一行指路。编排在 `internal/install/upgrade.go`，cmd 只做参数与输出。
  验收 `TestAcceptance_M7_Upgrade`（dry-run 不落盘 / 切默认 / 旧版本留着 / 二次报"已是最新" / 未装工具不报错）、
  `TestAcceptance_M7_UpgradeCLI`（`--json` 形状 + 文本输出）。`pk list` 的可升级提示同步改成 `pk upgrade <工具>`。
- 行数红线 11,200 → **11,300** → **11,450**（1.4 两项 + 新命令的实测驱动，落地 11,427）。
- **顺带修一个自己刚拖出来的事故**：发完 `gui-v0.10.4` 后 `releases/latest` 指向了 `gui-v0.10.4`，
  `scripts/install.sh` 拼出 `…/download/vgui-v0.10.4/…` —— **所有新用户的一行安装 404**，
  `smoke` 全矩阵也红。三处修法一致：只认 `^v\d+\.\d+\.\d+$` 的 tag，否则退回 `scripts/latest`
  （`scripts/install.sh`、`scripts/install.ps1`、`.github/workflows/smoke.yml`）；`docs/publishing.md` 记了这条坑。

### 1.4 已完成（GUI 0.11.0，2026-11-13）

- **中英双语界面**：`cmd/pk-desktop/frontend/src/i18n.ts` —— 中文是源串、英文查表，缺译文自动退回中文
  （不会出现 `missing.key`）；顶栏 `中文 / EN` 切换，默认跟 `navigator.language`，选择存 localStorage。
  CLI 的输出（体检问题、安装日志）仍是中文：`--json` 才是机器接口，人类文案只维护一份，避免两边漂移。
- **开机自启**：`cmd/pk-desktop/internal/autostart`（macOS LaunchAgent / Windows `HKCU\…\Run` / Linux XDG autostart），
  默认关、只写用户级条目、关闭只删自己那条；`/api/autostart` GET 读状态、POST 开关；设置页一个开关 + 条目路径。
  单测 `TestEnableDisableWritesUserLevelEntry`（HOME 沙箱：默认关 / 打开写文件 / 关闭删文件 / 重复关闭幂等）。
  实测：打开后 `~/Library/LaunchAgents/packnest-desktop.plist` 出现，关闭后消失。
- **go 索引换官方 CN 镜像**（1.4 #11）：`providers/go.toml` 的 `[index.urls]` 加 `cn = golang.google.cn/...`，
  cn 档拿索引从 2.0s 降到 **1.35s**，go.dev 留作回退；`TestGoIndexHasCNMirror` 锁住顺序（cn 先、default 先官方）。
- 用过 headless Chrome 分别以 `zh / en` 渲染自查（`--lang=en-US` 触发英文）：主面板、按钮、对话框、GC 分组标题都跟语言走。

### 1.3 已完成（2026-11-13，v1.3.1 已发）

- **多连接默认自适应**：`Jobs = 0` 为默认（每段约 8MB → 2~8 段），`--jobs 1` 强制单流、`--jobs N` 固定段数，
  `PK_JOBS` 改默认；续传现场 / 不支持 Range / 任一异常仍整体退回单流。
  验收 `TestAcceptance_M6_JobsAuto`（9MB 源默认分片，`--jobs 1` 零 Range 请求）。
- **`pk list` 可升级提示**：已装版本落后索引最新时给一行 `node 22.23.4 → 26.13.0  pk install node@latest`，
  `--json` 多一个 `outdated[].{name,installed,latest,advice}`。**只读 24h 索引缓存（离线的假客户端），
  没缓存就不提示** —— `pk list` 仍然是不联网命令。验收 `TestAcceptance_M6_ListOutdated`。
- **`pk doctor --fix`**：只修能安全自愈的两类（PATH 托管块 → 复用 `pk env --write`；pk/packnest 入口 → 重建），
  其余问题（锁、镜像、台账）绝不自动改；`--json` 加 `fixed[]`。内部调 `cmdEnv` 时把输出重定向到 `io.Discard`，
  避免污染 doctor 的 JSON 流。验收 `TestAcceptance_M6_DoctorFix`（含幂等：托管块只有一段）。
- **`pk gc --keep N`**：每个工具保留最新 N 个版本（按语义化版本排序，不是字符串比较），其余零引用的才回收；
  `GCResult` 加 `KeptRecent`，`--json` 加 `keep`。验收 `TestAcceptance_M6_GCKeep`。
- **两条 `shortcut:` 偿债**：
  ① 索引版本号的预发布识别不再只认 rc/beta/alpha/pre/next/canary/dev —— 改成「版本后第一段不在平台词表
  （darwin/linux/win*/arm64/amd64/x86_64/aarch64/musl…）里」才算预发布；
  ② Windows 全局锁从 `O_CREATE|O_EXCL`（非内核锁，且 1 小时清理会误删活锁）换成 `LockFileEx`（std `syscall`
  的 `NewLazyDLL` 直接取 kernel32，不引 x/sys），拿不到立刻返回 `ErrLocked`（退出码 5）。
- **行数红线 11,100 → 11,200**（1.3 实测 +157 行：多连接 15 / list 提示 55 / doctor --fix 42 / gc --keep 30 / 偿债 15）；
  落地后 11,150 / 11,200。
- **GUI 跟齐 1.3（`gui-v0.10.3`）**：已装列表顶部「可升级」行（`outdated[]`，点「升级」= `pk install <tool>@latest`，
  单行也有 `→ 新版本` 徽标）、体检页「一键修复」按钮（`pk doctor --fix`，修完把 `fixed[]` 逐条显示）、
  回收页「每工具保留 N 个」输入框（`pk gc --keep N`，结果里多一块"按保留数留下"）。`pkcli` 三个方法随之改签名
  （`List` 返回 `{Tools, Outdated}`、`Doctor(…, fix)`、`GC(…, keep)`），契约测试补了 `outdated[]` / `fixed[]` / `keep` 的解析断言。
- **`gui-v0.10.4`：没装 CLI 时给对应平台的安装命令**。`/api/status` 多一个 `platform`（`runtime.GOOS`），
  前端据此在顶栏给 Windows 的 `iwr … install.ps1 -UseB | iex` 或其余平台的 `curl … install.sh | sh`，带「复制」按钮
  （之前固定塞 `sh` 命令，Windows 用户照着敲会失败）；`status.reason` 为"找不到 pk"时不再重复显示，主界面保持隐藏。
- 发布 `gui-v0.10.3` 顺手修的两个发布链路坑（已写进 `docs/publishing.md`）：① `gui-build.yml` 用
  `scripts/latest`（CLI 版本）给 GUI 产物命名 → CI 产物成 `packnest-desktop_1.3.1_*`，`release-gui.sh`
  找不到；改成取 `guiVersion`。② `release-gui.sh` 把不存在的通配符写进 `gh release upload`，每次都失败却
  仍打印"已上传" → GitHub release 零附件；改成只传 `ls` 出来的真实文件、失败退出 5。
- **桌面端补齐用户向材料**：新增 `docs/gui.md`（下载/首次放行/浏览器形态/界面↔CLI 对照/故障排查/卸载）与
  `docs/en/gui.md`；README 与 README.en 各加「桌面端（可选）」一节（表格列出每个界面背后的 `pk` 命令）；
  官网中英文各加 `#gui` 段（定位、能做什么、下载三平台产物、前置条件、不是什么）+ 导航/页脚入口。
  GUI 版本号由 `scripts/build-site.sh` 从 `cmd/pk-desktop/main.go` 注入（`__GUI_VERSION__`），与 CLI 版本解耦。
- 同步：`docs/cli.md`（`--jobs`/`--keep`/`--fix`/可升级提示）、`docs/json-schema.md`（`outdated`/`fixed`/`KeptRecent`/`keep`）、
  README、官网中英文、`--json` golden 三份。
- 发布：tag **v1.3.1**（gitee + GitHub + brew tap + scoop + 官网同批）；`verify-release` 6 产物 sha256 全对。
  （v1.3.0 发完 8 分钟内被 smoke 的 windows-latest 抓到回归：`uninstall --all` 删 PK_ROOT 时
  `state/lock` 被本进程 handle 占住 → 报“零残留失败”、退出 1。修法：锁文件改成
  `CreateFile` + `FILE_SHARE_DELETE` 打开，锁本身仍是 `LockFileEx`。）
- 发布链路顺手补的两个坑：① `scripts/release-github.sh` 建的 release 若是 draft，
  `/releases/latest` 看不到它 → smoke 会退而装**上一个版本**（1.3.1 就装了 1.3.0），
  现在建完强制 `--draft=false`；② 删远端 tag 会连带 gitee release 的附件失效（全 404），
  必须重跑 `sh scripts/release-gitee.sh` 重传。
- 1.3.1 验收：smoke 5/5 全绿且日志里是 `packnest v1.3.1`（5 个平台）；`brew upgrade packnest`
  从 tap 装到 1.3.1、`packnest --version` 正确；官网中英文 `class="ver"` 都是 v1.3.1。
  官网 section 标题同时从 0.8rem 的纯大写小标签提到 1.4rem，补上缺失的中等字阶（impeccable 的
  `flat-type-hierarchy` 之前对整站命中）。
- 官网第二处设计债：数字条（`.stats`）的竖分隔线原来用 `border-left`，impeccable 的
  `cramped-padding` 一律判「文本离边框不足 8px」。排查结论：**规则读外部样式表时会丢掉 padding**
  （同一份 CSS 内联进页面就不报，`--no-config` 也一样），不是 padding 真的不够。改成
  1px 绝对定位伪元素当分隔线（`.stats li + li::before`），视觉一致、不参与盒模型，`detect site` 归零；
  忽略规则也一并撤掉（不留静音）。

### 1.1 已完成（2026-11-12，v1.1.0 已发 + GUI gui-v0.10.2）

- **卸载编排搬进 `internal/install`**（技术债清账）：`PlanUninstallAll`（只汇总不碰盘）/ `ApplyUninstallAll`
  （删运行时 + 自入口 shim + purge/keepCache），cmd 只留打印、确认、`--self` 与 env 托管块；
  单测 `TestUninstallAllPlanIsReadOnly` / `TestUninstallAllApply` / `TestUninstallAllPurge`；
  cmd/pk 3,136 → **3,086**，总计 10,627 → 10,683（余量 17）。

- **通用伴生包**：`[companions.<名>]` 不再只认 node→npm，`bundled|latest-compatible|pinned` 三种语义都生效，
  装进宿主 `<target>`（跟着宿主卸载/回收，不单独进台账）；`pinned` 必须给 `version`（缺失即清单非法）。
- **同源配对校验**：跨源分裂（同 host 认新摘要 + 别 host 给旧摘要）时按 `weak` 接受上游重发并改写 TOFU，
  `verify` 事件 `msg` 说明原因；同源一家之言仍硬失败。
- **GUI 补充 which / env / mirror write 页面**（9 命令的界面缺口补齐，GUI 侧不计入主线行数）。
- **多连接下载基准（先测再决定）**：npmmirror 上 48MB 的 node tar.gz，单流 1.14–1.23s（≈32–36 MB/s），
  2/4/8 段并行 0.72–1.03s → **约 1.3× 收益**（4 段最稳，8 段无进一步收益）。
  结论：收益有但不值当在这个点改下载主路径（每 48MB 省约 0.3s，代价是 Range 分片 + 拼装 + 校验路径的分支），
  若日后要做得整块实现并保留单流回退。
- 行数红线 10,500 → **10,700**（本次两项能力的实测驱动），`make loc` 现在 **10,627 / 10,700**；
- 发布：tag **v1.1.0**（gitee + GitHub + brew tap + scoop 同步，`verify-release` 6 产物 sha256 全对；smoke 5 runner 装的都是真 `packnest v1.1.0`，全绿）。

## v1.0.0 发布记录（2026-11-12）

**口径**：契约（`--json` schema 1）与命令面（9 个）**冻结不动**，只做收尾、验证与发版；新功能一律进 1.1。

| 项 | 结果 |
|---|---|
| 收尾改动 | 删掉 `--jobs` 空壳开关（接受却不生效，误导）；`pk uninstall node@22` 现在和 `use`/`install` 一样先解析到已装版本；`corepack enable` 前清悬空 shim（gc 回收 node 版本后会把 yarn 链断） |
| 用户视角验证 | 用 1.0.0 候选二进制在隔离 `PK_ROOT=/tmp/pkv10` 跑 17 项全链路（6 工具、外部 provider 真工具 ripgrep、坏清单、corepack、gc、项目锁、`uninstall --all --purge` 零残留），记录见 `docs/verify-1.0.md`；真机 `~/.packnest` 与 shell rc 一行未动 |
| 门禁 | 本地 `make check` 8 项全绿；`.github/workflows/ci.yml`（check + gui-contract）绿 |
| 产物 | 6 平台 tar.gz/zip + `checksums.txt`，本地构建可重复；`make verify-release` 线上 6 个 sha256 全对 |
| smoke | 打 tag 后重跑 `smoke.yml`：5 个 runner 装的都是真 `packnest v1.0.0`，`--version`/doctor/装 node/卸载零残留 全过 |
| 渠道 | gitee release、GitHub Releases、brew tap（`nekoleamo/homebrew-packnest`）、scoop bucket 均已同步，远端 hash 核对一致 |
| 顺手修 | 发布当天发现 `ci.yml` 的浅克隆导致 `VERSION=$(git describe)` 退化成短 SHA（GUI 版本门禁把它判成"太旧"）→ checkout 加 `fetch-depth: 0` |
| 1.0.1 补丁（同日） | 外部清单 `checksum.urls` 里的 `{filename}` 在无文件清单的索引（`github-releases`/`dirlist`）上回落到产物 URL 末段；实测 `rg.toml` 用 `{version}/{filename}.sha256` 直接装成（`weak`）。同时更正文档：`checksum.urls` 仍必填。6 产物 + tap/bucket 已同步 |

### GUI 界面细节记录（2026-11-11 第二轮 / 2026-11-12 第三、四轮）

**第四轮（gui-v0.10.2，补 9 命令的界面缺口）**
- **环境页**：`which`（命令名 → 真实路径，标"pk 入口 / 真实文件"）、`env`（PATH 五段按优先级列表 + 13 个环境变量，
  只读、带"项目级（pk.lock）"开关、可一键复制 `eval` 片段）；
- **镜像档页**补「写入 ~/.npmrc」按钮（二次确认 + 显示"新写入/已替换"，只改 registry 一行、写前备份）；
- 新增 `GET /api/which`、`GET /api/env`、`POST /api/mirror-write` 三个接口；`/api/mirror-write` 走 token 守卫与 POST。


- **全局包体检表格化**：`GLOBAL_*` 的问题（`detail` 里有 pkg/needs/have/file/bin）进单独一张表
  （包 / 问题 / 详情 / 级别），其余 issue 一行一条（消息 + 淡色 code 尾巴，code 仍可见便于报障）。
- **GC 结果可视化**：四个统计块（版本 / 缓存 / 释放空间 / 仍被引用）+ 分组清单；预演写"将删"、
  真删才是"已删"，顶部另有"预演结果（未删除）"徽标。
- **安装队列**：多条 spec 依次跑，复用同一个 `/api/install` NDJSON 流；每行独立进度条与状态，
  一条失败不影响后续（原因留在该行，可单独 ✕ 移出）；"装这个版本"直接进队并开跑。
- **深浅色手动切换**：跟随系统 / 浅色 / 深色，存 `localStorage[pk-theme]`；`<html data-theme>` 由 JS 定，
  `frontend/index.html` 里先跑一次同步脚本，避免首帧白闪。
- 顺手修的三处：镜像档下拉补上 `auto`（原先 `current=auto` 不在 `mirrors[]` 里 → 下拉空白）、
  版本下拉默认选最新（原先 `reverse()` 后默认落在 0.1.14 这种最老版本）、体检长消息不再溢出卡片。
- **Linux 运行时依赖清单**：各发行版包名（Debian/Ubuntu `libgtk-4-1 libwebkitgtk-6.0-4`、Fedora `gtk4 webkitgtk6.0`、
  Arch `gtk4 webkitgtk-6.0`、Alpine `gtk4.0 webkit2gtk-6.0`）写进 `cmd/pk-desktop/README.md`，并注明
  4.1 不够、老发行版（Debian 11 / Ubuntu 20.04）改用 `--serve` 浏览器形态。包名是查各发行版包索引核过的。
- **门禁上云（A2）**：新增 `.github/workflows/ci.yml`（此前 4 个 workflow 没有一个跑门禁）：
  `check` job 跑 `make check` 8 项，`gui-contract` job 构建真 `pk` 后跑 GUI 侧 `internal/pkcli`（含契约测试）。
  上线当天就抓到只在 Linux 红的 `env.RCFileFor`（非 Windows 上读了本机 `PSModulePath`，把预测的 windows
  profile 误判成 PS7）→ 已修（跨 OS 预测不读本机环境）并补回归用例 `TestRCFileForIgnoresForeignEnv`；
  两次 CI 均绿（`check` + `gui-contract` 全 success）。
- 文档清理（D）：`packaging/npm`（npm 渠道停用后的死骨架）已删；README「已知限制」两条过期项
  （pnpm 12 启动器、GUI 未开工）改成现状；progress 里 L4「未做/已做」自相矛盾已统一，
  伴生包那条改成实情：`applyCompanion` 硬编码 `Companions["npm"]`，其它键解析但不生效。
- 验法：临时 `PK_ROOT=/tmp/pkroot17` 装 node@20/22 + pnpm、手造带 `engines`/`gypfile` 的假全局包，
  用 `--serve` + headless Chrome 截图核对了表格/摘要/队列的两种配色（不动用户真实 `~/.packnest`）。

### 提醒（别重犯）

- `make loc` 是硬红线，任何新功能前先 `make loc`；减行只能靠**合并小文件**（拆文件是净增）
- gitee release **同名附件删不掉也换不了** → 重发只能删 release 重建（命令在 `docs/publishing.md`）
- 发布只在本机（唯一发布者）：CI 只构建不发布，win/linux GUI 用 `gh run download` 拉回再 `--from-dir` 上传
- GUI 改了前端必须 `make gui-frontend`（`frontend/dist` 提交进库，否则 Go 里是旧 UI）

## 行数预算现状

- **红线：10,700 行**（`cmd/` + `internal/`，不含 `*_test.go` 与 `internal/testkit`）：
  8,500 → 10,000（2026-11-10）→ 10,200 → 10,300（外部 provider 与 corepack 落地后按实测上调）→
  10,360 → 10,450 → 10,500（GUI 前置换来的 `--json` progress 事件与契约字段）→
  **10,700（2026-11-12 拍板：1.1 的通用伴生包 + deno 同源配对校验）**。
  每次上调都写进 `AGENTS.md` 硬约束表与 `~/Documents/Plan/17-包管理器/pn-1.1-计划.md` §三。
- 现状 **10,500 行**（余量 200）；`cmd/pk` 3,136 / 3,200；单文件无 >600 行。
- ✅ 门禁：`make loc`（`make check` 第 2 项，`tools/loccheck.sh`）——总计 > 红线退出 1；
  `cmd/pk` > 3,200 与单文件 > 600 只 warn。

## GUI 定位（2026-11-11 拍板）

**GUI 交付状态（2026-11-11 收口）**：Vue3+TS+Vite 前端、Wails v3.1.0 桌面壳 + `--serve` 浏览器形态、
托盘与单实例（占端口当互斥锁）、三平台产物已发到独立 tag `gui-v0.10.1`（gitee + GitHub）：
mac darwin arm64 `.app.zip`（本机构建；intel 2026-11-12 放弃）、windows amd64 `.zip`、linux amd64 `.tar.gz`（CI `gui-build.yml` 构建）。
GUI 已带图标（`packaging/genicon` 生成的"巢"标记 → `.icns`/托盘模板图）、`--version`；
签名/公证**拍板不做（2026-11-12）**：产物一律 ad-hoc 签名（`PK_SIGN_IDENTITY`/`PK_NOTARY_PROFILE` 机制保留，
将来想发 Developer ID 就设两个环境变量）；代价是 macOS 首次启动要用户在"隐私与安全性"里手动放行。

**GUI 前置 ④ 已完成**：托盘（关窗口收进托盘、菜单打开/退出）、单实例（占端口当互斥锁，第二个实例直接拒绝）、
桌面壳同时提供浏览器形态；三平台产物已随 **gui-v0.10.1** 发布（mac 本机构建，win/linux 由 gui-build.yml 构建后本机上传）。

**GUI 重发（2026-11-12）**：GUI 有自己的版本号（`main.go` 的 `guiVersion`），本轮升到 `0.10.1` 重发——
产物文件名带 0.10.1，字节与本地/CI 一致（mac 本机构建，win/linux 从 `gui-build` 拉回改名上传）。
同日拍板 **mac intel（darwin/amd64）放弃**：`gui-build.yml` 删掉 `macos-15-intel` 矩阵项（它长期 optional 失败）、
`release-gui.sh` 只构建 arm64，release 重建后是 3 个产物；Intel Mac 要 GUI 自己 `make gui-app`。

**GUI 界面细节（2026-11-11 第三轮）**：全局包体检表格化、GC 统计块+分组清单、多 spec 安装队列、
深浅色手动切换（跟随系统/浅色/深色）。明细见文末「已完成」节。

**GUI 界面细节（2026-11-11 第二轮）**：镜像页的"探测各源"表格（host+末段/状态/延迟）、体检页的
"实测原生模块（--probe）"开关、版本选择的"全部版本 + 本地过滤"、已装行的 weak/未校验/npm 徽标与
"复制安装路径"、统一错误文案（412 没装 CLI / 409 另一个 pk 正在运行 / 401 要 token）。
发布流程已并入 `docs/publishing.md`（「三、发布流程」第 ⑦ 步 + 「五点五」唯一清单）。

**GUI 前置 ③ 已完成**：可装版本/版本选择/卸载三个页面、`internal/pkcli` 的契约测试（用真 CLI 跑
`version/list/doctor/mirror list/gc --dry-run`）、`make gui / gui-app / gui-serve / gui-test`；
macOS `.app` 打包脚本（ad-hoc 签名，13.0+）；`--serve` 绑非回环地址时强制 `--token`。

**GUI 前置 ② 已完成（spike）**：`cmd/pk-desktop` 独立模块（Wails v3.1.0 + 零依赖前端），
`internal/pkcli` 负责"定位 CLI / 版本门禁 / 跑 `--json` / 解析 NDJSON"，已用假 `pk` 脚本单测
（定位、schema 与版本门禁、progress 事件流、退出码 5）。桌面壳与 `--serve`（浏览器）共用同一套 `/api/*`；
没装 CLI 时页面直接给安装命令。见 `cmd/pk-desktop/README.md`。

**GUI 前置 ① 已完成**：`pk install --json` 现在吐 `progress` 事件（`bytes_done`/`bytes_total`，约 200ms 节流），
这是"补充式 GUI"唯一缺的契约字段；`Event` 只增字段，文本模式不受影响（`TestInstallEmitsProgressEvents`）。

**GUI 是 CLI 的可视化补充，不是独立端**：必须先装 `pk`，GUI 通过子进程调 `pk <cmd> --json`（冻结 schema）驱动，
GUI 侧不 import 任何 `internal/*`。连带影响：

- 方案 §11 的"`pk-desktop → internal/core`"改为"`pk-desktop → pk` 子进程"；`internal/core` 门面不再需要；
- 技术债「卸载编排搬进 internal/install」**降级**（原本是为 GUI 复用，现在只是测试便利）；
- 升级/卸载都指回 CLI（`brew upgrade`、`pk uninstall --all --self`），GUI 不自带更新器与第二套状态。

## 实装验证

smoke CI（`.github/workflows/smoke.yml`）现在每个平台都装 node + **pnpm（平台包）** 并跑一次，
5/5 组合全绿（macos-15 / macos-15-intel / ubuntu-latest / ubuntu-24.04-arm / windows-latest）。

`docs/verify-1.0.md` 记录 2026-11-11 的 v0.9.0 用户视角验证（5/6 平台组合自动化 + 本机 macOS 全链路），
以及那一轮发现并修掉的三个问题（`pk use <大版本>` 报"还没装"、`--corepack` 对已装版本不重放、
`pk help` 与 `--help` 的开关说明重复）。

## 剩余项（非 GUI，2026-11-11 盘点）

| 项 | 状态 | 阻塞原因 |
|---|---|---|
| pnpm 第 4 个下载面 | ✅ 已解决（平台包 + 平台元数据校验，6 个平台包实测都存在） | — |
| 离线重装 / 缓存被删 | ✅ 已修（TOFU 命中不问网络；只有摘要不符才删缓存） | — |
| 内容寻址缓存（按 sha256 去重） | ✅ 已做（2026-11-11）：缓存文件落成 `<sha256>.<ext>`，TOFU 表里已知摘要时直接按内容找，同内容跨版本只存一份 | — |
| L4 `dlopen` 试加载检测 | ✅ 已做：`pk doctor --globals --probe` 用 `node -e "process.dlopen({exports:{}}, file)"` 试加载 `.node`（只加载二进制、不跑包代码） | — |
| 拒绝 https→http 的降级重定向 | ✅ 已做：`fetch.checkRedirect` 拒绝任何 https→http 的降级与 >10 次重定向 | — |
| 卸载编排挪进 `internal/install`（清 `cmd/pk` 3,220 warn） | ✅ 部分完成：渠道识别（`DetectInstallChannel` / `SelfRemoveHint`）已挪进 `internal/install`，`cmd/pk` 3,220 → **3,181**（warn 消失）；剩余打印/交互编排仍留在 cmd | — |
| winget 渠道 | ❌ 已放弃（2026-11-11 拍板）：不再生成草稿、不提 PR；`packaging/winget/` 与渠道识别已删 | — |
| windows-arm64 运行时、Gatekeeper 首启 | ⏳ 人工确认（步骤与通过标准见 `docs/verify-manual.md`） | 无公共 ARM Windows runner；ad-hoc 签名首启只能真人放行 |
| Windows `.cmd` shim | ✅ 2026-11-13 自动化（`smoke.yml` 的 `pwsh` 步骤） | — |

## 已知缺口（按计划补齐，不是遗漏）

- go 的国内索引源：npmmirror 没有 go 的产物/索引（实测）。**2026-11-13 补了 cn 档**：索引用官方 CN 镜像
  `golang.google.cn/dl/?mode=json&include=all`（实测 1.35s vs go.dev 2.0s，且 host 不同是天然灾备），
  default 档仍先 go.dev；两档都拿不到时走 24h stale 缓存。产物与校验和本来就能走 aliyun/index-inline。
  ⚠️ aliyun/tuna/ustc/huaweicloud 的 `/golang/` 路径**不提供** `?mode=json`（实测 HTML/404），别照着换源。
- python 的索引只声明 npmmirror（官方 GitHub 没有目录列举接口），官方只作为产物/校验和的回退源；
  若镜像索引整体不可用，只能靠 stale 缓存。
- **pnpm 已改成平台包**（2026-11-11）：产物换成 registry 上的 `@pnpm/exe.<os>-<arch>`（真二进制，~23MB），
  不再有"启动器首次运行自己下平台二进制"的第 4 个下载面；摘要在平台包自己的元数据里
  （`checksum.urls` 指向 `{registry}/@pnpm/exe.{platform}`，install 读它的 `dist.integrity`）。
  ⚠️ 限制：`@pnpm/exe.*` 只从 **11.0.0-rc.3** 开始发布。清单里用 `min_version = "11.0.0"` 声明下限，
  `pk install pnpm@10` 会在解析后**立刻报错并给建议**（corepack 或 `npm i -g pnpm@10`），不再去撞 404。另：之前用 JS tarball 装过同一版本的机器，
  TOFU 表里记的是旧摘要，重装会要求 `--force-rehash`（一次性）。
- ✅ **同源配对校验已做（1.1 通用层，2026-11-12）**：TOFU 记录与实际不符时，若「与产物同 host 的校验源认可新摘要」
  且「另一个 host 的源仍给旧摘要」（跨源分裂 = 上游重发的特征），按 `weak` 接受并改写记录，`verify` 事件 `msg` 说明原因；
  同源一家之言仍硬失败（可能镜像被换），`--force-rehash` 仍是显式覆盖口。验收 `TestVerifySameOriginRepublish` / `TestVerifyNoSameOriginStillFails`。
- ✅ 外部清单 `checksum.urls` 的 `{filename}` 在无文件清单的索引上（`github-releases`/`dirlist`）已回落到产物 URL 末段（1.0.1，
  `TestFilenamePlaceholderFallsBackToArtifactName`）。注意 `checksum.urls` 仍然必填（缺了清单非法）。
- ✅ **通用伴生包已做（1.1，2026-11-12）**：`applyGenericCompanions` 按 key 走 `registry`/`target`/`mode`，
  `bundled` 跳过、`latest-compatible` 取该 provider 最新版、`pinned` 读 `version`；装进宿主 `<target>`，
  跟着宿主一起卸载/回收（不单独进台账）。node→npm 仍是 `applyCompanion` 的 engines 反查特例。
  验收 `TestGenericCompanionInstalled`、`TestCompanionPinnedNeedsVersion`。

## 1.5（2026-11-13，**v1.5.0 已发**）

候选清单见 `~/Documents/Plan/17-包管理器/pn-1.5-候选清单.md`。三条一起做：两条功能 + 一条偿债。

- **#5 `PK_CACHE` 独立缓存目录**：`internal/paths` 多一个 `CacheRoot`（`Resolve` 读环境变量，`~`/相对路径照常展开），
  产物缓存 `archives/`、索引缓存 `meta/`、`COREPACK_HOME`、`npm_config_cache` 的兜底值全部跟着走；
  台账/锁/shim/`config.toml` 仍在 `PK_ROOT`。`pk uninstall --all --purge` **不动**外部缓存目录（通常是 CI 挂卷），
  预览里会明说一句。用例：`TestCacheEnvOverride`、`TestCacheDefaultsUnderRoot`、`TestCacheOutsideRoot`（装完
  产物在 `PK_CACHE/archives`、`PK_ROOT/cache` 不被创建、卸载不连带删）、`TestCorepackHomeFollowsCache`、
  `TestNpmCacheDirFollowsCache`。
- **#6 读 `package.json` 的 `engines.node`**：`internal/install/project.go` 的 `ProjectSpecs` ——
  优先级「命令行 > `pk.toml` `[tools]` > `package.json` `engines.node`」，engines 只补 `node` 且只读。
  `engines.node` 解析不了当场报错并给 `pk install node@<版本>` 的建议；`package.json` 写坏只提示一行、不拦安装。
  **只有在 `pk.toml` 存在时才写 `pk.lock`**（`HasProjectFile`）—— 不往没有 pk.toml 的仓库里多放文件，
  装完给一行"没写 pk.lock（想固化版本就加一份）"。用例：`TestProjectSpecs*`（6 个）、`TestHasProjectFile`、
  `TestInstallFromPackageJSONEngines`（CLI 层：提示出现、不再报"没有 pk.toml"）、
  `TestAcceptance_M7_PackageJSONEngines`（`>=22.9 <23` 选出 22.98.0；pk.toml 说 20 就赢）。
- **偿债：`pk list` 的数据装配下沉**：`internal/install/listview.go`（`ActiveVersions` / `InstalledEntries` /
  `OutdatedHints`）+ `internal/provider/rows.go`（`AvailableRows` + `AvailableRow`，`--json` 的 `available[]` 契约
  与 `tools[]` 形状不变，golden 未改）。`cmd/pk` 3,320 → **3,191**（回到 warn 线内），`cmd_list.go` 626 → **497**。
  代价是总量 +166（接口税），红线 11,450 → **11,600**（2026-11-13 拍板），落地 11,597。

**1.5 的取舍记录**：`engines.node` 只补 node、不补 pnpm/bun —— `packageManager` 字段（corepack 那套）语义不同，
混进来会让"pk.toml 权威"这条变模糊，留作以后单独评估。`PK_CACHE` 只搬缓存，没搬台账：台账与锁必须和 shim 在同一处，
否则"删了缓存目录"会连安装记录一起丢。

## 1.5 第二轮：整体 review + 性能优化（2026-11-13，随 v1.5.0 发布）

先量后改（同一台机器、同一台镜像、`/tmp` 下每次全新 `PK_ROOT`）。**三处延迟问题都是"代码说的和做的不一致"**：

| # | 问题（review 发现） | 根因 | 改法 | 前 → 后 |
|---|---|---|---|---|
| 1 | `auto` 档位的探测缓存**形同虚设**：`mirror.Detect` 虽然 `Load()` 了缓存，但**从不读它**，每次都现发 HEAD | 写缓存 + `CachedProfile` 都在，唯独 `Detect` 没用上（文档和 doctor 文案都声称"6h 内不再探"） | `Detect` 先按 TTL 命中缓存（`Result.Cached` 标记，事件里写"6h 内探过"），未命中才探 | `install --dry-run` **0.71s → 0.01s**（首次仍需 1.44s：官方 500ms 超时 + 国内 250ms） |
| 2 | 所有请求强制 `Accept-Encoding: identity` | 分片/续传必须 identity（Range 按原始字节），但索引请求被一起绑死了 | 抽出 `setAgent`：索引请求不设该头，交给 `http.Transport` 协商 gzip 并透明解压 | go 索引 **2.28MB → 557KB**；冷 `list --available` **1.54s → 0.98s** |
| 3 | `pk list` 每次都 `filepath.Walk` 整个工具树算占用 | 台账没存大小（node 装完 189MB / 4,729 个文件） | 安装时算一次写进台账 `size_bytes`；老台账（无该字段）才回退 walk | `pk list` **40ms → 10ms**；暖 `list --available` **29ms → 20ms** |

配套的代码质量项（都是"一份实现/标准库就够了"）：

- **原子落盘只剩一份**：`fetch` / `mirror` / `verify` / `store` / `install` 各写过一遍"写 `.tmp` → rename"，
  误差只在错误文案。统一到 `paths.WriteAtomic`（自动建父目录、失败清 tmp），顺带修好 `fetch` 那份不建目录的行为差异。
- **手写小工具交给标准库**：`contains` / `containsStr` → `slices.Contains`；`installedTools` / `installedVersions`
  → `store.Ledger.Versions`；`staticcheck` S1011；删死代码 `mustAlias`。`staticcheck ./...` 现在全绿。
- 行数：11,642（超线）→ **11,586 / 11,600**（余 14），`cmd/pk` 3,191 → **3,169**。

**量过但决定不做的**（写下来防止下次又去试）：

- **解压耗时**：node 的 189MB / 4,729 文件，Go 标准库 gzip 解压 **1.05s**，系统 `gzip -dc | tar` 是 0.67s。
  试过 `mkdirCache`（省 4,700 次重复 `MkdirAll`）+ 256KB 池化拷贝缓冲，实测 **1.054s / 1.134s**，与基线 1.08s 无差别
  → 瓶颈是 Go 的 flate 本身（第三方压缩库是禁引项），改动已回退，不留无用复杂度。
- **TOFU 每次重装重算 50MB 的 sha256**（~0.25s）：这是完整性保证，不能用 mtime 走捷径。
- **`pk list --available` 的冷启动 0.98s** 压不到 0.3s：瓶颈是 go 索引这一个请求（`include=all`，0.93s）。
  `?mode=json`（23KB，只含稳定版）能换，但会让 `pk list go --all` 缺历史版本 —— 留作后续选项，不在 1.5 动。
- **`auto` 探测结论 6h 内直接采信**：如果用户换了网络（或反向），最多 6h 后自愈；期间失败信息会给出
  `pk mirror use cn` / `pk mirror probe` 的出路，比"每次都多付 0.7s"划算（1.5 之前就是每次现探）。

文档同步：`docs/cli.md` / `docs/en/cli.md`（auto 缓存语义、`engines.node`、`PK_CACHE`）、`README.md` / `README.en.md`
（项目作用域表 + 工程约束的缓存例外）、官网中英文（install 行的 engines、下载段加 gzip、新增"缓存"一条）。
