package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// commandHelp 是 9 个命令的 `--help` 文本（方案 §12.2 第 10 条：命令要能自解释）。
var commandHelp = map[string]string{
	"install": `pk install [工具[@版本]]... [--frozen] [--insecure] [--force] [--force-rehash] [--quiet] [--jobs N]

  装工具。不带参数时按当前目录的 pk.toml 全装。
  作用域：没有 --global/--project 开关，由当前目录决定：
    当前目录有 pk.toml → 按项目声明装，并生成/更新 pk.lock（项目级）
    没有 pk.toml        → 装进共享目录；若该工具还没有全局默认，则自动设为默认
  磁盘上永远只有一份（~/.packnest/tools/<tool>/<ver>），项目级只体现"锁定 + PATH 顺序"，
  不复制副本；项目内生效用 pk env --project（direnv 里跑）。
  语义（方案 §18.9）：
    pk install                 按 pk.toml 全装（没有 pk.toml 则报错）
    pk install node            装 pk.toml 里 pin 的 node；没有则最新稳定版
    pk install node@22         显式 spec，不受 pk.toml 影响
  开关：
    --frozen    严格按 pk.lock（不做解析；清单与锁不一致即失败，CI 用）
    --insecure  允许无校验安装（标记 unverified；校验和源互相矛盾时也靠它放行）
    --force     重装已存在的版本
    --force-rehash  忽略本地校验表（state/known.json），重新向校验源求值并覆盖记录
    --jobs N    预留（当前固定单连接：国内 CDN 分片常更慢）
    --quiet     只报错，不打印进度与结果
  退出码：0 成功 / 1 通用 / 2 用法 / 3 网络 / 4 校验 / 5 锁被占用`,

	"uninstall": `pk uninstall <工具>@<版本> [--force]

  台账驱动卸载：只删台账里记录过的目录（不在台账里的目录只提示不删）。
  被引用（全局默认 / 项目锁 / 其他工具依赖）的版本需要 --force。`,

	"use": `pk use <工具>@<版本> [--project]
pk use --lock

  切换全局默认版本：写 config.toml、更新台账引用、把 shim 重指到该版本。
  只会切到**已安装**的版本（先 pk install）。
  --project  不改全局，改为「固定到当前项目」：写 pk.toml + 重新生成 pk.lock
  --lock     只按现有 pk.toml 重新生成 pk.lock（不切版本、不装东西）`,

	"list": `pk list [工具] [--major N] [--available] [--refresh] [--all|--limit N] [--json]

  不带参数：列出已装工具（* 是当前全局默认），带版本占用空间。这个不带网络。
  pk list node：只看 node —— 已装版本 + 可装版本，旧 → 新、最新在最下面；
                版本 ≤40 个时一版一行（带 latest / lts: <代号> / 已装 标签），
                更多时自动换成「按大版本分组 + 多列网格」（列数按终端宽度算），
                标记：* 最新 · l LTS · + 已装 · @ 当前默认。
  --major N   只看某个大版本（0.x 写 0.10），默认列该大版本的全部版本。
  --available 每个工具一行，给最新版与最近 5 个（索引 24h 缓存，离线读缓存）。
  --refresh   忽略缓存强制回源。
  --limit N   配合工具名：只列最近 N 个（默认 20）；--all 等于 --limit 0。`,

	"which": `pk which <命令> [--json]

  解析 shim 到真实二进制路径（调试 shim / 双名一致性检查）。
  例：pk which node；pk which pk 与 pk which packnest 必须指向同一个文件。`,

	"mirror": `pk mirror list|probe|use <档位>|write

  list    列出内嵌档位（default/cn/tuna/aliyun/ustc/auto）与当前档位
  probe   并行探测各工具索引源（结果缓存 6h；--tool 只探一个）
  use     切换档位（写 config.toml）
  write   把当前档位的 registry 写进 ~/.npmrc（先备份、只增不改）`,

	"doctor": `pk doctor [--json] [--globals] [--probe]

  环境体检，问题码固定（GUI 与脚本依赖）：
    PATH_ORDER / PATH_MISSING         bin 目录不在 PATH 最前
    LEDGER_BROKEN / LEDGER_RECOVERED  台账损坏 / 已从 .bak 恢复
    CONFIG_BROKEN / MIRROR_PROFILE_INVALID
    MIRROR_UNREACHABLE（--probe）      索引源不可达
    GO_TOOLCHAIN_NOT_LOCAL            装了 go 但 GOTOOLCHAIN 不是 local
    GLOBAL_ENGINES_UNMET / GLOBAL_NATIVE_ABI / GLOBAL_SHADOWS_PROJECT（--globals）
    FOREIGN_MANAGER                   检测到 proto/nvm/mise 等其他管理器（--foreign 列出版本）
    FOREIGN_SHIM_PRIORITY             别人的 shim 排在 pk 前面，会抢走 node
  退出码：有 error 级问题时为 1。`,

	"env": `pk env [--shell bash|zsh|sh|powershell] [--write] [--unset] [--completion SHELL] [--json]

  输出 shell 片段（PATH 三段 + registry/共享前缀变量）。
  默认只打印不落盘；--write 才写进 rc 的托管块（幂等，块外内容不动）。
  --completion bash|zsh|powershell 输出补全脚本：eval "$(pk env --completion zsh)"
  --rc PATH   指定写入的 rc 文件（默认按 $SHELL 猜）
  --project   把 pk.toml/pk.lock 锁定的版本前置到 PATH（项目级覆盖全局默认）：
              echo 'eval "$(pk env --project)"' > .envrc && direnv allow`,

	"gc": `pk gc [--dry-run] [--cache-max 5GB] [--json]

  回收零引用版本（被引用的列在 KeptRef 里）与超限的产物缓存（LRU）。
  默认上限取 config.toml 的 cache_max。`,

	"version": `pk version [--json]

  打印版本与构建信息（渠道安装时是机器可读输出）。`,
}

// printCommandHelp 打印某个命令的详细帮助。
func printCommandHelp(w io.Writer, cmd string) bool {
	if text, ok := commandHelp[cmd]; ok {
		fmt.Fprintln(w, text)
		return true
	}
	return false
}

// commandNames 返回有详细帮助的命令名（排序）。
func commandNames() []string {
	names := make([]string, 0, len(commandHelp))
	for name := range commandHelp {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// helpSummary 是 `pk help` 的总览（与 printUsage 一致，但列出可查询的命令）。
func helpSummary(w io.Writer) {
	fmt.Fprintln(w, "详细用法: pk help <命令>，可用命令: "+strings.Join(commandNames(), " / "))
}
