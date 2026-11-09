# packnest 项目规则

> 总体方案：`~/Documents/Plan/17-包管理器/pn-总体方案-20261109.md`（64KB，只在需要细节时按需读章节，别整份进上下文）。本文件只放**会改变行为的硬约束**，能少写就少写 —— 它每轮都进提示。

## 一句话

`pk`（发布名 `packnest`）是**多语言运行时安装器**：Go、单二进制、`CGO_ENABLED=0`，装 node/npm/pnpm/bun/deno/go/python，内置国内镜像档 + 自动探测，node 装完自动配对最新兼容 npm，卸载可回滚到零残留。GUI（Wails v3.1.0）在 M6+，v1 只有 CLI。

## 硬约束（写死为门禁，不许"顺手放宽"）

| # | 约束 | 越界后果 |
|---|---|---|
| 1 | **第三方依赖 = 2 个**：`Masterminds/semver/v3`、`BurntSushi/toml` | 编译时间、体积、供应链都崩 |
| 2 | **代码 ≤ 8,500 行**（不含测试，`make check` 统计 `cmd/`+`internal/`） | 违背"一个下午读得完" |
| 3 | **命令面 9 个**：install/uninstall/use/list/which/mirror/doctor/env/gc | 加命令先改方案文档 |
| 4 | **`CGO_ENABLED=0` 6 平台**：darwin/linux/windows × amd64/arm64 | 单二进制承诺作废 |
| 5 | **归档只 2 种**：tar.gz + zip。解压零第三方（`archive/tar`+`compress/gzip`+`archive/zip`） | 不许引 xz/7z/compress |
| 6 | **默认 `go test ./...` 零外网** | 真网测试一律 `//go:build nettest`，不进门禁 |
| 7 | **单根 `PK_ROOT`**（默认 `~/.packnest`），永不 sudo、永不写 shell rc（`pk env --write` 例外，带 `# managed by pk` 块） | 破坏"无损" |
| 8 | **默认不动用户已有配置**（不写 `~/.npmrc`、不迁移 `~/.npm` 缓存） | `pk mirror write` 才写，且写前备份 |

## 不许引的库（每条都有替代）

`cobra`/`urfave/cli`（std `flag` + 手写分派）· `viper`（只读 TOML）· `logrus`/`zap`（`log/slog`）· 第三方进度条（`\r` + 百分比，非 TTY 退化逐行）· i18n 框架（中文硬编码）· 任何 daemon/IPC（文件锁）· 任何 CGO 库。

## 代码约定

- **包职责按方案 §4.3 预算表**：`internal/{spec,provider,mirror,fetch,archive,verify,store,companion,shim,env,jsonout,toolchain,testkit}` + `cmd/pk`。新增包先改预算表。
- **依赖方向单向**：`cmd/pk → internal/*`；`internal` 之间只许 `上层 → 下层`，禁止环。GUI（未来 `cmd/pk-desktop`）只许 import `internal/core` 的公开函数，`core` 永不得出现 Wails 符号。M5 前 Wails 不进主 `go.mod`。
- **可测性从第一行代码开始**：`PK_ROOT` 取代硬编码路径 · `fetch` 的 `http.Client` 走接口注入（不是包级变量）· 时钟可注入（TTL/ETag 过期不用 `time.Sleep`）· 平台分支走 `GOOS` 参数 + 单测替身。
- **原子落盘**：下载 → `.part` → 校验 → 解压到 `tools/<tool>/<ver>.tmp.<rand>` → `rename` → 写台账（先 `.bak`）。任一步失败删 tmp，**不留半成品目录**。Windows `os.Rename` 不能覆盖目录 → `.old` 改名三步法。
- **路径统一走 `internal/paths`**（Windows 长路径 `\\?\` 前缀化），不许在业务代码里拼 `filepath.Join(home, ...)`。
- **错误码固定**：0 成功 / 1 通用 / 2 用法 / 3 网络 / 4 校验 / 5 已被其他 pk 占用锁。测试靠退出码断言。
- **`--json` 是 GUI 与脚本的唯一契约**：schema 字段只增不改（`schema:1`）；`install --json` 是 NDJSON 事件流（`resolve`/`download`/`verify`/`done`）。改了必须同步 golden 文件。
- 注释与文档中文，标识符英文。错误信息给**可执行建议**（"镜像最新到 vX，用 `pk install node@X` 或换档 `pk mirror use default`"），不静默降级。

## 测试规则（本项目最看重）

- **验收项 = 测试名**：里程碑验收先写空壳 `TestAcceptance_M<n>_<Name>`（编译期即登记），再填实现。方案 §12 列出的用例名**不许改名、不许删**。
- **夹具用代码造，不存二进制**：`internal/testkit` 里 `MakeNode(ver, npmVer)` / `MakeNpmRegistry(map)` / `Mirror(t, FS{...})`（`httptest.Server` 扮演 npmmirror/官方源），故障注入用可组合的 `testkit.Behavior`（`Status`/`TruncateAt`/`Slow`/`NoRange`/`WrongETag`/`StaleIndex`/`CorruptArchive`/`MissingChecksum`/`ChecksumMismatch`/`DropAfter`）。
- **门禁 `make check`（7 项）**：gofmt+vet → `go test -race -count=1` → 覆盖率（已实现包 ≥85%，cmd ≥70%；新包在 `Makefile` 的 `COVER_PKGS` 里登记后才纳入）→ 6 平台交叉编译 → fuzz 各 30s → benchmark 阈值（`BenchmarkShimResolve`<15ms、`BenchmarkSpecParse`<50µs）→ `--json` golden 对比。`make accept` 只跑 `TestAcceptance_*`。
- 只有 3 件事允许人工确认：Windows `.cmd` shim 被 PowerShell 调用、macOS Gatekeeper 首启、GUI 的中文字体/托盘。其余一律自动化。

## 里程碑口径

M0 骨架 → M1 node+npm（伴生 npm、fetch/shim/TOFU）→ M2 pnpm/bun/deno → M3 go+python → M4 台账/锁/GC/全局包 → M5 CLI 收口冻结 `--json` → **CLI-DONE 10 条全绿才动 GUI**。当前进度写在 `docs/progress.md`（每完成一个里程碑更新一行）。

## 两个必须写进测试的坑（别重犯）

1. **npm 预发布陷阱**：`12.0.0-pre.0.0` 的 engines 满足 node 20 且排序高于 `11.21.2` → 反查必须排除预发布。
2. **npm 补丁号级分界**：22.21.1→npm 11.x？不，22.22.2 才→12.x；24.14.0→11.x，24.15.0→12.x。按大版本算必错。

## 目录与产物

`cmd/pk/`（CLI）· `internal/*` · `providers/*.toml`（`//go:embed` 编进二进制，合计 <50KB，**首次使用只有一个下载面：产物本身**）· `frontend/`（M6）· `docs/`（README/9 命令 help/故障排查/progress）。

## 常用命令

```bash
make build      # CGO_ENABLED=0 本地构建 ./bin/pk
make test       # 零外网单测
make check      # 7 项门禁（提交前必跑）
make accept     # 只跑验收用例
make cross      # 6 平台交叉编译到 dist/
go run ./cmd/pk doctor --json
```
