# 发布指南（gitee / brew tap / scoop / 官网）

> npm 分发渠道已于 **2026-11-11 停用**（原因见文末「为什么不再发 npm」）。
> 现在的发布链路只有一条：**本机**构建 → gitee release → 渠道清单（brew tap / scoop bucket）。

## 一、唯一发布者：只在这台机器发

**发布者只能有一个。** 0.8.0 / 0.9.0 都因为「本机 + GitHub Actions 同时发布」出了事：

| 谁来构建 | darwin_arm64 的 sha256（0.9.0 实测） |
|---|---|
| 本机 macOS（= gitee 上真正被下载到的文件） | `5b8bdf5a…` |
| GitHub Actions ubuntu | `8cc7f131…` |

同一份源码、同一套 checksums 口径，**两台机器构建出的 tar.gz/zip 字节不同**（gzip/zip 实现差异），
于是同一个版本存在两套 sha256；而 gitee 的 `attach_files` 对同名文件**不替换**，
最后就是「tap 里的 hash ≠ 用户下载到的文件」→ `brew install` 报 `reports different checksum`。

现行规则：

- 云端 CI 只保留 `pages.yml`（官网）与 `gitee-mirror.yml`（代码/tag 镜像），**不再发布任何产物**；
- 旧的 `npm-publish.yml` 已删除（它既是 npm 发布，也是云端 gitee/tap 发布的来源）；
- 仓库变量 `PUBLISH_TAPS` / `PUBLISH_GITEE` 置为 `false`（历史遗留，现已无对应 job）。

## 二、凭据放哪儿（AI 不读凭据）

- 文件：`~/.pi/agent/local/secrets.env`（`chmod 600`，模板见 `packaging/secrets.env.example`）；
- 需要的键：`GITEE_TOKEN`（上传 release 用，勾 projects / releases 权限）；
- 推 brew tap / scoop bucket 走 `gh`（自己的 keyring），不需要额外 token；
- **凭据围栏**：AI 读这个文件会被直接拒绝，脚本在子进程里读到内存，日志只打「用了哪个来源」。

## 三、发布流程（照着做）

```bash
cd ~/Documents/Working/packnest

make check                              # ① 8 项门禁（含 make loc 行数红线）
make release                            # ② 6 平台产物 + checksums.txt → dist/v<ver>/
sh scripts/release-gitee.sh             # ③ 上传 gitee release（凭据只在本机）
make github-release                     # ③b 同步 GitHub Releases（海外主源，gh 自带凭据）
make verify-release                     # ④ 把线上产物拉回来对 checksums.txt（必跑）
# ⑤ 仓库里改版本号 → commit → tag → push（两个远端 + tag）
make publish-manifests                  # ⑥ verify-release → gen-manifests → release-taps（自带线上核对）

# ⑦ GUI（可选，独立 tag gui-v<ver>；见「五点五」）
make gui-test                           # 先跑 GUI 契约测试（用真 bin/pk 校验 --json）
sh scripts/release-gui.sh --version 0.10.2
#    三平台的 Windows/Linux 由 CI gui-build.yml 构建后下载回来，再用 --from-dir 上传
gh run download <run-id> -n gui-windows-amd64 -D /tmp/gui -R nekoleamo/packnest
gh run download <run-id> -n gui-linux-amd64   -D /tmp/gui -R nekoleamo/packnest
sh scripts/release-gui.sh --version 0.10.2 --from-dir /tmp/gui
```

**顺序不能乱**：清单里的 sha256 必须指向**已经发布**的那批产物。`make publish-manifests` 依赖
`verify-release`，而 `release-taps.sh` 自己推之前也会跑一次线上核对（`PK_SKIP_LIVE_CHECK=1` 可跳过，
`make publish-manifests` 已经跑过所以会跳过）——清单与线上不一致时**拒绝推送**。

发布后自检：

```bash
brew uninstall packnest && brew install nekoleamo/packnest/packnest   # 清缓存再装，验证 tap 与产物一致
pk --version
```

## 三点五、GitHub Releases（海外主源，和 gitee 同一批产物）

```bash
make github-release      # 用 gh 把 dist/v<ver>/ 的 6 产物 + checksums.txt 同步到 GitHub Releases
```

为什么要有：gitee 对**海外数据中心 IP 常返回 404**（GitHub Actions runner 实测），海外用户也慢；
`smoke.yml` 因此改从 GitHub Releases 取产物。产物仍是本机构建的同一批文件，**唯一发布者规则不变**。

海外/CI 安装：`sh scripts/install.sh --base https://github.com/nekoleamo/packnest/releases/download`

⚠️ 两个实测踩过的坑（2026-11-13，v1.3.1）：

- **release 不能停在 draft**：`/releases/latest` 看不到 draft，`smoke.yml` 会退而安装**上一个版本**
  （v1.3.1 的 smoke 因此装了 v1.3.0）。`release-github.sh` 现在建完强制 `--draft=false`；手动建 release 后
  务必确认 `gh release list` 里没有 `Draft`。
- **别删已经发过产物的 tag**：gitee 的 release 附件挂在 tag 上，删 tag 会让所有附件 404
  （1.3.1 挪 tag 到修好的提交后就是这样）。正确做法是**重跑** `sh scripts/release-gitee.sh` 重传附件。

## 三点七、发布签名（1.7 起）

产物清单可以签 sshsig，客户端（`install.sh` / `install.ps1`）在比 sha256 之前先验签。

```bash
make release                                            # 先出 6 产物 + checksums.txt
PK_SIGNING_KEY=/Volumes/usb/pk-release-2026 sh scripts/sign-release.sh
sh scripts/release-gitee.sh && sh scripts/release-github.sh
make verify-release                                     # 远端有 .sig 就一起验
```

规则（细节见 `~/Documents/Plan/17-包管理器/pn-1.7-产物签名-方案.md`）：
- **私钥离线**，只在发版时临时挂载；签名必须人工触发（自动签等于没签）。
- 只签 `checksums.txt`（覆盖 6 个产物），产物变 **8 件**：6 归档 + `checksums.txt` + `checksums.txt.sig`。
- 公钥要同时写进三处，否则用户侧认不出：`internal/verify/keys/allowed_signers`、
  `scripts/install.sh` 的 `ALLOWED_SIGNERS`、`scripts/install.ps1` 的 `$AllowedSigners`。
- 轮换时 allowed_signers 最多留两把（当前 + 下一把），先发带新公钥的版本，下一版才用新钥签。
- **备份**：可以用 Time Machine 等自动备份防丢失，但备份盘常连且未加密时，本机被接管就多了一条读私钥的路径；
  至少勾选「加密备份」，或把私钥排除出自动备份、只留一份离线冷备。半年做一次恢复演练（恢复 → 签一份清单 → 验签通过）。
- **撤销要清历史**：TM / 快照 / 云盘版本历史会留旧私钥副本。轮换或泄露处置后要一并删掉含旧钥的快照，
  否则三处公钥都删了，旧私钥仍能签出看起来合法的清单。
- **brew / scoop 不验签名**（只认清单里的 sha256），这是渠道能力边界，别在文档里承诺覆盖。

## 四、gitee（国内二进制主源）

- `sh scripts/release-gitee.sh` 建/复用 tag 对应的 release，逐个上传 `dist/v<ver>/packnest_*` 与 `checksums.txt`；
- 同名文件**不会**被替换：重发同一版本必须先删掉旧附件（或换版本号），否则线上还是旧字节；
- `install.sh` / `install.ps1` 默认从 `https://gitee.com/<owner>/<repo>/releases/download/v<ver>/<文件>` 取，
  版本号从 gitee release API → 仓库里的 `scripts/latest`（`make release` 自动更新，记得提交）。

## 五、Homebrew tap / Scoop bucket

- 模板：`packaging/brew/packnest.rb`、`packaging/scoop/packnest.json`（由 `scripts/gen-manifests.sh` 从
  `dist/v<ver>/checksums.txt` 生成，**别手改**）；
- 仓库：`nekoleamo/homebrew-packnest`（`Formula/packnest.rb`）、`nekoleamo/scoop-bucket`（`bucket/packnest.json`）；
- `sh scripts/release-taps.sh` 会推这两个仓库，推完回读核对（用 `git clone`，不用有 CDN 缓存的 raw）；
- winget 渠道已于 2026-11-11 放弃（不再生成草稿，也不再向 winget-pkgs 提 PR）。

## 五点五、GUI（pk-desktop）的产物

GUI 是**独立产物**（需要 cgo + 各平台 WebView 工具链），但有两条硬规则：

1. **必须先装 CLI**：GUI 只通过子进程调 `pk <cmd> --json` 干活（`docs/progress.md` 的"GUI 定位"）；
2. **发布仍守"本机唯一发布者"**：CI 只构建、不发布；上传动作全部在本机（mac 本机构建，
   Windows/Linux 下载 CI 产物后原样上传，字节与 CI 一致）。

### 产物与 tag 约定

| 项 | 约定 |
|---|---|
| tag | `gui-v<guiVer>`（**不是** CLI 的 `v<cliVer>`）：GUI 有独立版本号（`cmd/pk-desktop/main.go` 的 `guiVersion`），产物文件名里的 `<ver>` 就是这个数，与 tag 一致 |
| 文件 | `packnest-desktop_<ver>_<os>_<arch>.{app.zip,zip,tar.gz}` |
| 为什么单独 tag | gitee 的 release API 不返回附件 id，同名附件**删不掉也换不了**；GUI 会频繁重发，单独一个 release 才能整体删掉重建（`DELETE /releases/{id}`） |

### 本机打包（macOS，arm64 单架构）

```bash
sh scripts/release-gui.sh --version 0.10.2            # 构建 arm64 .app.zip 并上传
# 只打包不上传：
sh cmd/pk-desktop/packaging/build-macos.sh 0.10.2 arm64   # → dist/gui/packnest.app
```

打包逻辑只有一处（`cmd/pk-desktop/packaging/build-macos.sh`）：图标（`sips`/`iconutil` → `icon.icns`）、
`Info.plist` 版本注入、签名都在里面；`release-gui.sh` 只是打 zip + 上传（**mac intel/amd64 已放弃**，
2026-11-12 拍板；Intel Mac 要 GUI 就自己 `make gui-app` 出本机构建）。

### 签名与公证（2026-11-12 拍板：不做）

产物一律 **ad-hoc 签名**（别人首次打开要在「隐私与安全性」放行）。机制保留，将来想发 Developer ID
只需设两个环境变量、不改代码：

```bash
xcrun notarytool store-credentials notary        # 一次：填 Apple ID / 团队 / app 专用密码
PK_SIGN_IDENTITY="Developer ID Application: 你的名字 (TEAMID)" \
PK_NOTARY_PROFILE="notary" \
sh cmd/pk-desktop/packaging/build-macos.sh 0.10.2 arm64
```

- 有 `PK_SIGN_IDENTITY` → 硬化运行时 + 时间戳签名；有 `PK_NOTARY_PROFILE` → `notarytool submit --wait` + `stapler staple`
- 都不设 → ad-hoc（**当前默认**）

### Windows / Linux

由 `.github/workflows/gui-build.yml` 构建（Linux 需 GTK4 + WebKitGTK 6.0，Windows 需 WebView2 运行时），
产物只进 workflow artifact，下载后用 `--from-dir` 上传。

### 三个实测坑（2026-11-13，gui-v0.10.3 / v0.10.4）

- **`releases/latest` 会指向 GUI 的 tag**：GUI 有独立的 `gui-v*` release，发完 GUI 之后
  gitee/GitHub 的 `releases/latest` 返回的就是它 —— 所有"取最新 CLI 版本"的脚本必须校验 tag 形状
  （只认 `^v\d+\.\d+\.\d+$`，否则退回 `scripts/latest`）。v0.10.4 发布后 `install.sh` 真的拼出了
  `…/download/vgui-v0.10.4/…` 并 404，`smoke.yml` 也因此整条红。三处已修：
  `scripts/install.sh`、`scripts/install.ps1`、`.github/workflows/smoke.yml`。

- **CI 产物的版本号取自 `cmd/pk-desktop/main.go` 的 `guiVersion`**，不是 `scripts/latest`（那是 CLI 的）。
  之前用 CLI 版本命名，`release-gui.sh --version 0.10.3` 按 0.10.3 找 CI 产物自然一个都找不到。
- **`release-gui.sh` 只上传真实存在的产物**：早先把 `*.tar.gz` 等通配符直接写进 `gh release upload`，
  只构建了 mac 时整条命令每次都失败、重试 5 次后仍打印“已上传”，结果 GitHub release 上一个附件都没有。
  现在先 `ls` 出文件再传，失败以退出码 5 报出。

### 重发同一版本

gitee 不允许替换同名附件，所以**先删 release 再重建**：

```bash
# 取 id：curl -s "https://gitee.com/api/v5/repos/<owner>/<repo>/releases/tags/gui-v0.10.0"
curl -s -X DELETE "https://gitee.com/api/v5/repos/<owner>/<repo>/releases/<id>?access_token=$GITEE_TOKEN"
gh release delete gui-v0.10.0 --repo nekoleamo/packnest --yes
sh scripts/release-gui.sh --version 0.10.2             # 重建 + 上传
```

## 六、官网

`make site` 渲染 `site/` → `site/dist`（只注入版本号，来源是 `git describe` 的 tag），GitHub Pages 由 `pages.yml` 部署；
`pages.yml` 在**每次推 main**与**打 `v*` tag**时都会重跑（不用 paths 过滤 —— 版本号是 `git describe` 算的，
只改代码/只打 tag 时也得重建，否则站点会一直挂着上一个版本；2026-11-12 实测踩过：线上还显示 1.0.1）。
手动重建：`gh workflow run pages.yml --repo nekoleamo/packnest`。

版本号还有两个坑（2026-11-12 都踩过，已修在 `pages.yml` 与 `scripts/build-site.sh`）：

- `actions/checkout` 默认 **`fetch-tags: false`** —— 只给 `fetch-depth: 0` 拿不到 tag，
  `git describe` 退化成短 SHA，站点会渲染成 `vdev`（或把 `635c593` 这种短 SHA 误当版本号）；
- 所以 `build-site.sh` 只认 `vX.Y.Z` 形式的 tag（`--match 'v[0-9]*' --abbrev=0`，不用 `--always`），
  取不到就依次回退 `scripts/latest` → `dist/latest`，最后才 `dev`。
站点里的安装命令与渠道矩阵要和本文件保持一致（npm 那条已删）。

**第三个坑（2026-11-13，v1.5.0 踩到）：`github-pages` 环境保护规则默认只放行 `main`**，
而 `pages.yml` 也在 `v*` tag 上触发 —— 直接用 tag 触发会被拒：
`Tag "v1.5.0" is not allowed to deploy to github-pages due to environment protection rules`，
run 显示红叉（虽然随后的 main 推送会把站点刷成新版本）。一次修好：

```bash
gh api repos/nekoleamo/packnest/environments/github-pages/deployment-branch-policies \
  --jq '.branch_policies[] | {name,type}'          # 应同时有 main(branch) 与 v*(tag)
# 缺 tag 策略就补一条：
gh api -X POST repos/nekoleamo/packnest/environments/github-pages/deployment-branch-policies \
  -f name='v*' -f type='tag'
```

补完后 `gh run rerun <run-id>` 会转绿。线上版本核对：
`curl -s https://nekoleamo.github.io/packnest/ | grep -o 'v1\.[0-9]\.[0-9]'`（中英文各一次）。

## 七、为什么不再发 npm

1. **2FA**：npm 的 staged publish 需要 `npm stage approve` 带 OTP 或浏览器批准，`www.npmjs.com`
   大陆直连基本打不开（实测经代理也被 Cloudflare 拦 curl，返回 403；`registry.npmjs.org` 才通）。
2. **构建差异**：npm 平台包也是从归档来的，云端与本机构建的字节同样不同，会引入第二套 sha256。
3. **收益下降**：国内用户的主路径已经是 gitee + chubby 的脚本一行安装；brew/scoop 覆盖 macOS/Windows。

影响与保留：

- `install.sh` / `install.ps1` 的 npm 通道（`--from npm`、`-Mirror cn`）已移除/退化为 gitee；
- 已经用 `npm i -g packnest` 装过的旧版本照常能用，`pk uninstall --all` 仍识别这个渠道；
- 历史 staged 记录（0.9.0 的 7 个包）会在 npm 侧自然过期（约 7 天），不清理也不影响任何人。

## 八、发布前自检清单

```bash
make check                        # 8 项门禁（含行数红线）
make release && ls dist/v*/       # 6 产物 + checksums.txt
make verify-release               # 线上 vs 本地清单（发布后必跑）
sh scripts/release-taps.sh --dry-run   # 只打印将要推送的清单文件
brew install nekoleamo/packnest/packnest   # 渠道实装一次（先 brew uninstall）
```

### 账号 2FA（2026-11-13）

GitHub 对贡献者账号强制 2FA；未开启会在宽限期后**无法访问 GitHub.com**。发布链路全部是 token/密钥认证，
不靠交互登录，开启 2FA 后行为不变（只有 `gh auth login` 重授权、改密码、增删 SSH key 这类真敏感操作要输一次 TOTP）。
开启后跑一次三条自检：

```bash
gh auth status                     # 应仍显示 Logged in（keyring），scopes 含 repo / workflow
git ls-remote origin -q >/dev/null  # HTTPS + 凭据助手通道
gitee + curl 不受影响，可跑 make verify-release 顺带确认整链路
```

（最后一行不是命令：`make verify-release` 走 gitee + curl，与 GitHub 账号无关，顺手确认整条链路。）
