# pk 操作手册

> **中文** · [English](en/cli.md)
>
> 适用于 **packnest v0.7.x**。所有示例都能直接复制执行；`$` 开头是命令，其余是输出示例。
> 命令面固定 9 个：`install` `uninstall` `use` `list` `which` `mirror` `doctor` `env` `gc`，另有 `version` `help`。

## 目录

- [0. 一分钟上手](#0-一分钟上手)
- [1. install — 安装运行时](#1-install--安装运行时)
- [2. list — 查看已装与可装](#2-list--查看已装与可装)
- [3. which — 命令到底是谁](#3-which--命令到底是谁)
- [4. use — 切全局 / 固定到项目](#4-use--切全局--固定到项目)
- [5. uninstall — 卸载](#5-uninstall--卸载)
- [6. mirror — 镜像档](#6-mirror--镜像档)
- [7. doctor — 环境体检](#7-doctor--环境体检)
- [8. env — shell 环境](#8-env--shell-环境)
- [9. gc — 回收](#9-gc--回收)
- [10. version / help](#10-version--help)
- [11. 全局开关与退出码](#11-全局开关与退出码)
- [12. 常见任务](#12-常见任务)
- [13. 故障排查](#13-故障排查)

---

## 0. 一分钟上手

```bash
# 选镜像档（国内一次就够，之后自动判环境）
$ pk mirror use cn
镜像档已切到 cn（写入 /Users/you/.packnest/config.toml）

# 装 node，自动配对最新兼容的 npm
$ pk install node@22
解析 node@22.23.4
下载 node@22.23.4：47.7 MB
校验通过 node@22.23.4（network）
下载 npm@12.2.2：2.9 MB
伴生包 npm@12.2.2（replaced 11.20.9）
装好 node@22.23.4 → /Users/you/.packnest/tools/node/22.23.4 (npm 12.2.2 ← 自带 11.20.9)  （已设为全局默认）

# 让 PATH 生效（一次）
$ pk env --write
已写入 /Users/you/.zshrc 的托管块（块外内容未改动；备份在 /Users/you/.zshrc.pk.bak）
$ source ~/.zshrc

$ node -v && npm -v
v22.23.4
12.2.2
```

---

## 1. install — 安装运行时

```bash
pk install [工具[@版本]]... [--dry-run] [--frozen] [--insecure] [--force] [--quiet]
```

### 1.1 版本写法

| 写法 | 含义 | 示例 |
|---|---|---|
| `node@22.22.2` | 精确版本 | `pk install node@22.22.2` |
| `node@22` | 22.x 最新补丁 | 装到 22.23.4 |
| `node@latest` | 最新稳定版 | |
| `node@lts` | 最新 LTS | |
| `node` | 取 `pk.toml` 里 pin 的；没有则最新稳定 | |

```bash
$ pk install node@22.22.2
解析 node@22.22.2
下载 node@22.22.2：47.1 MB
校验通过 node@22.22.2（network）
装好 node@22.22.2 → /Users/you/.packnest/tools/node/22.22.2 (npm 12.2.2 ← 自带 11.19.0)
```

### 1.2 支持的 7 个工具

| 工具 | 说明 |
|---|---|
| `node` | 装完自动把自带 npm 换成「最新兼容版」（按 `engines.node` 反查，排除预发布） |
| `npm` | 一般不用单独装，它是 node 的伴生包 |
| `pnpm` | 需要 node：`pk install node` 之后再装（我们不会隐式装运行时） |
| `bun` / `deno` | 自带运行时的单二进制 |
| `go` | 装完 `pk env` 会带 `GOTOOLCHAIN=local`（否则 go 自己会再下一套工具链） |
| `python` | 只装 python-build-standalone 预编译版，永不源码编译 |

```bash
$ pk install pnpm            # 没装 node 时
pk: pnpm@ 安装失败: pnpm 需要 node：先 `pk install node` 再装 pnpm（我们不隐式安装运行时）
```

### 1.3 先看会做什么（不下载）

```bash
$ pk install go@1.27.3 --dry-run
镜像判定 go: cn (132ms)
解析 go@1.27.3
  产物 https://mirrors.aliyun.com/golang/go1.27.3.darwin-arm64.tar.gz
  校验 index-inline(tofu-then-verify)
计划：go@1.27.3（tar.gz）
```

### 1.4 离线安装（缓存命中 = 0 网络）

```bash
$ pk uninstall node@22.23.4 && pk --offline install node@22.23.4
缓存命中 node@22.23.4（47.7 MB，0 网络）
校验通过 node@22.23.4（tofu）
装好 node@22.23.4 → /Users/you/.packnest/tools/node/22.23.4
```

### 1.5 按项目安装 / CI 严格安装

```bash
$ cat pk.toml
[tools]
node = "22"
npm  = "auto"

$ pk install              # 按 pk.toml 装，并生成/更新 pk.lock
解析 node@22.23.4
装好 node@22.23.4 → …/tools/node/22.23.4 (npm 12.2.2 ← 自带 11.20.9)
已更新 pk.lock（node@22.23.4, npm@12.2.2）

$ pk install --frozen     # CI：严格按锁，不做解析
$ pk.toml 改成 24 后再 --frozen
pk: 清单与 pk.lock 不一致（--frozen 不做解析）：
  node 锁的是 22.23.4，不满足 pk.toml 的 "24"
建议：本地 `pk install` 更新锁并提交
```

### 1.6 其他开关

```bash
pk install node@22 --force      # 重装已存在的版本
pk install node@22 --quiet      # 只报错，不打印进度
pk install node@22 --insecure   # 允许无校验安装（标 unverified，不写 TOFU 表）
pk install node@22 --json       # NDJSON 事件流（GUI/脚本用）
```

---

## 2. list — 查看已装与可装

```bash
$ pk list
已装:
  * node   22.23.4      252.3 MB (npm 12.2.2 ← 自带 11.20.9)
    node   20.18.1      238.1 MB (npm 11.21.2 ← 自带 10.8.2)
  * pnpm   12.18.0       12.4 MB
```

`*` 表示当前**全局默认**版本。

```bash
$ pk list --available                 # 各工具最新可装版本（索引 24h 缓存，离线可读）
可装版本（档位 auto；缓存 24h，离线读缓存）:
  bun     1.4.4       近几个: 1.4.4, 1.4.3, 1.4.2, 1.4.1, 1.4.0
  deno    2.9.8       近几个: 2.9.8, 2.9.7, 2.9.6, 2.9.5, 2.9.4
  go      1.27.3      近几个: 1.27.3, 1.27.2, 1.27.1, 1.27.0, 1.26.10
  node    22.23.4     近几个: 26.13.0, 24.15.0, 22.23.4, …
  python  3.13.18     近几个: 3.13.18, 3.13.17, …

$ pk list --available --refresh        # 忽略缓存，强制回源
$ pk list --json                      # 机器可读（schema 1）
$ pk list --available --json
```

---

### 2.1 只看一个工具的全部版本

```bash
$ pk list node
node：可装 877 个版本（索引最新 26.13.0 · lts 26.13.0）
  已装 * 22.23.4
  可装   26.13.0 (Lithium) · 26.12.1 · 26.12.0 · …（默认列最近 20 个）
          …还有 857 个（pk list node --all 看全部）
  想装某个版本：pk install node@<版本>
```

`✓` 表示这个版本本机已装，`*` 是当前全局默认；括号里是上游给的 LTS 代号。
`--all` 不截断；`--json` 给出 `{tool, latest, lts, total, installed[], available[]}`。

> 索引里 npm/pnpm 走的是 registry 的 **abbreviated 元数据**（约 2MB，而不是 25MB），
> 单次索引拉取有 45s 总超时——慢源上会明确报"索引拉取超时"，不会再像以前那样一直挂着。

## 3. which — 命令到底是谁

```bash
$ pk which node
/Users/you/.packnest/tools/node/22.23.4/bin/node

$ pk which pk          # 与 packnest 必须解析到同一个文件
/Users/you/.packnest/bin/pk
$ pk which packnest
/Users/you/.packnest/bin/pk

$ pk which npm
/Users/you/.packnest/tools/node/22.23.4/bin/npm

$ pk which nope
pk: 命令 "nope" 不在 /Users/you/.packnest/bin（`pk list` 看已装工具，`pk env` 检查 PATH）
```

---

## 4. use — 切全局 / 固定到项目

### 4.1 切全局默认

```bash
$ pk use node@20.18.1
已切到 node@20.18.1（全局默认写进 /Users/you/.packnest/config.toml，shim 已重指）
$ node -v
v20.18.1
```

只会切到**已安装**的版本：

```bash
$ pk use node@18.20.4
pk: node@18.20.4 还没装（`pk list` 看已装版本，先 `pk install node@18.20.4`）
```

### 4.2 固定到项目（不影响全局）

```bash
$ cd ~/work/my-app
$ pk use node@22.22.2 --project
已固定到项目：node = "22.22.2"（写入 pk.toml，并更新 pk.lock）
项目内生效：echo 'eval "$(pk env --project)"' > .envrc && direnv allow
```

`pk.toml` 变成：

```toml
[tools]
node = "22.22.2"
```

### 4.3 只刷新锁（不切版本、不装东西）

```bash
$ pk use --lock
已更新 pk.lock（node@22.22.2, npm@12.2.2）
```

> 用 `latest` / `lts` 这类会漂移的别名时，务必跑一次 `pk use --lock` 把具体版本钉住，否则 CI 里 `--frozen` 会拒绝别名。

---

## 5. uninstall — 卸载

```bash
$ pk uninstall node@20.18.1
pk: node@20.18.1 仍被引用（global:），确需删除加 --force

$ pk uninstall node@20.18.1 --force
已卸载 node@20.18.1（缓存保留，离线可重装）
```

- 只删台账里记录过的目录：不在台账里的目录**只提示不删**
- 删掉的是默认版本时，会自动切到剩下最新的版本并重指 shim
- 缓存保留 → 重装是 0 网络

---

## 6. mirror — 镜像档

```bash
$ pk mirror list
当前档位: cn
    default  官方源              https://registry.npmjs.org
      境外直连，无代理时可能不可达
  * cn       npmmirror（阿里）    https://registry.npmmirror.com
      国内默认；npm 生态唯一完整镜像
    tuna     清华 TUNA           https://registry.npmmirror.com
    …
  (auto 表示自动探测：普通命令只探首选 + 上轮最优，结果缓存 6h)

$ pk mirror probe               # 并行探测各工具索引源（结果缓存 6h）
  OK     132ms https://registry.npmmirror.com/-/binary/node/index.json
  OK     211ms https://registry.npmmirror.com/node
  FAIL     0ms https://nodejs.org/dist/index.json
       └ 网络请求失败: context deadline exceeded

$ pk mirror use cn              # 切档（写 config.toml）
$ pk mirror use auto            # 自动：国外走官方、国内走镜像（结果缓存 6h）
$ pk mirror probe --tool node   # 只探某个工具
$ pk mirror write               # 把 registry 写进 ~/.npmrc（先备份、只改 registry= 一行）
```

**档位与 `auto`**：`auto` 会在第一次安装时先探官方索引（500ms HEAD），能用就用官方（国外不会多花一次请求），不能用才回落国内档。

---

## 7. doctor — 环境体检

```bash
$ pk doctor
  [info ] MIRROR_AUTO           镜像档为 auto：首次安装会探测并缓存 6h
  [warn ] PATH_ORDER            /Users/you/.packnest/bin 不在 PATH 最前
  [warn ] SHIM_MISSING          缺少 packnest 入口（`pk install` 会补齐）
  [info ] FOREIGN_MANAGER       检测到 /Users/you/.proto；两者都管 node 时请明确优先级（我们不自动改别人目录）
```

```bash
$ pk doctor --json               # 机器可读（issues 数组 + severity 决定退出码）
$ pk doctor --globals            # 全局包三级检测
$ pk doctor --globals --probe    # 额外用当前 node 逐个 dlopen 试加载（会执行包代码）
$ pk doctor --foreign            # 列出 proto/nvm/volta/asdf/mise/fnm 的 node 版本 + PATH 抢占告警
$ pk doctor --probe              # 探测镜像可达性（MIRROR_UNREACHABLE）
```

常见问题码：

| code | 含义 | 处置 |
|---|---|---|
| `PATH_ORDER` / `PATH_MISSING` | bin 目录不在最前 / 不在 PATH | `pk env --write && source ~/.zshrc` |
| `LEDGER_BROKEN` / `LEDGER_RECOVERED` | 台账损坏 / 已从 `.bak` 恢复 | 前者需手工修，后者无需处理 |
| `MIRROR_UNREACHABLE` | 索引源不可达 | `pk mirror use cn` 或 `PK_MIRROR` 指内网 |
| `GO_TOOLCHAIN_NOT_LOCAL` | 装了 go 但 `GOTOOLCHAIN != local` | `eval "$(pk env --shell bash)"` |
| `GLOBAL_ENGINES_UNMET` | 全局包 `engines.node` 不满足当前 node | 换 node 版本或升级该包 |
| `GLOBAL_NATIVE_ABI` | 全局包含原生模块（无 N-API 预编译） | 切回原版本或 `npm rebuild <pkg>` |
| `GLOBAL_SHADOWS_PROJECT` | 全局命令遮蔽项目 `node_modules/.bin` | 用 `./node_modules/.bin/<cmd>` |
| `FOREIGN_SHIM_PRIORITY` | 别的管理器 shim 排在 pk 前面 | 把 `~/.packnest/bin` 移到 PATH 最前 |

---

## 8. env — shell 环境

```bash
$ pk env --shell bash
export PATH=/Users/you/.packnest/bin:/Users/you/.packnest/global/bin:…:$PATH
export npm_config_prefix=/Users/you/.packnest/global
export npm_config_globalconfig=/Users/you/.packnest/global/etc/npmrc
export npm_config_cache=/Users/you/.npm
export npm_config_registry=https://registry.npmmirror.com
export PNPM_HOME=/Users/you/.packnest/global/pnpm
export BUN_INSTALL=/Users/you/.packnest/global/bun
export GOTOOLCHAIN=local
export GOPROXY=https://goproxy.cn
```

```bash
$ pk env                                          # 只打印（默认 bash 方言；自动跟随 $SHELL）
$ pk env --shell zsh | source /dev/stdin          # 临时生效
$ eval "$(pk env --shell bash)"                   # 临时生效（推荐写法）
$ pk env --write                                  # 写进 rc 的托管块（幂等，块外内容不动，先备份 .pk.bak）
$ pk env --write --rc ~/.config/fish/config.fish  # 指定文件
$ pk env --unset                                  # 删掉托管块
$ pk env --completion zsh                         # 输出补全脚本
$ eval "$(pk env --completion zsh)"               # 补全生效（bash/powershell 同理）
$ pk env --project                                # 项目级：把 pk.lock 锁定的版本前置到 PATH
$ pk env --json                                   # 机器可读
```

项目级生效（推荐 direnv）：

```bash
$ cd ~/work/my-app
$ echo 'eval "$(pk env --project)"' > .envrc
$ direnv allow
$ node -v            # 用的是这个项目 pk.lock 里锁的版本
```

---

## 9. gc — 回收

```bash
$ pk gc --dry-run
将回收（dry-run） 1 个零引用版本、0 个缓存文件，释放约 238.1 MB
  版本 node@20.18.1
保留（仍被引用）：2 项
  node@22.23.4 (global)
  pnpm@12.18.0 (global)

$ pk gc                          # 真删零引用版本 + 把缓存压到 cache_max（默认 5GB，LRU）
$ pk gc --cache-max 20G          # 临时放宽缓存上限
$ pk gc --json
```

- 只删台账里的记录；被引用的（全局默认 / 项目锁 / 工具依赖）一律保留
- 缓存按最近使用时间（`state/cache.json`）淘汰
- 启动时还会自动清扫崩溃残留（`*.tmp.*` / `*.old` / `*.part`，只清 24h 以上的）

---

## 10. version / help

```bash
$ pk --version
packnest v0.7.0 (go1.27.3, darwin/arm64)

$ pk --json version
{
  "schema": 1,
  "name": "packnest",
  "version": "0.7.0",
  "go": "go1.27.3",
  "os": "darwin",
  "arch": "arm64"
}

$ pk help                    # 总览
$ pk help install            # 单个命令详解
$ pk install --help          # 同上
```

---

## 11. 全局开关与退出码

| 开关 | 作用 |
|---|---|
| `--json` | 机器可读输出；`install` 是 NDJSON 事件流（`resolve`/`cache`/`download`/`verify`/`companion`/`done`） |
| `--offline` | 禁止一切网络访问（有缓存就能装） |
| `--yes` | 不交互确认 |
| `--root DIR` | 覆盖 `PK_ROOT`（调试/隔离用） |

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 通用失败（含"已被引用需 `--force`"） |
| `2` | 用法错误 |
| `3` | 网络失败（含探测全挂） |
| `4` | 校验失败（哈希不符、缺校验和、源之间矛盾） |
| `5` | 另一个 pk 正在运行（全局锁被占用） |

---

## 12. 常见任务

### 12.1 新机器（国内）从零到可用

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh          # 推荐：gitee 直取，不访问 GitHub
# Windows: iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex
# 已经在用 npm 管工具链的话也可以：npm i -g packnest

pk mirror use cn
pk install node@22
pk env --write && source ~/.zshrc
node -v && npm -v && pnpm -v   # pnpm 需要先 pk install pnpm
```

### 12.2 项目里锁定版本（入 CI）

```bash
cd my-app
pk install node@22            # 先装
pk use node@22.22.2 --project # 固定到 pk.toml + pk.lock
git add pk.toml pk.lock && git commit -m "chore: pin node"
# CI：
pk install --frozen
```

### 12.3 项目里自动用项目版本（direnv）

```bash
echo 'eval "$(pk env --project)"' > .envrc && direnv allow
```

### 12.4 切一套 node 版本做兼容性测试

```bash
for v in 18 20 22 24; do pk install node@$v; done
pk use node@18 && node -v
pk use node@24 && node -v
pk gc --dry-run                       # 看哪些版本已零引用可以清掉
```

### 12.5 内网/自建镜像

```bash
export PK_MIRROR_REGISTRY=https://nexus.local/repository/npm/
export PK_MIRROR_NODE_INDEX=https://nexus.local/node/index.json
export PK_MIRROR_NODE_ARTIFACT=https://nexus.local/node/v{version}/node-v{version}-{platform}.{ext}
export PK_MIRROR_NODE_CHECKSUM=https://nexus.local/node/v{version}/SHASUMS256.txt
pk install node@22
```

### 12.6 纯离线安装（先把产物带进来）

```bash
# 联网机器：装一次 → 复制缓存目录
pk install node@22
tar czf packnest-cache.tgz -C ~/.packnest cache

# 离线机器：解回缓存后 --offline 安装
tar xzf packnest-cache.tgz -C ~/.packnest
pk --offline install node@22
```

### 12.7 脚本里判断结果

```bash
if pk install node@22 --quiet; then
  echo "装了 $(pk which node)"
else
  case $? in
    3) echo "网络问题：pk mirror probe" ;;
    4) echo "校验问题：可能镜像被污染或上游重发过产物" ;;
    5) echo "有别的 pk 在跑，稍后重试" ;;
  esac
fi
```

---

## 13. 故障排查

完整版见 [troubleshooting.md](troubleshooting.md)，这里是最常见的几条：

| 现象 | 处置 |
|---|---|
| 装完 `node` 命令不存在 | `pk env --write && source ~/.zshrc`；`pk which node` 确认 |
| `网络失败: 索引全部不可用` | `pk mirror probe`；`pk mirror use cn`；检查 `HTTPS_PROXY` |
| `校验失败: 拿不到…校验和` | 内网用 `PK_MIRROR_<TOOL>_CHECKSUM`；确认可信后 `--insecure` |
| `校验失败: 校验和源之间不一致` | 报错会写明"产物与哪个源一致"；确认后 `--insecure`（上游重发过产物也会这样） |
| `--frozen` 报不一致 | 本地跑 `pk use --lock` 或 `pk install` 更新锁并提交 |
| `另一个 pk 正在运行` | 等它结束；确认无残留可删 `~/.packnest/state/lock` |
| 想彻底回滚 | `rm -rf ~/.packnest` —— 回到"没装过" |
| 与其他管理器抢命令 | `pk doctor --foreign`；把 `~/.packnest/bin` 放到 PATH 最前 |
