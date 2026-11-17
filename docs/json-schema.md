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
| `pk upgrade --json` | 内联结构 | `dry_run`、`changed`、`failed`、`items[].{tool,from,to,changed,skipped,error}`（1.4 第 10 个命令） |
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

多工具安装（1.7 起跨工具并行）时：**组内顺序不变**，组间可以交错；每行都带 `tool`，
按 `tool` 分组即可还原单工具事件流（GUI 就是这么消费的）。

| `ev` | 附带字段 | 说明 |
|---|---|---|
| `resolve` | `tool,version` | 版本解析完成 |
| `plan` | `tool,version,source,mode` | `--dry-run` 的计划（这种模式下**不会有** `done`） |
| `mirror` | `tool,source,latency_ms,cached` | 探测出镜像档（`cached=true` 表示 6h 内探过，没再发请求） |
| `progress` | `tool,version,bytes_done,bytes_total` | 下载字节进度（约 200ms 一次，`bytes_total` 未知时为 0；GUI 画进度条用） |
| `cache` | `tool,version,bytes` | 缓存命中（0 网络） |
| `download` | `tool,version,bytes,source` | 实际下载（`source` 是最终使用的 URL） |
| `verify` | `tool,version,mode,ok,msg` | `mode ∈ tofu/network/insecure`；`msg` 只在"上游重新发布按同源配对接受"时出现 |
| `companion` | `tool,version,replaced,path` | 伴生包替换：`replaced` 是被换掉的版本（原来只在 `msg` 里）；通用伴生包用 `path` 给落地位置 |
| `done` | `tool,version,path` | 落盘完成（**即使已经是最新、无需重装，也以 `done` 收尾**） |
| `uninstall` | `tool,version` | 卸载完成 |
| `abort` | `tool,msg` | **1.8 起**：`--atomic` 下这一批被整体放弃，本目标没有落盘（`msg` 说明原因）。出现在该目标其它事件之后；GUI 按 `tool` 分组时遇到 `abort` 就把整组作废 |

`ev` 的值是**只增不改**的：脚本与 GUI 必须忽略自己不认识的 `ev`（1.8 新增 `abort` 就是按这条加的）。

### 字段语义（1.8 拍板：`msg` 只是人话）

| 字段 | 契约 | 说明 |
|---|---|---|
| `ev` / `tool` / `version` | **稳定**，机器用它判断 | `ev` 只增不改（新值必须能被旧消费者忽略） |
| `ok` / `mode` / `source` / `path` | **稳定** | 判定"验过没、怎么验的、从哪来、落到哪" |
| `bytes_done` / `bytes_total` | **稳定** | 画进度用；`bytes_total=0` 表示总长未知 |
| `replaced` / `latency_ms` / `cached` | **稳定**（1.8 新增） | 伴生包替换前的版本 / 镜像探测延迟 / 是否命中 6h 探测缓存 |
| `msg_code` / `msg_args` | **稳定**（1.8 新增） | 这句话的结构化来源：自己按码表渲染也行；`--lang en` 下 `msg` 已经跟着变英文 |
| **`msg`** | **尽力而为**：可能中文、可能改措辞、可能不出现 | **不许对它做字符串匹配**。人类可读的补充说明，不是接口 |

为什么这么定：`internal/*` 的中文文案将来要做 L2（改成"错误码 + 参数"、由 CLI 渲染），
只要有人拿 `msg` 做判断，那条路就被永久堵死。所以从 1.8 起，凡是机器需要的信息一律有自己的字段
（`replaced`、`latency_ms`、`cached` 就是这么加出来的），`msg` 留给人看。

GUI（作为 CLI 的可视化补充，走子进程 + `--json`）只需要：读 `schema` 判契约版本；
进度条用 `progress.bytes_done/bytes_total`（`download` 只给最终大小）；"是否校验过"看 `verify.mode`；
结束看 `done`；破坏性命令要自己先确认再传 `--yes`。

### `doctor --fix` 的 `reload`（1.8.2 起，只增字段）

`pk doctor --fix` 修了 PATH 托管块 / 用户 Path 之后，**当前 shell 拿不到新环境**，所以多一个字段：

```json
{
  "schema": 1, "ok": false, "issues": [], "fixed": ["重写 shell 的 PATH 托管块（`pk env --write`，幂等）"],
  "reload": {
    "needed": true,
    "shell": "zsh",
    "command": "source /Users/you/.zshrc",
    "files": ["/Users/you/.zshrc"],
    "msg": "已写进 /Users/you/.zshrc，只对新开的 shell 生效：…"
  }
}
```

| 字段 | 契约 | 说明 |
|---|---|---|
| `reload.needed` | **稳定** | 只有这次真改了环境（rc 托管块 / 注册表用户 Path）才出现整个 `reload` |
| `reload.shell` | **稳定** | `zsh` / `bash` / `fish` / `powershell` / `other`——GUI 按它选措辞 |
| `reload.command` | **稳定** | 可直接粘进当前 shell 的一条命令；**空字符串**表示只能重开终端（cmd.exe / Windows 注册表 Path） |
| `reload.files` | **稳定** | 写过的 rc/profile；Windows 注册表写作 `registry:user-path` |
| `reload.msg` | **尽力而为** | 人话（跟 `--lang` 走），脚本别拿它做匹配 |

`pk env --write --json` 的输出里也带同一个 `reload` 对象（`rc` / `rcs` / `block` 之后追加）。

### `pk upgrade --self --json`（1.9 起）

检查（`--check`）与升级共用一份输出：

```json
{
  "schema": 1,
  "current": "1.8.2",
  "latest": "1.8.3",
  "update_available": true,
  "channel": "script-posix",
  "channel_hint": "rm -f ~/.local/bin/pk …",
  "source": "gitee",
  "dir_url": "https://gitee.com/…/releases/download/v1.8.3",
  "archive_url": "https://gitee.com/…/v1.8.3/packnest_1.8.3_darwin_arm64.tar.gz",
  "sources": [{ "name": "gitee", "base": "…", "latest_url": "…", "latency_ms": 320 }, { "name": "github", … }],
  "applied": false
}
```

| 字段 | 契约 | 说明 |
|---|---|---|
| `current` / `latest` / `update_available` | **稳定** | 只比 semver；本地比远端新（自编译）时 `update_available=false` |
| `channel` / `channel_hint` | **稳定** | `brew` / `scoop` / `npm` / `script-posix` / `script-windows` / `manual`；受管渠道**拒绝自替换***，只给 `channel_hint` 里的命令 |
| `source` / `sources[]` | **稳定** | 按探测延迟排好（`mirror.Prober` 的 6h 缓存）；GUI 拿这个顺序决定从哪下 |
| `dir_url` / `archive_url` | **稳定** | 该版本的发布目录与当前平台产物 |
| `applied` | **稳定** | 这次是否真的替换了二进制 |

失败时按"失败输出"那条规矩，额外输出一个没有 `ev` 的 `error` 对象（`code` 前缀是 `selfupdate.*`）。

## 失败输出（1.8 起，与事件流同流不同形）

`--json` 命令失败时，除了原有的退出码与人读 stderr，stdout 会多一条**没有 `ev` 字段**的对象：

```json
{
  "schema": 1,
  "error": {
    "code": "install.not_in_ledger",
    "args": ["node", "1.0.0"],
    "tool": "node",
    "spec": "1.0.0",
    "msg": "台账里没有 node@1.0.0（`pk list` 看已装的版本）",
    "exit": 1
  }
}
```

| 字段 | 契约 | 说明 |
|---|---|---|
| `error.code` | **稳定**，只增不改 | `internal/*` 的问题码（见 `cmd/pk/present/codes.go`）；**没码时省略**，脚本必须容忍缺字段 |
| `error.args` | **稳定**（排序与数量同码表模板） | 码参数（全部字符串化），脚本要拼自己的提示时直接用 |
| `error.tool` / `error.spec` | **稳定** | 只在多工具批量安装时有值：哪个目标失败了 |
| `error.exit` | **稳定** | 与进程退出码一致（0/1/2/3/4/5） |
| `error.msg` | **尽力而为** | 同事件流里的 `msg`：可能中文、可能改措辞、可能不出现 |

消费规则：

1. **无 `ev` 的对象一律当作非事件**（GUI 与脚本都应跳过，不要当 `ev:""` 事件渲染）；
2. 失败判定看**退出码**优先，`error.code` 用于细分；
3. 旧版本 pk 只给退出码与 stderr —— 新消费者的逻辑必须能在“没有这条对象”时工作；
4. 人读 stderr 与退出码**不变**（这条对象是纯增量）。

## doctor 问题码（脚本与 GUI 依赖）

| code | severity | 含义 / 处置 |
|---|---|---|
| `PATH_ORDER` / `PATH_MISSING` | warn | bin 目录不在 PATH 最前 / 不在 PATH |
| `ROOT_ON_SYNC_DIR` | warn | `PK_ROOT` 在云同步目录里（锁可能失效、台账可能被回滚） |
| `ROOT_ABSENT` | info | `PK_ROOT` 目录不存在（还没装过任何工具） |
| `LEDGER_SCHEMA_NEWER` | error | 台账 schema 比本程序支持的新（降级运行），先升级 pk |
| `SHIM_ALIAS_MISMATCH` | warn | `pk` 与 `packnest` 指向不同文件 |
| `LEDGER_BROKEN` | error | 台账与 `.bak` 都坏（可手工修或删掉重装） |
| `LEDGER_RECOVERED` | warn | 主台账坏，已用 `.bak` 恢复 |
| `LEDGER_INCONSISTENT` | error | 台账记录的目录不存在等 |
| `CONFIG_BROKEN` | error | `config.toml` 解析失败 |
| `TRUST_KEYS` / `TRUST_KEYS_UNCONFIGURED` | info | 内置发布签名公钥的条数 / 为空（自己构建的正常，官方发布版应带 1-2 把） |
| `MIRROR_PROFILE_INVALID` | error | 档位名非法 |
| `MIRROR_UNREACHABLE` | error | `--probe` 下索引源不可达（`detail.advice` 给换档建议） |
| `MIRROR_AUTO` | info | 档位为 auto，首次安装会探测并缓存 6h |
| `GO_TOOLCHAIN_NOT_LOCAL` | warn | 装了 go 但 `GOTOOLCHAIN != local`（会多下一次工具链） |
| `GLOBAL_ENGINES_UNMET` | warn | 全局包 `engines.node` 不满足当前 node |
| `GLOBAL_NATIVE_ABI` | warn | 全局包含原生模块且无 N-API 预编译（切版本会失效）；`--globals --probe` 下还会用 `process.dlopen` 实测（只加载二进制，`detail.probe` 带失败原因） |
| `GLOBAL_SHADOWS_PROJECT` | info | 全局命令名遮蔽项目 `node_modules/.bin` |
| `GLOBALS_SCAN_FAILED` | warn | `--globals` 下读不了某个全局前缀目录（`detail.prefix` / `detail.manager`） |
| `GLOBALS_NONE` | info | npm 全局前缀里没有包（`detail.prefix`） |
| `SHIM_MISSING` / `SHIM_BROKEN` / `SHIM_PLAIN` | warn/info | 自身双名 shim 状态 |
| `FOREIGN_MANAGER` | info | 检测到 proto/nvm/mise 等（只提示，不自动改） |
| `FOREIGN_SHIM_PRIORITY` | warn | 别人的 shim 排在 `~/.packnest/bin` 前面 |
| `FOREIGN_NONE` | info | `--foreign` 下没找到任何 proto/nvm/volta/asdf/mise/fnm/nodenv 的 node 版本目录 |
| `PROVIDERS_BAD` | error | 内嵌 TOML 清单自身不自洽（编译产物坏了，重装 pk） |
| `PROVIDERS_DIRS` | info | 生效的外部清单目录（`detail.dirs`） |
| `PROVIDER_EXTERNAL_INVALID` | error | 外部清单非法（http 未放行 / 绝对路径 / `..` / pbs / 未知占位符 / 超 256KB / id 与文件名不符），`detail.file` 指向该文件 |
| `PROVIDER_EXTERNAL_CONFLICT` | warn | 外部清单与内嵌（或先加载的外部清单）同 id，被忽略 |
| `PROVIDER_HTTP_URL` | warn | 外部清单用了 `PK_ALLOW_HTTP=1` 放行的 http 源（校验强度最高 `weak`）；`detail.transport` 记进台账 |
| `GO_INSTALL_NOT_IN_PATH` | info | `go install` 的产物目录（`GOBIN`，默认 `~/go/bin`）不在 PATH |
| `PIP_USER_SITE_UNMANAGED` | info | pip 用户级安装（`~/.local/lib/pythonX`）不在 pk 治理范围 |
