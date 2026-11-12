# pk-desktop —— packnest 的可视化补充界面

> **定位**：GUI 不是独立端。**必须先装 CLI**（`pk`），这里只把 `pk <cmd> --json` 的结果投影成界面。
> 不 import 主模块任何 `internal/*`，业务真相永远在 CLI 与 `PK_ROOT`（方案 §11 定案 2026-11-11）。

## 现在能做什么（spike 版）

| 界面 | 背后调用 |
|---|---|
| 已装工具列表 / 设为默认 / 卸载 | `pk list --json`、`pk use <tool>@<version> --json`、`pk uninstall <tool> --yes` |
| 可装版本 / 版本选择 | `pk list --available --json`、`pk list <tool> --json` |
| 安装 + 进度条 | `pk install <spec> --json`（NDJSON 流，`progress` 事件驱动进度条） |
| 镜像档切换 | `pk mirror list --json`、`pk mirror use <name>` |
| 磁盘回收 | `pk gc --json`（GET = 预演，POST = 真删） |
| 体检 | `pk doctor --json [--globals]` |

## 跑起来

```bash
cd cmd/pk-desktop
go build -o ../../bin/pk-desktop .

# ① 浏览器形态（前端是普通静态页 + /api/*，无需 Wails 运行时）
../../bin/pk-desktop --serve --addr 127.0.0.1:7799

# ② 桌面壳（Wails v3.1.0；macOS/Windows/Linux 各自需要 WebView 运行时）
../../bin/pk-desktop
```

没装 CLI 时页面顶部会直接给出安装命令；GUI 自己**不会**下载任何东西。

## 为什么不用 Wails 绑定

前端走 `/api/*`（普通 HTTP + NDJSON 流）而不是 Wails 的 JS 绑定，好处是：

- 桌面壳与 `--serve` 共用一套代码，浏览器里也能验收；
- 契约就是 `docs/json-schema.md`，不需要生成前端绑定；
- 前端可以用任何框架（现在是零依赖的 vanilla，后续换 Vue3 时只换 `assets/`）。

## 测试

`internal/pkcli` 有单测：

- `pkcli_test.go`：假 `pk` 脚本覆盖定位 / schema 与版本门禁 / NDJSON 事件流 / 退出码 5；
- `contract_test.go`：拿**真实的** `bin/pk` 跑 `version/list/doctor/mirror list/gc --dry-run`，
  断言 GUI 侧结构体还能解析（契约漂移先在这里红；用临时 `PK_ROOT`，不碰用户状态）。
`cmd/pk-desktop` 不参与 `make loc`（GUI 单列预算）。
