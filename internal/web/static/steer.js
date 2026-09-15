/* Explicit native current-run control. No prompt/queue enqueue, transcript
 * mutation, persistence, retry, or reconnect replay lives in this module. */
(() => {
  "use strict";
  const stores = new Map(), maxStores = 16, encoder = new TextEncoder();
  const statuses = new Set(["accepted", "delivered", "discarded", "uncertain"]);
  const labels = {accepted: "Accepted · awaiting native delivery", delivered: "Delivered · confirmed by native run", discarded: "Discarded · confirmed by native run", uncertain: "Outcome uncertain · may still be delivered; review before submitting again"};
  const $ = (s, root) => root?.querySelector(s);
  let view;
  function validID(id) { return typeof id === "string" && id.length > 0 && id.length <= 256 && !/[\x00\r\n\t]/.test(id); }
  function validProjection(raw, api) {
    if (!raw || typeof raw.live_steer_token !== "string" || raw.live_steer_token.length > 256 || !Number.isSafeInteger(raw.revision) || raw.revision < 0 || typeof raw.can_steer !== "boolean" || !Array.isArray(raw.items) || raw.items.length > 8) return false;
    if (raw.can_steer && (!validID(raw.live_steer_token) || raw.revision <= 0)) return false;
    let bytes = 0; const requests = new Set(), items = new Set();
    for (const item of raw.items) {
      if (!item || !validID(item.request_id) || requests.has(item.request_id) || !statuses.has(item.status) || !api.validText(item.text) || item.item_id && (!validID(item.item_id) || items.has(item.item_id))) return false;
      if (item.status !== "uncertain" && !validID(item.item_id)) return false;
      requests.add(item.request_id); if (item.item_id) items.add(item.item_id); bytes += encoder.encode(item.text).length;
    }
    return bytes <= 256 * 1024;
  }
  function init(api) {
    dispose();
    const root = api.root, dialog = $("#live-steer-dialog", root), history = $("#live-steer-history", root);
    if (!dialog || !history) return;
    const key = JSON.stringify([root.dataset.project, root.dataset.session, root.dataset.instance]);
    const store = stores.get(key) || {text: "", revision: 0};
    stores.delete(key); stores.set(key, store);
    while (stores.size > maxStores) stores.delete(stores.keys().next().value);
    const controller = new AbortController();
    const current = view = {api, root, dialog, history, key, store, controller, snapshotRevision: -1, projection: null, present: false};
    $("[data-steer-text]", dialog).value = store.text;
    root.addEventListener("click", onClick, {signal: controller.signal});
    dialog.addEventListener("submit", event => { if (event.target.matches("[data-steer-form]")) { event.preventDefault(); event.stopPropagation(); submit(); } }, {signal: controller.signal});
    const input = $("[data-steer-text]", dialog);
    input.addEventListener("input", () => {
      if (encoder.encode(input.value).length > 64 * 1024) { input.value = store.text; store.error = "Steering is limited to 64 KiB; your previous draft is kept."; }
      else { store.text = input.value; store.revision++; }
      draw();
    }, {signal: controller.signal});
    for (const type of ["compositionstart", "compositionend"]) input.addEventListener(type, () => { current.composing = type === "compositionstart"; draw(); }, {signal: controller.signal});
    input.addEventListener("keydown", event => { if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.isComposing && !current.composing) { event.preventDefault(); submit(); } }, {signal: controller.signal});
    dialog.addEventListener("close", () => { if (view === current) { api.changed(); if (current.returnFocus?.isConnected && !current.returnFocus.disabled) current.returnFocus.focus({preventScroll: true}); } }, {signal: controller.signal});
  }
  function dispose() {
    if (!view) return;
    const current = view; view = null;
    current.controller.abort();
    if (current.store.busy) { current.store.busy = null; current.store.unknown = true; current.store.error = "The steering result is unknown. Your draft is kept; nothing is retried automatically."; }
    if (current.dialog.open) current.dialog.close();
  }
  function blocking() { return !!view && !!(view.dialog.open || view.store.busy || view.store.unknown); }
  function authority() {
    const ui = view?.ui;
    return !!ui && view.present && ui.connected && !ui.busy && !ui.unknown && !ui.stopping && !ui.editing && ui.status === "running" && !view.invalid && view.projection?.can_steer === true && !view.store.busy;
  }
  function canSteer() { return authority() && !view.store.unknown; }
  function canDismissUnknown() {
    const ui = view?.ui;
    // This releases only local draft ownership. It neither grants mutation
    // authority nor clears the app's separate request-uncertainty guard.
    return !!ui && view.present && ui.connected && !ui.busy && !ui.stopping && !ui.editing && ui.status === "idle" && !view.invalid && !!view.store.unknown && !view.store.busy;
  }
  function bindReviewed() {
    if (!authority()) return false;
    view.store.token = view.projection.live_steer_token; view.store.baseRevision = view.projection.revision;
    return true;
  }
  function open(trigger) {
    if (!canSteer() && !(view?.store.unknown && (authority() || canDismissUnknown()))) return false;
    if (!view.store.unknown && !bindReviewed()) return false;
    view.returnFocus = trigger?.parentElement?.classList.contains("manager-menu-enabled") ? document.querySelector("[data-session-menu]") : trigger || document.activeElement;
    if (!view.store.unknown) view.store.error = "";
    if (!view.dialog.open) view.dialog.showModal();
    $("[data-steer-text]", view.dialog).focus();
    view.api.changed(); draw(); return true;
  }
  function render(snapshot, ui, present = true) {
    if (!view) return;
    view.ui = ui; view.present = present;
    if (snapshot && Number.isSafeInteger(snapshot.revision) && snapshot.revision >= view.snapshotRevision) {
      view.snapshotRevision = snapshot.revision;
      view.invalid = snapshot.steer != null && !validProjection(snapshot.steer, view.api);
      view.projection = !view.invalid ? snapshot.steer : null;
    }
    draw();
  }
  function draw() {
    if (!view) return;
    const {root, dialog, history, store, projection} = view;
    const trigger = $("[data-steer-open]", root);
    if (trigger) { trigger.hidden = (!view.present || !projection && !view.invalid) && !store.unknown; trigger.disabled = !(canSteer() || store.unknown && (authority() || canDismissUnknown())); trigger.title = "Direct the current run at its next safe native boundary. Queue next is a separate follow-up."; }
    const stale = projection && (!store.token || store.token !== projection.live_steer_token || store.baseRevision !== projection.revision);
    const submitButton = $("[data-steer-submit]", dialog);
    submitButton.disabled = !canSteer() || !!stale || view.composing || !view.api.validText(store.text);
    submitButton.textContent = store.busy ? "Sending steering…" : "Send steering";
    $("[data-steer-text]", dialog).readOnly = !!store.busy;
    const error = $("[data-steer-error]", dialog);
    error.textContent = store.error || (stale ? "The reviewed run changed. Your draft is kept. Review the current run again before sending." : !authority() && dialog.open ? "Steering is unavailable while the run is stopped, disconnected, needs attention, or another control owns it. Your draft is kept." : "");
    error.hidden = !error.textContent;
    const review = $("[data-steer-review]", dialog); review.hidden = !(store.unknown || stale || store.rejected); review.disabled = !authority() && !canDismissUnknown();
    review.textContent = canDismissUnknown() ? "Keep draft and dismiss" : "Review current run";
    const items = projection?.items || [];
    history.hidden = !items.length;
    const list = $("[data-steer-items]", history), existing = new Map([...list.children].map(row => [row.dataset.steerRequest, row]));
    for (const item of items) {
      let row = existing.get(item.request_id); existing.delete(item.request_id);
      if (!row) {
        row = document.createElement("li"); row.dataset.steerRequest = item.request_id;
        const state = document.createElement("p"); state.dataset.steerState = "";
        const text = document.createElement("pre"); text.dataset.steerSource = "";
        const copy = document.createElement("button"); copy.type = "button"; copy.className = "quiet"; copy.dataset.steerCopy = ""; copy.textContent = "Review text";
        row.append(state, text, copy); list.append(row);
      }
      $("[data-steer-state]", row).textContent = labels[item.status];
      $("[data-steer-source]", row).textContent = item.text;
      $("[data-steer-copy]", row).disabled = !!store.busy;
    }
    for (const row of existing.values()) row.remove();
  }
  async function submit() {
    if (!canSteer() || view.composing || !view.api.validText(view.store.text)) return false;
    const current = view, {store, api, projection} = current;
    if (store.token !== projection.live_steer_token || store.baseRevision !== projection.revision) return false;
    const operation = {token: store.token, revision: store.baseRevision, request: crypto.randomUUID(), text: store.text, draftRevision: store.revision, project: current.root.dataset.project, instance: current.root.dataset.instance, session: api.session};
    store.busy = operation; store.error = ""; store.rejected = false; api.changed(); draw();
    try {
      const result = await api.request("steer", {session_id: api.session, live_steer_token: operation.token, steer_revision: String(operation.revision), request_id: operation.request, text: operation.text});
      if (view !== current || store.busy !== operation) return false;
      const ack = result?.steer_ack;
      if (!ack || ack.live_steer_token !== operation.token || ack.request_id !== operation.request || !validID(ack.item_id) || ack.status !== "accepted" || !validProjection(result.steer, api) || result.project_id !== operation.project || result.instance_id !== operation.instance || result.session_id !== operation.session || current.root.dataset.project !== operation.project || current.root.dataset.instance !== operation.instance || current.root.dataset.session !== operation.session) throw new Error("Unverified steering receipt");
      // Acceptance alone does not claim delivery. SSE/native item states win;
      // an older HTTP snapshot must never repaint delivered as accepted. The
      // captured token binds the receipt, not current admission: completion may
      // retire that token before this waiter resumes. Never apply its snapshot
      // or grant authority to a newer root based on this acceptance receipt.
      store.error = "Accepted by the native run. Acceptance is not delivery; see the steering history for its confirmed outcome.";
      if (store.revision === operation.draftRevision && store.text === operation.text) { store.text = ""; store.revision++; $("[data-steer-text]", current.dialog).value = ""; }
      store.unknown = false;
      return true;
    } catch (_) {
      if (view !== current || store.busy !== operation) return false;
      store.unknown = true;
      store.error = "Steering was rejected or its outcome is unknown. Your draft is kept. Review the current run and native history before any new explicit submission; nothing is retried automatically.";
      return false;
    } finally {
      if (view === current && store.busy === operation) { store.busy = null; api.changed(); draw(); }
    }
  }
  function onClick(event) {
    const target = event.target.closest("[data-steer-open],[data-steer-close],[data-steer-review],[data-steer-copy]");
    if (!target || !view?.root.contains(target)) return;
    event.preventDefault();
    if (target.matches("[data-steer-open]")) open(target);
    else if (target.matches("[data-steer-close]")) view.dialog.close();
    else if (target.matches("[data-steer-review]") && canDismissUnknown()) {
      view.store.unknown = false; view.store.rejected = false;
      view.store.token = ""; view.store.baseRevision = 0;
      view.store.error = "Draft kept. The earlier steering outcome is still uncertain unless native history confirms it. Dismissal does not mean delivery or discard; nothing is retried automatically.";
      if (view.dialog.open) view.dialog.close();
      view.api.changed(); draw();
    }
    else if (target.matches("[data-steer-review]") && bindReviewed()) { view.store.unknown = false; view.store.rejected = false; view.store.error = "Reviewed the current run. Sending again is a new explicit request, not a retry; an uncertain earlier request may already have been delivered."; view.api.changed(); draw(); }
    else if (target.matches("[data-steer-copy]") && !view.store.busy) {
      const item = view.projection?.items.find(item => item.request_id === target.closest("[data-steer-request]").dataset.steerRequest);
      if (!item) return;
      // Never silently replace either an existing steering draft or composer.
      if (view.store.text && view.store.text !== item.text) { view.store.error = "Your steering draft is kept. Copy the history text manually to avoid replacing it."; }
      else { view.store.text = item.text; view.store.revision++; $("[data-steer-text]", view.dialog).value = item.text; }
      view.returnFocus = target;
      if (!view.dialog.open) view.dialog.showModal();
      view.api.changed(); draw();
    }
  }
  window.SnowSteer = Object.freeze({init, dispose, render, blocking, canSteer, open});
})();
