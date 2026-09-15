/* Ordinary saved-history controls. No prompt/tool replay, filesystem undo,
 * generic RPC commands, automatic mutation retry or detached-child activation. */
(() => {
  "use strict";
  const $ = (selector, scope) => scope?.querySelector(selector), drafts = new Map();
  const actions = new Set(["history-branch-fork", "history-session-fork", "history-branch-rename"]);
  const id = value => typeof value === "string" && value.length > 0 && value.length <= 256 && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
  let view;
  function validName(value, action) {
    return typeof value === "string" && !!value && value === value.trim() && new TextEncoder().encode(value).length <= 256 && [...value].length <= (action === "history-session-fork" ? 72 : 64) && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
  }
  function init(api) {
    dispose(); const panel = $("[data-history-controls]", api.root), dialog = $("#versions-dialog", api.root);
    if (!panel || !dialog || api.root.dataset.historyControlEnabled !== "true") return;
    const key = JSON.stringify([api.identity.project_id, api.identity.session_id]), draft = drafts.get(key) || {name: ""};
    drafts.delete(key); drafts.set(key, draft); while (drafts.size > 16) drafts.delete(drafts.keys().next().value);
    const controller = new AbortController(); view = {api, panel, dialog, draft, controller};
    $("[data-history-name]", panel).value = draft.name;
    $("[data-history-consent]", panel).checked = false;
    $("[data-history-confirmation]", panel).hidden = true;
    panel.addEventListener("click", click, {signal: controller.signal});
    panel.addEventListener("submit", event => { event.preventDefault(); commit(); }, {signal: controller.signal});
    panel.addEventListener("input", event => {
      if (event.target.matches("[data-history-name]")) { draft.name = event.target.value; event.target.setCustomValidity(""); }
      controls();
    }, {signal: controller.signal});
    panel.addEventListener("change", controls, {signal: controller.signal});
    for (const type of ["compositionstart", "compositionend"]) panel.addEventListener(type, () => { if (view) { view.composing = type === "compositionstart"; controls(); } }, {signal: controller.signal});
    dialog.addEventListener("close", () => { if (view?.dialog === dialog && !view.busy) cancel(); }, {signal: controller.signal});
    controls();
  }
  function dispose() {
    if (!view) return; const old = view; view = null; old.controller.abort();
    // Never abort an admitted mutation; the app owns its uncertain-outcome fence.
  }
  function selection() {
    const selected = window.SnowVersions?.selection(), identity = view?.api.identity;
    return selected && identity && ["project_id", "instance_id", "session_id"].every(key => selected[key] === identity[key]) && Number.isSafeInteger(selected.revision) && selected.revision > 0 && selected.revision === view.snapshot?.revision ? selected : null;
  }
  function sameSelection(a, b) { return !!a && !!b && ["project_id", "instance_id", "session_id", "revision", "current_branch_id", "current_tip_id", "branch_id", "tip_id", "name"].every(key => a[key] === b[key]); }
  function safe() { return !!view?.ui?.safe && !view.busy && !view.invalid && !view.composing; }
  function render(snapshot, ui) { if (view) { view.snapshot = snapshot; view.ui = ui; controls(); } }
  function selectionChanged() { controls(); }
  function controls() {
    if (!view) return;
    const {panel, confirmation} = view, selected = selection(), ready = safe() && !!selected;
    panel.hidden = view.ui?.supported !== true;
    $("[data-history-name]", panel).disabled = !!confirmation || !!view.busy;
    for (const button of panel.querySelectorAll("[data-history-review]")) button.disabled = !ready || !!confirmation;
    $("[data-history-confirmation]", panel).hidden = !confirmation;
    $("[data-history-confirm]", panel).disabled = !ready || !confirmation || !sameSelection(confirmation.target, selected) || !$("[data-history-consent]", panel).checked;
    $("[data-history-cancel]", panel).disabled = !!view.busy;
    $("[data-history-notice]", panel).textContent = view.error || (view.busy ? "Applying the explicitly confirmed history action… Do not retry it." : !selected ? "Preview a saved version at the current revision before choosing an action. Refresh Versions if this selection has changed." : !view.ui?.safe ? "History changes require an idle, connected conversation with no pending goal, unfinished turn, queued/review work or conflicting action." : "These actions change saved conversation history only. Your unsent message draft is kept.");
  }
  function prepare(action) {
    if (!actions.has(action) || !safe() || view.confirmation) return;
    const target = selection(); if (!target) return;
    const input = $("[data-history-name]", view.panel), name = input.value;
    if (!validName(name, action)) { input.setCustomValidity(`Use a nonempty name with no surrounding spaces or control characters, at most ${action === "history-session-fork" ? 72 : 64} characters and 256 UTF-8 bytes.`); input.reportValidity(); return; }
    if (action === "history-branch-rename" && !validName(target.name, action)) { view.error = "The saved label cannot authorize this rename. Refresh Versions."; controls(); return; }
    view.error = ""; view.confirmation = {action, target: Object.freeze({...target}), name};
    $("[data-history-consent]", view.panel).checked = false;
    const description = action === "history-branch-fork" ? "Create and activate a new branch in this conversation. It inherits the selected history's mode and effective thinking; current provider, model and permissions stay unchanged." : action === "history-session-fork" ? "Create a detached saved conversation in this workspace. The current conversation and branch stay selected. The child appears in inventory and is opened only by your separate explicit Open action." : `Rename only the selected branch label from “${target.name}” to “${name}”. The current history and branch selection do not change.`;
    $("[data-history-target]", view.panel).textContent = `Selected branch ${target.branch_id}; exact saved tip ${target.tip_id || "(empty history)"}. Name: ${name}. ${description}`;
    $("[data-history-confirm]", view.panel).textContent = action === "history-branch-fork" ? "Create and activate branch" : action === "history-session-fork" ? "Create detached conversation" : "Rename selected branch";
    view.api.reserve(true); controls(); $("[data-history-consent]", view.panel).focus({preventScroll: true});
  }
  function cancel() {
    if (!view || view.busy) return;
    view.confirmation = null; $("[data-history-consent]", view.panel).checked = false; view.api.reserve(false); controls();
  }
  async function commit() {
    if (!safe() || !view.confirmation || !view.dialog.open || !$("[data-history-consent]", view.panel).checked || !sameSelection(view.confirmation.target, selection())) return;
    const current = view, {action, target, name} = current.confirmation;
    const fields = {session_id: target.session_id, expected_revision: String(target.revision), current_branch_id: target.current_branch_id, current_tip_id: target.current_tip_id, branch_id: target.branch_id, tip_id: target.tip_id, name};
    if (action === "history-branch-rename") fields.old_name = target.name;
    current.confirmation = null; current.busy = true; controls();
    let result;
    try { result = await current.api.mutate(action, fields); } catch { result = null; }
    if (view !== current) return;
    current.busy = false; current.api.reserve(false);
    // A consumed confirmation cannot be resubmitted after any outcome. A read
    // reload never automatically reauthorizes this mutation.
    current.invalid = true;
    if (!result) { current.error = "History action outcome needs review. Your drafts are kept. Nothing will be retried automatically; reload the workspace before continuing."; controls(); return; }
    if (action !== "history-branch-fork" && (!sameMetadata(result, target, name) || action === "history-session-fork" && (!id(result.child_session_id) || result.child_session_id === target.session_id))) {
      current.error = "Unverified history response. Reload the workspace; do not retry this mutation."; controls(); return;
    }
    current.invalid = false;
    current.error = action === "history-session-fork" ? "Detached conversation created. Refreshing saved-conversation inventory; choose Open explicitly to use it. Current history is unchanged." : action === "history-branch-rename" ? "Selected branch renamed. Refresh Versions before reviewing another action." : "New branch activated. No prompt or tools were replayed.";
    if (action === "history-session-fork") current.api.refreshInventory?.(result.child_session_id);
    if (action === "history-branch-rename") window.SnowVersions?.refresh();
    controls();
  }
  function sameMetadata(result, target, name) {
    return ["project_id", "instance_id", "session_id"].every(key => result[key] === target[key]) && result.branch_id === target.branch_id && result.tip_id === target.tip_id && result.name === name && Number.isSafeInteger(result.revision) && result.revision >= target.revision;
  }
  function click(event) {
    const button = event.target.closest("button"); if (!view || !button) return;
    if (button.matches("[data-history-review]")) prepare(button.dataset.historyReview);
    else if (button.matches("[data-history-cancel]")) cancel();
  }
  window.SnowHistoryControls = Object.freeze({init, render, dispose, selectionChanged, validName});
})();
