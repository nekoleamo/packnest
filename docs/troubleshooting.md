# 故障排查（含与 proto / nvm / mise 共存）

先跑 `pk doctor`（加 `--json` 给脚本用，`--globals` 查全局包，`--probe` 会发 HEAD 探测镜像）。
问题码的完整表在 [json-schema.md](json-schema.md)。

## 1. 装不上 / 一直失败

| 现象 | 判断 | 处置 |
|---|---|---|
| `网络失败: 索引全部不可用` | 档位选错或代理没生效 | `pk mirror probe --tool node` 看哪一路通；`pk mirror use cn`；`HTTPS_PROXY` 要 export |
| `校验失败: 拿不到…校验和` | 所有校验和源都不可用（首次装、纯内网） | 有内网源就 `PK_MIRROR_<TOOL>_CHECKSUM=<url>`；否则确认产物来源可信后 `--insecure`（会标 unverified） |
| `校验失败: 校验和源之间不一致` | 镜像被污染 **或上游重新发布过产物**（实测 deno v2.9.8） | 报错里会写"产物与哪个源一致"；确认后 `--insecure`，不要盲目加 |
| `校验失败: sha256 与本地记录不一致` | TOFU 表里记的和现在下到的不一样 | 先确认不是镜像问题；`rm ~/.packnest/state/known.json` 会重新 TOFU（有风险，别顺手做） |
| `解压失败（产物可能被截断）` | 缓存里是半截文件 | 已自动删除缓存，重试即可；反复失败就 `pk gc --cache-max 1` 清空缓存 |
| `--frozen` 报不一致 | `pk.toml` 与 `pk.lock` 漂移 | 本地跑一次 `pk install` 更新锁并提交；CI 不解析版本 |

## 2. 命令找不到 / 版本不对

```bash
pk which node          # shim 指到哪个真实二进制
pk list                # * 是当前全局默认
pk use node@22.22.2    # 切默认（只会切到已安装的）
pk env --shell bash    # 看 PATH 三段与 registry 变量
```

- `PATH_ORDER`：`~/.packnest/bin` 必须**在 PATH 最前**，否则会被系统/其他管理器的 node 抢。
- `SHIM_MISSING`：跑一次任意 `pk install`/`pk use` 就会补齐 `pk` 与 `packnest` 两个入口。
- 装了但 `which` 找不到：确认 `eval "$(pk env --shell bash)"` 已生效（`pk env --write` 可写进 rc 托管块）。

## 3. 全局包（`npm i -g`）相关的三个告警

| 码 | 含义 | 处置 |
|---|---|---|
| `GLOBAL_ENGINES_UNMET` | 全局包声明的 `engines.node` 不满足当前 node | 换 node 版本，或升级/降级该全局包 |
| `GLOBAL_NATIVE_ABI` | 含原生模块、无 N-API 预编译 → 换 node 版本会失效 | 换回原版本，或 `npm rebuild <pkg>`（离线，走缓存） |
| `GLOBAL_SHADOWS_PROJECT` | 全局命令名与项目 `node_modules/.bin` 同名 | `npm run` 里不受影响；交互 shell 里用 `./node_modules/.bin/<cmd>` |

全局包挂在 `~/.packnest/global`（`npm_config_prefix`），`npm_config_globalconfig` 也引到了这里，
所以**切 node 版本不会丢全局包，也不会丢 `npm config set -g`**。缓存沿用 `~/.npm`（不迁移不复制）。

## 4. 与 proto / nvm / mise 共存

`pk` 只写 `~/.packnest`，**绝不改别人的目录**。三者都会往 PATH 里塞 shim，所以顺序决定谁生效：

```bash
echo $PATH | tr ':' '\n' | head -5      # 看谁在最前
pk doctor | grep FOREIGN_MANAGER        # 会列出检测到的其他管理器
```

三选一（`pk doctor` 也会这么建议）：

1. **只让 pk 管**：从 shell rc 里去掉对方初始化行（proto 的 `~/.proto/env`、nvm 的 `nvm.sh`）。
2. **只让 pk 管 node**，其他工具继续用对方的：把 `~/.packnest/bin` 放在对方面前。
3. **明确分工**：比如 node 用 pk、rust/go 用 proto —— 但注意 go 两边都管会抢 `GOROOT/PATH`。

`pk doctor` 对 proto 的提示是只读的：**不会删、不会改** `~/.proto` 里的任何东西。
想迁移全局包：`pk doctor --globals` 先看 engines/ABI，再在新 prefix 里 `npm i -g` 一遍（缓存命中，不联网）。

## 5. 断电 / 强杀之后

- 崩溃残留（`tools/*/*.tmp.*`、`*.old`、`cache/archives/*.part`）会在**下一次写操作前**被清扫（只清 24h 以上的）。
- 台账损坏：`pk list` 会自动用 `installs.json.bak` 恢复（`LEDGER_RECOVERED`）；两个都坏才会 `LEDGER_BROKEN`，
  这时删掉 `state/installs.json*` 重装（`tools/` 下的目录**不会**被自动删，确认后手工清理）。
- `another pk is running`（退出码 5）：有另一个 pk 进程持锁，等它结束；确认没有残留进程时可以删 `state/lock`。

## 6. 想彻底回滚

```bash
rm -rf ~/.packnest      # 回到"没装过"，这是"无损"的最强定义（方案 §8.2 第 5 条）
```

卸载单个工具用 `pk uninstall node@22.22.2`（被引用要 `--force`），缓存会保留，离线可重装。
