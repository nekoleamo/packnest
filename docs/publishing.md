# 发布指南（npm / gitee / brew）

## 一、npm 发布需要什么

**需要凭据**，三选一：

| 方式 | 适合 | 怎么给 |
|---|---|---|
| `npm login` | 本地手工发一次 | 浏览器 OAuth 或用户名/密码/OTP，凭据落 `~/.npmrc` |
| **Granular Access Token** | CI / 换机器 | npmjs.com → *Access Tokens* → *Generate New Token* → *Granular*：<br>① Packages and scopes：选 `packnest` **和** `@packnest` 组织（read and write）<br>② Expiration：按需（最长 1 年）<br>③ **勾上 Bypass 2FA**（否则非交互发布会报 OTP 错）<br>把 token 放 `NODE_AUTH_TOKEN` |
| Classic token | 老账号 | *Access Tokens* → Classic → Automation（旧版，仍可用） |

账号侧还要：
- 邮箱已验证；
- 包名可用：`packnest` 目前空闲（方案 §20 实测）；`@packnest` 是**组织/scope**，首次发布时自动创建（或先在网页建组织）；
- **scoped 包默认 private（要钱）→ 必须 `--access public`**（我们的脚本已经带）。

2FA 账号：token 勾 Bypass 2FA，或发布时 `--otp 123456`（脚本支持 `--otp`）。

## 一·五、token 放哪儿（AI 不读凭据）

本项目规约：**凭据文件不读、不打印、不入库**。所以 token 不放仓库、不放 `package.json`，只有两个安全落点：

| 场景 | 落点 | 怎么放 |
|---|---|---|
| **本机自动化** | `~/.pi/agent/local/secrets.env`（权限 600，已在 `.gitignore` 里） | `sh scripts/set-npm-token.sh` —— 交互粘贴、不回显、不多写一行别的键 |
| **CI** | 平台 Secret（GitHub Actions `secrets.NPM_TOKEN` / gitee 私密变量） | 流水线里 `NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}` |

发布脚本 `scripts/release-npm.sh --publish` 自己按顺序找凭据，**只 echo "用了哪个来源"，从不打印值**：

1. `NODE_AUTH_TOKEN` 环境变量；
2. `PK_NPM_TOKEN_FILE`（默认 `~/.pi/agent/local/secrets.env`）里的 `NPM_TOKEN=`（会检查权限，不是 600 会给警告）；
3. `~/.npmrc`（`npm login` 产生）。

秘书（我）这边永远不需要读 token：脚本在子进程里 `export`，日志里只有 `<token 不显示>`；仓库里的 `.npmrc` 只放
`registry` 和 `access=public` 这类非机密配置。
实测：把假 token 放进临时凭据文件跑 `--dry-run`，输出里对 token 的命中数为 **0**。

### 只放本地（推荐，不上任何云端 Secret）

完全可以。凭据只存在于这台机器的 `~/.pi/agent/local/secrets.env`（600），发布也在本机跑：

```bash
make check                 # 门禁（可选但推荐）
make publish               # = make release + scripts/release-npm.sh --publish
```

CI 侧有两种"零云端凭据"的用法：

| 方案 | 做法 | 代价 |
|---|---|---|
| **A. CI 只构建** | 默认的 `packaging/ci/npm-publish.yml`（发布步骤已注释），tag 触发后只出产物；你下载产物后在本机 `make publish` | 发布要手动点一下 |
| **B. 自托管 runner** | 把同一个 job 挂到自己机器的 runner，publish 步骤取消注释但不配 Secret——token 由**那台机器**的凭据文件提供 | 机器要常在线 |

只有第三种（云端 GitHub 托管 runner + Secret）才需要把 token 放到云上，本仓库模板默认不这么做。

轮换：npmjs.com 删旧 token → `sh scripts/set-npm-token.sh` 重录 → 重新发版。

## 二、发布流程（本项目）

```bash
make release                     # 6 平台产物 + checksums → dist/
sh scripts/release-npm.sh        # → dist/npm/（6 个平台包 + 主包）
sh scripts/release-npm.sh --dry-run          # 只打包 + 校验 manifest，不发
NODE_AUTH_TOKEN=npm_xxx sh scripts/release-npm.sh --publish          # 真发
NODE_AUTH_TOKEN=npm_xxx sh scripts/release-npm.sh --publish --otp 123456
sh scripts/release-npm.sh --publish --tag next                        # 预发布号必带 tag
```

**顺序**：脚本先发 6 个 `@packnest/<os>-<arch>` 子包，再发主包 `packnest`
（主包的 `optionalDependencies` 指向子包；反过来用户 `npm i` 的瞬间会 404）。
预发布版本（`0.6.0-M5` 这种）npm 强制要求 `--tag`，脚本自动用 `next`，正式发版用 `1.0.0` 即可走 `latest`。

**国内不必额外交付**：发到 npmjs 后 npmmirror 通常几分钟内自动同步；想立刻同步：

```bash
curl -X PUT https://registry.npmmirror.com/-/package/packnest/syncs
curl -X PUT 'https://registry.npmmirror.com/-/package/%40packnest%2Fdarwin-arm64/syncs'
```

（npmmirror 是只读镜像，**不能**直接 publish 到它。）

## 三、gitee（免费二进制主源）

- 建仓库 → 创建 release（tag `v<版本>`）→ 上传 `dist/v<版本>/*.tar.gz|zip` 与 `checksums.txt`；
- `install.sh`/`install.ps1` 默认从 `https://gitee.com/<owner>/<repo>/releases/download/v<版本>/<文件>` 取；
- 脚本本身放 `scripts/install.sh`（raw 地址，`curl … | sh` 用）。

## 四、Homebrew（自有 tap，免费）

`packaging/brew/packnest.rb` 是模板：把 `url`/`sha256` 换成 gitee release 的实际值，放进自己的 tap 仓库
`homebrew-<名字>`，用户 `brew install <owner>/<tap>/packnest`（命令名仍是 `pk`）。

## 五、发布前自检

```bash
make check                        # 7 项门禁
make release && ls dist/v*/
sh scripts/release-npm.sh --dry-run                # 7 个包 manifest 都过
./scripts/install.sh --dry-run --from npm          # 通道连通性
```
