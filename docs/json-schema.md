# `--json` 契约 v1（冻结）

> 方案 §12.2 第 8 条：**GUI 开工前 CLI 的输出合约必须冻结**。本文件定义 `schema: 1` 的全部形状，
> `internal/jsonout/testdata/*.golden.*` 是它的机器可读版本（`make golden` 会跑）。

## 规则

1. `schema` 字段恒为 `1`。破坏性变更 → `schema: 2` 并保留旧字段一个版本。
2. **只增不改**：新字段可以加，已有字段的语义/类型/名字不许改。
3. 数组字段永远输出 `[]` 而不是 `null`（脚本不需要判空）。
4. 空值字段用 `omitempty` 省略（如 `bytes`、`source`）。
5. 退出码固定：`0` 成功 / `1` 通用失败 / `2` 用法 / `3` 网络 / `4` 校验 / `5` 锁被占用。

## 文档型输出（单个 JSON）

| 命令 | 形状 | 稳定字段 |
|---|---|---|
| `pk list --json` | `list.golden.json` | `tools[].{name,version,active,path,npm{version,replaced},requires,verified,size_bytes}` |
| `pk list --available --json` | 内联结构 | `mirror`、`available[].{name,latest,lts,recent,requires,stale,error}`（与 `tools` 同时给） |
| `pk doctor --json` | `doctor.golden.json` | `ok`、`issues[].{code,severity,message,detail}`；`severity ∈ info/warn/error` |
| `pk version --json` | `version.golden.json` | `name,version,go,os,arch` |
| `pk which --json` | `which.golden.json` | `name,path,shim` |
| `pk gc --json` | `gc.golden.json` | `Removed,CacheRemoved,KeptRef,FreedBytes,DryRun,cache_max_bytes` |
| `pk use --json` | 内联结构 | `tool,version,config` |
| `pk mirror list --json` | 内联结构 | `current`、`mirrors[].{name,display,registry,current}` |
| `pk mirror probe --json` | 内联结构 | `results[].{url,ok,status,latency_ms,checked_at,err}` |
| `pk env --json` | 内联结构 | `shell,path[],env[].{key,value},script_hint` |
| `pk env --write --json` | 内联结构 | `rc,block` |

## 事件流（NDJSON，`pk install --json`）

每行一个独立 JSON 对象，`ev` 是判别字段；顺序保证 `resolve → (cache|download) → verify → [companion] → done`。

| `ev` | 附带字段 | 说明 |
|---|---|---|
| `resolve` | `tool,version` | 版本解析完成 |
| `cache` | `tool,version,bytes` | 缓存命中（0 网络） |
| `download` | `tool,version,bytes,source` | 实际下载（`source` 是最终使用的 URL） |
| `verify` | `tool,version,mode,ok` | `mode ∈ tofu/network/insecure` |
| `companion` | `tool,version,msg` | 伴生包替换（`msg` 形如 `replaced 11.20.0`） |
| `done` | `tool,version,path` | 落盘完成 |
| `uninstall` | `tool,version` | 卸载完成 |

GUI 只需要：读 `schema` 判契约版本；进度条用 `download.bytes`；"是否校验过"看 `verify.mode`。

## doctor 问题码（脚本与 GUI 依赖）

| code | severity | 含义 / 处置 |
|---|---|---|
| `PATH_ORDER` / `PATH_MISSING` | warn | bin 目录不在 PATH 最前 / 不在 PATH |
| `LEDGER_BROKEN` | error | 台账与 `.bak` 都坏（可手工修或删掉重装） |
| `LEDGER_RECOVERED` | warn | 主台账坏，已用 `.bak` 恢复 |
| `LEDGER_INCONSISTENT` | error | 台账记录的目录不存在等 |
| `CONFIG_BROKEN` | error | `config.toml` 解析失败 |
| `MIRROR_PROFILE_INVALID` | error | 档位名非法 |
| `MIRROR_UNREACHABLE` | error | `--probe` 下索引源不可达（`detail.advice` 给换档建议） |
| `MIRROR_AUTO` | info | 档位为 auto，首次安装会探测并缓存 6h |
| `GO_TOOLCHAIN_NOT_LOCAL` | warn | 装了 go 但 `GOTOOLCHAIN != local`（会多下一次工具链） |
| `GLOBAL_ENGINES_UNMET` | warn | 全局包 `engines.node` 不满足当前 node |
| `GLOBAL_NATIVE_ABI` | warn | 全局包含原生模块且无 N-API 预编译（切版本会失效） |
| `GLOBAL_SHADOWS_PROJECT` | info | 全局命令名遮蔽项目 `node_modules/.bin` |
| `SHIM_MISSING` / `SHIM_BROKEN` / `SHIM_PLAIN` | warn/info | 自身双名 shim 状态 |
| `FOREIGN_MANAGER` | info | 检测到 proto/nvm/mise 等（只提示，不自动改） |
