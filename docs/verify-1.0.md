# 实装验证记录（v1.0.0，2026-11-12）

> 目的：`make check` 只证明"编得过 + 单测绿"，装不装得上、卸不卸得干净必须在真机上跑。
> 本轮用 **1.0.0 候选二进制**（`VERSION=1.0.0 make build`）在隔离 `PK_ROOT=/tmp/pkv10` 里跑用户视角全链路；
> 真实 `~/.packnest` 与用户 shell rc 一行未动。

## 一、本机实测（macOS 26 / arm64）

| # | 项目 | 结果 |
|---|---|---|
| 1 | `pk --version` / `version --json` | ✅ `packnest v1.0.0 (go1.27.3, darwin/arm64)`、`schema: 1` |
| 2 | `pk install node@22` | ✅ 22.23.5 + 伴生 npm 12.2.2（自带 10.9.9 被替换），事件流 `download → companion → done` |
| 3 | `pk env` + 双名 shim | ✅ `node -v`/`npm -v` 跑通，`node` 与 `pk` 都在 `<PK_ROOT>/bin` |
| 4 | 六工具全装：node/npm(伴生)/pnpm(平台包)/bun/go/python | ✅ 全部装成并跑出真版本（pnpm 12.20.0、bun 1.4.4、go 1.27.4、Python 3.15.2、pip 26.3）；总耗时 < 40s |
| 5 | `pk use` / `pk which` | ✅ `use node@22` 写全局默认并重指 shim；`which node` 指到 `tools/node/22.23.5/bin/node` |
| 6 | **外部 provider（真工具）**：自己写 `rg.toml`（GitHub Releases + per-artifact 校验） | ✅ `pk install rg@latest` → 15.2.0，`pk list` 带 `*` 与来源脚注，`rg --version` 可跑 |
| 7 | 坏外部清单混入 | ✅ `pk doctor` 报 `PROVIDER_EXTERNAL_INVALID` 并逐条指出缺什么（`artifact.ext` 缺 darwin → 补；`artifact.platform` 缺 windows-amd64 → 补；`checksum.format` 非法 → 补），CLI/其它工具照常可用 |
| 8 | `pk doctor --json` | ✅ `ok: true`；只剩 `PROVIDERS_DIRS`/`MIRROR_AUTO` info 与 `PATH_MISSING`（未 source 时的预期）/`GO_TOOLCHAIN_NOT_LOCAL`/`PIP_USER_SITE_UNMANAGED` |
| 9 | `pk env --write` 幂等 | ✅ 只写自己的 `# >>> packnest >>>` 块，二次写入块数仍为 1；备份 `<rc>.pk.bak` |
| 10 | `pk mirror write` | ✅ 只改 `registry=` 一行（官方 → npmmirror），先备份 `npmrc.pk.bak` |
| 11 | `pk install node@24 --corepack` + `yarn` | ✅ `global/corepack/bin` 出现 pnpm/pnpx/yarn/yarnpkg；`yarn --version` = 1.22.22（corepack 惰性下载走 npmmirror） |
| 12 | `pk gc --dry-run` / `pk gc` | ✅ 预演与实际一致（回收 1 个零引用版本 188.6 MB，保留 6 项引用） |
| 13 | 告警解耦：卸载后再装回 | ✅ `pk install node@22 --offline` 命中缓存，`node -v` 正常 |
| 14 | **项目级**：`pk use node@22 --project` | ✅ 生成 `pk.toml`（`[tools] node = "22.23.5"`）+ `pk.lock`（含 `companion_of = "node@22.23.5"`，不写 URL） |
| 15 | `pk env --project` + `pk install --frozen` | ✅ 项目版本前置到 PATH 最前；`--frozen` 按锁重放成功 |
| 16 | `pk uninstall --all --yes --purge` | ✅ 5 个运行时全清 + shim/rc 托管块/Path 清理，**`PK_ROOT` 整个删除（零残留）**；渠道识别为 `manual` 并给出"删掉这个文件"的最后一步 |
| 17 | 用户 shell 未被波及 | ✅ 真实 `~/.zshrc` 无 packnest 块、无 `.pk.bak` 产生（本轮 rc 测试都写在 `/tmp`） |

## 二、验证中发现并修掉的两个 bug（都在 1.0.0 里）

1. **`pk uninstall node@22`（大版本说明符）报"不在台账里"** —— `use`/`install` 都支持大版本说明符，
   卸载却只认精确版本，用户按直觉敲就撞上。
   修法：`cmdUninstall` 先 `inst.ResolveInstalled(tool, version)` 再删（与 `pk use` 同一条解析路径）。
   验收：`TestUninstallResolvesMajorSpec`；实测 `pk uninstall node@22` → `已卸载 node@22.23.5`。
2. **`corepack enable` 被悬空软链打断** —— `pk gc` 回收某 node 版本后，`global/corepack/bin` 里指向它的
   yarn/pnpm shim 变悬空；下次装 node 时 `corepack enable` 在 `realpath` 上失败（打一屏栈，安装本身仍成功）。
   修法：`enableCorepack` 前先 `pruneDanglingShims`（只删悬空软链，Windows 的 `.cmd` 自然跳过）。
   验收：`TestPruneDanglingShims`；实测 gc 掉 node@24 后 `install node@22 --corepack` 自动把 yarn 指回 22.23.5。

## 三、发现并已修的限制（1.0.1）

外部清单里 `checksum.urls` 用 `{filename}` 占位符时，`github-releases` / 无文件清单的索引拿不到文件名 →
渲染成空串 → 抓 `.../.sha256` 404 → 报"拿不到校验和（所有源都不可用）"。
修法：`urlVars` 在索引没给文件名时**回落到产物 URL 的末段**（产物名本来就只由 `{version}/{platform}/{ext}` 决定）。
验收：`TestFilenamePlaceholderFallsBackToArtifactName`；实测外部清单 `rg.toml` 用
`{version}/{filename}.sha256` 直接装成（`verified: weak`）。

> 更正：早前写的"`per-artifact` 不写 `checksum.urls` 会自动派生 `<产物URL>.sha256`"是**错的** ——
> 缺 `checksum.urls` 的清单会被判 `PROVIDER_EXTERNAL_INVALID`（工具直接不可见）。

## 四、自动化覆盖

- `make check` 8 项（gofmt+vet / 行数 / race+覆盖率 / 6 平台交叉 / fuzz 30s / bench / golden）——
  **已上云**：`.github/workflows/ci.yml` 的 `check` job；
- GUI 契约：`ci.yml` 的 `gui-contract` job 用真 `pk` 跑 `cmd/pk-desktop/internal/pkcli`；
- 真机装一遍：`smoke.yml` 5/5 组合（macos-15 / macos-15-intel / ubuntu-latest / ubuntu-24.04-arm / windows-latest）；
- CRC 之外的 3 项仍靠人工确认：Windows ARM64 运行时、PowerShell 调 `.cmd` shim、macOS Gatekeeper 首启。

## 五、1.1 两项能力的验证（2026-11-12，v1.1.0）

| 能力 | 验证方式 | 结果 |
|---|---|---|
| 通用伴生包（`[companions.<名>]`） | `TestGenericCompanionInstalled`（两个假 provider：宿主 + 伴生，断言落到 `<宿主>/<target>` 且吐 `companion` 事件）、`TestCompanionPinnedNeedsVersion` | ✅ |
| 上游重发的同源配对校验 | `TestVerifySameOriginRepublish`（同 host 认新摘要 + 别 host 给旧摘要 → weak 并改写 TOFU）、`TestVerifyNoSameOriginStillFails`（只有同源一家之言 → 仍要 `--force-rehash`）；真网回归：外部清单 rg + `pk install node@22` 的 `verify` 事件形状不变 | ✅ |

> 上游重发无法在真网复现（要等上游真去换同一版本的产物），所以这条以单测锁定判据；
> 真网只做"没回归"的确认：`pk install node@22` → `{"ev":"verify","mode":"network","ok":true}`。

## 附：v0.9.0 轮记录（2026-11-11，原文保留）

> 目的：`make check` 只证明"编得过 + 单测绿"，装不装得上、卸不卸得干净必须在真机上跑。
> 这份文件记录**用户视角**的验证结果、发现的问题与修复，以及还没覆盖的部分。

### 一、本机实测（macOS 26 / arm64，隔离 `PK_ROOT`）

全部在 `/tmp/pkv` 里跑（`PK_ROOT=/tmp/pkv/root`），不碰真实 `~/.packnest`。

| # | 项目 | 结果 |
|---|---|---|
| 1 | `install.sh` 全新安装（gitee 直取） | ✅ 0.9.0，sha256 与 `checksums.txt` 一致 |
| 2 | `pk doctor --json` | ✅ `schema:1`、`ok:true`、`PROVIDERS_DIRS` info 正常 |
| 3 | `pk install node@22` | ✅ 22.23.5 + 伴生 npm 12.2.2（自带 10.9.9 被替换） |
| 4 | shim 生效（`pk env` 后 `node -v` / `npm -v`） | ✅ v22.23.5 / 12.2.2，路径都在 `PK_ROOT/bin` |
| 5 | 「6 个工具全装一遍」：node / npm（伴生）/ **pnpm（平台包 23MB，pnpx 也可用）** / bun / go / python（pip 26.3） | ✅ 全部装成并跑通 |
| 5b | **外部 provider（真工具）**：ripgrep 15.2.0，GitHub Releases + 每产物 `.sha256` | ✅ 装成、`rg --version` 可跑、`pk list` 带 `*` 与脚注 |
| 6 | 坏外部清单混入 | ✅ 只记 `PROVIDER_EXTERNAL_INVALID`，`pk list` / `install node` 照常 |
| 7 | `pk install node@24 --corepack` | ✅ `global/corepack/bin` 出现 yarn/yarnpkg/pnpm/pnpx |
| 8 | `yarn --version`（corepack 惰性下载） | ✅ 1.22.22，走 `COREPACK_NPM_REGISTRY`（npmmirror） |
| 9 | `pk use node@24` / `node@22`（大版本说明符） | ✅ 解析到已装版本（见 §二.1，原来报"还没装"） |
| 10 | `pk uninstall --all --purge --yes` | ✅ 3 个运行时全清、`PK_ROOT` 整个删除（零残留） |
| 11 | darwin/amd64 产物（Rosetta 直跑） | ✅ `packnest v0.9.0 (go1.27.3, darwin/amd64)`、doctor 正常 |
| 12 | linux/amd64、linux/arm64 产物结构 | ✅ ELF、**statically linked**（CGO 未泄漏）、mtime 钉在 2020-01-01（可重复构建） |
| 13 | windows/amd64、windows/arm64 产物结构 | ✅ 单 `packnest.exe`、mtime 钉死；`install.ps1` 用 PowerShell 7.4.6 解析器校验 0 错误 + `-DryRun` 两个通道输出正确 |

### 二、发现的问题（本轮已修）

1. **`pk use node@22` 报"还没装"** —— 只认精确版本，大版本/范围说明符直接失败。
   实际用户流程是 `pk install node@22` 再 `pk use node@22`，这条路径必踩。
   修法：`install.Activate` 先 `ResolveInstalled(tool, spec)`（精确命中优先，否则 `spec.Best` 在已装版本里取最优，不联网）；
   `pk use --project` 同样解析。验收：`pk use node@24` → 24.22.3，`pk use node@22` → 22.23.5。
2. **`pk install node@X --corepack` 对"已装版本"不重放** —— 命中"已是最新"就直接返回，corepack 没启用。
   修法：已装分支里同样调用 `enableCorepack`（用户这次显式要了）。验收：删掉 `global/corepack/bin` 后重跑，目录被重建。
3. **`pk help <cmd>` 与 `--help` 的开关说明重复**（同一批 flag 写两遍）。
   修法：help 文本只留语义与示例，开关列表由 `flag` 定义生成（`fs.PrintDefaults()`），
   `pk help <cmd>` 尾部提示 `pk <cmd> --help`；顺带把 4 处过期的 flag 描述（"（M4）"/"（M5 后续）"）更新。

### 二点五、第二轮 review 又抓到的两个 bug（pnpm 平台包引入的回归）

4. **离线重装必失败**：pnpm 的平台包元数据是**每次**都去拉的（在 `verifyArtifact` 里），
   于是 `pk install pnpm --offline` 报「离线模式下禁止访问网络」——TOFU 表里明明记过这个版本。
   修法：TOFU 命中且没给 `--force-rehash` 时不去取元数据。
5. **校验失败会删缓存**：`fetchArtifactVerified` 把任何校验错误都当成"缓存脏了"→ 删掉产物重下；
   离线时重下必然失败，缓存就被永久毁掉（实测复现：一次离线重装后再也装不上）。
   修法：只有**摘要不符**才删/重下，离线与网络失败一律保留缓存。
   回归用例：`internal/install/cache_offline_test.go`（两个）。

### 三、自动化：`.github/workflows/smoke.yml`（只验证，不发布）

**首次跑通：2026-11-11，5/5 组合全绿（workflow conclusion = success）**：
`macos-15` / `macos-15-intel` / `ubuntu-latest` / `ubuntu-24.04-arm` / `windows-latest`。

跑通过程中修掉的 CI 自身问题（都不是产品 bug）：macos-13 已下线（任务永远 queued）、
Linux 上没有 `shasum`（改用 `sha256sum`）、git-bash 的 `sha256sum` 输出带前导 `\`、
git-bash 没有 `/dev/stderr`、`pk doctor` 有 error 级问题时退出码是 1（断言要落盘再 grep）、
**gitee 对海外数据中心 IP 返回 404**（改成从 GitHub Releases 取产物，见下）。

`workflow_dispatch` 或打 tag 时跑，矩阵覆盖 **5/6 个平台组合**，从 GitHub Releases 拉**用户真正会下到的产物**：

| runner | 平台 | 覆盖 |
|---|---|---|
| macos-15 | darwin-arm64 | ✅ |
| macos-15-intel | darwin-amd64 | ✅ |
| ubuntu-latest | linux-amd64 | ✅ |
| ubuntu-24.04-arm | linux-arm64 | ✅ |
| windows-latest | windows-amd64 | ✅（git-bash 不认 `.cmd` shim，只核对台账） |
| — | windows-arm64 | ❌ 无公共 ARM runner，**人工确认** |
