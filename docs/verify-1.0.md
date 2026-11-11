# 1.0 前实装验证记录（2026-11-11，v0.9.0）

> 目的：`make check` 只证明"编得过 + 单测绿"，装不装得上、卸不卸得干净必须在真机上跑。
> 这份文件记录**用户视角**的验证结果、发现的问题与修复，以及还没覆盖的部分。

## 一、本机实测（macOS 26 / arm64，隔离 `PK_ROOT`）

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

## 二、发现的问题（本轮已修）

1. **`pk use node@22` 报"还没装"** —— 只认精确版本，大版本/范围说明符直接失败。
   实际用户流程是 `pk install node@22` 再 `pk use node@22`，这条路径必踩。
   修法：`install.Activate` 先 `ResolveInstalled(tool, spec)`（精确命中优先，否则 `spec.Best` 在已装版本里取最优，不联网）；
   `pk use --project` 同样解析。验收：`pk use node@24` → 24.22.3，`pk use node@22` → 22.23.5。
2. **`pk install node@X --corepack` 对"已装版本"不重放** —— 命中"已是最新"就直接返回，corepack 没启用。
   修法：已装分支里同样调用 `enableCorepack`（用户这次显式要了）。验收：删掉 `global/corepack/bin` 后重跑，目录被重建。
3. **`pk help <cmd>` 与 `--help` 的开关说明重复**（同一批 flag 写两遍）。
   修法：help 文本只留语义与示例，开关列表由 `flag` 定义生成（`fs.PrintDefaults()`），
   `pk help <cmd>` 尾部提示 `pk <cmd> --help`；顺带把 4 处过期的 flag 描述（"（M4）"/"（M5 后续）"）更新。

## 二点五、第二轮 review 又抓到的两个 bug（pnpm 平台包引入的回归）

4. **离线重装必失败**：pnpm 的平台包元数据是**每次**都去拉的（在 `verifyArtifact` 里），
   于是 `pk install pnpm --offline` 报「离线模式下禁止访问网络」——TOFU 表里明明记过这个版本。
   修法：TOFU 命中且没给 `--force-rehash` 时不去取元数据。
5. **校验失败会删缓存**：`fetchArtifactVerified` 把任何校验错误都当成"缓存脏了"→ 删掉产物重下；
   离线时重下必然失败，缓存就被永久毁掉（实测复现：一次离线重装后再也装不上）。
   修法：只有**摘要不符**才删/重下，离线与网络失败一律保留缓存。
   回归用例：`internal/install/cache_offline_test.go`（两个）。

## 三、自动化：`.github/workflows/smoke.yml`（只验证，不发布）

**首次跑通：2026-11-11，5/5 组合全绿（workflow conclusion = success）**：
`macos-15` / `macos-15-intel` / `ubuntu-latest` / `ubuntu-24.04-arm` / `windows-latest`。

跑通过程中修掉的 CI 自身问题（都不是产品 bug）：macos-13 已下线（任务永远 queued）、
Linux 上没有 `shasum`（改用 `sha256sum`）、git-bash 的 `sha256sum` 输出带前导 `\`、
git-bash 没有 `/dev/stderr`、`pk doctor` 有 error 级问题时退出码是 1（断言要落盘再 grep）、
**gitee 对海外数据中心 IP 返回 404**（改成从 GitHub Releases 取产物，见下）。


`workflow_dispatch` 或打 tag 时跑，矩阵覆盖 **5/6 个平台组合**，从 gitee 拉**用户真正会下到的产物**：

| runner | 平台 | 覆盖 |
|---|---|---|
| macos-15 | darwin-arm64 | ✅ |
| macos-15-intel | darwin-amd64 | ✅ |
| ubuntu-latest | linux-amd64 | ✅ |
| ubuntu-24.04-arm | linux-arm64 | ✅ |
| windows-latest | windows-amd64 | ✅（git-bash 不认 `.cmd` shim，只核对台账） |
| — | windows-arm64 | ❌ 无公共 ARM runner，**人工确认** |

产物来自 **GitHub Releases**（海外主源）：gitee 对海外数据中心 IP 常返回 404（runner 实测），
而两个源是本机同一批构建、sha256 一致——`make verify-release` 逐条核对 gitee 那份。
海外安装：`sh scripts/install.sh --base https://github.com/nekoleamo/packnest/releases/download`。

每个组合都做：sha256 校验 → `--version` → `doctor --json` → `install node@22` → 外部坏清单不影响 CLI
→ `uninstall --all --purge` 后 `PK_ROOT` 必须不存在。

## 四、还没覆盖的（下一轮）

- **windows-arm64 运行时**（无公共 runner）、**Windows `.cmd` shim 被 PowerShell 调用**（项目规则允许人工确认）
- **macOS Gatekeeper 首启**（brew 装出来的包在首次运行时的提示）
- 校验强度：`node@22` 走 cn 档只有 1 个校验源 → 落 `weak`（设计如此，非 bug；要 `strong` 得让官方源也通）
- 缓存不做内容寻址（同版本跨平台重复下载，已在 `docs/progress.md` 记录）
- pnpm 12 的第 4 个下载面：实测 `@pnpm/exe.<os>-<arch>` 在 registry 里有对应版本 + `dist.integrity`，
  可以改成"一次装完、离线可用"，属下一轮的候选任务

## 五、1.0 出口条件（建议）

- [ ] smoke 矩阵 5 个组合全绿（含本轮 workflow 首次跑通）
- [ ] windows-arm64 + Windows shim + Gatekeeper 三项人工确认一次，结果写回本文件
- [ ] 行数预算留出 ≥30 行余量（当前 **10,299 / 10,300，余量 1**，任何改动前必须先砍）
- [ ] `docs/progress.md` 的"已知缺口"里不再有"用户可感知"的条目
