<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api, install, type InstallEvent, type Issue, type Mirror, type Tool, type AvailableTool } from "./api";

const status = ref<{ ok: boolean; reason?: string; path?: string; version?: string }>({ ok: false });
const tools = ref<Tool[]>([]);
const available = ref<AvailableTool[]>([]);
const versions = ref<{ version: string; installed?: boolean; lts?: string }[]>([]);
const issues = ref<Issue[]>([]);
const mirror = ref<Mirror | null>(null);
const spec = ref("node@22");
const tool = ref("");
const picked = ref("");
const globals = ref(false);
const log = ref("");
const busy = ref(false);
const percent = ref(0);
const gcOut = ref("");

const installed = computed(() => tools.value);

async function refreshAll() {
  await Promise.all([loadTools(), loadMirrors(), loadDoctor(), loadAvailable()]);
}
async function loadStatus() {
  try {
    const s = await api.status();
    status.value = { ok: s.ok, reason: s.reason, path: s.path, version: s.cli?.version };
  } catch (e) {
    status.value = { ok: false, reason: String(e) };
  }
}
async function loadTools() {
  tools.value = (await api.tools()).tools ?? [];
}
async function loadAvailable() {
  available.value = (await api.available()).available ?? [];
  if (!tool.value && available.value.length) {
    tool.value = available.value[0].name;
    await loadVersions();
  }
}
async function loadVersions() {
  if (!tool.value) return;
  const v = await api.versions(tool.value);
  versions.value = (v.available ?? []).slice().reverse();
  picked.value = versions.value[0]?.version ?? "";
}
async function loadMirrors() {
  mirror.value = await api.mirrors();
}
async function loadDoctor() {
  issues.value = (await api.doctor(globals.value)).issues ?? [];
}

async function doInstall(target?: string) {
  const t = target ?? spec.value.trim();
  if (!t || busy.value) return;
  busy.value = true;
  percent.value = 0;
  log.value = "";
  try {
    await install(t, (ev: InstallEvent) => {
      if (ev.ev === "progress" && ev.bytes_total) {
        percent.value = Math.round(((ev.bytes_done ?? 0) / ev.bytes_total) * 100);
      }
      log.value += `${ev.ev} ${ev.tool ?? ""}${ev.version ? "@" + ev.version : ""} ${ev.mode ?? ev.msg ?? ""}\n`;
    });
  } catch (e) {
    log.value += `错误：${String(e)}\n`;
  } finally {
    busy.value = false;
    setTimeout(() => (percent.value = 0), 800);
    await refreshAll();
  }
}

async function setDefault(t: Tool) {
  await api.use(t.name, t.version);
  await loadTools();
}
async function removeTool(t: Tool) {
  if (!confirm(`卸载 ${t.name} 的全部版本？`)) return;
  try {
    await api.uninstall(t.name);
  } catch (e) {
    alert(String(e));
  }
  await loadTools();
}
async function switchMirror() {
  if (!mirror.value) return;
  await api.useMirror(mirror.value.current);
  await loadMirrors();
}
async function runGC(run: boolean) {
  if (run && !confirm("执行 GC 会删除零引用版本与超限缓存，继续？")) return;
  gcOut.value = JSON.stringify(await api.gc(run), null, 2);
  if (run) await loadTools();
}
function fmtMB(n?: number) {
  return n ? (n / (1 << 20)).toFixed(1) + " MB" : "—";
}

onMounted(async () => {
  await loadStatus();
  if (status.value.ok) await refreshAll();
});
</script>

<template>
  <header>
    <h1>packnest</h1>
    <span class="meta" v-if="status.ok">CLI {{ status.version }} · {{ status.path }}</span>
    <span class="meta" v-else>
      找不到 pk：{{ status.reason }} —— 先装 CLI：
      <code>curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh</code>
    </span>
  </header>

  <main v-if="status.ok">
    <section>
      <h2>已装工具</h2>
      <div class="row">
        <input v-model="spec" placeholder="node@22" size="14" />
        <button :disabled="busy" @click="doInstall()">安装</button>
        <button @click="refreshAll">刷新</button>
      </div>
      <div class="row">
        <select v-model="tool" @change="loadVersions">
          <option v-for="a in available" :key="a.name" :value="a.name">{{ a.name }} · {{ a.latest }}</option>
        </select>
        <select v-model="picked">
          <option v-for="v in versions" :key="v.version" :value="v.version">
            {{ v.version }}{{ v.installed ? "（已装）" : "" }}{{ v.lts ? " · lts " + v.lts : "" }}
          </option>
        </select>
        <button :disabled="busy" @click="doInstall(`${tool}@${picked}`)">装这个版本</button>
      </div>
      <div class="bar" v-if="percent > 0"><i :style="{ width: percent + '%' }"></i></div>
      <pre v-if="log">{{ log }}</pre>
      <table>
        <thead><tr><th>工具</th><th>版本</th><th>来源</th><th>大小</th><th></th></tr></thead>
        <tbody>
          <tr v-for="t in installed" :key="t.name + t.version" :class="{ active: t.active }">
            <td>{{ t.name }}</td>
            <td>{{ t.version }}</td>
            <td class="dim">{{ t.source || "embedded" }}</td>
            <td>{{ fmtMB(t.size_bytes) }}</td>
            <td>
              <button :disabled="t.active" @click="setDefault(t)">{{ t.active ? "当前默认" : "设为默认" }}</button>
              <button class="danger" @click="removeTool(t)">卸载</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <section>
      <h2>镜像档</h2>
      <div class="row">
        <select v-if="mirror" v-model="mirror.current">
          <option v-for="m in mirror.mirrors" :key="m.name" :value="m.name">
            {{ m.name }}{{ m.current ? "（当前）" : "" }}
          </option>
        </select>
        <button @click="switchMirror">切换</button>
      </div>

      <h2 style="margin-top: 16px">体检</h2>
      <div class="row">
        <button @click="loadDoctor">运行 pk doctor</button>
        <label class="hint"><input type="checkbox" v-model="globals" @change="loadDoctor" /> 含全局包</label>
      </div>
      <div v-if="!issues.length" class="hint">没有发现问题</div>
      <div v-for="(i, n) in issues" :key="n" class="issue">
        <span :class="'sev-' + i.severity">[{{ i.severity }}]</span>
        <span>{{ i.code }}</span>
        <span class="dim">{{ i.message }}</span>
      </div>
    </section>

    <section>
      <h2>磁盘占用与回收</h2>
      <div class="row">
        <button @click="runGC(false)">预演</button>
        <button class="danger" @click="runGC(true)">执行 GC</button>
      </div>
      <pre v-if="gcOut">{{ gcOut }}</pre>
    </section>

    <section>
      <h2>说明</h2>
      <p class="hint">
        这是 <b>pk</b> 的可视化补充：所有操作都通过调用本机已安装的 <code>pk</code>（子进程 + <code>--json</code>）完成，
        没有装 CLI 时这里不会自己下载任何东西。
      </p>
    </section>
  </main>
</template>

<style>
:root {
  --fg: #111; --dim: #666; --bg: #fff; --line: #e5e5e5; --accent: #0a7d4f; --danger: #b91c1c;
  color-scheme: light dark;
}
@media (prefers-color-scheme: dark) {
  :root { --fg: #e8e8e8; --dim: #9a9a9a; --bg: #161616; --line: #2c2c2c; --accent: #4ade80; --danger: #f87171; }
}
* { box-sizing: border-box; }
body { margin: 0; font: 14px/1.55 ui-sans-serif, -apple-system, "Segoe UI", "PingFang SC", sans-serif; color: var(--fg); background: var(--bg); }
header { padding: 14px 18px; border-bottom: 1px solid var(--line); display: flex; gap: 12px; align-items: baseline; flex-wrap: wrap; }
header h1 { font-size: 15px; margin: 0; letter-spacing: .3px; }
.meta { color: var(--dim); font-size: 12px; }
main { padding: 18px; display: grid; gap: 18px; grid-template-columns: 1fr 1fr; }
@media (max-width: 860px) { main { grid-template-columns: 1fr; } }
section { border: 1px solid var(--line); border-radius: 10px; padding: 14px; }
section h2 { font-size: 13px; margin: 0 0 10px; color: var(--dim); font-weight: 600; letter-spacing: .4px; text-transform: uppercase; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--line); }
th { color: var(--dim); font-weight: 500; }
tr.active td:first-child::before { content: "● "; color: var(--accent); }
button { font: inherit; padding: 5px 10px; border-radius: 7px; border: 1px solid var(--line); background: transparent; color: inherit; cursor: pointer; margin-right: 6px; }
button:hover:not(:disabled) { border-color: var(--accent); }
button:disabled { opacity: .55; cursor: default; }
button.danger:hover { border-color: var(--danger); color: var(--danger); }
input, select { font: inherit; padding: 6px 9px; border-radius: 7px; border: 1px solid var(--line); background: transparent; color: inherit; }
.row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 10px; }
.bar { height: 6px; border-radius: 999px; background: var(--line); overflow: hidden; margin-bottom: 8px; }
.bar > i { display: block; height: 100%; background: var(--accent); transition: width .18s linear; }
.dim { color: var(--dim); }
.hint { color: var(--dim); font-size: 12px; }
pre { white-space: pre-wrap; word-break: break-all; font: 12px/1.5 ui-monospace, Menlo, monospace; color: var(--dim); margin: 8px 0 0; max-height: 180px; overflow: auto; }
.issue { display: flex; gap: 8px; font-size: 13px; padding: 3px 0; }
.sev-warn { color: #b45309; } .sev-error { color: var(--danger); } .sev-info { color: var(--dim); }
</style>
