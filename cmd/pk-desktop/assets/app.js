// 极简前端：只做"调 /api/* 并渲染"，不含任何业务判断（真相在 CLI）。
const $ = (id) => document.getElementById(id);
const fmtMB = (n) => (n ? (n / (1 << 20)).toFixed(1) + " MB" : "—");

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  return res;
}

async function loadStatus() {
  const s = await (await api("/api/status")).json();
  if (!s.ok) {
    $("status").innerHTML = "找不到 pk：" + s.reason + " —— 先装 CLI：<code>curl -fsSL https://gitee.com/null_593_5354/packnest/raw/main/scripts/install.sh | sh</code>";
    return false;
  }
  $("status").textContent = `CLI ${s.cli.version}（schema ${s.cli.schema}）· ${s.path}`;
  return true;
}

async function loadTools() {
  const { tools } = await (await api("/api/tools")).json();
  const tb = $("tools").tBodies[0];
  tb.innerHTML = "";
  for (const t of tools) {
    const tr = document.createElement("tr");
    if (t.active) tr.className = "active";
    tr.innerHTML = `<td>${t.name}</td><td>${t.version}</td><td class="dim">${t.source || "embedded"}</td><td>${fmtMB(t.size_bytes)}</td>`;
    const td = document.createElement("td");
    const btn = document.createElement("button");
    btn.textContent = t.active ? "当前默认" : "设为默认";
    btn.disabled = !!t.active;
    btn.onclick = async () => {
      await api("/api/use", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ tool: t.name, version: t.version }) });
      await loadTools();
    };
    const del = document.createElement("button");
    del.textContent = "卸载";
    del.style.marginLeft = "6px";
    del.onclick = async () => {
      if (!confirm(`卸载 ${t.name} 的全部版本？`)) return;
      await api("/api/gc").catch(() => {}); // 占位：确保 API 可用
      const res = await fetch(`/api/available`); // no-op 保活
      await res.text();
      const u = await fetch(`/api/uninstall`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ tool: t.name }) });
      if (!u.ok) alert(await u.text());
      await loadTools();
    };
    td.appendChild(btn);
    td.appendChild(del);
    tr.appendChild(td);
    tb.appendChild(tr);
  }
}

async function loadMirrors() {
  const m = await (await api("/api/mirrors")).json();
  const sel = $("mirror");
  sel.innerHTML = "";
  for (const x of m.mirrors) {
    const o = document.createElement("option");
    o.value = x.name; o.textContent = `${x.name}${x.current ? "（当前）" : ""}`;
    if (x.current) o.selected = true;
    sel.appendChild(o);
  }
}

async function loadDoctor() {
  const globals = $("globals").checked ? "1" : "0";
  const { issues } = await (await api(`/api/doctor?globals=${globals}`)).json();
  $("issues").innerHTML = issues.length
    ? issues.map((i) => `<div class="issue"><span class="sev-${i.severity}">[${i.severity}]</span><span>${i.code}</span><span class="dim">${i.message}</span></div>`).join("")
    : '<span class="hint">没有发现问题</span>';
}

// 安装：读 NDJSON 流，progress 事件驱动进度条，done/error 收尾。
async function install() {
  const spec = $("spec").value.trim();
  if (!spec) return;
  const bar = $("bar"), fill = bar.querySelector("i"), log = $("log");
  bar.hidden = false; log.hidden = false; log.textContent = "";
  fill.style.width = "0%";
  const res = await fetch(`/api/install?spec=${encodeURIComponent(spec)}`);
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const lines = buf.split("\n"); buf = lines.pop();
    for (const line of lines) {
      if (!line.trim()) continue;
      const ev = JSON.parse(line);
      if (ev.ev === "progress" && ev.bytes_total) fill.style.width = `${Math.round((ev.bytes_done / ev.bytes_total) * 100)}%`;
      log.textContent += `${ev.ev} ${ev.tool || ""}${ev.version ? "@" + ev.version : ""} ${ev.mode || ev.msg || ""}\n`;
      log.scrollTop = log.scrollHeight;
    }
  }
  setTimeout(() => { bar.hidden = true; }, 600);
  await loadTools();
}

async function gc(run) {
  const res = await api("/api/gc" + (run ? "" : ""), { method: run ? "POST" : "GET" });
  const data = await res.json();
  const out = $("gcOut");
  out.hidden = false;
  out.textContent = JSON.stringify(data, null, 2);
  if (run) await loadTools();
}

// 可装版本：先拿"每个工具最新版"，再按工具取最近 40 个版本
async function loadAvailable() {
  const { available } = await (await api("/api/available")).json();
  const pick = $("toolPick");
  pick.innerHTML = "";
  for (const t of available) {
    if (t.error) continue;
    const o = document.createElement("option");
    o.value = t.name; o.textContent = t.name + (t.latest ? " · " + t.latest : "");
    pick.appendChild(o);
  }
  await loadVersions();
}

async function loadVersions() {
  const tool = $("toolPick").value;
  const vp = $("versionPick");
  vp.innerHTML = "";
  if (!tool) return;
  const v = await (await api(`/api/versions?tool=${encodeURIComponent(tool)}`)).json();
  for (const a of (v.available || []).slice().reverse()) {
    const o = document.createElement("option");
    o.value = a.version;
    o.textContent = a.version + (a.installed ? "（已装）" : "") + (a.lts ? " · lts " + a.lts : "");
    vp.appendChild(o);
  }
}

$("toolPick").onchange = loadVersions;
$("installPicked").onclick = async () => {
  const tool = $("toolPick").value, ver = $("versionPick").value;
  if (!tool || !ver) return;
  $("spec").value = `${tool}@${ver}`;
  await install();
  await loadAvailable();
};
$("installBtn").onclick = install;
$("refreshBtn").onclick = () => { loadTools(); loadDoctor(); };
$("mirrorBtn").onclick = async () => {
  await api("/api/mirrors", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ use: $("mirror").value }) });
  await loadMirrors();
};
$("doctorBtn").onclick = loadDoctor;
$("gcPreview").onclick = () => gc(false);
$("gcRun").onclick = () => { if (confirm("执行 GC 会删除零引用版本与超限缓存，继续？")) gc(true); };

(async () => {
  if (await loadStatus()) {
    await Promise.all([loadTools(), loadMirrors(), loadDoctor(), loadAvailable()]);
  }
})();
