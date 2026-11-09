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

### 这个文件到底怎么写

路径：`~/.pi/agent/local/secrets.env`（目录不存在就 `mkdir -p ~/.pi/agent/local`）。格式是**纯 `KEY=value`，一行一个**：

```sh
NPM_ACCESS_TOKEN=npm_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

键名兼容（脚本按顺序找，命中即用）：`NPM_ACCESS_TOKEN` → `NPM_TOKEN` → `NODE_AUTH_TOKEN` → `NPM_AUTH_TOKEN`。
所以叫 `NPM_ACCESS_TOKEN` 也行，不用改名。

三种写法，任选（推荐第一种，值不进 shell history）：

```sh
# 1) 交互输入，不回显，自动 chmod 600（推荐）
sh scripts/set-npm-token.sh

# 2) 手工写：先建空文件并设权限，再用编辑器粘贴（不要 echo 到命令行，会进 history）
mkdir -p ~/.pi/agent/local && install -m 600 /dev/null ~/.pi/agent/local/secrets.env
${EDITOR:-vim} ~/.pi/agent/local/secrets.env

# 3) 从别处一次性导入（管道，不落 history）
read -rs T && printf 'NPM_TOKEN=%s\n' "$T" >> ~/.pi/agent/local/secrets.env && chmod 600 ~/.pi/agent/local/secrets.env
```

要点：
- **不要加引号、不要 `export`**（脚本按 `KEY=value` 直接解析）；
- 文件里已有的其他键（比如 gitee 的）**不用动**，`set-npm-token.sh` 只增改 `NPM_TOKEN=` 这一行；
- 仓库里有模板 `packaging/secrets.env.example`，照抄键名即可，真值永远不进仓库；
- 验证是否生效（不显示值）：`PK_NPM_TOKEN_FILE=~/.pi/agent/local/secrets.env sh scripts/release-npm.sh --dry-run` 不会读它，
  真正发布时看输出里的 `凭据：/…/secrets.env（值不显示）`；
- 本机有"凭据围栏"：AI 直接读该文件会被拒绝（实测 grep 已被拦），这是预期行为。

### 怎么做到真正无人值守（不再弹浏览器）

npm 的 2FA 有两种模式，决定自动化难度：

| 账号模式 | 现象 | 自动化方案 |
|---|---|---|
| `auth-and-writes` + token **没**勾 Bypass 2FA | 发几个包后弹 `npmjs.com/auth/cli/...` 链接 | ① 重建 token 勾 **Bypass 2FA**（推荐）② 或让脚本用本地 TOTP 自动出码 ③ 或把账号改成 `auth-only` |
| `auth-and-writes` + token **勾了** Bypass | 完全非交互 | 直接用 `make publish` |
| `auth-only` | 只在登录时要求 | 直接用 `make publish` |

**方案②的做法**：在同一个凭据文件里加一行 base32 的 2FA 密钥（就是你配 Authenticator 时的那串）：

```sh
NPM_TOTP_SECRET=JBSWY3DPEHPK3PXP
```

`release-npm.sh` 检测到它就用 python3 按 RFC 6238 算 6 位码，自动 `--otp`（不打印、过 30 秒即失效）。
仍想手动指定：`sh scripts/release-npm.sh --publish --otp 123456`。

发布失败时脚本会直接把这三条处置打出来，不用去猜。

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
