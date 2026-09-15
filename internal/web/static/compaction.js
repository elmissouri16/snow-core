/* Manual provider-context work, never a prompt or command tunnel. Lost HTTP
 * receipts are reconciled by shared read-only snapshots, never by replay. */
(() => {
  "use strict";
  const $ = (selector, root) => root?.querySelector(selector);
  const states = new Set(["pending", "running", "completed", "noop", "fallback", "canceled", "failed", "uncertain"]);
  const id = (value, empty = false) => typeof value === "string" && (empty || !!value) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
  const count = value => Number.isSafeInteger(value) && value >= 0 && value <= 1073741824;
  const positive = value => Number.isSafeInteger(value) && value > 0;
  let view;
  function validCompaction(c, session) {
    if (!c || !states.has(c.state) || c.session_id !== session || !id(c.branch_id) || !id(c.expected_tip_id, true) || !count(c.summarized_messages) || !count(c.retained_messages) || typeof c.progress_done !== "boolean" || typeof c.used_fallback !== "boolean") return false;
    const captured = id(c.request_id) && id(c.compaction_id) && c.turn_id === c.compaction_id && c.turn_origin === "compact" && positive(c.root_epoch) && positive(c.turn_sequence);
    return captured || ["pending", "failed", "uncertain"].includes(c.state) && c.request_id === "" && c.compaction_id === "" && c.turn_id === "" && c.turn_origin === "" && c.root_epoch === 0 && c.turn_sequence === 0;
  }
  function validACK(result, identity, fields) {
    const a = result?.compaction_ack;
    return result?.project_id === identity.project_id && result?.instance_id === identity.instance_id && result?.session_id === identity.session_id && positive(result.revision) && result.revision >= Number(fields.expected_revision) && !!a && id(a.compaction_id) && a.turn_id === a.compaction_id && a.turn_origin === "compact" && positive(a.root_epoch) && positive(a.turn_sequence) && a.session_id === fields.session_id && a.branch_id === fields.branch_id;
  }
  function init(api) {
    dispose();
    const dialog = $("#compaction-dialog", api.root);
    if (!dialog) return;
    const controller = new AbortController();
    view = {api, dialog, controller, snapshot: null, reviewed: null};
    api.root.addEventListener("click", onClick, {signal: controller.signal});
    dialog.addEventListener("change", controls, {signal: controller.signal});
    dialog.addEventListener("cancel", event => {event.preventDefault(); close();}, {signal: controller.signal});
    dialog.addEventListener("close", () => {if (view?.dialog === dialog && !view.committing) release();}, {signal: controller.signal});
  }
  function release() {
    if (!view) return;
    view.reviewed = null;
    $("[data-compaction-consent]", view.dialog).checked = false;
    view.api.reserve(false);
  }
  function close() {
    if (!view) return;
    view.api.closeDialog(view.dialog);
    if (!view.committing) release();
  }
  function dispose() {
    if (!view) return;
    const old = view; view = null;
    old.controller.abort(); old.api.closeDialog(old.dialog); old.api.reserve(false);
  }
  function scope(snapshot) {
    const g = snapshot?.goal;
    if (!g || g.session_id !== view.api.identity.session_id || !id(g.branch_id) || !id(g.tip_id, true) || !positive(snapshot.revision)) return null;
    return {session_id: g.session_id, branch_id: g.branch_id, expected_tip_id: g.tip_id, expected_revision: String(snapshot.revision)};
  }
  function idleSafe() {
    const s = view?.snapshot, g = s?.goal;
    return !!view?.ui?.safe && !view.invalid && s?.status === "idle" && !s.cancel_requested && !s.permission && !s.input && !s.queue?.items?.length && !!g && !g.running && (!g.goal_id || ["complete", "budget_limited"].includes(g.status)) && !["pending", "running", "uncertain"].includes(s.compaction?.state);
  }
  function unchanged() {
    return !!view?.reviewed && JSON.stringify(view.reviewed) === JSON.stringify(scope(view.snapshot));
  }
  function review() {
    if (!view || view.committing || !idleSafe()) return;
    view.reviewed = scope(view.snapshot); view.notice = "";
    $("[data-compaction-consent]", view.dialog).checked = false;
    controls();
  }
  function render(snapshot, ui) {
    if (!view) return;
    view.snapshot = snapshot; view.ui = ui;
    view.invalid = snapshot?.compaction != null && !validCompaction(snapshot.compaction, view.api.identity.session_id);
    controls();
  }
  function description(c) {
    if (!c) return "No manual compaction has been requested in this live conversation.";
    const counts = `${c.summarized_messages.toLocaleString()} messages summarized · ${c.retained_messages.toLocaleString()} retained`;
    switch (c.state) {
      case "pending": return "Request reserved. Waiting for the native operation receipt. Stop cancels this operation.";
      case "running": return c.progress_done ? "Compaction progress received; waiting for native cleanup and authoritative context refresh. Stop remains available." : "Manual compaction is running. It may use provider tokens. Stop cancels this operation.";
      case "completed": return `Manual compaction completed · ${counts}. Context and usage refreshed.`;
      case "noop": return "No compaction was needed. Context and usage refreshed; no message was sent.";
      case "fallback": return `Compaction used a fallback checkpoint · ${counts}. Context and usage refreshed; this was not a full provider summary.`;
      case "canceled": return "Manual compaction canceled. Context and usage refreshed; provider usage or a checkpoint may already have been saved.";
      case "failed": return "Manual compaction failed or was rejected. Review the current conversation; nothing will be retried automatically.";
      case "uncertain": return "The worker disconnected before the result could be verified. Close and explicitly reopen this project to inspect saved context. Nothing will be retried.";
      default: return "Manual compaction state could not be verified.";
    }
  }
  function controls() {
    if (!view) return;
    const {api, dialog, snapshot: s, ui} = view;
    for (const button of api.root.querySelectorAll("[data-compaction-open]")) {button.hidden = !ui?.supported; button.disabled = !ui?.readable || view.invalid;}
    const c = view.invalid ? null : s?.compaction;
    const notice = view.invalid ? "Could not verify manual compaction state. Controls are disabled." : view.notice ? `${view.notice} ${description(c)}` : description(c);
    $("[data-compaction-status]", dialog).textContent = notice;
    const inline = $("[data-compaction-inline]", api.root);
    if (inline) {inline.hidden = !c; inline.textContent = description(c);}
    $("[data-compaction-scope]", dialog).textContent = view.reviewed ? `Session: ${view.reviewed.session_id}\nBranch: ${view.reviewed.branch_id}\nTip: ${view.reviewed.expected_tip_id || "(empty history)"}${unchanged() ? "" : "\nChanged: review the current context before continuing."}` : "Review the idle current context before authorizing provider work.";
    $("[data-compaction-model]", dialog).textContent = s ? `${s.provider || "Unknown"} / ${s.model || "Unknown"} · ${s.mode || "unknown mode"}` : "Unknown";
    $("[data-compaction-review]", dialog).disabled = !idleSafe() || !!view.committing;
    $("[data-compaction-consent]", dialog).disabled = !idleSafe() || !unchanged() || !!view.committing;
    $("[data-compaction-confirm]", dialog).disabled = !idleSafe() || !unchanged() || !!view.committing || !$("[data-compaction-consent]", dialog).checked;
    // Stop is owned exclusively by shared root cancellation. Never disable or
    // hide it here while the panel or HTTP admission is reserved.
  }
  async function commit() {
    if (!view || view.committing || !idleSafe() || !unchanged() || !$("[data-compaction-consent]", view.dialog).checked) return;
    const current = view, fields = {...current.reviewed};
    current.committing = true; current.api.reserve(true); controls();
    let result = false;
    try {result = await current.api.commit(fields);} catch { /* shared layer reconciles read-only */ }
    if (view !== current) return;
    current.committing = false; current.reviewed = null;
    $("[data-compaction-consent]", current.dialog).checked = false;
    current.api.reserve(false);
    // A fast terminal snapshot must never be downgraded to "running" by ACK.
    current.notice = result ? "" : "The request outcome needs review. Read the current status; nothing will be retried automatically.";
    controls();
  }
  function onClick(event) {
    const button = event.target.closest("button"); if (!view || !button) return;
    if (button.matches("[data-compaction-open]") && view.ui?.readable && !view.invalid) {view.api.openDialog(view.dialog, button); if (idleSafe()) {view.api.reserve(true); review();}}
    else if (button.matches("[data-compaction-close]")) close();
    else if (button.matches("[data-compaction-review]")) review();
    else if (button.matches("[data-compaction-confirm]")) commit();
  }
  window.SnowCompaction = Object.freeze({init, render, dispose, validCompaction, validACK});
})();
