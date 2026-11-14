# setup-packnest（GitHub Action）

在 CI 里装 `pk` 并按 `pk.toml` 装好项目工具链，**装一次，缓存后面全命中**。

```yaml
- uses: nekoleamo/setup-pk@v1
  with:
    version: 1.7.0
    installer-sha256: 9f2c…（见下）
- run: node -v && pnpm -v      # 版本都来自你的 pk.toml
```

## 输入

| 输入 | 默认 | 说明 |
|---|---|---|
| `version` | `1.7.0` | packnest（`pk` CLI）的版本，不带 `v` |
| `installer-sha256` | **必填** | 该版本 `scripts/install.sh` 的 sha256 |
| `install-dir` | `$RUNNER_TOOL_CACHE/pk/<ver>/bin` | pk 装到哪 |
| `cache` | `true` | 是否缓存 `PK_CACHE` |
| `cache-dir` | `$RUNNER_TEMP/pk-cache` | 缓存目录（只放 Archives/meta/corepack/npm 缓存） |
| `cache-key-extra` | 空 | 追加到缓存 key（例如别的清单文件 hash） |
| `skip-verify` | `false` | 透传安装脚本 `--skip-verify`（不推荐） |
| `install` | `true` | 装完 pk 后是否立刻 `pk install` |
| `working-directory` | `.` | 读 `pk.toml` / `pk.lock` 的目录 |

输出：`pk-version`、`cache-hit`。

## 怎么算这个 `installer-sha256`

安装脚本会随每个 tag 发布，值必须**跟着版本换**：

```bash
V=1.7.0
curl -fsSL "https://gitee.com/null_593_5354/packnest/raw/v${V}/scripts/install.sh" | shasum -a 256
# 输出就是 installer-sha256 的值（也记录在仓库 docs/publishing.md 的发版清单里）
```

实例（v1.6.0 的值）：

```bash
$ curl -fsSL "https://gitee.com/null_593_5354/packnest/raw/v1.6.0/scripts/install.sh" | shasum -a 256
f4cbfab385f94bd15d4cbbb5345a2ed37e4ef71a7024179dbec6827283a6729c
```

为什么自己 pin 而不是"信任 https"：action 会**执行**这段脚本。pin 住 tag 的内容，镜像站就算被换包也执行不了
别人的脚本 —— 这与 pk 对产物的 sshsig 签名是同一个威胁模型（`pk` 二进制本身仍会被安装脚本验签）。

## 缓存怎么命中

key = `pk-cache-<os>-<arch>-<version>-<pk.toml/pk.lock/package.json 的 hash>-<cache-key-extra>`，
restore-keys 前缀回退到"同 os/arch/version 的最近一份"。

- **第一次**：未命中 → 下载产物、校验、解压，缓存目录写满；
- **第二次往后**：命中 → `pk install` 读缓存里的归档与索引，**零网络**（可离线跑）；
- `pk.toml` / `pk.lock` 一改 key 就变（清单变了必须重新解析，不能吃旧缓存）。

缓存只挂 `PK_CACHE` 是有意的：台账（`state/`）、shim（`bin/`）、锁（`pk.lock`）每次都重新算，
否则会出现"缓存里的台账说装了 node@20，清单写着 22"这种互相矛盾的状态。

## 示例

| 文件 | 用途 |
|---|---|
| `examples/ci.yml` | 项目里的日常用法（装完直接跑测试） |
| `examples/self-test.yml` | 本 action 自己的验收：**冷缓存 → 热缓存** 两条路径，命中时断言零网络 |

## 已知边界

- 只有 `linux/macOS`（跑 bash）与 `windows`（Git Bash）三种 runner；别的系统自己 `sh install.sh`；
- `pk install --frozen` 需要仓库里提交了 `pk.lock`（没提交就按 `pk.toml` 解析）；
- 这个 action **不替你做** `npm ci` / `pnpm install`：它只保证"运行时与包管理器版本正确"，依赖安装仍是你自己的事。

## 建仓步骤（一次性，约 2 分钟）

这个目录是给**独立仓库** `nekoleamo/setup-pk` 用的；packnest 仓库只留源文件。

**最省事：一条命令**（幂等，可重复跑；会自己算好 pin 值）：

```bash
sh packaging/setup-pk/publish.sh --dry-run   # 先看它要做什么
sh packaging/setup-pk/publish.sh             # 建仓 + 推四件 + 设变量 + 打 v1
```

它默认取**最近一个 `v*` tag**（现在是 `1.6.0`）并用那个 tag 上 `scripts/install.sh` 的 sha256 做 pin；
要换版本：`sh packaging/setup-pk/publish.sh --version 1.7.0`。

**手动做**的话是下面四步：

```bash
# 1) 建空仓库（public，Action 必须 public 才能被外部仓库引用）
gh repo create nekoleamo/setup-pk --public \
  --description "Install packnest (pk) and the toolchain declared in pk.toml"

# 2) 只把这四件推过去（不要带 packnest 的整棵树）
work=$(mktemp -d) && cd "$work" && git init -q
cp <packnest>/packaging/setup-pk/action.yml .
cp <packnest>/packaging/setup-pk/README.md .
mkdir -p .github/workflows && cp <packnest>/packaging/setup-pk/examples/self-test.yml .github/workflows/
cp <packnest>/packaging/setup-pk/examples/ci.yml examples-ci.yml   # 示例，不放 workflows
git add -A && git commit -qm "setup-packnest action" && git branch -M main
git remote add origin git@github.com:nekoleamo/setup-pk.git && git push -q -u origin main

# 3) 仓库变量（Settings → Variables，或 gh api）：self-test 会用它们
gh variable set PK_VERSION --repo nekoleamo/setup-pk --body 1.7.0
gh variable set PK_INSTALLER_SHA256 --repo nekoleamo/setup-pk \
  --body "$(curl -fsSL https://gitee.com/null_593_5354/packnest/raw/v1.7.0/scripts/install.sh | shasum -a 256 | cut -d' ' -f1)"

# 4) 打 tag 让外部仓库能按主版本引用（`uses: nekoleamo/setup-pk@v1`）
git tag -a v1 -m "setup-packnest v1" && git push -q origin v1
```

**验收（DoD）**：仓库里 `self-test` 工作流跑绿 —— `cold` job 断言 `cache-hit=false` 且出现 `download` 事件，
`warm` job 断言 `cache-hit=true`、清掉工具树后重装出现 `cache` 事件（命中缓存 = 零网络）。

> 为什么 `installer-sha256` 用仓库变量而不是写死在 action 里：变量可以按版本改，而 action 代码不变；
> 且换版本时 pin 值必须跟着换（tag 下的脚本内容变了），写死会静默失效。
