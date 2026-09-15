/* Explicit, bounded process inspection. No generic RPC, launch, PID, automatic
 * worker activation, or mutation replay. Cursor authority never crosses chats. */
(() => {
  "use strict";
  let view;
  const $ = (s, root) => root?.querySelector(s);
  const bytes = value => new TextEncoder().encode(value).length;
  const idOK = id => typeof id === "string" && /^proc_[a-f0-9]{32}$/.test(id);
  const statusOK = value => ["running", "stopped", "exited"].includes(value);
  function node(tag, text, cls) { const el = document.createElement(tag); el.textContent = text; if (cls) el.className = cls; return el; }
  function same(v) {
    const root = $("#live-session", document);
    return view === v && v.panel.isConnected && root?.dataset.project === v.project && root.dataset.instance === v.instance && root.dataset.session === v.session;
  }
  function scope(v) {
    if (same(v)) return true;
    if (view === v) dispose();
    return false;
  }
  function visible(v) { return scope(v) && v.panel.open && document.visibilityState === "visible" && v.panel.getClientRects().length > 0; }
  function error(v, text) { const el = $("[data-process-error]", v.panel); el.textContent = text; el.hidden = !text; }
  // Scope authority and its entire presentation retire together. Clearing only
  // rows/output leaves a misleading name, cursor and EOF from another chat.
  function resetPanel(panel) {
    if (!panel) return;
    $("[data-process-list]", panel)?.replaceChildren();
    for (const selector of ["[data-process-status]", "[data-process-error]", "[data-process-log-heading]", "[data-process-log-status]", "[data-process-output]"]) {
      const element = $(selector, panel); if (element) element.textContent = "";
    }
    for (const selector of ["[data-process-log-panel]", "[data-process-error]"]) {
      const element = $(selector, panel); if (element) element.hidden = true;
    }
    for (const button of panel.querySelectorAll("button")) button.disabled = true;
  }
  function init() {
    dispose();
    const panel = $("#managed-processes", document);
    const root = $("#live-session", document);
    if (!panel || !root || !["project", "instance", "session"].every(key => typeof root.dataset[key] === "string" && !!root.dataset[key] && root.dataset[key] === panel.dataset["process" + key[0].toUpperCase() + key.slice(1)])) return;
    const controller = new AbortController();
    const v = view = {panel, project: panel.dataset.processProject, instance: panel.dataset.processInstance, session: panel.dataset.processSession, controller, records: new Map(), cursor: undefined, selected: "", eof: false, busy: false, unknown: false};
    const options = {signal: controller.signal};
    $("[data-process-refresh]", panel).disabled = false;
    panel.addEventListener("toggle", () => { clearTimeout(v.timer); if (visible(v)) refresh(v); }, options);
    document.addEventListener("visibilitychange", () => { clearTimeout(v.timer); if (visible(v)) refresh(v); }, options);
    panel.addEventListener("click", event => {
      const button = event.target.closest("button"); if (!button || !scope(v)) return;
      if (button.matches("[data-process-refresh]")) refresh(v);
      if (button.matches("[data-process-log-more]")) logs(v);
      if (button.dataset.processLogs && !v.busy && v.records.has(button.dataset.processLogs)) { resetLog(v); v.selected = button.dataset.processLogs; v.cursor = undefined; v.eof = false; $("[data-process-output]", panel).textContent = ""; logs(v); }
      if (button.dataset.processStop) stop(v, button.dataset.processStop);
    }, options);
    if (visible(v)) refresh(v);
  }
  function resetLog(v) {
    v.selected = ""; v.cursor = undefined; v.eof = false;
    $("[data-process-log-panel]", v.panel).hidden = true;
    for (const selector of ["[data-process-log-heading]", "[data-process-log-status]", "[data-process-output]"]) $(selector, v.panel).textContent = "";
    $("[data-process-log-more]", v.panel).disabled = true;
  }
  function dispose() {
    const old = view; view = undefined;
    if (old) {
      clearTimeout(old.timer); old.controller.abort(); old.records.clear();
      old.selected = ""; old.cursor = undefined; old.eof = false; old.busy = false;
      resetPanel(old.panel);
      old.panel.open = false;
      const dialog = old.panel.closest("dialog");
      if (dialog?.open) dialog.close();
    }
    const panel = $("#managed-processes", document);
    if (panel !== old?.panel) resetPanel(panel);
  }
  async function request(v, action, fields = {}) {
    if (!scope(v)) throw new Error("stale");
    const csrf = $('input[name="csrf"]', document)?.value || "";
    const signal = AbortSignal.any([v.controller.signal, AbortSignal.timeout(12000)]);
    const response = await fetch(`/projects/${encodeURIComponent(v.project)}/processes/${action}`, {
      method: "POST", credentials: "same-origin", redirect: "error", cache: "no-store", signal,
      headers: {"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", Accept: "application/json"},
      body: new URLSearchParams({csrf, instance_id: v.instance, session_id: v.session, ...fields})
    });
    if (!response.ok) throw new Error("rejected");
    // Bound before parsing; transport and host projections are bounded too.
    const text = await response.text(); if (text.length > 256 * 1024) throw new Error("oversized");
    const data = JSON.parse(text);
    if (!scope(v)) throw new Error("stale");
    if (data.project_id !== v.project || data.instance_id !== v.instance || data.result?.session_id !== v.session) throw new Error("stale");
    return data.result;
  }
  function validRecord(p) { return p && idOK(p.process_id) && typeof p.name === "string" && bytes(p.name) <= 64 && statusOK(p.status); }
  function paint(v) {
    const list = $("[data-process-list]", v.panel);
    v.rows ||= new Map();
    for (const [id, item] of v.rows) if (!v.records.has(id)) { item.row.remove(); v.rows.delete(id); }
    let at = list.firstChild;
    for (const p of v.records.values()) {
      let item = v.rows.get(p.process_id);
      if (!item) {
        const row = node("li", "", "process-row"), identity = node("span", "", "process-identity"), label = node("span", "");
        identity.append(label, node("small", p.process_id)); row.append(identity);
        const read = node("button", "Logs", "quiet"); read.type = "button"; read.dataset.processLogs = p.process_id;
        const stop = node("button", "Stop", "quiet"); stop.type = "button"; stop.dataset.processStop = p.process_id;
        row.append(read, stop); item = {row, label, read, stop}; v.rows.set(p.process_id, item);
      }
      const label = `${p.name} · ${p.status}${p.ready ? " · ready" : ""}`;
      if (item.label.textContent !== label) item.label.textContent = label;
      item.read.disabled = v.busy; item.stop.disabled = v.busy || v.unknown || p.status !== "running";
      if (item.row !== at) {
        if (list.moveBefore && item.row.isConnected) list.moveBefore(item.row, at);
        else list.insertBefore(item.row, at);
      }
      at = item.row.nextSibling;
    }
  }
  function schedule(v) { clearTimeout(v.timer); if (visible(v) && !v.unknown) v.timer = setTimeout(() => refresh(v), 3000); }
  async function refresh(v) {
    if (!visible(v) || v.busy) return;
    v.busy = true;
    try {
      const result = await request(v, "list");
      if (!scope(v)) return;
      if (!Array.isArray(result.processes) || result.processes.length > 128 || typeof result.truncated !== "boolean" || result.processes.some(p => !validRecord(p)) || new Set(result.processes.map(p => p.process_id)).size !== result.processes.length) throw new Error("invalid inventory");
      v.records = new Map(result.processes.map(p => [p.process_id, p]));
      $("[data-process-status]", v.panel).textContent = `${v.records.size} managed process${v.records.size === 1 ? "" : "es"}${result.truncated ? " · inventory truncated" : ""}`;
      if (!v.unknown) error(v, "");
    } catch (_) { if (same(v)) error(v, "Inventory unavailable. Review the current live conversation; no work was started."); }
    finally { if (same(v)) { v.busy = false; paint(v); schedule(v); } }
  }
  async function logs(v) {
    if (!visible(v) || v.busy || v.eof || !v.records.has(v.selected)) return;
    v.busy = true; const id = v.selected;
    try {
      const result = await request(v, "logs", {process_id: id, max_bytes: "32768", ...(v.cursor === undefined ? {} : {cursor: String(v.cursor)})});
      if (!scope(v)) return;
      if (result.process_id !== id || !statusOK(result.status) || typeof result.output !== "string" || bytes(result.output) > 32768 || !Number.isSafeInteger(result.next_cursor) || result.next_cursor < (v.cursor ?? 0) || !Number.isSafeInteger(result.omitted_bytes) || result.omitted_bytes < 0 || typeof result.eof !== "boolean") throw new Error("invalid logs");
      v.cursor = result.next_cursor; v.eof = result.eof;
      const output = $("[data-process-output]", v.panel);
      // Show one bounded page, not an ever-growing terminal transcript.
      output.textContent = result.output.replace(/\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "").replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, "");
      $("[data-process-log-panel]", v.panel).hidden = false;
      $("[data-process-log-heading]", v.panel).textContent = `${v.records.get(id).name} · output`;
      $("[data-process-log-status]", v.panel).textContent = `Cursor ${v.cursor} · ${result.omitted_bytes} bytes omitted${v.eof ? " · end of output" : " · more may arrive"}`;
      $("[data-process-log-more]", v.panel).disabled = v.eof;
    } catch (_) { if (same(v)) error(v, "Output unavailable. Select Logs again to read a fresh bounded page."); }
    finally { if (same(v)) { v.busy = false; paint(v); schedule(v); } }
  }
  async function stop(v, id) {
    if (!visible(v) || v.busy || v.unknown || !v.records.has(id) || v.records.get(id).status !== "running") return;
    if (!window.confirm(`Stop managed process ${v.records.get(id).name} (${id}) in this conversation?`)) return;
    v.busy = true; paint(v); clearTimeout(v.timer);
    try {
      const result = await request(v, "stop", {process_id: id, grace_ms: "2000"});
      if (!scope(v)) return;
      if (!validRecord(result.process) || result.process.process_id !== id) throw new Error("invalid stop acknowledgment");
      v.records.set(id, result.process); error(v, "");
    } catch (_) {
      // Latch: neither refresh nor reconnect ever retries a possibly committed Stop.
      v.unknown = true;
      if (same(v)) error(v, "Stop was not acknowledged; it may have succeeded or been denied by policy. No retry was queued. Refresh inventory to inspect; reopen this panel’s workspace before issuing another Stop.");
    } finally { if (same(v)) { v.busy = false; paint(v); schedule(v); } }
  }
  window.SnowProcesses = {init, dispose};
})();
