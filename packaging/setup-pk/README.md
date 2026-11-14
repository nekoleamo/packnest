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
