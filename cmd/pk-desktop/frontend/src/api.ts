// 与 internal/pkcli 的结构体一一对应；改这里就等于改 GUI 的契约视图。
export interface Status {
  ok: boolean;
  reason?: string;
  path?: string;
  cli?: { schema: number; name: string; version: string };
}
export interface Tool {
  name: string;
  version: string;
  active: boolean;
  path?: string;
  verified?: string;
  source?: string;
  size_bytes?: number;
  npm?: { version: string; replaced: string };
}
export interface AvailableTool {
  name: string;
  latest: string;
  lts?: string;
  source?: string;
  error?: string;
}
export interface Versions {
  tool: string;
  latest: string;
  total: number;
  installed: { version: string; active: boolean }[];
  available: { version: string; lts?: string; installed?: boolean }[];
}
export interface Issue {
  code: string;
  severity: "info" | "warn" | "error";
  message: string;
  // 结构随 code 变：GLOBAL_* 带 pkg/needs/have/path/file/bin，PATH_* 带 want/path 等
  detail?: Record<string, unknown>;
}
export interface Mirror {
  current: string;
  mirrors: { name: string; display: string; registry: string; current: boolean }[];
}
export interface GCResult {
  Removed?: string[];
  CacheRemoved?: string[];
  KeptRef?: string[];
  // 1.3：--keep N 保留的版本
  KeptRecent?: string[];
  FreedBytes?: number;
  DryRun?: boolean;
  keep?: number;
}
// 1.3：pk list --json 的 outdated（已装版本落后索引最新时才有；只读索引缓存，不联网）
export interface Outdated {
  name: string;
  installed: string;
  latest: string;
  advice: string;
}
export interface ProbeEntry {
  url: string;
  ok: boolean;
  status: number;
  latency_ms: number;
  err?: string;
}
export interface WhichResult {
  name: string;
  path: string;
  shim: boolean;
}
export interface EnvDoc {
  shell: string;
  path: string[];
  env: { key: string; value: string }[];
  script_hint?: string;
}
export interface MirrorWriteResult {
  file: string;
  registry: string;
  replaced: boolean;
}

export interface InstallEvent {
  ev: string;
  tool?: string;
  version?: string;
  bytes_done?: number;
  bytes_total?: number;
  mode?: string;
  msg?: string;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  if (!res.ok) throw new Error(friendly(res.status, await res.text()));
  return res.json() as Promise<T>;
}

// friendly 把 HTTP 状态翻译成用户能懂的提示（409 = CLI 的退出码 5：另一个 pk 正在运行）。
export function friendly(status: number, text: string): string {
  if (status === 412) return "找不到 pk：GUI 需要先安装 CLI";
  if (status === 409) return "另一个 pk 正在运行，稍后再试";
  if (status === 401) return "需要 token（--serve 绑了非回环地址）";
  return text || `HTTP ${status}`;
}

export const api = {
  status: () => json<Status>("/api/status"),
  tools: () => json<{ tools: Tool[]; outdated?: Outdated[] }>("/api/tools"),
  available: () => json<{ available: AvailableTool[] }>("/api/available"),
  versions: (tool: string, all = false) =>
    json<Versions>(`/api/versions?tool=${encodeURIComponent(tool)}${all ? "&all=1" : ""}`),
  mirrors: () => json<Mirror>("/api/mirrors"),
  useMirror: (use: string) =>
    json<Mirror>("/api/mirrors", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ use }) }),
  // fix=true 时后端会用 `pk doctor --fix` 修 PATH 托管块与 pk/packnest 入口
  doctor: (globals: boolean, probe: boolean, fix = false) =>
    json<{ issues: Issue[]; fixed?: string[] }>(
      `/api/doctor?globals=${globals ? 1 : 0}&probe=${probe ? 1 : 0}&fix=${fix ? 1 : 0}`,
    ),
  probe: (tool?: string) =>
    json<{ results: ProbeEntry[] }>(`/api/probe${tool ? "?tool=" + encodeURIComponent(tool) : ""}`),
  // keep>0 = 每个工具保留最新 N 个版本（`pk gc --keep N`）
  gc: (run: boolean, keep = 0) =>
    json<GCResult>(`/api/gc?keep=${keep}`, { method: run ? "POST" : "GET" }),
  use: (tool: string, version: string) =>
    json("/api/use", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ tool, version }) }),
  uninstall: (tool: string) =>
    json("/api/uninstall", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ tool }) }),
  // which / env 只读；mirror-write 会改用户的 ~/.npmrc（调用方先弹确认）
  which: (cmd: string) => json<WhichResult>(`/api/which?cmd=${encodeURIComponent(cmd)}`),
  env: (project = false) => json<EnvDoc>(`/api/env?project=${project ? 1 : 0}`),
  mirrorWrite: (file = "") =>
    json<MirrorWriteResult>("/api/mirror-write", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ file }) }),
};

// install 是 NDJSON 流：progress 事件驱动进度条，done/error 收尾。
export async function install(spec: string, onEvent: (ev: InstallEvent) => void): Promise<void> {
  const res = await fetch(`/api/install?spec=${encodeURIComponent(spec)}`);
  if (!res.ok || !res.body) throw new Error((await res.text()) || res.statusText);
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const lines = buf.split("\n");
    buf = lines.pop() ?? "";
    for (const line of lines) {
      if (!line.trim()) continue;
      try {
        onEvent(JSON.parse(line) as InstallEvent);
      } catch {
        /* 忽略非 JSON 行 */
      }
    }
  }
}
