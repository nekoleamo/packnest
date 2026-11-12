# 外部 provider 清单：字段级说明

> 内嵌清单只有 6 个工具（node / npm / pnpm / bun / go / python）。想装 zig / uv / jdk / ripgrep 这类长尾，
> 自己写一份 TOML 丢进 `$PK_ROOT/providers/`（或 `PK_PROVIDERS_DIR` 指的目录）即可 —— **清单是纯数据，不执行任何代码**。
>
> 安全边界（https 强制、路径不许 `..`、≤256KB 等）见 [README 的「外部 provider 清单」](../README.md)；
> 本文只讲**每个字段是什么意思、什么值合法**。范例：仓库里的 `providers/*.toml`（bun/node/pnpm 各演示一种索引形态）。

## 0. 最小可用清单

```toml
id = "rg"                 # 必须等于文件名（rg.toml）；与内嵌同名会被忽略（PROVIDER_EXTERNAL_CONFLICT）
display = "ripgrep"
kind = "binary"           # binary | registry

[index]
kind = "github-releases"
[index.urls]
default = "https://api.github.com/repos/BurntSushi/ripgrep/releases?per_page=100"

[artifact]
[artifact.urls]
default = "https://github.com/BurntSushi/ripgrep/releases/download/{version}/ripgrep-{version}-{platform}.tar.gz"
[artifact.ext]
darwin  = "tar.gz"
linux   = "tar.gz"
windows = "zip"
[artifact.platform]
darwin-arm64  = "aarch64-apple-darwin"
darwin-amd64  = "x86_64-apple-darwin"
linux-amd64   = "x86_64-unknown-linux-musl"
linux-arm64   = "aarch64-unknown-linux-gnu"
windows-amd64 = "x86_64-pc-windows-msvc"

[checksum]
format = "per-artifact"
mode   = "tofu-then-verify"
[checksum.urls]
default = "https://github.com/BurntSushi/ripgrep/releases/download/{version}/{filename}.sha256"

[layout]
strip = 1
bins  = ["rg"]
```

## 1. 顶层字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | string | ✅ | 工具 id；**必须等于文件名**（`rg.toml` ↔ `id = "rg"`），也是 `pk install <id>` 的名字 |
| `display` | string | ✅ | 给人看的名字（`pk list --available`、doctor 里出现） |
| `kind` | string | ✅ | `binary`（普通产物）或 `registry`（npm registry 上的 JS 包，如 npm/pnpm） |
| `requires` | []string | | 依赖的工具 id（只用于展示/校验，不自动装） |
| `[index]` `[artifact]` `[checksum]` `[layout]` | table | ✅ | 见下 |
| `[companions.<名>]` | table | | 伴生包，见 §6 |
| `[toolchain]` | table | | 装完写进环境变量的东西，见 §7 |

## 2. `[index]` 版本索引

| 字段 | 合法值 | 说明 |
|---|---|---|
| `kind` | `json` \| `registry` \| `dirlist` \| `github-releases` \| `pbs` | 索引形态（`pbs` 只允许内嵌清单） |
| `urls` | map（`default` + 可选 `cn`/`tuna`…） | 按镜像档选：没配当前档就用 `default`；值可以是**同一个数组** |
| `path` | string | `json` 型索引里版本列表的字段路径（默认自动猜 `versions`） |
| `version_prefix` | string | 去掉版本号前缀（go 的 `go1.27.3` → `version_prefix = "go"`） |

四种 `kind` 分别要什么样的响应：

| kind | 期望的响应 | 典型例子 |
|---|---|---|
| `json` | 一个 JSON 对象，里面有版本数组 | nodejs.org 的 `index.json` |
| `registry` | npm registry 的包元数据（`versions[].engines/dist`） | `{registry}/npm`、`{registry}/@pnpm/exe.{platform}` |
| `dirlist` | 目录列举：`[{"name":"v1.2.3/","type":"directory"}]` | npmmirror 的 `/-/binary/<tool>/` |
| `github-releases` | GitHub Releases API 数组（读 `tag_name` 与 `assets[]`） | 大部分 Rust/Go 工具 |
| `pbs` | python-build-standalone 的两级目录（**只能内嵌**） | python |

> `github-releases` 的好处是顺带拿到了每个 asset 的文件名，产物模板里可以用 `{filename}`；
> 其它 kind 没有文件清单，`{filename}` 会**回落成产物 URL 的末段**（1.0.1 起）。

## 3. `[artifact]` 产物

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `urls` | map | ✅ | 产物地址模板（同上按档选）。可用占位符见 §8 |
| `ext` | map（`darwin`/`linux`/`windows` → 扩展名） | ✅（除 `layout.single`） | 只允许 **tar.gz / tgz / zip** |
| `platform` | map（`<goos>-<goarch>` → 平台串） | ✅ | 至少要给 `darwin-arm64`、`linux-amd64`、`windows-amd64` |
| `min_version` | string | | 低于它的版本**解析完立刻报错并给建议**（pnpm 平台包从 11 才有） |

`platform` 的键是 pk 的 6 个平台键：`darwin-arm64` / `darwin-amd64` / `linux-amd64` / `linux-arm64` /
`windows-amd64` / `windows-arm64`；值是上游自己怎么叫这个平台（`aarch64-apple-darwin` 之类）。

## 4. `[checksum]` 校验和

| 字段 | 合法值 | 说明 |
|---|---|---|
| `format` | `gnu-sums` \| `per-artifact` \| `registry-integrity` \| `index-inline` | 校验和文件的形态；后两种的摘要直接在索引/元数据里 |
| `mode` | `tofu-then-verify` \| `verify` \| `none` | TOFU（首次多源交叉验证后固化）/ 每次都比 / 不校验 |
| `urls` | map | **必填**（`registry-integrity`、`index-inline` 例外） |

| format | 校验和文件长什么样 |
|---|---|
| `gnu-sums` | `SHASUMS256.txt`：每行 `<sha256>  <文件名>`，按产物文件名取一行 |
| `per-artifact` | 每个产物旁边一个 `…<文件名>.sha256`，文件里只有一行（可带文件名） |
| `registry-integrity` | 不用 urls：读 registry 元数据的 `dist.integrity`（sha512 SRI）或 `dist.shasum`（sha1） |
| `index-inline` | 不用 urls：索引条目里自带 `sha256`（go 的 `?mode=json`） |

> 校验强度按**校验源**算：产物与校验和都走 http → 最多 `weak`；产物 http 但 ≥2 个 https 校验源一致 → 仍算 `strong`。
> 内部 http 源要 `PK_ALLOW_HTTP=1` 显式放行，否则清单直接判非法。

## 5. `[layout]` 解压布局

| 字段 | 类型 | 说明 |
|---|---|---|
| `strip` | int | 剥掉几层目录（node 的 tar 内是 `node-vX/...` → `strip = 1`） |
| `single` | string | 产物是**单文件**时落到这个相对路径（如 `bin/singlefile`）；给了它就不用 `strip` |
| `bins` | []string | 要暴露成命令的名字（写进 `<PK_ROOT>/bin` 的 shim） |
| `bin_targets` | map | bin 名 → 解压树里的真实入口（npm 是 `bin/npm-cli.js`，需要 node 解释执行） |

## 6. `[companions.<名>]` 伴生包

装 = 主产物的同时把另一个 provider 的产物塞进**主产物目录内部**（和 node 里的 npm 一样，跟着宿主一起卸载/回收）。

| 字段 | 合法值 | 说明 |
|---|---|---|
| `mode` | `bundled` \| `latest-compatible` \| `pinned` | 自带（不动）/ 取该 provider 最新版 / 用 `version` 指定的版本 |
| `registry` | 工具 id | 到哪个 provider 取产物（必须是能加载到的清单） |
| `target` | 相对路径 | 落到主产物目录的哪个位置，如 `lib/node_modules/npm` |
| `version` | string | 仅 `pinned` 用，且**必填**（缺了清单非法） |

> node→npm 是特例：它的 `latest-compatible` 走 engines 反查（挑满足该 node 的最新 npm），
> 其它键则是"取该 provider 的最新版"。

## 7. `[toolchain]` 环境变量

```toml
[toolchain]
[toolchain.env]
GOTOOLCHAIN = "local"     # 装完后 pk env / shim 会导出这些变量
```

只影响 `pk env` 输出的变量与 shim 运行时的环境，不写用户 rc。

## 8. URL 模板占位符

| 占位符 | 含义 |
|---|---|
| `{version}` | 解析出来的版本号（不带前缀 `v`） |
| `{platform}` | `[artifact.platform]` 里映射出来的平台串 |
| `{ext}` | 该平台的归档扩展名 |
| `{registry}` | 当前镜像档的 registry 基地址（npm/pnpm 用） |
| `{tool}` | provider 的 `id` |
| `{filename}` | 产物文件名（`github-releases` 有 asset 清单时准确；否则回落成产物 URL 末段） |
| `{tag}` | 索引里的原始 tag（`bun-v1.4.4` 这种带前缀的） |

> 未知占位符会让清单加载失败（`PROVIDER_EXTERNAL_INVALID`，doctor 会指出是哪个文件）。
> 含 `{registry}` 的模板会**按镜像回退链逐个渲染**，所以"国内档挂了退官方"是自动的。

## 9. 排错

```bash
pk doctor --json | grep -A3 PROVIDER_    # 哪个文件、什么原因
pk list --available                      # 外部工具带 * 号，看得到 latest
pk install <id>@<版本> --dry-run          # 只打印解析结果（产物 URL / 校验方式），不下载
```

常见的三个坑：

1. `id` 与文件名不一致 → `PROVIDER_EXTERNAL_INVALID`；
2. 忘了 `[artifact.ext]` 或 `[artifact.platform]` 的必填键 → doctor 会指名道姓说缺哪个；
3. `per-artifact` 的校验和模板用了 `{filename}` 而索引没有文件清单 → 1.0.1 起会回落，老版本会 404。
