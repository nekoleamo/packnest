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
