(() => {
  "use strict";
  const copy = value => JSON.parse(JSON.stringify(value));
  const json = value => new Response(JSON.stringify(copy(value)), {headers: {'Content-Type': 'application/json'}});
  // Production-shaped DTOs; no credentials, provider calls, or disk prompts.
  const snapshot = {
    project_id: "00000000-0000-4000-8000-000000000002", instance_id: "instance-one", session_id: "session-one", session_name: "First task",
    provider: "host-provider", model: "host-model", mode: "default", thinking: "off", status: "idle", revision: 1,
    error: "", messages: [], activities: [], history_truncated: false, activities_truncated: false, permission: null, input: null,
    telemetry: {available: false, context_available: false, estimated: true, input_tokens: 0, output_tokens: 0, total_tokens: 0, context_tokens: 0, context_window: 0}
  };
  const choices = {
    project_id: "00000000-0000-4000-8000-000000000002", instance_id: "instance-one",
    models: [{provider: "host-provider", id: "host-model", name: "Host Model", context_window: 32768}, {provider: "host-provider", id: "special-id", name: "Distinct display label", context_window: 16384}, {provider: "other-host", id: "host-model", name: "Other model", context_window: 65536}],
    sessions: [{session_id: "session-one", name: "First task", active: true, updated_at: 1}, {session_id: "session-two", name: "Second task", active: false, updated_at: 1}],
    models_partial: false, models_truncated: false, sessions_available: true, sessions_truncated: false, telemetry: snapshot.telemetry
  };
  localStorage.setItem("snow-manager-theme", window.__workflowTheme || "dark");
  const fixture = window.fixture = {snapshot, choices, requests: [], offline: false, unauthorized: false, deferredPolls: [], deferPoll: false, errors: [], storageWrites: []};
  const setItem = Storage.prototype.setItem;
  Storage.prototype.setItem = function(key, value) { fixture.storageWrites.push(key); return setItem.call(this, key, value); };
  window.addEventListener("error", event => fixture.errors.push(event.message));
  window.addEventListener("unhandledrejection", event => fixture.errors.push(String(event.reason)));
  // file:// fixture has no application origin for history URL updates.
  const replaceState = history.replaceState.bind(history);
  history.replaceState = (state, title) => replaceState(state, title);
  const timer = window.setTimeout;
  window.setTimeout = (fn, ms, ...args) => timer(fn, ms === 2000 ? 60 : ms, ...args);
  window.fetch = (url, options) => {
    const request = {url: String(url), options, fields: Object.fromEntries(new URLSearchParams(options.body))};
    fixture.requests.push(request);
    const target = new URL(request.url, location.origin);
    if (options.method === "GET" && target.pathname === "/access/browsers" && !target.search)
      return Promise.resolve(json({limit: 8, browsers: []}));
    if (options.method === "GET" && target.pathname === `/projects/${snapshot.project_id}/sidebar-sessions` && target.search === '?offset=0')
      return Promise.resolve(json({project_id: snapshot.project_id, instance_id: snapshot.instance_id, active_session_id: snapshot.session_id,
        available: true, delete_supported: false, sessions: choices.sessions, has_more: false, next_offset: 0}));
    if (options.method === "GET" && target.pathname === `/projects/${snapshot.project_id}/runtime/events`) return Promise.resolve({ok: false, status: 501});
    if (options.method === "GET") {
      if (target.pathname !== `/projects/${snapshot.project_id}/runtime` || target.search) throw new Error('Unexpected read: ' + target.pathname);
      if (fixture.closed) return Promise.resolve({ok: false, status: 404});
      if (fixture.offline) return Promise.reject(new TypeError("Network disconnected"));
      if (fixture.unauthorized) return Promise.resolve({ok: false, status: 401});
      if (fixture.deferPoll) return new Promise(resolve => { request.resolve = data => resolve(json(data)); fixture.deferredPolls.push(request); });
      return Promise.resolve({ok: true, json: async () => copy(fixture.snapshot)});
    }
    return new Promise((resolve, reject) => {
      request.resolve = data => { request.settled = true; resolve(json(data)); };
      request.reject = () => { request.settled = true; reject(new TypeError("Reply lost")); };
    });
  };
  // Exercise production HTMX lifecycle without an HTTP server or network. New
  // workspace markup represents an explicitly reviewed server response.
  const workspace = document.querySelector("#workspace").outerHTML;
  window.htmx = {process: () => {}, ajax: async (method, url, options) => {
    if (method === "POST" && options?.handler) {
      const response = await window.fetch(url, {method, body: new URLSearchParams(options.values)});
      options.handler(options.source, {xhr: {status: response.ok ? 200 : response.status, responseURL: url,
        responseText: JSON.stringify(await response.json()), getResponseHeader: () => "application/json"}});
      return;
    }
    const old = document.querySelector("#workspace");
    old.dispatchEvent(new CustomEvent("htmx:beforeSwap", {bubbles: true, detail: {target: old}}));
    old.dispatchEvent(new CustomEvent("htmx:beforeCleanupElement", {bubbles: true, detail: {elt: old}}));
    const container = document.createElement("div"); container.innerHTML = workspace;
    const replacement = container.firstElementChild;
    const region = replacement.querySelector("#live-session");
    Object.assign(region.dataset, {project: fixture.snapshot.project_id, instance: fixture.snapshot.instance_id, session: fixture.snapshot.session_id, revision: String(fixture.snapshot.revision), status: fixture.snapshot.status});
    old.replaceWith(replacement);
    replacement.dispatchEvent(new CustomEvent("htmx:afterSwap", {bubbles: true, detail: {target: replacement}}));
  }};
})();
