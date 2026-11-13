<script setup lang="ts">
import { computed, onMounted, ref, watchEffect } from "vue";
import { locale, setLocale, t } from "./i18n";
import { api, install, type AvailableTool, type EnvDoc, type GCResult, type InstallEvent, type Issue, type Mirror, type Outdated, type ProbeEntry, type Tool, type WhichResult } from "./api";

const status = ref<{ ok: boolean; reason?: string; path?: string; version?: string; platform?: string }>({ ok: false });
const tools = ref<Tool[]>([]);
// 1.3：已装版本落后索引最新时后端会在 /api/tools 里带上 outdated（只读索引缓存，不联网）
const outdated = ref<Outdated[]>([]);
const fixed = ref<string[]>([]);
// 开机自启（默认关；只写用户级条目）
const autoStart = ref<{ supported: boolean; enabled: boolean; path: string; note?: string } | null>(null);
async function loadAutostart() {
  try {
    autoStart.value = await api.autostart();
  } catch {
    autoStart.value = null;
  }
}
async function toggleAutostart() {
  if (!autoStart.value) return;
  try {
    autoStart.value = await api.setAutostart(!autoStart.value.enabled);
  } catch (e) {
    err.value = String(e);
  }
}
const available = ref<AvailableTool[]>([]);
const versions = ref<{ version: string; installed?: boolean; lts?: string }[]>([]);
const issues = ref<Issue[]>([]);
const mirror = ref<Mirror | null>(null);
const tool = ref("");
const picked = ref("");
const globals = ref(false);
const deepProbe = ref(false);
const allVersions = ref(false);
const verFilter = ref("");
const probeTool = ref("");
const probeRows = ref<ProbeEntry[]>([]);
const probing = ref(false);
const err = ref("");
const busy = ref(false);

const installed = computed(() => tools.value);

async function refreshAll() {
  await Promise.all([loadTools(), loadMirrors(), loadDoctor(), loadAvailable()]);
}
onMounted(loadAutostart);
async function loadStatus() {
  try {
    const s = await api.status();
    status.value = { ok: s.ok, reason: s.reason, path: s.path, version: s.cli?.version, platform: s.platform };
  } catch (e) {
    status.value = { ok: false, reason: String(e) };
  }
}
// 没装 CLI 时引导用的安装命令：Windows 用 PowerShell 版，其余平台用 sh 版
const installCmd = computed(() =>
  status.value.platform === "windows"
    ? "iwr https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.ps1 -UseB | iex"
    : "curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh",
);
const copiedHint = ref(false);
async function copyInstallCmd() {
  try {
    await navigator.clipboard.writeText(installCmd.value);
    copiedHint.value = true;
    setTimeout(() => (copiedHint.value = false), 1600);
  } catch {
    /* 没有剪贴板权限时用户仍可手动选中 */
  }
}

async function loadTools() {
  const res = await api.tools();
  tools.value = res.tools ?? [];
  outdated.value = res.outdated ?? [];
}
// 可升级的提示按工具名索引：升级按钮直接给 `pk install <tool>@latest`（与 CLI 的建议一致）
const outdatedFor = (name: string) => outdated.value.find((o) => o.name === name);
async function upgrade(name: string) {
  if (!confirm(t("把 ") + name + t(" 升到最新版（") + (outdatedFor(name)?.latest ?? "latest") + t("）？旧版本会保留，可用「卸载」清理。"))) return;
  err.value = "";
  busy.value = true;
  try {
    const spec = `${name}@latest`;
    await install(spec, () => {});
    await Promise.all([loadTools(), loadVersions()]);
  } catch (e) {
    err.value = String(e);
  } finally {
    busy.value = false;
  }
}
async function loadAvailable() {
  const list = (await api.available()).available ?? [];
  // node 排最前（其余保持 CLI 顺序），首屏默认选中的就是最常用的那个
  list.sort((a, b) => rank(a.name) - rank(b.name));
  available.value = list;
  if (!tool.value && available.value.length) {
    tool.value = available.value[0].name;
    await loadVersions();
  }
}
function rank(name: string) {
  const order = ["node", "pnpm", "bun", "go", "python", "npm"];
  const i = order.indexOf(name);
  return i < 0 ? order.length : i;
}
async function loadVersions() {
  if (!tool.value) return;
  const v = await api.versions(tool.value, allVersions.value);
  // CLI 已经按新到旧返回，保持原序：第一项就是最新，默认选中它
  versions.value = v.available ?? [];
  picked.value = versions.value[0]?.version ?? "";
}

// 版本过滤：本地过滤已加载的列表（--all 时是全部版本）
const shownVersions = computed(() => {
  const q = verFilter.value.trim();
  if (!q) return versions.value;
  return versions.value.filter((v) => v.version.includes(q));
});
async function loadMirrors() {
  const m = await api.mirrors();
  mirror.value = m;
  mirrorPick.value = m.current;
}

// 下拉里把 auto 一并列出来（`pk mirror list` 只在末尾用括号提它，但它是个合法档位）
const mirrorPick = ref("auto");
const mirrorOpts = computed(() => [
  { name: "auto", label: "auto（自动探测，缓存 6h）" },
  ...(mirror.value?.mirrors ?? []).map((m) => ({ name: m.name, label: `${m.name} · ${m.display}` })),
]);
async function loadDoctor() {
  err.value = "";
  fixed.value = [];
  try {
    issues.value = (await api.doctor(globals.value, deepProbe.value)).issues ?? [];
  } catch (e) {
    err.value = String(e);
  }
}
// 一键修复：只修 PATH 托管块与 pk/packnest 入口（其余问题只报不改，和 CLI 一致）
async function fixProblems() {
  err.value = "";
  try {
    const res = await api.doctor(globals.value, deepProbe.value, true);
    fixed.value = res.fixed ?? [];
    issues.value = res.issues ?? [];
    if (fixed.value.length) await loadTools();
  } catch (e) {
    err.value = String(e);
  }
}
async function runProbe() {
  probing.value = true;
  err.value = "";
  try {
    probeRows.value = (await api.probe(probeTool.value || undefined)).results ?? [];
  } catch (e) {
    err.value = String(e);
  } finally {
    probing.value = false;
  }
}
async function copyPath(p?: string) {
  if (!p) return;
  try {
    await navigator.clipboard.writeText(p);
  } catch {
    /* 浏览器/壳里可能没有剪贴板权限 */
  }
}

// ---- 主题：auto 跟随系统，light/dark 手动钉死；写进 <html data-theme> 由 CSS 变量接管 ----
type Theme = "auto" | "light" | "dark";
const themeOpts: { v: Theme; label: string }[] = [
  { v: "auto", label: "跟随系统" },
  { v: "light", label: "浅色" },
  { v: "dark", label: "深色" },
];
const theme = ref<Theme>((localStorage.getItem("pk-theme") as Theme) || "auto");
const mq = window.matchMedia("(prefers-color-scheme: dark)");
const sysDark = ref(mq.matches);
mq.addEventListener("change", (e) => (sysDark.value = e.matches));
watchEffect(() => {
  const dark = theme.value === "dark" || (theme.value === "auto" && sysDark.value);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  localStorage.setItem("pk-theme", theme.value);
});

// ---- 安装队列：多个 spec 依次跑，复用同一个 /api/install 流 ----
interface QueueItem {
  spec: string;
  state: "wait" | "run" | "ok" | "fail";
  msg: string;
  percent: number;
}
const queue = ref<QueueItem[]>([]);
const queueInput = ref("node@22");

function enqueue(spec?: string) {
  const s = (spec ?? queueInput.value).trim();
  if (!s) return;
  queue.value.push({ spec: s, state: "wait", msg: "", percent: 0 });
  queueInput.value = "";
}
function dequeue(i: number) {
  if (queue.value[i]?.state === "run") return;
  queue.value.splice(i, 1);
}
function clearDone() {
  queue.value = queue.value.filter((q) => q.state === "wait" || q.state === "run");
}
const pending = computed(() => queue.value.some((q) => q.state === "wait"));

// evText 把 CLI 的 NDJSON 事件翻译成一行人话（progress 太密，只在进度条上体现）
function evText(ev: InstallEvent): string {
  const at = ev.version ? "@" + ev.version : "";
  switch (ev.ev) {
    case "resolve":
      return `解析 → ${ev.tool ?? ""}${at}`;
    case "download":
      return ev.bytes_total ? `下载 ${fmtMB(ev.bytes_done)} / ${fmtMB(ev.bytes_total)}` : "下载中…";
    case "verify":
      return `校验 ${ev.mode ?? ""}`.trim();
    case "done":
      return `完成 ${ev.tool ?? ""}${at}`;
    default:
      return `${ev.ev} ${ev.msg ?? ""}`.trim();
  }
}

async function runQueue() {
  if (busy.value) return;
  busy.value = true;
  err.value = "";
  try {
    for (const item of queue.value) {
      if (item.state !== "wait") continue;
      item.state = "run";
      item.percent = 0;
      item.msg = "启动中…";
      try {
        await install(item.spec, (ev) => {
          if (ev.ev === "progress") {
            if (ev.bytes_total) item.percent = Math.round(((ev.bytes_done ?? 0) / ev.bytes_total) * 100);
            return;
          }
          item.msg = evText(ev);
          if (ev.ev === "done") item.percent = 100;
        });
        item.state = "ok";
        item.percent = 100;
      } catch (e) {
        // 一条失败不影响后续：队列继续，状态留在行上
        item.state = "fail";
        item.msg = String(e);
      }
    }
  } finally {
    busy.value = false;
    await refreshAll();
  }
}

async function setDefault(t: Tool) {
  await api.use(t.name, t.version);
  await loadTools();
}
async function removeTool(tool: Tool) {
  if (!confirm(t("卸载 ") + tool.name + t(" 的全部版本？"))) return;
  try {
    await api.uninstall(tool.name);
  } catch (e) {
    alert(String(e));
  }
  await loadTools();
}
async function switchMirror() {
  if (!mirror.value || mirrorPick.value === mirror.value.current) return;
  try {
    await api.useMirror(mirrorPick.value);
  } catch (e) {
    err.value = String(e);
  }
  await loadMirrors();
}

// ---- 环境页：which（命令 → 真实路径）+ env（当前 PATH/变量）+ mirror write ----
const whichCmd = ref("node");
const whichRes = ref<WhichResult | null>(null);
const whichErr = ref("");
const envDoc = ref<EnvDoc | null>(null);
const envProject = ref(false);
const npmrcOut = ref("");

async function runWhich() {
  if (!whichCmd.value.trim()) return;
  whichErr.value = "";
  whichRes.value = null;
  try {
    whichRes.value = await api.which(whichCmd.value.trim());
  } catch (e) {
    whichErr.value = String(e);
  }
}
async function loadEnv() {
  err.value = "";
  try {
    envDoc.value = await api.env(envProject.value);
  } catch (e) {
    err.value = String(e);
  }
}
async function copyEval() {
  if (!envDoc.value) return;
  const sh = envDoc.value.shell === "powershell" ? "powershell" : "bash";
  try {
    await navigator.clipboard.writeText(`eval "$(pk env --shell ${sh})"`);
  } catch {
    /* 没剪贴板权限就算了 */
  }
}
async function writeNpmrc() {
  if (!confirm(t("把当前镜像档的 registry 写进 ~/.npmrc？只改 registry 一行，写前会备份。"))) return;
  err.value = "";
  try {
    const r = await api.mirrorWrite();
    npmrcOut.value = `${r.file} → ${r.registry}${r.replaced ? t("（原有行已替换）") : t("（新写入）")}`;
  } catch (e) {
    err.value = String(e);
  }
}

// ---- GC：结果做成分组表格，不再直接打 JSON ----
const gcRes = ref<GCResult | null>(null);
// 保留版本数：0 = 不保留（只清零引用），N>0 = 每个工具留最新 N 个（`pk gc --keep N`）
const gcKeep = ref(0);
async function runGC(run: boolean) {
  const keepNote = gcKeep.value > 0 ? t("（每个工具保留最新 ") + gcKeep.value + t(" 个）") : "";
  if (run && !confirm(t("执行 GC 会删除零引用版本与超限缓存") + keepNote + t("，继续？"))) return;
  err.value = "";
  try {
    gcRes.value = await api.gc(run, gcKeep.value);
  } catch (e) {
    err.value = String(e);
  }
  if (run) await loadTools();
}
// 预演与真删的描述不一样（"将删" vs "已删"），别让用户以为已经删了
const gcVerb = computed(() => (gcRes.value?.DryRun ? t("将删") : t("已删")));
const gcGroups = computed(() => {
  const r = gcRes.value;
  if (!r) return [];
  return [
    { title: `${gcVerb.value}版本`, rows: r.Removed ?? [] },
    { title: `${gcVerb.value}缓存`, rows: r.CacheRemoved ?? [] },
    { title: "仍被引用（保留）", rows: r.KeptRef ?? [] },
    { title: `按 --keep 保留（每工具最近 ${r.keep ?? 0} 个）`, rows: r.KeptRecent ?? [] },
  ].filter((g) => g.rows.length);
});

// ---- 体检：全局包问题单独表格化（detail 里有 pkg/needs/have/path） ----
const isGlobalIssue = (i: Issue) => !!(i.detail?.pkg || i.detail?.global_pkg);
const globalIssues = computed(() => issues.value.filter(isGlobalIssue));
const otherIssues = computed(() => issues.value.filter((i) => !isGlobalIssue(i)));

const codeLabels: Record<string, string> = {
  GLOBAL_ENGINES_UNMET: "node 版本不满足",
  GLOBAL_NATIVE_ABI: "原生模块 ABI",
  GLOBAL_SHADOWS_PROJECT: "遮蔽项目命令",
  GLOBALS_NONE: "没有全局包",
  GLOBALS_SCAN_FAILED: "全局包扫描失败",
  PATH_MISSING: "PATH 缺目录",
  PATH_ORDER: "PATH 顺序",
  SHIM_MISSING: "入口缺失",
  SHIM_BROKEN: "入口损坏",
  SHIM_PLAIN: "入口不是我方",
  LEDGER_BROKEN: "台账损坏",
  LEDGER_RECOVERED: "台账已恢复",
  LEDGER_INCONSISTENT: "台账不一致",
  MIRROR_AUTO: "镜像档 auto",
  MIRROR_UNREACHABLE: "镜像不可达",
  MIRROR_PROFILE_INVALID: "镜像档无效",
  CONFIG_BROKEN: "配置损坏",
  ROOT_ABSENT: "还没装过工具",
  PROVIDERS_BAD: "外部清单有问题",
  PROVIDERS_DIRS: "外部清单目录",
  PROVIDER_HTTP_URL: "外部清单用 http",
  FOREIGN_NONE: "没有其他版本管理器",
  FOREIGN_MANAGER: "检测到其他版本管理器",
  FOREIGN_SHIM_PRIORITY: "其他入口优先",
  GO_TOOLCHAIN_NOT_LOCAL: "GOTOOLCHAIN 不在本地",
  GO_INSTALL_NOT_IN_PATH: "GOBIN 不在 PATH",
  PIP_USER_SITE_UNMANAGED: "pip 用户目录未纳管",
};
const issueLabel = (code: string) => codeLabels[code] ?? code;

// globalWhat 把全局包问题的 detail 拼成一句能对上表头的话
function globalWhat(i: Issue): string {
  const d = i.detail ?? {};
  switch (i.code) {
    case "GLOBAL_ENGINES_UNMET":
      return `要求 node ${d.needs}，当前 ${d.have}`;
    case "GLOBAL_NATIVE_ABI":
      return d.file ? `加载失败：${baseName(String(d.file))}` : "原生模块与当前 node ABI 绑定";
    case "GLOBAL_SHADOWS_PROJECT":
      return `命令 ${d.bin} 遮蔽项目的 ${d.project_pkg}`;
    default:
      return i.message;
  }
}
const globalPkg = (i: Issue) => String(i.detail?.pkg ?? i.detail?.global_pkg ?? "—");
function baseName(p: string) {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] || p;
}

function shortURL(u: string) {
  // 只显示 host + 末段，避免长 URL 撑破表格
  try {
    const x = new URL(u);
    const tail = x.pathname.split("/").filter(Boolean).slice(-2).join("/");
    return x.host + "/" + tail;
  } catch {
    return u;
  }
}

function fmtMB(n?: number) {
  return n ? (n / (1 << 20)).toFixed(1) + " MB" : "—";
}

onMounted(async () => {
  await loadStatus();
  if (status.value.ok) await refreshAll();
  if (status.value.ok) await loadEnv();
});
</script>

<template>
  <header>
    <h1>packnest</h1>
    <span class="meta" v-if="status.ok">CLI {{ status.version }} · {{ status.path }}</span>
    <span class="meta" v-else>
      {{ t("首次使用先装 CLI（窗口重开后自动接管）：") }}
      <code>{{ installCmd }}</code>
      <button @click="copyInstallCmd" :title="'复制安装命令'">{{ copiedHint ? "已复制" : "复制" }}</button>
      <span v-if="status.reason && !status.reason.startsWith('找不到 pk')" class="dim">（{{ status.reason }}）</span>
    </span>
    <div class="seg" style="margin-left: auto">
      <button :class="{ on: locale === 'zh' }" @click="setLocale('zh')">{{ t("中文") }}</button>
      <button :class="{ on: locale === 'en' }" @click="setLocale('en')">EN</button>
    </div>
    <div class="seg">
      <button
        v-for="th in themeOpts"
        :key="th.v"
        :class="{ on: theme === th.v }"
        :title="t('配色：') + t(th.label)"
        @click="theme = th.v"
      >
        {{ t(th.label) }}
      </button>
    </div>
  </header>

  <main v-if="status.ok">
    <p v-if="err" class="err">⚠ {{ err }}</p>
    <section>
      <h2>{{ t("已装工具") }}</h2>
      <div class="row">
        <button @click="refreshAll">{{ t("刷新") }}</button>
        <span class="hint">{{ installed.length }} {{ t("条记录") }}</span>
      </div>
      <div v-if="outdated.length" class="row">
        <span class="badge">{{ t("可升级") }}</span>
        <span v-for="o in outdated" :key="o.name" class="hint">
          {{ o.name }} {{ o.installed }} → {{ o.latest }}
          <button :disabled="busy" @click="upgrade(o.name)">{{ t("升级") }}</button>
        </span>
      </div>
      <div v-if="!installed.length" class="hint">{{ t("还没有装任何工具：在下面的「安装队列」里加一条。") }}</div>
      <table v-else>
        <thead><tr><th>{{ t("工具") }}</th><th>{{ t("版本") }}</th><th>{{ t("来源") }}</th><th>{{ t("大小") }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="tool in installed" :key="tool.name + tool.version" :class="{ active: tool.active }">
            <td>{{ tool.name }}</td>
            <td>
              {{ tool.version }}
              <span v-if="outdatedFor(tool.name)?.installed === tool.version" class="badge" :title="'索引最新 ' + outdatedFor(tool.name)?.latest">
                → {{ outdatedFor(tool.name)?.latest }}
              </span>
            </td>
            <td class="dim">{{ tool.source || "embedded" }}</td>
            <td>{{ fmtMB(tool.size_bytes) }}</td>
            <td>
              <span v-if="tool.verified === 'weak'" class="badge weak" :title="t('只有 1 个校验源')">weak</span>
              <span v-if="tool.verified === 'unverified'" class="badge bad" :title="t('未校验')">{{ t("未校验") }}</span>
              <span v-if="tool.npm?.version" class="badge" :title="'自带 ' + (tool.npm.replaced || '?')">npm {{ tool.npm.version }}</span>
              <button :disabled="tool.active" @click="setDefault(tool)">{{ tool.active ? t("当前默认") : t("设为默认") }}</button>
              <button @click="copyPath(tool.path)" :title="t('复制安装路径')">{{ t("路径") }}</button>
              <button class="danger" @click="removeTool(tool)">{{ t("卸载") }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section>
      <h2>{{ t("安装队列") }}</h2>
      <div class="row">
        <input v-model="queueInput" placeholder="node@22 / pnpm@latest" size="18" @keyup.enter="enqueue(); runQueue()" />
        <button @click="enqueue()">{{ t("加入队列") }}</button>
        <button :disabled="busy || !pending" @click="runQueue()">{{ busy ? t("安装中…") : t("开始安装") }}</button>
        <button :disabled="busy" @click="clearDone()">{{ t("清除已完成") }}</button>
      </div>
      <div v-if="!queue.length" class="hint">{{ t("队列为空：填一个 spec 加进来，多条会按顺序依次装。") }}</div>
      <table v-else>
        <thead><tr><th>spec</th><th>{{ t("进度") }}</th><th>{{ t("状态") }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="(q, i) in queue" :key="i">
            <td class="mono">{{ q.spec }}</td>
            <td>
              <span class="mini"><i :style="{ width: q.percent + '%' }"></i></span>
              <span class="dim">{{ q.percent }}%</span>
            </td>
            <td :class="q.state === 'fail' ? 'bad' : q.state === 'ok' ? 'ok' : 'dim'">{{ q.msg || "排队中" }}</td>
            <td><button class="ghost" :disabled="q.state === 'run'" :title="t('移出队列')" @click="dequeue(i)">✕</button></td>
          </tr>
        </tbody>
      </table>

      <h2 style="margin-top: 16px">{{ t("可选版本") }}</h2>
      <div class="row">
        <select v-model="tool" @change="loadVersions">
          <option v-for="a in available" :key="a.name" :value="a.name">{{ a.name }} · {{ a.latest }}</option>
        </select>
        <label class="hint"><input type="checkbox" v-model="allVersions" @change="loadVersions" /> {{ t("全部版本") }}</label>
        <input v-model="verFilter" :placeholder="t('过滤版本')" size="8" />
      </div>
      <div class="row">
        <select v-model="picked">
          <option v-for="v in shownVersions" :key="v.version" :value="v.version">
            {{ v.version }}{{ v.installed ? "（已装）" : "" }}{{ v.lts ? " · lts " + v.lts : "" }}
          </option>
        </select>
        <button :disabled="busy" @click="enqueue(`${tool}@${picked}`); runQueue()">{{ t("装这个版本") }}</button>
      </div>
    </section>

    <section>
      <h2>{{ t("镜像档") }}</h2>
      <div class="row">
        <select v-if="mirror" v-model="mirrorPick">
          <option v-for="m in mirrorOpts" :key="m.name" :value="m.name">{{ t(m.label) }}</option>
        </select>
        <button :disabled="mirrorPick === mirror?.current" @click="switchMirror">{{ t("切换") }}</button>
        <span class="hint">{{ t("当前") }}：{{ mirror?.current }}</span>
      </div>

      <div class="row">
        <input v-model="probeTool" :placeholder="t('只探某个工具（留空=全部）')" size="18" />
        <button :disabled="probing" @click="runProbe">{{ probing ? t("探测中…") : t("探测各源") }}</button>
      </div>
      <div class="row">
        <button class="danger" @click="writeNpmrc">{{ t("写入 ~/.npmrc") }}</button>
        <span class="hint">{{ t("只改 registry 一行；写前自动备份 npmrc.pk.bak") }}</span>
        <span v-if="npmrcOut" class="ok mono">{{ npmrcOut }}</span>
      </div>
      <table v-if="probeRows.length">
        <thead><tr><th>{{ t("源") }}</th><th>{{ t("状态") }}</th><th>{{ t("延迟") }}</th></tr></thead>
        <tbody>
          <tr v-for="(p, i) in probeRows" :key="i">
            <td class="mono">{{ shortURL(p.url) }}</td>
            <td :class="p.ok ? 'ok' : 'bad'">{{ p.ok ? p.status : p.err || "失败" }}</td>
            <td>{{ p.latency_ms }} ms</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section>
      <h2>{{ t("环境") }}</h2>
      <div class="row">
        <input v-model="whichCmd" :placeholder="t('命令名（node / pnpm …）')" size="14" @keyup.enter="runWhich" />
        <button @click="runWhich">{{ t("哪来的？") }}</button>
        <span v-if="whichErr" class="bad">{{ whichErr }}</span>
        <span v-else-if="whichRes" class="ok mono">
          {{ whichRes.path }}{{ whichRes.shim ? "（pk 入口）" : "（真实文件）" }}
        </span>
      </div>

      <div class="row">
        <button @click="loadEnv">{{ t("读取当前环境") }}</button>
        <label class="hint"><input type="checkbox" v-model="envProject" @change="loadEnv" /> {{ t("项目级（pk.lock）") }}</label>
        <button @click="copyEval">{{ t("复制 eval 片段") }}</button>
      </div>
      <div v-if="envDoc">
        <table>
          <thead><tr><th>{{ t("PATH（按优先级）") }}</th></tr></thead>
          <tbody>
            <tr v-for="(p, i) in envDoc.path" :key="i"><td class="mono">{{ p }}</td></tr>
          </tbody>
        </table>
        <div class="gcgroup">
          <h3>{{ t("环境变量") }}（{{ envDoc.env?.length ?? 0 }}）</h3>
          <div v-for="(kv, i) in envDoc.env" :key="i" class="mono dim ellipsis" :title="kv.value">
            {{ kv.key }}={{ kv.value }}
          </div>
        </div>
      </div>
      <div v-else class="hint">{{ t("点「读取当前环境」看 pk 到底导出了什么（只读，不写任何 rc）。") }}</div>
    </section>

    <section>
      <h2>{{ t("体检") }}</h2>
      <div class="row">
        <button @click="loadDoctor">{{ t("运行 pk doctor") }}</button>
        <button @click="fixProblems" :title="t('只修 PATH 托管块与 pk/packnest 入口，其余只报不改')">{{ t("一键修复") }}</button>
        <label class="hint"><input type="checkbox" v-model="globals" @change="loadDoctor" /> {{ t("含全局包") }}</label>
        <label class="hint"><input type="checkbox" v-model="deepProbe" @change="loadDoctor" /> {{ t("实测原生模块（--probe）") }}</label>
      </div>
      <div v-for="(f, i) in fixed" :key="'fx' + i" class="ok">已修 {{ f }}</div>
      <div v-if="!issues.length" class="hint">{{ t("没有发现问题") }}</div>

      <template v-if="globalIssues.length">
        <h3>全局包（{{ globalIssues.length }}）</h3>
        <table>
          <thead><tr><th>{{ t("包") }}</th><th>{{ t("问题") }}</th><th>{{ t("详情") }}</th><th>{{ t("级别") }}</th></tr></thead>
          <tbody>
            <tr v-for="(i, n) in globalIssues" :key="n">
              <td class="mono">{{ globalPkg(i) }}</td>
              <td>{{ issueLabel(i.code) }}</td>
              <td class="dim">{{ globalWhat(i) }}</td>
              <td :class="'sev-' + i.severity">{{ i.severity }}</td>
            </tr>
          </tbody>
        </table>
        <p class="hint">
          {{ t("引擎不满足 / ABI 绑定：切 node 版本前先处理，或在对应 node 版本下") }} <code>npm rebuild &lt;pkg&gt;</code>。
        </p>
      </template>

      <template v-if="otherIssues.length">
        <h3 v-if="globalIssues.length">其他（{{ otherIssues.length }}）</h3>
        <div v-for="(i, n) in otherIssues" :key="n" class="issue">
          <span :class="'sev-' + i.severity">[{{ i.severity }}]</span>
          <span class="dim msg">
            {{ i.message }}
            <span class="mono tag" :title="issueLabel(i.code)">{{ i.code }}</span>
          </span>
        </div>
      </template>
    </section>

    <section>
      <h2>{{ t("磁盘占用与回收") }}</h2>
      <div class="row">
        <label class="hint" :title="t('每个工具保留最新的 N 个版本（pk gc --keep N）；0 = 不按版本保留')">
          {{ t("每工具保留") }}
          <input v-model.number="gcKeep" type="number" min="0" max="20" style="width: 4rem" />
          {{ t("个") }}
        </label>
        <button @click="runGC(false)">{{ t("预演") }}</button>
        <button class="danger" @click="runGC(true)">{{ t("执行 GC") }}</button>
        <span v-if="gcRes?.DryRun" class="badge">{{ t("预演结果（未删除）") }}</span>
      </div>
      <template v-if="gcRes">
        <div class="stats">
          <div class="stat"><b>{{ gcRes.Removed?.length ?? 0 }}</b><span>{{ gcVerb }}版本</span></div>
          <div class="stat"><b>{{ gcRes.CacheRemoved?.length ?? 0 }}</b><span>{{ gcVerb }}缓存</span></div>
          <div class="stat"><b>{{ fmtMB(gcRes.FreedBytes) }}</b><span>{{ t("释放空间") }}</span></div>
          <div class="stat"><b>{{ gcRes.KeptRef?.length ?? 0 }}</b><span>{{ t("仍被引用") }}</span></div>
          <div v-if="(gcRes.KeptRecent?.length ?? 0) > 0" class="stat">
            <b>{{ gcRes.KeptRecent?.length ?? 0 }}</b><span>{{ t("按保留数留下") }}</span>
          </div>
        </div>
        <div v-if="!gcGroups.length" class="hint">{{ t("没有可回收的东西。") }}</div>
        <div v-for="g in gcGroups" :key="g.title" class="gcgroup">
          <h3>{{ g.title }}（{{ g.rows.length }}）</h3>
          <div v-for="(r, n) in g.rows" :key="n" class="mono dim ellipsis" :title="r">{{ r }}</div>
        </div>
      </template>
      <div v-else class="hint">{{ t("点「预演」看会删什么（不会真删）。") }}</div>
    </section>

    <section>
      <h2>{{ t("设置") }}</h2>
      <div class="row">
        <label class="hint" :title="t('默认关；只写用户级自启条目（LaunchAgent / HKCU Run / ~/.config/autostart）')">
          <input type="checkbox" :checked="autoStart?.enabled" :disabled="!autoStart?.supported"
                 @change="toggleAutostart" />
          {{ t("开机自启") }}
        </label>
        <span v-if="autoStart && !autoStart.supported" class="hint">{{ autoStart.note || t("当前平台不支持") }}</span>
        <span v-else-if="autoStart" class="hint mono ellipsis" :title="autoStart.path">{{ autoStart.path }}</span>
      </div>
      <div class="hint">{{ t("默认关；只写用户级自启条目（LaunchAgent / HKCU Run / ~/.config/autostart）") }}</div>
    </section>

    <section>
      <h2>{{ t("说明") }}</h2>
      <p class="hint">
        {{ t("这是") }} <b>pk</b> {{ t("的可视化补充：所有操作都通过调用本机已安装的") }} <code>pk</code>{{ t("（子进程 +") }} <code>--json</code>{{ t("）完成，没有装 CLI 时这里不会自己下载任何东西。") }}
      </p>
    </section>
  </main>
</template>

<style>
:root {
  --fg: #111; --dim: #666; --bg: #fff; --line: #e5e5e5; --accent: #0a7d4f; --danger: #b91c1c;
  --hover: rgba(0, 0, 0, .05);
}
:root[data-theme="dark"] {
  --fg: #e8e8e8; --dim: #9a9a9a; --bg: #161616; --line: #2c2c2c; --accent: #4ade80; --danger: #f87171;
  --hover: rgba(255, 255, 255, .07);
}
* { box-sizing: border-box; }
body { margin: 0; font: 14px/1.55 ui-sans-serif, -apple-system, "Segoe UI", "PingFang SC", sans-serif; color: var(--fg); background: var(--bg); }
header { padding: 14px 18px; border-bottom: 1px solid var(--line); display: flex; gap: 12px; align-items: baseline; flex-wrap: wrap; }
header h1 { font-size: 15px; margin: 0; letter-spacing: .3px; }
.meta { color: var(--dim); font-size: 12px; }
.seg { margin-left: auto; display: flex; gap: 2px; }
.seg button { margin: 0; padding: 3px 8px; font-size: 12px; border-color: transparent; color: var(--dim); }
.seg button.on { border-color: var(--line); background: var(--hover); color: var(--fg); }
main { padding: 18px; display: grid; gap: 18px; grid-template-columns: 1fr 1fr; align-items: start; }
@media (max-width: 860px) { main { grid-template-columns: 1fr; } }
section { border: 1px solid var(--line); border-radius: 10px; padding: 14px; }
section h2 { font-size: 13px; margin: 0 0 10px; color: var(--dim); font-weight: 600; letter-spacing: .4px; text-transform: uppercase; }
section h3 { font-size: 12px; margin: 12px 0 6px; color: var(--dim); font-weight: 600; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--line); }
th { color: var(--dim); font-weight: 500; }
tr.active td:first-child::before { content: "● "; color: var(--accent); }
button { font: inherit; padding: 5px 10px; border-radius: 7px; border: 1px solid var(--line); background: transparent; color: inherit; cursor: pointer; margin-right: 6px; }
button:hover:not(:disabled) { border-color: var(--accent); }
button:disabled { opacity: .55; cursor: default; }
button.danger:hover { border-color: var(--danger); color: var(--danger); }
button.ghost { border-color: transparent; color: var(--dim); padding: 2px 6px; }
input, select { font: inherit; padding: 6px 9px; border-radius: 7px; border: 1px solid var(--line); background: transparent; color: inherit; }
.row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 10px; }
.mini { display: inline-block; width: 80px; height: 5px; border-radius: 999px; background: var(--line); overflow: hidden; vertical-align: middle; margin-right: 6px; }
.mini > i { display: block; height: 100%; background: var(--accent); transition: width .18s linear; }
.dim { color: var(--dim); }
.hint { color: var(--dim); font-size: 12px; }
.ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.issue { display: flex; gap: 8px; font-size: 13px; padding: 3px 0; align-items: baseline; }
.issue .msg { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.issue .tag { font-size: 11px; opacity: .55; margin-left: 4px; }
.stats { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 6px; }
.stat { border: 1px solid var(--line); border-radius: 8px; padding: 8px 12px; min-width: 84px; }
.stat b { display: block; font-size: 16px; }
.stat span { color: var(--dim); font-size: 12px; }
.gcgroup { margin-top: 8px; }
.badge { display: inline-block; font-size: 11px; padding: 1px 6px; border-radius: 999px; border: 1px solid var(--line); color: var(--dim); margin-right: 6px; }
.badge.weak { color: #b45309; border-color: #b45309; }
.badge.bad { color: var(--danger); border-color: var(--danger); }
.mono { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
.ok { color: var(--accent); } .bad { color: var(--danger); }
.err { grid-column: 1 / -1; margin: 0; padding: 8px 12px; border: 1px solid var(--danger); border-radius: 8px; color: var(--danger); font-size: 13px; }
.sev-warn { color: #b45309; } .sev-error { color: var(--danger); } .sev-info { color: var(--dim); }
</style>
