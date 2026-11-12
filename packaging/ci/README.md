# CI 说明

工作流都在仓库根目录 `.github/workflows/`（GitHub 只认这个位置，
放在 `packaging/ci/` 里不会被执行）：

| 文件 | 作用 | 开关 |
|---|---|---|
| `ci.yml` | **门禁**：`make check` 8 项（fmt+vet / 行数 / race+覆盖率 / 6 平台交叉 / fuzz / bench / golden）+ GUI 侧 `internal/pkcli` 契约测试（用真 `pk`） | 默认开（push main / PR / 手作） |
| `smoke.yml` | 真机装一遍（从 GitHub Releases 取产物，5 个 runner 组合） | tag 或手作 |
| `gui-build.yml` | 构建 GUI 三平台产物（**只产 artifact，不发布**） | 手作 / tag |
| `pages.yml` | 渲染官网并部署到 GitHub Pages | 默认开 |
| `gitee-mirror.yml` | 把 main 与 tag 镜像到 gitee（国内访问主源） | 仓库 Variable `SYNC_GITEE=true` + Secret `GITEE_TOKEN` |

**发布不在云端做。** 0.8.0 / 0.9.0 都因为「本机 + GitHub Actions 同时发布」出现了两套 sha256
（ubuntu 与 macOS 构建出的 tar.gz/zip 字节不同），tap 里的 hash 与 gitee 上的产物对不上，
`brew install` 直接报 `reports different checksum`。现在规定：

- 发布只在本机：`make release` → `sh scripts/release-gitee.sh` → `make publish-manifests`；
- 旧的 `npm-publish.yml`（npm Trusted Publishing + gitee 上传 + tap 同步）已删除，
  **npm 分发渠道于 2026-11-11 停用**（stage/approve 需要 2FA，且同样有跨机器构建差异问题）。

细节见 `docs/publishing.md` 的「唯一发布者」一节。

## 门禁为什么要上云

2026-11-12 盘点发现：4 个 workflow 里**没有一个在跑门禁**，loc / 覆盖率 / fuzz / 交叉编译 / golden
全靠“本机记得跑”，换机器或忘了就漏。`ci.yml` 上线当天就抓到一个只在 Linux 上红的用例
（`env.RCFileFor` 在非 Windows 上读了本机 `PSModulePath`，把“预测 windows profile 路径”误判成 PS7），
已修并补了回归用例 —— 这正是本地单机跑不出来的那类问题。
