/* Explicit live-run follow-ups. This module never admits a normal prompt or
 * renders chat history: app.js owns bound HTTP/SSE and the server projection. */
(() => {
  "use strict";
  const stores = new Map(), maxStores = 16, maxItems = 8;
  const states = new Set(["pending", "starting", "held", "uncertain"]);
  const $ = (selector, scope) => scope?.querySelector(selector);
  let view;
  function node(tag, className = "", text = "") {
    const el = document.createElement(tag); el.className = className; el.textContent = text; return el;
  }
  function button(label, attribute) {
    const el = node("button", "quiet", label); el.type = "button"; el.setAttribute(attribute, ""); return el;
  }
  function init(api) {
    dispose();
    const root = api.root, panel = $("#live-queue-next", root);
    if (!panel || root.dataset.queueNextEnabled !== "true") return;
    const key = JSON.stringify([root.dataset.project, root.dataset.session, root.dataset.instance]);
    let store = stores.get(key);
    if (!store) store = {editors: new Map(), unknown: false};
    stores.delete(key); stores.set(key, store);
    while (stores.size > maxStores) stores.delete(stores.keys().next().value);
    const controller = new AbortController();
    view = {api, root, panel, key, store, controller, queue: null, snapshotRevision: -1, composeRevision: 0};
    root.addEventListener("click", onClick, {signal: controller.signal});
    root.addEventListener("submit", onSubmit, {signal: controller.signal});
    root.addEventListener("input", event => {
      if (event.target.matches("[data-queue-text]")) {
        const editor = store.editors.get(event.target.closest("[data-queue-item-id]").dataset.queueItemId);
        if (editor) { editor.text = event.target.value; editor.revision++; }
      }
    }, {signal: controller.signal});
    for (const type of ["compositionstart", "compositionend"]) root.addEventListener(type, event => {
      if (event.target.id === "live-prompt") { view.composing = type === "compositionstart"; view.composeRevision++; api.changed(); }
      else if (event.target.matches("[data-queue-text]")) {
        const editor = store.editors.get(event.target.closest("[data-queue-item-id]").dataset.queueItemId);
        if (editor) { editor.composing = type === "compositionstart"; editor.revision++; api.changed(); }
      }
    }, {signal: controller.signal});
  }
  function dispose() {
    if (view) {
      view.controller.abort();
      if (view.store.busy) { view.store.busy = false; view.store.unknown = true; view.store.reviewable = false; }
    }
    view = null;
  }
  function retainedReview() { return !!view?.queue?.items.some(item => ["held", "uncertain"].includes(item.state)); }
  function blocking() { return !!view && !!(view.store.busy || view.store.unknown || view.invalid || retainedReview()); }
  function canChange() {
    const ui = view?.ui;
    return !!ui && ui.connected && !ui.busy && !ui.unknown && !ui.stopping && !ui.editing && !view.invalid && !view.store.busy && !view.store.unknown && !!view.queue?.token;
  }
  function canEnqueue() { return canChange() && !view.composing && view.ui.status === "running" && view.queue.can_enqueue === true && view.queue.items.length < maxItems; }
  function validQueue(raw) {
    if (!raw || typeof raw.token !== "string" || raw.token.length > 256 || !Number.isSafeInteger(raw.revision) || raw.revision < 0 || typeof raw.can_enqueue !== "boolean" || !Array.isArray(raw.items) || raw.items.length > maxItems) return false;
    const ids = new Set(); let bytes = 0;
    for (const item of raw.items) {
      if (!item || typeof item.id !== "string" || !item.id || item.id.length > 256 || ids.has(item.id) || !states.has(item.state) || !view.api.validText(item.text)) return false;
      ids.add(item.id); bytes += new TextEncoder().encode(item.text).length;
    }
    return bytes <= 256 * 1024;
  }
  function render(snapshot, ui, present = true) {
    if (!view) return;
    view.ui = ui;
    if (snapshot && Number.isSafeInteger(snapshot.revision) && snapshot.revision >= view.snapshotRevision) {
      const raw = snapshot.queue;
      if (raw == null) { view.queue = null; view.invalid = false; view.lastRaw = null; }
      else if (raw !== view.lastRaw) {
        view.lastRaw = raw;
        if (!validQueue(raw)) view.invalid = true;
        else if (!view.queue || raw.token !== view.queue.token || raw.revision >= view.queue.revision) { view.queue = raw; view.invalid = false; }
        else view.invalid = true;
      }
      view.snapshotRevision = snapshot.revision;
      if (view.store.unknown && !view.store.busy && ui.connected && (view.store.unknownRevision == null || snapshot.revision > view.store.unknownRevision)) view.store.reviewable = true;
    }
    if (present) draw();
  }
  function draw() {
    if (!view) return;
    const {root, panel, store, queue, ui} = view;
    const enqueueButton = $("[data-queue-next]", root);
    enqueueButton.hidden = !["running", "permission", "input"].includes(ui?.status);
    enqueueButton.disabled = !canEnqueue();
    enqueueButton.textContent = store.busy?.action === "queue-enqueue" ? "Queuing…" : "Queue next";
    enqueueButton.title = canEnqueue() ? "Queue this draft after the current work; it is not sent immediately" : "Queue next needs a connected running turn accepting follow-ups; approval and question takeovers pause queuing";
    const items = queue?.items || [], ids = new Set(items.map(item => item.id));
    panel.hidden = !items.length && !store.editors.size && !store.unknown && !store.error && !store.copiedDraft && !view.invalid;
    $("[data-queue-count]", panel).textContent = `${items.length} / ${maxItems}`;
    const error = $("[data-queue-error]", panel);
    error.hidden = !store.error && !view.invalid;
    error.textContent = view.invalid ? "Could not verify the pending queue. Controls are disabled; nothing will be retried." : store.error || "";
    $("[data-queue-unknown]", panel).hidden = !store.unknown;
    $("[data-queue-reviewed]", panel).disabled = !store.unknown || !store.reviewable || !ui?.connected || !!store.busy;
    $("[data-queue-copy-notice]", panel).hidden = !store.copiedDraft;
    if (!enqueueButton.hidden) {
      $("#composer-hint", root).textContent = canEnqueue() ? "Ctrl / ⌘ + Enter to queue next · Enter for a new line" : "Queue next is paused · your draft is kept";
      $("#composer-state", root).textContent = store.unknown ? "Review the queue request outcome; nothing will retry" : store.busy ? "Updating pending messages · Stop remains available" : items.some(item => item.state === "pending") ? "Pending messages are separate from your draft" : "Turn in progress · Queue next is an explicit action";
    } else {
      $("#composer-hint", root).textContent = "Ctrl / ⌘ + Enter to send · Enter for a new line";
      if (retainedReview()) $("#composer-state", root).textContent = "Remove reviewed pending items before starting or switching conversations";
    }
    const list = $("[data-queue-items]", panel), rows = new Map([...list.children].map(row => [row.dataset.queueItemId, row]));
    const desired = [...items];
    for (const [id, editor] of store.editors) if (!ids.has(id)) desired.push({id, text: editor.text, state: "missing"});
    let position = 0;
    for (const item of desired) {
      let row = rows.get(item.id);
      if (!row) { row = createRow(item.id); list.append(row); }
      updateRow(row, item);
      const at = list.children[position++];
      if (at !== row) {
        if (list.moveBefore && row.isConnected) list.moveBefore(row, at || null); else list.insertBefore(row, at || null);
      }
      rows.delete(item.id);
    }
    const focused = document.activeElement;
    for (const row of rows.values()) row.remove();
    if (focused && !focused.isConnected && document.activeElement === document.body) $("#live-prompt", root)?.focus({preventScroll: true});
  }
  function createRow(id) {
    const row = node("article", "queue-next-item"); row.dataset.queueItemId = id;
    const heading = node("div", "queue-next-item-heading"), status = node("span", "queue-next-item-state");
    status.dataset.queueItemState = ""; heading.append(status);
    const source = node("pre", "queue-next-source"); source.dataset.queueSource = "";
    const actions = node("div", "queue-next-actions");
    const copy = button("Copy to draft", "data-queue-copy-draft"); copy.dataset.queueCopy = "";
    actions.append(button("Edit", "data-queue-edit"), button("Remove", "data-queue-remove"), copy);
    const form = node("form", "queue-next-editor"); form.dataset.queueEditor = ""; form.noValidate = true; form.hidden = true;
    const input = node("textarea"); input.dataset.queueText = ""; input.maxLength = 65536; input.rows = 3; input.setAttribute("aria-label", "Edit pending message");
    const formActions = node("div", "queue-next-actions"), save = button("Save", "data-queue-save"); save.type = "submit";
    formActions.append(save, button("Cancel", "data-queue-cancel"));
    const error = node("p", "error"); error.dataset.queueEditorError = ""; error.setAttribute("role", "alert"); error.hidden = true;
    form.append(input, formActions, error); row.append(heading, source, actions, form); return row;
  }
  function updateRow(row, item) {
    const editor = view.store.editors.get(item.id), editable = item.state === "pending" && canChange() && view.ui.status === "running" && (!editor || editor.token === view.queue.token);
    const review = ["held", "uncertain"].includes(item.state);
    row.dataset.queueState = item.state;
    $("[data-queue-item-state]", row).textContent = ({pending: "Pending · after current work", starting: "Delivery starting · locked", held: "Held for review · not scheduled", uncertain: "Delivery unknown · may already have been delivered", missing: "No longer pending · review the conversation; your edit is kept"})[item.state];
    const source = $("[data-queue-source]", row); if (source.textContent !== item.text) source.textContent = item.text;
    $("[data-queue-edit]", row).hidden = !!editor || item.state !== "pending";
    $("[data-queue-edit]", row).disabled = !editable;
    $("[data-queue-remove]", row).hidden = item.state !== "pending" && !review;
    $("[data-queue-remove]", row).disabled = !!editor || !(editable || review && canChange());
    $("[data-queue-remove]", row).title = review ? "Remove from this review list only; this does not undo or revoke delivery" : "Remove this pending message before delivery starts";
    $("[data-queue-copy-draft]", row).hidden = !["held", "uncertain", "missing"].includes(item.state);
    $("[data-queue-copy-draft]", row).disabled = !!view.store.busy || !!view.ui?.editing;
    const form = $("[data-queue-editor]", row); form.hidden = !editor;
    if (editor) {
      const input = $("[data-queue-text]", form);
      if (!editor.composing && input.value !== editor.text) input.value = editor.text;
      $("[data-queue-save]", form).disabled = !editable || !!editor.composing;
      $("[data-queue-cancel]", form).disabled = view.store.busy?.id === item.id;
      const error = $("[data-queue-editor-error]", form); error.hidden = !editor.error; error.textContent = editor.error || "";
    }
  }
  function sameDraft(left, right) {
    return left.value === right.value && left.revision === right.revision && left.start === right.start && left.end === right.end && left.direction === right.direction;
  }
  async function mutate(action, id, text) {
    if (!canChange() || action === "queue-enqueue" && !canEnqueue()) return false;
    const current = view, {api, store, queue} = current;
    const item = queue.items.find(item => item.id === id);
    if (action !== "queue-enqueue" && (!item || !(item.state === "pending" && current.ui.status === "running" || action === "queue-remove" && ["held", "uncertain"].includes(item.state)))) return false;
    if (action !== "queue-remove" && !api.validText(text)) {
      store.error = "Enter a nonblank message of at most 64 KiB, without null or malformed Unicode characters."; api.changed(); return false;
    }
    const editor = store.editors.get(id), editorRevision = editor?.revision;
    if (action === "queue-remove" && editor || action === "queue-update" && editor?.composing) return false;
    const operation = {action, id, focus: document.activeElement, revision: action === "queue-update" ? editor?.baseRevision : queue.revision, token: action === "queue-update" ? editor?.token : queue.token};
    if (!Number.isSafeInteger(operation.revision) || operation.token !== queue.token) return false;
    store.busy = operation; store.error = ""; store.reviewable = false; api.changed();
    try {
      const result = await api.request(action, {session_id: api.session, queue_token: operation.token, queue_revision: String(operation.revision), ...(id ? {item_id: id} : {}), ...(action !== "queue-remove" ? {text} : {})});
      if (view !== current || store.busy !== operation) return false;
      if (!validQueue(result.queue) || result.queue.token !== operation.token || current.queue?.token !== operation.token || result.queue.revision <= operation.revision) throw new Error("Unverified queue acknowledgement");
      if (action === "queue-update" && editor) {
        const editingNow = document.activeElement?.matches("[data-queue-text]") && document.activeElement.closest("[data-queue-item-id]")?.dataset.queueItemId === id;
        if (!editingNow && !editor.composing && editor.revision === editorRevision && editor.text === text) store.editors.delete(id);
        else editor.baseRevision = result.queue.revision;
      }
      if (action === "queue-remove") store.editors.delete(id);
      operation.verified = true; return true;
    } catch (error) {
      if (view !== current || store.busy !== operation) return false;
      // A transport failure or ambiguous worker error is never proof that the
      // queue did not change. Preserve drafts and require a fresh human review.
      store.unknown = true; store.unknownRevision = current.snapshotRevision;
      store.error = "Queue request outcome needs review. Your text is kept. Nothing will be retried automatically.";
      if (editor) editor.error = "Your edit is kept. Review the current queue, then explicitly Save again if appropriate.";
      return false;
    } finally {
      if (view === current && store.busy === operation) {
        store.busy = false; api.changed();
        if (operation.verified && operation.focus && document.activeElement === document.body) {
          const target = operation.focus.isConnected && operation.focus.getClientRects().length && !operation.focus.disabled ? operation.focus : $("#live-prompt", current.root);
          if (target?.getClientRects().length && !target.closest("[inert]")) target.focus({preventScroll: true});
        }
        // Re-read only. This never retries enqueue/update/remove.
        if (store.unknown) {
          try { await api.request(); if (view === current && current.ui?.connected && !current.invalid) store.reviewable = true; } catch (_) { /* Connection guards remain authoritative. */ }
          if (view === current) api.changed();
        }
      }
    }
  }
  async function enqueue() {
    if (!canEnqueue()) return;
    const current = view, draft = current.api.draft(), composition = current.composeRevision;
    if (await mutate("queue-enqueue", "", draft.value)) {
      if (view !== current) return;
      if (!current.composing && current.composeRevision === composition && sameDraft(draft, current.api.draft())) current.api.writeDraft("");
      current.api.changed();
    }
  }
  function onClick(event) {
    const target = event.target.closest("button"); if (!target || !view) return;
    const {store, api} = view;
    if (target.matches("[data-queue-next]")) { enqueue(); return; }
    if (target.matches("[data-queue-reviewed]")) {
      if (store.unknown && store.reviewable && view.ui.connected && !store.busy) { store.unknown = false; store.error = "";
        for (const editor of store.editors.values()) if (editor.token === view.queue?.token) editor.baseRevision = view.queue.revision;
        api.changed(); } return;
    }
    if (target.matches("[data-queue-restore-draft]")) {
      if (store.copiedDraft && !store.busy && !view.ui.editing) { api.writeDraft(store.copiedDraft.value, store.copiedDraft); store.copiedDraft = null; api.changed(); } return;
    }
    const row = target.closest("[data-queue-item-id]"); if (!row) return;
    const id = row.dataset.queueItemId, item = view.queue?.items.find(item => item.id === id);
    if (target.matches("[data-queue-edit]") && canChange() && item?.state === "pending" && view.ui.status === "running") {
      store.editors.set(id, {text: item.text, revision: 0, baseRevision: view.queue.revision, token: view.queue.token}); draw(); $("[data-queue-text]", row).focus();
    } else if (target.matches("[data-queue-cancel]") && store.busy?.id !== id) { store.editors.delete(id); draw(); $("[data-queue-edit]", row)?.focus(); }
    else if (target.matches("[data-queue-remove]")) mutate("queue-remove", id);
    else if (target.matches("[data-queue-copy-draft]") && !store.busy && !view.ui.editing && ["held", "uncertain", "missing"].includes(row.dataset.queueState)) {
      const text = store.editors.get(id)?.text ?? item?.text;
      if (typeof text !== "string") return;
      store.copiedDraft ||= api.draft(); api.writeDraft(text); api.changed();
    }
  }
  function onSubmit(event) {
    if (!event.target.matches("[data-queue-editor]")) return;
    event.preventDefault(); event.stopPropagation();
    const id = event.target.closest("[data-queue-item-id]").dataset.queueItemId, editor = view?.store.editors.get(id);
    if (editor) mutate("queue-update", id, editor.text);
  }
  window.SnowQueue = Object.freeze({init, dispose, render, blocking, canEnqueue, enqueue});
})();
