# 故障排查（含与 proto / nvm / mise 共存）

> **中文** · [English](en/troubleshooting.md)

先跑 `pk doctor`（加 `--json` 给脚本用，`--globals` 查全局包，`--probe` 会发 HEAD 探测镜像）。
问题码的完整表在 [json-schema.md](json-schema.md)。

### Windows：`iwr … | iex` 报"远程主机强迫关闭了一个现有的连接"

下载 3MB 的 zip 时连接被重置。上层原因通常是 Windows PowerShell 5.1 的老 TLS 默认值，
或者系统代理对国内长连接不友好。0.7.7 起安装脚本自己扛：

- 显式 `SecurityProtocol = Tls12`
- 每轮先试 **绕过系统代理的 WebClient**，再试系统代理，最后 IWR；失败重试 3 轮（1s/2s 退避）
- 仍然失败 → 提示重试 / 换网络 / 用 `-Base` 指向自托管源（npm 平台包通道已于 2026-11-11 停用）
- `.tgz` 解压用 `tar.exe`（Win10 1803+ 自带），找不到就退回 `tar`

手动兜底（任选）：

```powershell
# 1) 存到本地再跑（便于看报错、可加重试）
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB -OutFile pk.ps1
.\pk.ps1

# 2) 自己下 zip 再用本地静态源装
#    packnest_<版本>_windows_amd64.zip + checksums.txt 放同一目录，起个 http 服务后：
.\pk.ps1 -Base http://127.0.0.1:8000

# 3) 浏览器下载 zip 解压，把 packnest.exe 放进 %LOCALAPPDATA%\packnest\bin，改名 pk.exe
```

## 1. 装不上 / 一直失败

| 现象 | 判断 | 处置 |
|---|---|---|
| `网络失败: 索引全部不可用` | 档位选错或代理没生效 | `pk mirror probe --tool node` 看哪一路通；`pk mirror use cn`；`HTTPS_PROXY` 要 export |
| `校验失败: 拿不到…校验和` | 所有校验和源都不可用（首次装、纯内网） | 有内网源就 `PK_MIRROR_<TOOL>_CHECKSUM=<url>`；否则确认产物来源可信后 `--insecure`（会标 unverified） |
| `校验失败: 校验和源之间不一致` | 镜像被污染 **或上游重新发布过产物** | 报错里会写"产物与哪个源一致"；确认后 `--insecure`，不要盲目加 |
| `校验失败: sha256 与本地记录不一致` | TOFU 表里记的和现在下到的不一样（上游重新发布过、或表被写脏） | 先确认不是镜像问题，再加 `--force-rehash`（重新向校验源求值并覆盖这一条；等价于只重算这一个版本，不用删整张表） |
| `解压失败（产物可能被截断）` | 缓存里是半截文件 | 已自动删除缓存，重试即可；反复失败就 `pk gc --cache-max 1` 清空缓存 |
| `--frozen` 报不一致 | `pk.toml` 与 `pk.lock` 漂移 | 本地跑一次 `pk install` 更新锁并提交；CI 不解析版本 |

### Windows：`pk env --write` 之后新开 PowerShell 还是不认识 `node`

两个坑，都已在 0.7.3 修掉（旧版本手动处理见下）：

1. **写错了文件**：PowerShell 有两套互不相干的 profile——
   `Documents\WindowsPowerShell\…`（系统自带的 5.1）和 `Documents\PowerShell\…`（pwsh 7）。
   旧版只写 7 那一份，5.1 里永远不生效。现在按环境判断版本，判不出来就**两份都写**；
   `Documents` 被 OneDrive 重定向时也写进 OneDrive 那份。
2. **语法写错了**：Windows 上没有 `$SHELL`，旧版把 shell 判成 bash，于是把 `export PATH=…`
   写进了 `~/.bashrc`——Windows 上没人会读这个文件。现在 Windows 默认按 PowerShell 处理
   （Git Bash 里 `$SHELL` 有值，仍按 bash 写）。

自查：

```powershell
pk env --shell powershell                 # 先看片段对不对（PATH 里应有 ~\.packnest\bin）
pk env --write                            # 再写；输出会列出实际写入的每个 profile
$PROFILE                                  # 确认是哪一个 profile 在生效
Test-Path $PROFILE                        # 以及文件是否真的存在
```

### Windows：profile 存在但每个 shell 启动都报"禁止运行脚本"

`PowerShellScriptsBlocked` 会先看执行策略。`Restricted` / `AllSigned` 会拒绝加载**未签名**的
profile 脚本（我们写的正是这种），表现是每次开窗口都弹：

```
无法加载文件 …\Microsoft.PowerShell_profile.ps1，因为在此系统上禁止运行脚本。
```

0.7.4 起的行为：

- `pk env --write` 先判断策略；**被禁时不写 profile**（写了只会每次报错），并清掉旧块；
- PATH 改走**注册表里的用户 Path**（`HKCU\Environment`），不受执行策略影响，cmd / PowerShell /
  Git Bash 都认——`node` 立刻可用；
- 旧 Path 备份在 `<PK_ROOT>\state\user-path.bak.txt`，撤销用 `pk env --unset`。

如果你还想让 `npm_config_registry` / `GOPROXY` 这些也常驻（它们只能放在 profile 里），二选一：

```powershell
# 1) 放开当前用户的脚本策略（不需要管理员；只允许本地脚本，下载来的仍需签名）
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned

# 2) 不改策略，每次会话手动接一次
pk env --shell powershell | Invoke-Expression
```

旧版本手动处理（任选其一）：`pk env --write --shell powershell` 直到输出里出现 `Documents\WindowsPowerShell\…`；
或者直接把 `%USERPROFILE%\.packnest\bin` 加进「系统属性 → 环境变量 → Path」。

## 2. 命令找不到 / 版本不对

```bash
pk which node          # shim 指到哪个真实二进制
pk list                # * 是当前全局默认
pk use node@22.22.2    # 切默认（只会切到已安装的）
pk env --shell bash    # 看 PATH 五段与 registry 变量
```

- `PATH_ORDER`：`~/.packnest/bin` 必须**在 PATH 最前**，否则会被系统/其他管理器的 node 抢。
- `SHIM_MISSING`：跑一次任意 `pk install`/`pk use` 就会补齐 `pk` 与 `packnest` 两个入口。
- 装了但 `which` 找不到：确认 `eval "$(pk env --shell bash)"` 已生效（`pk env --write` 可写进 rc 托管块）。

## 2.5 外部 provider 清单不生效

```bash
pk doctor --json | grep -A3 PROVIDER_     # 哪个文件、什么原因
pk list --available                       # 外部工具带 * 号
ls ~/.packnest/providers                  # 文件名必须等于 id（zig.toml ↔ id = "zig"）
```

| 码 | 常见原因 | 处置 |
|---|---|---|
| `PROVIDER_EXTERNAL_INVALID` | 文件名与 `id` 不符 / URL 不是 https 且没设 `PK_ALLOW_HTTP=1` / `layout.single` 是绝对路径或含 `..` / 用了 `index.kind = "pbs"` / 模板里有未知占位符 / 文件超过 256KB | 按 `doctor` 给的原文改；`PK_ALLOW_HTTP=1` 放行的 http 源最多判 `weak` |
| `PROVIDER_EXTERNAL_CONFLICT` | 与内嵌清单（或先加载的外部清单）同 id | 内嵌永远赢，改个新 id；清单按文件名排序，先加载的生效 |
| `PROVIDER_HTTP_URL` | http 源已被 `PK_ALLOW_HTTP=1` 放行 | 能换 https 就换；否则接受最高 `weak` 的校验强度 |

外部清单出问题**不会**让 `pk install node` 之类失败：坏文件被跳过、只记 issue。
`PK_PROVIDERS_DIR` 是**追加**语义（可多个目录，用 `os.PathListSeparator` 分隔），不支持指向 `http(s)://`。

## 2.6 pnpm 装不上

`pnpm` 的产物是 registry 上的平台包 `@pnpm/exe.<os>-<arch>`（真二进制）。两个已知坑：

- **`pk install pnpm@10` / `@9` 会 404**：平台包从 `11.0.0-rc.3` 才开始发布。老版本请用
  `corepack`（`pk install node@22 --corepack` 后 `corepack prepare pnpm@10 --activate`）或 `npm i -g pnpm@10`。
- **重装同一版本报「与本地记录不一致」**：如果你在这台机器上之前用旧方式（JS tarball）装过该版本，
  TOFU 表里是旧产物的摘要。用 `pk install pnpm@<版本> --force-rehash` 覆盖一次即可。

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

### Windows：`pk uninstall --all --self` 之后 pk.exe 还在

Windows 不允许删正在运行的程序，所以 `--self` 不是"立刻删"，而是**安排一个分离进程延时删**：
起一个隐藏的 `cmd.exe`，等约 3 秒（此时 pk 已退出、文件句柄释放），再删掉安装目录里的
`pk.exe` / `packnest.exe`，目录空了再一并移除。**只删这两个文件，绝不 `rd /s` 整个目录**
（你可能用 `-Dir` 指到了共享目录，比如 `C:\Tools\bin`）。

**为什么 0.7.8 那次没删掉**：旧实现把 `ping … & del "路径" & rd "路径"` 整串交给
`cmd.exe /c`，而 Go 会按 Windows 规则给带空格的参数加引号、把内部引号转义成 `\"…\"`，
cmd.exe 不认识反斜杠转义 —— 命令行被拼坏，cmd.exe 什么都没做（错误还被 `>nul` 吞了）。
0.7.9 起改成把命令写成 `%TEMP%\packnest-self-remove-*.cmd`，只把**文件路径**交给 cmd.exe。

排查有没有真的执行：看 `%TEMP%\packnest-self-remove.log`（每次会写一行 REMOVED / FAILED）。

如果 3 秒后还在（极少见，通常是某个进程仍占着文件）：

```powershell
# 确认没人占用（关掉所有 PowerShell / Windows Terminal 窗口）
Get-Process pk, packnest -ErrorAction SilentlyContinue
# 手动删
Remove-Item -Force "$env:LOCALAPPDATA\packnest\bin\pk.exe","$env:LOCALAPPDATA\packnest\bin\packnest.exe"
Remove-Item "$env:LOCALAPPDATA\packnest\bin" -ErrorAction SilentlyContinue   # 目录空了才删
```

另外，`install.ps1` 会把**安装目录**写进用户 Path（和 `pk env --write` 写的五段 `~/.packnest`
目录不是一回事）。0.7.7 起 `uninstall --all` 会把这两拨一起从注册表用户 Path 里摘掉，
不会再留一个指向空目录的死项。

## 6. 想彻底回滚

```bash
rm -rf ~/.packnest      # 回到"没装过"，这是"无损"的最强定义（方案 §8.2 第 5 条）
```

卸载单个工具用 `pk uninstall node@22.22.2`（被引用要 `--force`），缓存会保留，离线可重装。
