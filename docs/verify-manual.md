# 需要人工确认的两项（自动化覆盖不到）

> 出处：`docs/progress.md` 的「剩余项」表。这两项**不是没做**，而是没有可信的自动化环境：
> 无公共 ARM Windows runner、Gatekeeper 首启只能由真人点。
> 下面每条都写了"怎么做"和"通过的标准"，做完把结论追加到本文件末尾。
>
> **2026-11-13 更新**：原来第 2 条「Windows `.cmd` shim 被 PowerShell 调用」已自动化 ——
> `smoke.yml` 新增 Windows 专属 `pwsh` 步骤，真 PowerShell 调 `pk.cmd` / `packnest.cmd` /
> `node.cmd` / `npm.cmd` 并断言输出，再走零残留卸载（首次运行即通过：`pk.cmd --version ->
> packnest v1.3.1 (windows/amd64)（v1.4.0 起同样通过，v1.5.1 复测通过）`、`node.cmd -v -> v22.23.5`、`npm.cmd -v -> 12.2.2`）。
> 下面保留原文，作为"如果 CI 挂了怎么手动复核"的步骤清单。

## 1. Windows ARM64 运行时

**为什么只能人工**：GitHub 没有公共 ARM Windows runner；`smoke.yml` 矩阵里 windows 只有 amd64。

**怎么做**（任意一台 ARM64 Windows 11 机器，PowerShell 7）：

```powershell
iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -OutFile install.ps1
pwsh -NoProfile -File install.ps1 -DryRun      # 先看它解析出的平台是不是 windows-arm64
pwsh -NoProfile -File install.ps1
pk --version                                    # 期望：packnest v<版本> (…, windows/arm64)
pk doctor                                       # 期望：ok:true，无 SHIM_MISSING / LEDGER_*
pk install node@22
node -v
pk uninstall --all --yes --purge
Test-Path $env:PK_ROOT                          # 期望：False（零残留）
```

**通过标准**：`--version` 显示 `windows/arm64`；`pk install node@22` 成功且 `node -v` 正常；
`uninstall --all --purge` 后 `PK_ROOT` 目录消失、用户 Path 里的 packnest 条目被清掉。

## 2. Windows `.cmd` shim 被 PowerShell 调用（✅ 2026-11-13 已自动化，见文首说明）

**自动化位置**：`.github/workflows/smoke.yml` 的 `Windows：PowerShell 调 .cmd shim` 步骤
（`shell: pwsh`，装完 node 后逐个调 `pk.cmd` / `packnest.cmd` / `node.cmd` / `npm.cmd`）。
**手动复核为什么仍需这份清单**：本地 git-bash 不认 `.cmd` 分派，CI 挂了时要有人真机确认。

**怎么做**（amd64 Windows 即可）：

```powershell
pk install node@22
Get-Command npm                                 # 期望：npm.cmd，路径在 <PK_ROOT>\bin
npm --version
npm config get registry                         # 期望：当前镜像档的 registry
npm i -g pnpm
pnpm --version                                  # 全局包 bin 也能从 PATH 找到
pk doctor --globals                             # 期望：没有 GLOBAL_* warn
```

**通过标准**：`npm`/`pnpm` 都能直接执行（不需要 `npm.cmd` 后缀），`npm config get registry` 走镜像档；
`pk doctor --globals` 无 `GLOBAL_ENGINES_UNMET` / `GLOBAL_NATIVE_ABI`。

## 3. macOS Gatekeeper 首启

**为什么只能人工**：产物是 **ad-hoc 签名**（2026-11-12 拍板不做 Developer ID / 公证），
首次打开必须由用户在「隐私与安全性」里放行一次；这一步没有 API 能自动化。

**怎么做**（任意一台**没装过**的 Mac）：

```bash
# 从发布页下载 GUI（gui-v*，只发 Apple Silicon）
curl -LO https://gitee.com/null_593_5354/packnest/releases/download/gui-v0.10.2/packnest-desktop_0.10.2_darwin_arm64.app.zip
unzip -q packnest-desktop_0.10.2_darwin_arm64.app.zip
spctl -a -vvv -t exec packnest.app ; echo "退出码=$?"   # 期望：rejected（ad-hoc）
open packnest.app                                      # 期望：弹"无法验证开发者" → 系统设置里放行 → 再次 open 能起窗口
```

同时确认 CLI 一侧（用户主路径）：

```bash
curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh
pk --version      # brew/脚本装的 CLI 不在 Gatekeeper 管辖内（未签名二进制 + 无 quarantine 属性时可直接跑）
```

**通过标准**：`spctl` 判 rejected（符合 ad-hoc 预期，不是 bug）；放行一次后 GUI 能正常起窗口、
能读到 CLI（顶部显示 `CLI <版本> · <路径>`）；CLI 用脚本装完直接可用。

## 记录格式

```
### <日期> 第 N 项 ✅
平台/版本：Windows 11 ARM64 (24H2) · pk 1.1.0
结果：全部通过 / 失败点与诉求
```

## 附：已确认的自动化覆盖（不需要人工）

- 6 平台产物结构与静态链接：`make release` + `make verify-release`（本地 mac）
- Linux/macOS 三平台真机装一遍 + 卸载零残留：`smoke.yml` 5 个 runner
- `install.ps1` 的语法与 `-DryRun` 分支：`smoke.yml` 的 windows 步骤（PowerShell 7 解析）
- GUI ↔ CLI 的 `--json` 契约：`ci.yml` 的 `gui-contract` job（真 `pk`）
