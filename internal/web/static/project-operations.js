(() => {
  "use strict";
  const roots = new WeakMap(), scopes = new Map(); // Tab memory only, scoped to current browser authorization.
  const uuid = value => typeof value === "string" && /^[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}$/.test(value);
  const bytes = value => new TextEncoder().encode(value).length;
  const text = (value, limit) => typeof value === "string" && bytes(value) <= limit && !/[\u0000-\u001f\u007f-\u009f]/u.test(value);
  const path = value => text(value, 4096) && value.startsWith("/");
  const leaf = value => text(value, 128) && value.length > 0 && value.trim() === value && value !== "." && value !== ".." && !/[\/\\]/u.test(value);
  function remote(value) {
    if (!text(value, 512) || !/^https:\/\/[a-zA-Z0-9.-]+\/[a-zA-Z0-9._/-]+$/.test(value)) return false;
    const parts = value.slice(value.indexOf("/", 8) + 1).split("/");
    return parts.length >= 2 && parts.length <= 8 && parts.every(part => part.length > 0 && part.length <= 128 && part !== "." && part !== "..");
  }
  const identity = value => value && path(value.path) && /^[0-9]{1,20}$/.test(value.device) && /^[0-9]{1,20}$/.test(value.inode);
  const activeStates = new Set(["admitted", "creating", "running", "cancel_requested"]);
  const labels = { admitted: "Admitted", creating: "Creating destination", running: "Running", cancel_requested: "Stop requested — cleanup not yet confirmed", awaiting_registration: "Files retained — registration pending", succeeded: "Created and registered", failed: "Failed — destination may be partial", canceled: "Canceled after cleanup — files retained", interrupted: "Interrupted — ownership or outcome may be unknown", needs_review: "Needs review — do not assume completion" };
  function validOperation(op) {
    if (!op || !uuid(op.id) || !["create", "clone"].includes(op.kind) || !leaf(op.name) || !Object.hasOwn(labels, op.state) || !Number.isSafeInteger(op.revision) || op.revision < 1 || !identity(op.parent) || !["observed", "not_observed", "unknown"].includes(op.outcome)) return false;
    if (op.project_id && !uuid(op.project_id) || op.kind === "clone" && !remote(op.remote) || op.kind === "create" && op.remote) return false;
    if (op.child?.path && (!identity(op.child) || op.child.path !== `${op.parent.path === "/" ? "" : op.parent.path}/${op.name}`)) return false;
    if (["running", "succeeded", "awaiting_registration"].includes(op.state) && (!identity(op.child) || op.outcome !== "observed")) return false;
    if (op.state === "succeeded" && !uuid(op.project_id) || op.state === "awaiting_registration" && op.project_id) return false;
    return [op.created_at, op.updated_at].every(value => Number.isSafeInteger(value) && value > 0) && op.updated_at >= op.created_at;
  }
  function node(tag, content, className) {
    const result = document.createElement(tag);
    if (content !== undefined) result.textContent = content;
    if (className) result.className = className;
    return result;
  }
  async function boundedJSON(response) {
    if (!response.ok) throw new Error(response.status === 401 || response.status === 403 ? "auth" : response.status === 404 ? "missing" : "request");
    let raw;
    if (response.body?.getReader) {
      const reader = response.body.getReader(), chunks = []; let size = 0;
      try {
        for (;;) {
          const { done, value } = await reader.read(); if (done) break;
          size += value.length;
          if (size > 65536) { await reader.cancel(); throw new Error("bounded"); }
          chunks.push(value);
        }
      } finally { reader.releaseLock(); }
      const data = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length; }
      raw = new TextDecoder("utf-8", { fatal: true }).decode(data);
    } else {
      raw = await response.text(); if (bytes(raw) > 65536) throw new Error("bounded");
    }
    return JSON.parse(raw);
  }
  function scopeFor(auth) {
    if (!scopes.has(auth)) {
      if (scopes.size >= 8) {
        const idle = [...scopes].find(([, scope]) => !scope.pending);
        if (idle) scopes.delete(idle[0]);
      }
      scopes.set(auth, { pending: false, uncertain: "", draft: { parent: "", name: "", kind: "create", remote: "" } });
    }
    return scopes.get(auth);
  }
  function init(root) {
    if (roots.has(root)) return;
    const $ = name => root.querySelector(`[data-op-${name}]`);
    const auth = $("csrf").value, scope = scopeFor(auth);
    const state = { generation: 0, reader: null, reading: false, scope, grant: null, review: null, offset: 0, next: 0, more: false, operations: [], folder: null, composing: false };
    roots.set(root, state);
    const enabled = root.dataset.enabled === "true";
    const current = generation => root.isConnected && state.generation === generation && $("csrf").value === auth;
    const status = message => { $("status").textContent = message; };
    function saveDraft() {
      scope.draft = { parent: $("parent").value, name: $("name").value, kind: $("kind").value, remote: remote($("remote").value) ? $("remote").value : "" };
    }
    for (const name of ["parent", "name", "kind", "remote"]) $(name).value = scope.draft[name];
    function update() {
      const busy = state.reading || scope.pending, unavailable = !enabled || busy;
      root.setAttribute("aria-busy", String(busy));
      $("draft").disabled = unavailable || !!state.review || !!scope.uncertain;
      for (const name of ["refresh", "check", "first", "next"]) $(name).disabled = unavailable;
      $("back").disabled = scope.pending;
      $("confirm-check").disabled = unavailable;
      $("confirm").disabled = unavailable || !state.review || !$("confirm-check").checked;
      $("review").disabled = unavailable || !state.grant || Date.now() >= state.grant.expires_at;
      $("remote-label").hidden = $("kind").value !== "clone";
      $("remote-help").hidden = $("kind").value !== "clone";
      $("next").hidden = !state.more; $("first").hidden = state.offset === 0;
      $("uncertain").hidden = !scope.uncertain;
      $("uncertain-text").textContent = scope.uncertain ? `Request ${scope.uncertain}. Its response was not reviewed here. Check the durable record; do not recreate or retry a clone to discover its outcome.` : "";
      root.querySelectorAll("[data-op-row-action]").forEach(button => { button.disabled = unavailable || !!state.review || !!scope.uncertain; });
    }
    state.sync = () => { if (root.isConnected && $("csrf").value === auth) update(); };
    function clearReview() { state.review = null; $("confirmation").hidden = true; $("confirm-check").checked = false; }
    function clearGrant() { state.grant = null; $("grant").textContent = "No parent selected. Explicitly select a parent; a selection lasts five minutes and authorizes only one request."; }
    function retire() {
      saveDraft(); state.generation++; state.reader?.abort(); state.reader = null; state.reading = false;
      clearReview(); clearGrant(); $("remote").value = ""; scope.draft.remote = "";
      $("folders").hidden = true; state.folder = null; update();
    }
    state.retire = retire;
    state.setParentPath = value => {
      if (!current(state.generation) || scope.pending || !path(value)) return false;
      state.reader?.abort(); state.generation++; state.reading = false;
      $("parent").value = value; clearReview(); clearGrant(); saveDraft(); update(); $("parent").focus(); return true;
    };
    function edit(event) {
      if (scope.pending || state.review) return;
      if (event.target === $("parent")) clearGrant();
      clearReview(); saveDraft(); update();
    }
    for (const name of ["parent", "name", "kind", "remote"]) {
      $(name).addEventListener("input", edit); $(name).addEventListener("change", edit);
      $(name).addEventListener("compositionstart", () => { state.composing = true; });
      $(name).addEventListener("compositionend", () => { state.composing = false; saveDraft(); });
      $(name).addEventListener("keydown", event => { if (event.key === "Enter" && !event.isComposing && !state.composing) event.preventDefault(); });
    }
    const fetchOptions = signal => ({ credentials: "same-origin", cache: "no-store", redirect: "error", signal, headers: { Accept: "application/json" } });
    function readOptions(controller) { return fetchOptions(AbortSignal.any([controller.signal, AbortSignal.timeout(15000)])); }
    function postOptions(fields, signal) {
      const options = fetchOptions(signal);
      options.method = "POST"; options.headers["Content-Type"] = "application/x-www-form-urlencoded";
      options.body = new URLSearchParams({ csrf: auth, ...fields }); return options;
    }
    async function readRequest(url, options, apply, message) {
      if (!enabled || scope.pending || !root.isConnected || $("csrf").value !== auth) return;
      state.reader?.abort(); const generation = ++state.generation, controller = new AbortController(); state.reader = controller;
      clearReview(); state.reading = true; status(message); update();
      try {
        const result = await boundedJSON(await fetch(url, options(controller)));
        if (current(generation)) apply(result);
      } catch (error) {
        if (current(generation)) status(error.message === "auth" ? "Browser authorization changed. Pair or reload this manager before continuing." : "Could not read current host metadata. Refresh explicitly; no operation was retried.");
      } finally { if (current(generation)) { state.reading = false; state.reader = null; update(); } }
    }
    function renderOperations() {
      $("list").replaceChildren();
      for (const op of state.operations) {
        const row = node("li"); row.dataset.operationId = op.id;
        row.append(node("strong", `${op.name} · ${labels[op.state]}`));
        row.append(node("small", `${op.kind === "clone" ? "Clone" : "Create"} · outcome ${op.outcome} · revision ${op.revision}`));
        row.append(node("small", `Reference ${op.id}`, "mono"));
        row.append(node("small", op.child?.path || `${op.parent.path === "/" ? "" : op.parent.path}/${op.name}`, "mono"));
        if (op.remote) row.append(node("small", op.remote, "mono"));
        if (op.project_id) row.append(node("small", `Registered project ${op.project_id}. No agent was activated.`, "mono"));
        if (op.outcome === "unknown") row.append(node("p", "Ownership or completion is unknown. Observe the destination before deciding what to register; never retry the clone as recovery.", "fine"));
        const actions = node("div", undefined, "project-operation-actions");
        function action(label, verb) {
          const button = node("button", label, "button"); button.type = "button"; button.dataset.opRowAction = verb;
          button.addEventListener("click", () => reviewAction(op, verb)); actions.append(button);
        }
        if (activeStates.has(op.state)) { if (op.state !== "cancel_requested") action("Request stop…", "cancel"); }
        else {
          action("Observe identity…", "reconcile");
          if (!op.project_id) action(op.state === "awaiting_registration" && op.outcome === "observed" ? "Register retained destination…" : "Review ordinary registration…", "register");
          action("Dismiss record only…", "dismiss");
        }
        row.append(actions); $("list").append(row);
      }
      $("page").textContent = state.operations.length ? `Records ${state.offset + 1}–${state.offset + state.operations.length}.` : "No records on this page.";
      update();
    }
    function load(offset = 0) {
      if (state.reading) return;
      return readRequest(`/operations?offset=${offset}`, readOptions, page => {
        if (!page || !Array.isArray(page.operations) || page.operations.length > 32 || !page.operations.every(validOperation) || new Set(page.operations.map(op => op.id)).size !== page.operations.length || !Number.isSafeInteger(page.next_offset) || page.next_offset !== offset + page.operations.length || page.next_offset > 128 || typeof page.has_more !== "boolean" || page.has_more && page.operations.length === 0) throw new Error("inventory");
        state.offset = offset; state.next = page.next_offset; state.more = page.has_more; state.operations = page.operations;
        renderOperations(); status("Read current durable operations. Refresh to observe changes; no work was replayed.");
      }, "Reading durable operation metadata…");
    }
    state.refresh = load;
    $("refresh").addEventListener("click", () => load(0)); $("first").addEventListener("click", () => load(0)); $("next").addEventListener("click", () => { if (state.more) load(state.next); });
    function browse(directory = "", offset = 0) {
      if (state.reading || scope.uncertain || directory !== "" && !path(directory)) return;
      $("folders").hidden = false;
      return readRequest("/projects/folders", controller => postOptions({ path: directory, offset: String(offset) }, AbortSignal.any([controller.signal, AbortSignal.timeout(15000)])), page => {
        if (!page || !path(page.path) || !path(page.parent) || !Array.isArray(page.folders) || page.folders.length > 256 || !page.folders.every(folder => text(folder.name, 4096) && path(folder.path)) || !Number.isSafeInteger(page.next_offset) || page.next_offset < 0 || page.next_offset > 4096 || typeof page.has_more !== "boolean") throw new Error("folders");
        state.folder = page; $("folder-path").textContent = page.path; $("folder-list").replaceChildren();
        for (const folder of page.folders) {
          const item = node("li"), button = node("button", folder.name, "quiet"); button.type = "button";
          button.addEventListener("click", () => browse(folder.path)); item.append(button); $("folder-list").append(item);
        }
        $("up").disabled = page.path === page.parent; $("folder-use").disabled = false; $("folder-more").hidden = !page.has_more;
        status(page.limited ? "Folder scan limit reached. Enter an absolute host path if needed." : "Folder browsing is read only. Use this path, then explicitly select the parent.");
      }, "Reading folders on the Snow host…");
    }
    $("browse").addEventListener("click", () => browse($("parent").value)); $("home").addEventListener("click", () => browse());
    $("up").addEventListener("click", () => { if (state.folder) browse(state.folder.parent); });
    $("folder-more").addEventListener("click", () => { if (state.folder?.has_more) browse(state.folder.path, state.folder.next_offset); });
    $("folder-use").addEventListener("click", () => { if (state.folder && !state.reading) { state.setParentPath(state.folder.path); $("folders").hidden = true; } });
    $("folder-close").addEventListener("click", () => { state.reader?.abort(); state.generation++; state.reading = false; $("folders").hidden = true; update(); $("browse").focus(); });
    $("select").addEventListener("click", async () => {
      if (scope.pending || scope.uncertain || state.reading || state.composing || !enabled) return;
      const selected = $("parent").value;
      if (!path(selected)) { status("Enter an absolute parent folder on the Snow host."); return; }
      clearGrant(); clearReview();
      // This only allocates authority, not filesystem work. Closing can abort
      // the read-side request; any unobserved grant expires and is never reused.
      await readRequest("/projects/folders/select", controller => postOptions({ path: selected }, AbortSignal.any([controller.signal, AbortSignal.timeout(15000)])), grant => {
        if (!grant || !uuid(grant.operation_id) || !path(grant.path) || !Number.isSafeInteger(grant.expires_at) || grant.expires_at <= Date.now() || grant.expires_at > Date.now() + 301000) throw new Error("grant");
        state.grant = grant; $("parent").value = grant.path; saveDraft();
        $("grant").textContent = `Selected ${grant.path}. Expires ${new Date(grant.expires_at).toLocaleTimeString()}. One request only; host-user OS authority.`;
        status("Parent selected. Enter a new name, then review before any filesystem change.");
      }, "Validating the explicitly selected parent…");
    });
    function showReview(review, title, detail, effects, button) {
      state.review = review; $("confirmation").hidden = false; $("confirm-title").textContent = title;
      $("confirm-detail").textContent = detail; $("confirm-effects").textContent = effects; $("confirm").textContent = button;
      $("confirm-check").checked = false; update(); $("back").focus();
    }
    $("review").addEventListener("click", () => {
      if (scope.pending || scope.uncertain || state.reading || state.composing || !state.grant || !enabled) return;
      if (Date.now() >= state.grant.expires_at) { clearGrant(); status("Parent selection expired. Explicitly select it again before reviewing."); update(); return; }
      const kind = $("kind").value, name = $("name").value, locator = $("remote").value;
      if (!["create", "clone"].includes(kind) || !leaf(name) || kind === "clone" && !remote(locator)) {
        status("Review requires one valid leaf name and, for cloning, an anonymous HTTPS repository URL without credentials, query, fragment, escapes, port, or SSH.");
        if (kind === "clone" && !remote(locator)) { $("remote").value = ""; scope.draft.remote = ""; }
        return;
      }
      const request = { operation_id: state.grant.operation_id, name }; if (kind === "clone") request.remote = locator;
      const destination = `${state.grant.path === "/" ? "" : state.grant.path}/${name}`;
      showReview({ action: kind, request, expires: state.grant.expires_at }, kind === "clone" ? "Confirm repository clone" : "Confirm folder creation", `${destination}${kind === "clone" ? `\nFrom ${locator}` : ""}`,
        `${kind === "clone" ? "Create this destination and download the repository using anonymous HTTPS. " : "Create this empty destination. "}The host OS user's permissions apply; no startup-root confinement or disk quota is provided. Partial files are retained on failure or stop. Registration requires a separate explicit review after completion; no agent is activated. Closing this panel does not stop accepted work.`, kind === "clone" ? "Clone repository" : "Create folder");
    });
    function reviewAction(op, action) {
      if (scope.pending || scope.uncertain || state.reading || state.review || !enabled) return;
      const ordinary = action === "register" && !(op.state === "awaiting_registration" && op.outcome === "observed");
      const effects = {
        cancel: "Request stop for this exact operation revision. Stop is not complete until worker cleanup is confirmed. Any destination and partial files remain; this is not rollback.",
        reconcile: "Observe the recorded directory identity only. This does not retry mkdir, clone, or registration, and does not stop any process.",
        register: ordinary ? "Explicit ordinary registration of the directory currently at this destination. Its ownership or operation outcome is not established. This does not retry cloning and will not convert unknown or failed work into a created success. No agent is activated." : "Register only the exact retained child identity. Files and outcome are already recorded. This does not retry cloning, execute project code, or activate an agent.",
        dismiss: "Remove this settled operation's manager record only. No files, project registration, or session will be deleted. A dismissed request ID cannot be reused."
      };
      const title = { cancel: "Confirm stop request", reconcile: "Confirm identity observation", register: ordinary ? "Review ordinary registration" : "Confirm retained registration", dismiss: "Confirm metadata-only dismissal" };
      if (!Object.hasOwn(effects, action)) return;
      const request = { revision: String(op.revision) }; if (ordinary) request.review = "true";
      showReview({ action, id: op.id, revision: op.revision, request }, title[action], `${op.name}\n${op.child?.path || `${op.parent.path === "/" ? "" : op.parent.path}/${op.name}`}\nReference ${op.id} · revision ${op.revision}`, effects[action], title[action]);
    }
    $("back").addEventListener("click", () => { if (!scope.pending) { clearReview(); update(); $("refresh").focus(); } });
    $("confirm-check").addEventListener("change", update);
    $("confirm").addEventListener("click", async () => {
      const review = state.review;
      if (!enabled || !review || !$("confirm-check").checked || scope.pending || scope.uncertain || state.reading || state.composing || !current(state.generation)) return;
      const creation = review.action === "create" || review.action === "clone";
      if (creation && (!state.grant || Date.now() >= review.expires || state.grant.operation_id !== review.request.operation_id)) { clearReview(); clearGrant(); status("Parent selection expired. No operation was submitted; select and review again."); update(); return; }
      const id = creation ? review.request.operation_id : review.id, generation = ++state.generation;
      const endpoint = creation ? `/projects/${review.action}` : `/operations/${id}/${review.action}`;
      scope.pending = true; scope.uncertain = id;
      clearReview(); if (creation) clearGrant(); $("remote").value = ""; scope.draft.remote = "";
      update(); status("Submitting the explicit request once. Closing this panel does not cancel accepted work.");
      try {
        // Deliberately independent of the view's abort controller. Timeout or
        // disconnect means uncertainty, never permission to replay a mutation.
        const response = await fetch(endpoint, postOptions(review.request, AbortSignal.timeout(15000)));
        if (review.action === "dismiss" && response.status === 204) {
          if (!current(generation)) return;
          state.operations = state.operations.filter(op => op.id !== id); scope.uncertain = "";
          renderOperations(); status("Operation record dismissed. Files and project registration are unchanged.");
        } else {
          const op = await boundedJSON(response);
          if (!current(generation)) return;
          if (!validOperation(op) || op.id !== id || !creation && op.revision <= review.revision) throw new Error("result");
          scope.uncertain = "";
          const index = state.operations.findIndex(value => value.id === id);
          if (index >= 0) state.operations[index] = op;
          else { state.operations = [op]; state.offset = 0; state.more = false; }
          renderOperations(); status(`${labels[op.state]}. Refresh to observe durable progress. No navigation or activation was performed.`);
        }
      } catch (error) {
        if (current(generation)) status(error.message === "auth" ? "Browser authorization changed. The request may already have taken effect. Pair or reload, then inspect durable operations; never replay it." : "Could not confirm this request's outcome. Do not retry cloning or creation. Check its durable record; no mutation will be replayed automatically.");
      } finally {
        scope.pending = false;
        // Retired requests may unlock a replacement view, but may not publish
        // their stale result into it. The request-ID uncertainty guard remains.
        document.querySelectorAll("[data-project-operations]").forEach(view => {
          const other = roots.get(view); if (other?.scope === scope) other.sync?.();
        });
      }
    });
    $("check").addEventListener("click", async () => {
      const id = scope.uncertain; if (!uuid(id) || scope.pending || state.reading || !enabled) return;
      state.reader?.abort(); const generation = ++state.generation, controller = new AbortController(); state.reader = controller; state.reading = true; update();
      status("Checking the existing request ID only; no operation is being retried…");
      try {
        const response = await fetch(`/operations/${id}`, readOptions(controller));
        if (response.status === 404) {
          if (!current(generation)) return; scope.uncertain = "";
          status("No retained record exists for that ID. Its grant cannot be reused. Inspect the host before explicitly selecting a parent for any new operation."); return;
        }
        const op = await boundedJSON(response); if (!current(generation)) return;
        if (!validOperation(op) || op.id !== id) throw new Error("record");
        scope.uncertain = ""; state.operations = [op]; state.offset = 0; state.more = false;
        renderOperations(); status(`${labels[op.state]}. Existing record observed without retrying work.`);
      } catch (error) { if (current(generation)) status(error.message === "auth" ? "Browser authorization changed. Pair or reload, then inspect existing operations." : "The request remains unconfirmed. Check again explicitly; never retry the clone to recover its status."); }
      finally { if (current(generation)) { state.reading = false; state.reader = null; update(); } }
    });
    root.addEventListener("toggle", () => {
      if (!root.open) retire(); else { update(); if (!scope.pending && !scope.uncertain) load(0); }
    });
    update(); if (root.open && enabled && !scope.pending && !scope.uncertain) load(0);
  }
  function scan() { document.querySelectorAll("[data-project-operations]").forEach(init); }
  window.SnowProjectOperations = {
    init,
    // Existing picker integration fills a draft only. This never selects a
    // grant, submits a create/clone, registers a project, or activates a runtime.
    setParentPath(root, value) { init(root); return roots.get(root)?.setParentPath(value) || false; },
    refresh(root) { return roots.get(root)?.refresh(0); },
    dispose(root) { roots.get(root)?.retire(); }
  };
  document.addEventListener("DOMContentLoaded", scan);
  document.addEventListener("htmx:afterSwap", scan);
  document.addEventListener("htmx:beforeCleanupElement", event => {
    const target = event.detail?.elt;
    document.querySelectorAll("[data-project-operations]").forEach(root => { if (target && (target === root || target.contains(root))) roots.get(root)?.retire(); });
  });
  document.addEventListener("close", event => {
    document.querySelectorAll("[data-project-operations]").forEach(root => { if (event.target?.contains(root)) roots.get(root)?.retire(); });
  }, true);
  window.addEventListener("pagehide", () => {
    document.querySelectorAll("[data-project-operations]").forEach(root => roots.get(root)?.retire());
    for (const scope of scopes.values()) scope.draft.remote = "";
  });
  if (document.readyState !== "loading") scan();
})();
