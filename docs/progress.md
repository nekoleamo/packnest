# 进度

> 口径：方案 §12 的里程碑。**验收项 = 测试名**，`make accept` 全绿才算完成。

## 里程碑

| 阶段 | 内容 | 状态 | 验收用例 |
|---|---|---|---|
| **M0** | 骨架：module、9 命令、provider 加载、镜像探测、testkit 假镜像、L1 单测 | ✅ 完成（2026-11-10） | `M0_ProviderLoad` ✅ · `M0_MirrorProbe` ✅ · `M0_DoctorNoPanic` ✅ · `M0_DualNameConsistent` ✅ |
| **M1** | node + npm 伴生：fetch(Range/ETag) · 解压 · 校验+TOFU · shim · 伴生反查 | ⏳ 下一步 | `M1_NpmCompanion` / `_NpmPinConflict` / `_PatchLevelBoundary` / `_OfflineFromCache` / `_ShimLatency` / `_UninstallClean` / `_WindowsAtomicReplace` / `_WindowsZipNoSymlink` |
| M2 | pnpm / bun / deno | 未开始 | `M2_PnpmRequiresNode` / `_SingleFileLayout` / `_ThreePlatformSmoke` |
| M3 | go + python（`GOTOOLCHAIN=local`、目录列举式索引） | 未开始 | `M3_GoNoDoubleDownload` / `_PythonNoCompile` / `_PbsDirectoryIndex` |
| M4 | 台账/锁/GC/全局包 | 未开始 | `M4_GcZeroRef` / `_LedgerRecover` / `_FrozenMismatchExitCode` / `_CtrlCNoPartial` / `_GlobalSurvivesNodeSwitch` / `_GlobalBinNoReshim` / `_NativeGlobalDetect` / `_StartupSweep` |
| M5 | CLI 收口（doctor 全量、错误码定型、`--json` 冻结、6 平台产物、分发） | 未开始 | §12.2 的 10 条 CLI-DONE |
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

## 已知缺口（按计划补齐，不是遗漏）

- `internal/archive` / `verify` / `companion` / `toolchain` 尚未创建：M1 建 archive/verify/companion，M3 建 toolchain。
  覆盖率门禁暂未统计它们；建包的同时纳入 `make check`。
- npm/pnpm 的 `layout.bin_targets`（`bin/pnpm.cjs` 等）已在清单里声明，落地逻辑随 M1/M2 的解压层实现。
- python 产物名含 release 标签 `{tag}`，`ArtifactURL` 现在会明确报错；M3 的目录列举式索引补齐。
- go 的国内索引源只写了官方 `?mode=json`（自带 sha256），M3 补国内源或改走 stale 缓存。
- `pk env --write`（写 rc 的 `# managed by pk` 块）与 `mirror write`（写 `~/.npmrc`）在 M5。
