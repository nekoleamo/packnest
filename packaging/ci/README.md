# CI 说明

工作流文件在仓库根目录 **`.github/workflows/npm-publish.yml`**（GitHub 只认这个位置，
放在 `packaging/ci/` 里不会被执行）。

它负责：门禁 → 6 平台产物 → npm 发布（Trusted Publishing/OIDC，无 token）→
可选的三项：gitee release 上传、brew/scoop 清单同步、gitee 代码镜像。
开关与凭据见该文件头部注释与 `docs/publishing.md`。
