# CI 说明

工作流只做两件事，都在仓库根目录 `.github/workflows/`（GitHub 只认这个位置，
放在 `packaging/ci/` 里不会被执行）：

| 文件 | 作用 | 开关 |
|---|---|---|
| `pages.yml` | 渲染官网并部署到 GitHub Pages | 默认开 |
| `gitee-mirror.yml` | 把 main 与 tag 镜像到 gitee（国内访问主源） | 仓库 Variable `SYNC_GITEE=true` + Secret `GITEE_TOKEN` |

**发布不在云端做。** 0.8.0 / 0.9.0 都因为「本机 + GitHub Actions 同时发布」出现了两套 sha256
（ubuntu 与 macOS 构建出的 tar.gz/zip 字节不同），tap 里的 hash 与 gitee 上的产物对不上，
`brew install` 直接报 `reports different checksum`。现在规定：

- 发布只在本机：`make release` → `sh scripts/release-gitee.sh` → `make publish-manifests`；
- 旧的 `npm-publish.yml`（npm Trusted Publishing + gitee 上传 + tap 同步）已删除，
  **npm 分发渠道于 2026-11-11 停用**（stage/approve 需要 2FA，且同样有跨机器构建差异问题）。

细节见 `docs/publishing.md` 的「唯一发布者」一节。
