# `--json` 契约 v1（冻结）

> 方案 §12.2 第 8 条：**GUI 开工前 CLI 的输出合约必须冻结**。本文件定义 `schema: 1` 的全部形状，
> `internal/jsonout/testdata/*.golden.*` 是它的机器可读版本（`make golden` 会跑）。

## 规则

1. `schema` 字段恒为 `1`。破坏性变更 → `schema: 2` 并保留旧字段一个版本。
2. **只增不改**：新字段可以加，已有字段的语义/类型/名字不许改。
3. 数组字段永远输出 `[]` 而不是 `null`（脚本不需要判空）。
4. 空值字段用 `omitempty` 省略（如 `bytes`、`source`）。
5. 退出码固定：`0` 成功 / `1` 通用失败 / `2` 用法 / `3` 网络 / `4` 校验 / `5` 锁被占用。
6. **破坏性命令在 `--json` 下不做交互确认**（`uninstall <工具>`、`uninstall --all`）：
   没有 `--yes` 就退出 1，绝不因为"没人按 y"而卡住脚本。

## 文档型输出（单个 JSON）

| 命令 | 形状 | 稳定字段 |
|---|---|---|
| `pk list --json` | `list.golden.json` | `tools[].{name,version,active,path,npm{version,replaced},requires,verified,size_bytes,source}`；`source ∈ embedded/external`（`omitempty`）；`outdated[].{name,installed,latest,advice}`（1.3 新增，只读索引缓存，没缓存就省略） |
| `pk list --available --json` | 内联结构 | `mirror`、`available[].{name,latest,lts,recent,requires,stale,source,error}`（与 `tools` 同时给） |
| `pk doctor --json` | `doctor.golden.json` | `ok`、`issues[].{code,severity,message,detail}`；`severity ∈ info/warn/error`；`fixed[]`（1.3 新增，`--fix` 实际做过的动作，`omitempty`） |
| `pk version --json` | `version.golden.json` | `name,version,go,os,arch` |
| `pk which --json` | `which.golden.json` | `name,path,shim` |
| `pk gc --json` | `gc.golden.json` | `Removed,CacheRemoved,KeptRef,KeptRecent,FreedBytes,DryRun,cache_max_bytes,keep`（`KeptRecent`/`keep` 是 1.3 的 `--keep N`，`omitempty`） |
| `pk use --json` | 内联结构 | `tool,version,config` |
| `pk mirror list --json` | 内联结构 | `current`、`mirrors[].{name,display,registry,current}` |
| `pk mirror probe --json` | 内联结构 | `results[].{url,ok,status,latency_ms,checked_at,err}` |
| `pk env --json` | 内联结构 | `shell,path[],env[].{key,value},script_hint` |
| `pk list <工具> --json` | 内联结构 | `tool,latest,lts,total,installed[].{version,active},available[].{version,lts,installed},stale` |
| `pk uninstall <工具> --json`（不带版本 = 删全部） | 内联结构 | `tool,removed[]`，失败时带 `error` |
| `pk uninstall --all --json` | 内联结构 | `removed[],purged,root,channel,self_hint,failures[]`（`channel ∈ brew/npm/scoop/script/script-posix/script-windows/manual`） |
| `pk env --write --json` | 内联结构 | `rc`（首个）、`rcs[]`（全部；Windows 上 5.1 与 7 两份都写）、`block` |
| `pk env --unset --json` | 无输出 | 只返回退出码（清理是幂等的） |

## 事件流（NDJSON，`pk install --json`）

每行一个独立 JSON 对象，`ev` 是判别字段；顺序保证 `resolve → (cache|download) → verify → [companion] → done`。

| `ev` | 附带字段 | 说明 |
|---|---|---|
| `resolve` | `tool,version` | 版本解析完成 |
| `progress` | `tool,version,bytes_done,bytes_total` | 下载字节进度（约 200ms 一次，`bytes_total` 未知时为 0；GUI 画进度条用） |
| `cache` | `tool,version,bytes` | 缓存命中（0 网络） |
| `download` | `tool,version,bytes,source` | 实际下载（`source` 是最终使用的 URL） |
| `verify` | `tool,version,mode,ok,msg` | `mode ∈ tofu/network/insecure`；`msg` 只在"上游重新发布按同源配对接受"时出现 |
| `companion` | `tool,version,msg` | 伴生包替换（`msg` 形如 `replaced 11.20.0`） |
| `done` | `tool,version,path` | 落盘完成 |
| `uninstall` | `tool,version` | 卸载完成 |

GUI（作为 CLI 的可视化补充，走子进程 + `--json`）只需要：读 `schema` 判契约版本；
进度条用 `progress.bytes_done/bytes_total`（`download` 只给最终大小）；"是否校验过"看 `verify.mode`；
结束看 `done`；破坏性命令要自己先确认再传 `--yes`。

## doctor 问题码（脚本与 GUI 依赖）

| code | severity | 含义 / 处置 |
|---|---|---|
| `PATH_ORDER` / `PATH_MISSING` | warn | bin 目录不在 PATH 最前 / 不在 PATH |
| `ROOT_ON_SYNC_DIR` | warn | `PK_ROOT` 在云同步目录里（锁可能失效、台账可能被回滚） |
| `LEDGER_SCHEMA_NEWER` | error | 台账 schema 比本程序支持的新（降级运行），先升级 pk |
| `SHIM_ALIAS_MISMATCH` | warn | `pk` 与 `packnest` 指向不同文件 |
| `LEDGER_BROKEN` | error | 台账与 `.bak` 都坏（可手工修或删掉重装） |
| `LEDGER_RECOVERED` | warn | 主台账坏，已用 `.bak` 恢复 |
| `LEDGER_INCONSISTENT` | error | 台账记录的目录不存在等 |
| `CONFIG_BROKEN` | error | `config.toml` 解析失败 |
| `MIRROR_PROFILE_INVALID` | error | 档位名非法 |
| `MIRROR_UNREACHABLE` | error | `--probe` 下索引源不可达（`detail.advice` 给换档建议） |
| `MIRROR_AUTO` | info | 档位为 auto，首次安装会探测并缓存 6h |
| `GO_TOOLCHAIN_NOT_LOCAL` | warn | 装了 go 但 `GOTOOLCHAIN != local`（会多下一次工具链） |
| `GLOBAL_ENGINES_UNMET` | warn | 全局包 `engines.node` 不满足当前 node |
| `GLOBAL_NATIVE_ABI` | warn | 全局包含原生模块且无 N-API 预编译（切版本会失效）；`--globals --probe` 下还会用 `process.dlopen` 实测（只加载二进制，`detail.probe` 带失败原因） |
| `GLOBAL_SHADOWS_PROJECT` | info | 全局命令名遮蔽项目 `node_modules/.bin` |
| `SHIM_MISSING` / `SHIM_BROKEN` / `SHIM_PLAIN` | warn/info | 自身双名 shim 状态 |
| `FOREIGN_MANAGER` | info | 检测到 proto/nvm/mise 等（只提示，不自动改） |
| `FOREIGN_SHIM_PRIORITY` | warn | 别人的 shim 排在 `~/.packnest/bin` 前面 |
| `PROVIDERS_DIRS` | info | 生效的外部清单目录（`detail.dirs`） |
| `PROVIDER_EXTERNAL_INVALID` | error | 外部清单非法（http 未放行 / 绝对路径 / `..` / pbs / 未知占位符 / 超 256KB / id 与文件名不符），`detail.file` 指向该文件 |
| `PROVIDER_EXTERNAL_CONFLICT` | warn | 外部清单与内嵌（或先加载的外部清单）同 id，被忽略 |
| `PROVIDER_HTTP_URL` | warn | 外部清单用了 `PK_ALLOW_HTTP=1` 放行的 http 源（校验强度最高 `weak`）；`detail.transport` 记进台账 |
| `GO_INSTALL_NOT_IN_PATH` | info | `go install` 的产物目录（`GOBIN`，默认 `~/go/bin`）不在 PATH |
| `PIP_USER_SITE_UNMANAGED` | info | pip 用户级安装（`~/.local/lib/pythonX`）不在 pk 治理范围 |
