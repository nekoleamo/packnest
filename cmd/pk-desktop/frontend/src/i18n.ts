// GUI 的最小 i18n：**中文是源字符串**，英文靠字典查表。
//
// 为什么这么做（而不是 key 表）：界面文案本来就写中文，翻译只在需要时补；
// 找不到译文就退回中文，永远不会出现 "missing.key" 这种空洞。
// 新增/改动文案不需要先登记 key，改中文即改源串。
import { ref } from "vue";

export type Locale = "zh" | "en";

const dict: Record<string, string> = {
  // ---- 顶部 ----
  "首次使用先装 CLI（窗口重开后自动接管）：": "Install the CLI first (this window picks it up on reopen):",
  "复制": "Copy",
  "已复制": "Copied",
  "跟随系统": "System",
  "浅色": "Light",
  "深色": "Dark",
  "配色：": "Theme: ",
  "中文": "中文",
  EN: "EN",

  // ---- 已装工具 ----
  "已装工具": "Installed tools",
  "刷新": "Refresh",
  "条记录": "records",
  "可升级": "Upgradable",
  "升级": "Upgrade",
  "工具": "Tool",
  "版本": "Version",
  "来源": "Source",
  "大小": "Size",
  "当前默认": "Default",
  "设为默认": "Set default",
  "路径": "Path",
  "卸载": "Uninstall",
  "复制安装路径": "Copy install path",
  "未校验": "unverified",
  "只有": "only",
  "个校验源": "checksum source",
  "自带": "bundled ",
  "还没有装任何工具：在下面的「安装队列」里加一条。":
    "Nothing installed yet — add a spec to the install queue below.",

  // ---- 安装队列 ----
  "安装队列": "Install queue",
  "加入队列": "Add to queue",
  "开始安装": "Start",
  "清除已完成": "Clear finished",
  "移出队列": "Remove",
  "进度": "Progress",
  "状态": "Status",
  "排队中": "queued",
  "启动中": "starting",
  "队列为空：填一个 spec 加进来，多条会按顺序依次装。":
    "Queue is empty — add a spec; multiple specs install one after another.",
  "可选版本": "Available versions",
  "全部版本": "All versions",
  "过滤版本": "Filter versions",
  "装这个版本": "Install this version",

  // ---- 镜像档 ----
  "镜像档": "Mirror profile",
  "切换": "Switch",
  "当前": "current",
  "探测各源": "Probe sources",
  "探测中…": "probing…",
  "环境变量": "Environment variables",
  "auto（自动探测，缓存 6h）": "auto (probe, cached 6h)",
  "只探某个工具（留空=全部）": "Probe one tool (empty = all)",
  "写入": "Write to ",
  "只改 registry 一行；写前自动备份 npmrc.pk.bak":
    "only the registry line changes; a backup is written first",

  // ---- 环境 ----
  "环境": "Environment",
  "哪来的": "Where from?",
  "读取当前环境": "Read current environment",
  "项目级": "Project-level ",
  "复制eval片段": "Copy eval snippet",
  "PATH（按优先级）": "PATH (by priority)",
  "点「读取当前环境」看 pk 到底导出了什么（只读，不写任何 rc）。":
    'Click "Read current environment" to see what pk exports (read-only, never writes your rc).',

  // ---- 体检 ----
  "体检": "Doctor",
  "运行pkdoctor": "Run pk doctor",
  "一键修复": "Fix now",
  "只修 PATH 托管块与 pk/packnest 入口，其余只报不改":
    "only repairs the PATH managed block and the pk/packnest entries; everything else is reported, not changed",
  "含全局包": "Include global packages",
  "实测原生模块（--probe）": "Really load native modules (--probe)",
  "没有发现问题": "No problems found",
  "全局包": "Global packages",
  "包": "Package",
  "问题": "Problem",
  "详情": "Details",
  "级别": "Level",
  "引擎不满足 / ABI 绑定：切 node 版本前先处理，或在对应 node 版本下":
    "engines unmet / ABI bound: fix before switching node, or run under that node version",
  "已修": "Fixed",

  // ---- 回收 ----
  "磁盘占用与回收": "Disk usage & reclaim",
  "每工具保留": "Keep per tool",
  "个": "",
  "预演": "Preview",
  "执行GC": "Run GC",
  "预演结果（未删除）": "Preview (nothing deleted)",
  "将删版本": "Versions to remove",
  "将删缓存": "Cache to remove",
  "已删版本": "Versions removed",
  "已删缓存": "Cache removed",
  "仍被引用（保留）": "Still referenced (kept)",
  "按保留数留下": "Kept by --keep",
  "释放空间": "Freed",
  "没有可回收的东西。": "Nothing to reclaim.",
  "点「预演」看会删什么（不会真删）。":
    'Click "Preview" to see what would be deleted (nothing is).',

  // ---- 设置 / 说明 ----
  "设置": "Settings",
  "开机自启": "Launch at login",
  "默认关；只写用户级自启条目（LaunchAgent / HKCU Run / ~/.config/autostart）":
    "off by default; writes a user-level entry only (LaunchAgent / HKCU Run / ~/.config/autostart)",
  "说明": "About",
  "这是": "This is ",
  "的可视化补充：所有操作都通过调用本机已安装的":
    "'s visual supplement: every action calls the installed ",
  "（子进程 + ": " (subprocess + ",
  "）完成，": ") on your machine, ",
  "没有装 CLI 时这里不会自己下载任何东西。":
    "and nothing is ever downloaded when the CLI is missing.",

  // ---- 对话框 / 行内提示 ----
  "把当前镜像档的 registry 写进 ~/.npmrc？只改 registry 一行，写前会备份。":
    "Write the current mirror's registry into ~/.npmrc? Only that one line changes, with a backup first.",
  "原有行已替换": "existing line replaced",
  "新写入": "written",
  "执行 GC 会删除零引用版本与超限缓存": "GC deletes unreferenced versions and over-limit cache",
  "，继续？": ", continue?",
  "（每个工具保留最新 ": " (keep the newest ",
  " 个）": " per tool)",
  "把 ": "Upgrade ",
  " 升到最新版（": " to the newest version (",
  "）？旧版本会保留，可用「卸载」清理。":
    ")? Old versions are kept; clean them with Uninstall.",
  "卸载 ": "Uninstall all versions of ",
  " 的全部版本？": "?",
  "解析": "resolve",
  "下载中…": "downloading…",
  "下载": "download",
  "校验": "verify",
  "完成": "done",
  "缓存命中": "cache hit",
  "解析→": "resolving → ",
  "索引最新": "index latest ",
  "将删": "to remove",
  "已删": "removed",
  "（原有行已替换）": " (existing line replaced)",
  "（新写入）": " (written)",
  "当前平台不支持": "not supported on this platform",

  // ---- 补齐：模板里实际用到的键（与源串逐字一致，含空格与标点）----
  "仍被引用": "Still referenced",
  "写入 ~/.npmrc": "Write to ~/.npmrc",
  "只有 1 个校验源": "only 1 checksum source",
  "命令名（node / pnpm …）": "command name (node / pnpm …)",
  "哪来的？": "Where from?",
  "复制 eval 片段": "Copy eval snippet",
  "延迟": "Latency",
  "执行 GC": "Run GC",
  "每个工具保留最新的 N 个版本（pk gc --keep N）；0 = 不按版本保留":
    "keep the newest N versions per tool (pk gc --keep N); 0 = no version-based keeping",
  "源": "Source",
  "安装中…": "installing…",
  "运行 pk doctor": "Run pk doctor",
  "项目级（pk.lock）": "Project-level (pk.lock)",
  "（子进程 +": " (subprocess + ",
  "）完成，没有装 CLI 时这里不会自己下载任何东西。":
    ") on your machine; nothing is downloaded when the CLI is missing.",
};

const stored = typeof localStorage !== "undefined" ? localStorage.getItem("packnest.locale") : null;

function detect(): Locale {
  if (stored === "zh" || stored === "en") return stored;
  const lang = typeof navigator !== "undefined" ? navigator.language || "" : "";
  return lang.toLowerCase().startsWith("zh") ? "zh" : "en";
}

export const locale = ref<Locale>(detect());

export function setLocale(l: Locale) {
  locale.value = l;
  try {
    localStorage.setItem("packnest.locale", l);
  } catch {
    /* 无存储权限时只在本次会话生效 */
  }
}

// t 把中文源串翻成当前语言；没有译文就原样返回（英文界面里中文句子也算可读的降级）。
export function t(zh: string): string {
  if (locale.value === "zh") return zh;
  const hit = dict[zh];
  return hit === undefined ? zh : hit;
}
