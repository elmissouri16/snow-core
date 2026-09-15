(() => {
  "use strict";
  // Tab-memory metadata only: no transcript, durable preference, or runtime action.
  const groups = new Map(), maxRows = 100, maxGroups = 100;
  const $ = (selector, scope = document) => scope.querySelector(selector);
  let root, generation = 0, hideSessions = false;
  const set = (node, name, value) => { if (node.getAttribute(name) !== value) node.setAttribute(name, value); };
  const rowsIn = tree => [...tree.querySelectorAll("[data-shell-session]")];
  function branch(project) {
    return [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === project);
  }
  function entry(row) {
    const project = row.dataset.sidebarProject;
    let state = groups.get(project);
    if (!state) {
      if (groups.size >= maxGroups) {
        const oldest = groups.keys().next().value;
        groups.get(oldest).request?.abort(); groups.delete(oldest);
      }
      state = {project, expanded: $("[data-workspace-toggle]", row)?.getAttribute("aria-expanded") === "true", rows: [], instance: "", loaded: false, available: true};
      groups.set(project, state);
    }
    return state;
  }
  function visibility(row, state) {
    const button = $("[data-workspace-toggle]", row), tree = $("[data-workspace-sessions]", row);
    if (button) set(button, "aria-expanded", String(state.expanded));
    if (tree && tree.hidden !== (!state.expanded || hideSessions)) tree.hidden = !state.expanded || hideSessions;
  }
  function selectedSession(project) {
    if (root?.dataset.project !== project) return "";
    const live = $("#live-session[data-runtime=true]");
    return live?.dataset.project === project ? live.dataset.session : root.dataset.session || "";
  }
  function stamp(link, project, session, instance) {
    const href = "/?" + new URLSearchParams({view: "projects", project, session});
    let process = false;
    for (const [name, value] of Object.entries({href, "hx-get": href, "hx-target": "#workspace", "hx-swap": "outerHTML", "hx-push-url": "true", "hx-sync": "#project-navigation:replace", "data-shell-session-open": "", "data-project": project, "data-instance": instance})) {
      if (link.getAttribute(name) !== value) { link.setAttribute(name, value); process = true; }
    }
    if (process) window.htmx?.process(link);
  }
  function render(row, state) {
    const tree = $("[data-workspace-sessions]", row);
    if (!tree || !state.loaded) return;
    const scroller = tree.closest(".project-tree"), scrollTop = scroller?.scrollTop;
    const focused = tree.contains(document.activeElement) ? document.activeElement : null;
    const existing = rowsIn(tree), current = existing.find(node => node.hasAttribute("data-shell-live-session"));
    const selected = selectedSession(state.project);
    // Preserve the authoritative current row and its Rename launcher even if
    // the inventory's bounded window does not include this older session.
    let items = state.rows;
    if (current && !items.some(item => item.session_id === current.dataset.shellSession)) {
      items = [{session_id: current.dataset.shellSession, name: $("a span", current)?.textContent || "New conversation"}, ...items].slice(0, maxRows);
    }
    const byID = new Map(existing.map(node => [node.dataset.shellSession, node]));
    if (current) byID.set(current.dataset.shellSession, current);
    const keep = new Set();
    let cursor = tree.firstElementChild;
    for (const item of items) {
      let node = byID.get(item.session_id);
      if (!node) {
        node = document.createElement("div"); node.className = "shell-session-row"; node.dataset.shellSession = item.session_id;
        const link = document.createElement("a"), label = document.createElement("span"); link.append(label); node.append(link);
      }
      keep.add(node);
      const link = $("a", node), label = $("span", link);
      // Current title is runtime-owned; inventory reads must not roll it back.
      const title = node === current ? label?.textContent : item.name || "Untitled session";
      if (label && label.textContent !== title) label.textContent = title;
      if (label && label.title !== title) label.title = title;
      stamp(link, state.project, item.session_id, state.instance);
      window.SnowSessionActions?.renderRow(node, {project: state.project, session: item.session_id, name: title, instance: state.instance,
        active: item.session_id === state.activeSession, available: state.available && !state.error, supported: state.deleteSupported === true});
      if (item.session_id === selected) set(link, "aria-current", "page");
      else if (link.hasAttribute("aria-current")) link.removeAttribute("aria-current");
      if (link.classList.contains("selected")) link.classList.remove("selected");
      if (node.hidden) node.hidden = false;
      if (node !== cursor) tree.insertBefore(node, cursor);
      cursor = node.nextElementSibling;
    }
    existing.forEach(node => { if (!keep.has(node)) node.remove(); });
    status(tree, state);
    if (focused?.isConnected && document.activeElement !== focused) focused.focus({preventScroll: true});
    if (scroller && scroller.scrollTop !== scrollTop) scroller.scrollTop = scrollTop;
  }
  function status(tree, state) {
    let label = "", action = "";
    if (state.loading) label = "Loading sessions…";
    else if (state.error) { label = "Sessions could not be loaded."; action = "Retry"; }
    else if (state.loaded && !state.available) { label = "Saved sessions are unavailable."; action = "Retry"; }
    else if (state.loaded && !state.rows.length) label = "No saved sessions";
    if (!state.loading && !state.error && state.available && state.hasMore && state.rows.length < maxRows) action = "Load more";
    else if (!state.loading && state.available && (state.truncated || state.hasMore) && !action) label = "Showing a limited session list";
    let note = $("[data-sidebar-session-status]", tree);
    if (label) {
      if (!note) { note = document.createElement("p"); note.dataset.sidebarSessionStatus = ""; note.setAttribute("role", "status"); tree.append(note); }
      if (note.textContent !== label) note.textContent = label;
    } else note?.remove();
    let button = $("[data-sidebar-session-more]", tree);
    if (state.loading && button) {
      set(button, "aria-disabled", "true");
    } else if (action) {
      if (button?.hasAttribute("aria-disabled")) button.removeAttribute("aria-disabled");
      if (!button) { button = document.createElement("button"); button.type = "button"; button.className = "quiet"; button.dataset.sidebarSessionMore = ""; tree.append(button); }
      if (button.textContent !== action) button.textContent = action;
      set(button, "data-offset", action === "Load more" ? String(state.nextOffset) : "0");
    } else if (button) {
      if (document.activeElement === button) tree.closest("[data-sidebar-project]")?.querySelector("[data-workspace-toggle]")?.focus({preventScroll: true});
      button.remove();
    }
    if (state.loading) set(tree, "aria-busy", "true");
    else if (tree.hasAttribute("aria-busy")) tree.removeAttribute("aria-busy");
  }
  async function readJSON(response) {
    if (!response.ok) throw new Error("inventory unavailable");
    const reader = response.body.getReader();
    let size = 0, text = "";
    const decoder = new TextDecoder();
    try {
      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        size += value.length;
        if (size > 256 * 1024) throw new Error("inventory too large");
        text += decoder.decode(value, {stream: true});
      }
      return JSON.parse(text + decoder.decode());
    } finally { await reader.cancel(); reader.releaseLock(); }
  }
  async function load(row, state, offset = 0) {
    state.request?.abort();
    const controller = new AbortController(), epoch = generation, mounted = root, instance = state.instance;
    state.request = controller; state.loading = true; state.error = false;
    const tree = $("[data-workspace-sessions]", row);
    status(tree, state);
    const timer = setTimeout(() => controller.abort(), 10000);
    const valid = () => row.isConnected && root === mounted && epoch === generation && state.request === controller && groups.get(state.project) === state;
    try {
      const response = await fetch(`/projects/${encodeURIComponent(state.project)}/sidebar-sessions?offset=${offset}`, {credentials: "same-origin", signal: controller.signal, headers: {Accept: "application/json"}});
      const data = await readJSON(response);
      if (!valid()) return;
      const live = $("#live-session[data-runtime=true]");
      if (data.project_id !== state.project || typeof data.instance_id !== "string" || (offset && data.instance_id !== instance) || (live?.dataset.project === state.project && data.instance_id !== live.dataset.instance)) throw new Error("stale inventory");
      if (!Array.isArray(data.sessions) || data.sessions.length > maxRows || data.sessions.some(item => !item || typeof item.session_id !== "string" || !item.session_id || item.session_id.length > 256 || typeof item.name !== "string" || item.name.length > 4096)) throw new Error("invalid inventory");
      const combined = new Map((offset ? state.rows : []).map(item => [item.session_id, item]));
      data.sessions.forEach(item => combined.set(item.session_id, {session_id: item.session_id, name: item.name, updated_at: item.updated_at}));
      state.rows = [...combined.values()].slice(0, maxRows); state.instance = data.instance_id;
      state.available = data.available === true; state.loaded = true;
      state.deleteSupported = data.delete_supported === true && typeof data.active_session_id === "string";
      state.activeSession = data.active_session_id || "";
      state.nextOffset = Number.isSafeInteger(data.next_offset) && data.next_offset > offset ? data.next_offset : 0;
      state.hasMore = data.has_more === true && state.nextOffset > 0;
      state.truncated = data.truncated === true;
    } catch (_) { if (valid()) state.error = true; }
    finally {
      clearTimeout(timer);
      if (valid()) { state.loading = false; state.request = null; if (state.loaded) render(row, state); else status(tree, state); }
    }
  }
  function initialize() {
    const next = $("#workspace");
    if (!next || next === root) return;
    root = next; generation++;
    groups.forEach(state => { state.request?.abort(); state.request = null; state.loading = false; });
    const present = new Set([...root.querySelectorAll("[data-sidebar-project]")].map(row => row.dataset.sidebarProject));
    groups.forEach((_, project) => { if (!present.has(project)) groups.delete(project); });
    root.querySelectorAll("[data-sidebar-project]").forEach(row => {
      const state = entry(row), tree = $("[data-workspace-sessions]", row);
      if (!tree) return;
      const live = $("#live-session[data-runtime=true]");
      const mountedInstance = live?.dataset.project === state.project ? live.dataset.instance || "" : "";
      if (root.dataset.project === state.project && state.instance !== mountedInstance) {
        state.rows = []; state.loaded = false; state.instance = mountedInstance;
      }
      // Server current rows remain useful while the first inventory is pending.
      rowsIn(tree).forEach(node => stamp($("a", node), state.project, node.dataset.shellSession, state.instance));
      visibility(row, state); render(row, state);
      if (state.expanded && !hideSessions && !state.loaded) load(row, state);
    });
  }
  function syncVisibility(hide) {
    hideSessions = hide;
    document.querySelectorAll("[data-sidebar-project]").forEach(row => {
      const state = entry(row); visibility(row, state);
      if (!hide && state.expanded && !state.loaded && !state.loading && $("[data-workspace-sessions]", row)) load(row, state);
    });
  }
  function syncCurrent(live) {
    if (!live) return;
    const row = branch(live.dataset.project), state = row && entry(row);
    if (!state || !state.loaded) return;
    const title = $("[data-live-title]", live)?.textContent || "New conversation";
    const signature = JSON.stringify([live.dataset.instance, live.dataset.session, title]);
    if (state.currentSignature === signature) return;
    state.currentSignature = signature;
    if (state.instance !== live.dataset.instance) {
      state.loaded = false; state.rows = []; state.instance = live.dataset.instance; state.deleteSupported = false;
      row.querySelectorAll("[data-shell-session-menu]").forEach(button => { set(button, "data-delete-available", "false"); });
      if (state.expanded) load(row, state); return;
    }
    state.activeSession = live.dataset.session;
    const found = state.rows.find(item => item.session_id === live.dataset.session);
    if (found) found.name = title;
    else state.rows = [{session_id: live.dataset.session, name: title}, ...state.rows].slice(0, maxRows);
    render(row, state);
  }
  document.addEventListener("click", event => {
    if (!(event.target instanceof Element)) return;
    const button = event.target.closest("[data-workspace-toggle],[data-sidebar-session-more]");
    if (!button) return;
    const row = button.closest("[data-sidebar-project]");
    if (!row) return;
    event.preventDefault(); event.stopPropagation();
    const state = entry(row);
    if (button.matches("[data-workspace-toggle]")) {
      state.expanded = !state.expanded; visibility(row, state);
      if (state.expanded && !hideSessions) load(row, state);
      else { state.request?.abort(); state.request = null; state.loading = false; }
    } else if (!state.loading) load(row, state, Number(button.dataset.offset) || 0);
  });
  function invalidate(project) {
    const state = groups.get(project), row = branch(project);
    if (!state) return;
    state.request?.abort(); state.request = null; state.loading = false; state.loaded = false; state.deleteSupported = false;
    row?.querySelectorAll("[data-shell-session-menu]").forEach(button => { set(button, "data-delete-available", "false"); });
    if (row && state.expanded && !hideSessions) void load(row, state);
  }
  function deleted(project, session) {
    const state = groups.get(project), row = branch(project);
    if (!state) return;
    state.request?.abort(); state.request = null; state.loading = false;
    state.rows = state.rows.filter(item => item.session_id !== session);
    if (row && state.loaded) render(row, state);
    // Refresh pagination and capability state; deletion never guesses the next session.
    invalidate(project);
  }
  window.SnowSidebarSessions = {initialize, syncVisibility, syncCurrent, invalidate, deleted};
})();
