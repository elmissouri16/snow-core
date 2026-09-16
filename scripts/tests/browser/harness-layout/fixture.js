// Installed by CDP before any production script. Markup, styles, renderers,
// lifecycle, and controls all remain the actual embedded Go page/assets.
(() => {
  "use strict";
  const copy = value => structuredClone(value);
  const config = window.__harnessConfig;
  const snapshot = copy(config.snapshot);
  const fixture = window.harnessFixture = {name: config.name, source: config.source, snapshot, updates: copy(config.updates || []), theme: config.theme || "dark", requests: [], errors: [], offline: false, clipboard: []};
  Object.defineProperty(navigator, "clipboard", {configurable: true, value: {writeText: async value => { fixture.clipboard.push(value); }}});
  window.addEventListener("error", event => fixture.errors.push(event.message));
  window.addEventListener("unhandledrejection", event => fixture.errors.push(String(event.reason)));
  localStorage.setItem("snow-manager-theme", fixture.theme);
  const timer = window.setTimeout;
  window.setTimeout = (fn, ms, ...args) => timer(fn, ms === 2000 ? 40 : ms, ...args);
  fixture.update = fields => { Object.assign(fixture.snapshot, fields, {revision: fixture.snapshot.revision + 1}); };
  fixture.folderMode = "populated";
  const sidebarCatalogs = new Map();
  const browserFetch = typeof window.fetch === "function" ? window.fetch.bind(window) : null;
  // Only accepted, exact startup inventory reads are excluded from interaction
  // counts. All requests remain recorded, including rejected inventory requests.
  fixture.nonInventoryRequests = () => fixture.requests.filter(request => request.kind !== "browser-inventory" && request.kind !== "sidebar-inventory");
  fixture.inventoryComplete = () => fixture.requests.filter(request => request.kind === "browser-inventory").length === document.querySelectorAll("[data-browser-inventory]").length;
  const response = (value, status = 200) => Promise.resolve(new Response(JSON.stringify(value), {
    status, headers: {"Content-Type": "application/json"}
  }));
  const fail = message => { fixture.errors.push(message); return response({error: message}, 409); };
  window.fetch = (input, options = {}) => {
    const url = new URL(String(input), location.href);
    if (options.headers?.["X-Snow-Navigation"] === "workspace" && browserFetch) return browserFetch(input, options);
    const method = options.method || "GET";
    const fields = Object.fromEntries(new URLSearchParams(options.body));
    const record = {path: url.pathname, method, fields};
    fixture.requests.push(record);
    if (url.origin !== location.origin) return fail("External network request blocked");
    if (url.pathname === "/access/browsers") {
      const count = fixture.requests.filter(request => request.kind === "browser-inventory").length;
      if (method !== "GET" || url.search || url.hash || options.body !== undefined ||
          options.credentials !== "same-origin" || options.cache !== "no-store" || options.redirect !== "error" ||
          Object.keys(options.headers || {}).length !== 1 || options.headers.Accept !== "application/json" ||
          count >= document.querySelectorAll("[data-browser-inventory]").length) return fail("Unexpected browser inventory request");
      record.kind = "browser-inventory";
      return response({browsers: [{id: "browser_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", label: "Fictional layout browser", current: true,
        created: "2026-01-01T00:00:00Z", last_used: "2026-01-02T00:00:00Z", expires: "2026-01-31T00:00:00Z"}], limit: 8});
    }
    if (/^\/projects\/[^/]+\/sidebar-sessions$/.test(url.pathname)) {
      const project = decodeURIComponent(url.pathname.split("/")[2]);
      const bootstrap = JSON.parse(document.querySelector('[data-react-page="shell"]').dataset.reactProps);
      const registered = bootstrap.projects.some(row => row.id === project);
      const group = [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === project);
      if (method !== "GET" || !registered || url.hash || options.body !== undefined || options.credentials !== "same-origin" ||
          options.headers?.Accept !== "application/json" || Object.keys(options.headers || {}).length !== 1 || [...url.searchParams.keys()].length > 1 || [...url.searchParams.keys()].some(key => key !== "offset") ||
          !/^(0|[1-9][0-9]{0,3})$/.test(url.searchParams.get("offset") || "0")) return fail("Unexpected sidebar inventory request");
      record.kind = "sidebar-inventory";
      const current = project === fixture.snapshot?.project_id;
      if (!sidebarCatalogs.has(project)) sidebarCatalogs.set(project, group ? [...group.querySelectorAll("[data-shell-session]")].map(row => ({session_id: row.dataset.shellSession, name: row.querySelector("a span")?.textContent || "Untitled session", updated_at: "2026-01-01T00:00:00Z"})) : bootstrap.project === project ? bootstrap.sessions : []);
      const sessions = current ? [{session_id: fixture.snapshot.session_id, name: fixture.snapshot.session_name || "Layout fixture", updated_at: "2026-01-01T00:00:00Z"},
        {session_id: "fixture-other", name: "Earlier workspace plan", updated_at: "2025-12-31T00:00:00Z"}] : sidebarCatalogs.get(project);
      const offset = Number(url.searchParams.get("offset") || 0), end = Math.min(sessions.length, offset + (current ? 100 : 25));
      return response({project_id: project, instance_id: current ? fixture.snapshot.instance_id : "", available: true,
        sessions: sessions.slice(offset, end), truncated: false, has_more: end < sessions.length, next_offset: end < sessions.length ? end : 0});
    }
    // Runtime-panel fixtures have a separate exact read allowlist. Never fall
    // through to older fixtures' opt-in attention responses or folder browsing.
    if (fixture.name.startsWith("runtime-")) {
      const identity = {project_id: snapshot.project_id, instance_id: snapshot.instance_id, session_id: snapshot.session_id};
      const prefix = `/projects/${identity.project_id}/`;
      const action = url.pathname.startsWith(prefix) ? url.pathname.slice(prefix.length) : "";
      const headers = options.headers || {};
      const post = method === "POST";
      const entries = [...new URLSearchParams(options.body).entries()];
      const exact = expected => entries.length === Object.keys(fields).length && entries.length === Object.keys(expected).length && entries.every(([key, value]) => expected[key] === value);
      if (url.search || url.hash || options.credentials !== "same-origin" || options.cache !== "no-store" || options.redirect !== "error" ||
          headers.Accept !== "application/json" || Object.keys(headers).length !== (post ? 2 : 1) ||
          post && headers["Content-Type"] !== "application/x-www-form-urlencoded;charset=UTF-8") return fail("Unexpected runtime-panel read transport");
      if (!post && method === "GET" && options.body === undefined && action === "runtime") {
        record.kind = "runtime-snapshot"; fixture.deliveredRevision = fixture.snapshot.revision;
        return response(copy(fixture.snapshot));
      }
      if (!post || fixture.name !== "runtime-panels") return fail(`Unexpected runtime-panel request: ${method} ${action}`);
      const scope = {csrf: "fixture-only-not-a-credential", instance_id: identity.instance_id, session_id: identity.session_id};
      const envelope = {...identity, revision: fixture.snapshot.revision};
      const processID = "proc_" + "a".repeat(32);
      let value;
      if (action === "runtime/versions-list" && exact(scope)) {
        value = {...envelope, current_branch_id: "fixture-branch", current_tip_id: "fixture-tip", has_more: false, next_cursor: "",
          versions: Array.from({length: 24}, (_, i) => ({branch_id: i ? `fixture-branch-${i}` : "fixture-branch", tip_id: i ? `fixture-tip-${i}` : "fixture-tip", name: `Saved conversation version ${i + 1} — readable bounded history`, current: i === 0}))};
      } else if (action === "runtime/version-preview" && exact({...scope, branch_id: "fixture-branch-1", tip_id: "fixture-tip-1"})) {
        value = {...envelope, branch_id: fields.branch_id, tip_id: fields.tip_id, has_more: false, next_cursor: "", messages: Array.from({length: 12}, (_, i) => ({role: "assistant", text: `Saved public message ${i + 1}\n` + "Readable preview text. ".repeat(24)}))};
      } else if (action === "runtime/goal-inspect" && exact({...scope, branch_id: "fixture-branch"})) {
        value = {...copy(fixture.snapshot), ...envelope};
      } else if (action === "runtime/reasoning-inspect" && exact(scope)) {
        value = {...envelope, branch_id: "fixture-branch", tip_id: "fixture-tip", provider: snapshot.provider, model: snapshot.model,
          mode: snapshot.mode, permission_mode: snapshot.permission_mode, thinking: snapshot.thinking, reasoning_summary: "auto", text_verbosity: "medium",
          thinking_levels: ["off", "low", "medium", "high"], reasoning_summaries: ["auto", "concise", "detailed"], text_verbosities: ["low", "medium", "high"], defaults_available: false, current_session_available: true};
      } else if (action === "processes/list" && exact(scope)) {
        value = {...identity, result: {session_id: identity.session_id, truncated: false, processes: Array.from({length: 20}, (_, i) => ({process_id: i ? "proc_" + i.toString(16).padStart(32, "0") : processID, name: `Workspace check ${i + 1}`, status: "running", ready: true}))}};
      } else if (action === "processes/logs" && exact({...scope, process_id: processID, max_bytes: "32768"})) {
        value = {...identity, result: {session_id: identity.session_id, process_id: processID, status: "running", output: "Public bounded log output.\n".repeat(120), next_cursor: 3000, omitted_bytes: 0, eof: true}};
      } else return fail(`Unexpected runtime-panel mutation or read: ${action}`);
      record.kind = "runtime-panel-read";
      return response(value);
    }
    if (url.pathname === "/projects/folders" && method === "POST" && fields.csrf === "fixture-only-not-a-credential") {
      if (fixture.folderMode === "error") return response({error: "Fixture host folder inaccessible"}, 403);
      const parent = "/fixture", path = fields.path || parent;
      return response({path, parent, folders: fixture.folderMode === "empty" ? [] : Array.from({length: 40}, (_, i) => ({name: `Folder ${i} ${"long-name-".repeat(12)}`, path: `${path}/folder-${i}`})), has_more: fixture.folderMode === "limited", limited: fixture.folderMode === "limited", next_offset: 40});
    }
    if (!snapshot) return fail("Unactivated page attempted an API request");
    const prefix = `/projects/${snapshot.project_id}/`;
    if (!url.pathname.startsWith(prefix)) return fail("Request targeted an unexpected project");
    const action = url.pathname.slice(prefix.length);
    if (action === "runtime/events" && method === "GET") return response({error: "Fixture intentionally exercises polling fallback"}, 501);
    if (action === "runtime" && method === "GET") {
      if (fixture.offline) return Promise.reject(new TypeError("Synthetic disconnected host"));
      fixture.deliveredRevision = fixture.snapshot.revision;
      return response(copy(fixture.snapshot));
    }
    if (method !== "POST" || fields.csrf !== "fixture-only-not-a-credential") return fail("Missing explicit CSRF-bound POST");
    if (action.startsWith("runtime/")) {
      if (fields.instance_id !== snapshot.instance_id) return fail("Missing or stale runtime instance");
      if (fixture.allowActions && ["runtime/input", "runtime/permission", "runtime/abort"].includes(action)) {
        fixture.update({status: "idle", input: null, permission: null});
        return response(copy(fixture.snapshot));
      }
      if (action !== "runtime/choices") return fail(`Unexpected runtime mutation: ${action}`);
      if (fixture.name === "model-unavailable") return response({error: "Synthetic discovery unavailable"}, 503);
      return response({
        project_id: snapshot.project_id, instance_id: snapshot.instance_id,
        models: fixture.name === "model-empty" ? [] : [
          {provider: snapshot.provider, id: snapshot.model, name: "Host model", context_window: 32768},
          {provider: "other-host", id: snapshot.model, name: "Same ID, different host provider", context_window: 65536},
          ...Array.from({length: 28}, (_, index) => ({provider: "other-host", id: `host-model-${index}`, name: `Extended host model ${index} — ${"Long display name ".repeat(5)}`, context_window: 65536}))
        ],
        sessions: [
          {session_id: snapshot.session_id, name: snapshot.session_name, active: true, updated_at: 1},
          {session_id: "fixture-previous", name: "Review project structure", active: false, updated_at: 1}
        ],
        models_partial: false, models_truncated: false, sessions_available: true, sessions_truncated: false,
        telemetry: snapshot.telemetry
      });
    }
    if (action === "inspect/files") return response({path: ".", entries: [
      {name: "internal", path: "internal", kind: "directory"},
      {name: "README.md", path: "README.md", kind: "file"},
      {name: "go.mod", path: "go.mod", kind: "file"}
    ], next_offset: 0, has_more: false, limited: false});
    if (action === "inspect/file") return response({path: fields.path, text: "# Public read-only fixture\n" + "long-text-".repeat(150), size: 2000, truncated: true});
    if (action === "inspect/changes") return response({available: true, reason: "", changes: [{path: "README.md", kind: "unstaged", status: "Modified"}], limited: false});
    if (action === "inspect/diff") return response({available: true, reason: "", path: fields.path, kind: fields.kind, text: "diff --git a/README.md b/README.md\n-old\n+public fixture change\n", truncated: false});
    return fail(`Unmocked API request: ${action}`);
  };
})();
