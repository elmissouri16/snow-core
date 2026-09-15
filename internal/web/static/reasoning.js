/* Explicit, capability-derived response controls. No worker activation, model
 * name heuristics, generic RPC tunnel, automatic retry, or browser persistence. */
(() => {
  "use strict";
  const $ = (s, root) => root?.querySelector(s);
  const fields = {thinking: "Thinking", reasoning_summary: "Reasoning summary", text_verbosity: "Text verbosity"};
  const options = {thinking: "thinking_levels", reasoning_summary: "reasoning_summaries", text_verbosity: "text_verbosities"};
  const facts = ["project_id", "instance_id", "session_id", "branch_id", "tip_id", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity"];
  const text = value => typeof value === "string" && value.length > 0 && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
  let view;
  function valid(result, identity) {
    return !!result && ["project_id", "instance_id", "session_id"].every(k => result[k] === identity[k]) && facts.every(k => k === "tip_id" ? result[k] === "" || text(result[k]) : text(result[k])) && Number.isSafeInteger(result.revision) && result.revision > 0 && ["default", "plan"].includes(result.mode) && ["ask", "deny", "allow"].includes(result.permission_mode) && result.defaults_available === false && result.current_session_available === true && Object.values(options).every(k => result[k] == null || Array.isArray(result[k]) && result[k].length <= 16 && result[k].every(text) && new Set(result[k]).size === result[k].length);
  }
  function init(api) {
    dispose(); const dialog = $("#reasoning-dialog", api.root); if (!dialog) return;
    const controller = new AbortController(); view = {api, dialog, controller};
    resetPresentation(view);
    api.root.addEventListener("click", click, {signal: controller.signal});
    dialog.addEventListener("change", event => { if (event.target.matches("[data-reasoning-field]")) fillValues(); controls(); }, {signal: controller.signal});
    dialog.addEventListener("cancel", event => { event.preventDefault(); close(); }, {signal: controller.signal});
    dialog.addEventListener("close", () => { if (view?.dialog === dialog && !view.committing) cancel(); }, {signal: controller.signal});
    for (const name of ["compositionstart", "compositionend"]) dialog.addEventListener(name, () => { if (view?.dialog === dialog) { view.composing = name === "compositionstart"; controls(); } }, {signal: controller.signal});
    controls();
  }
  function dispose() {
    if (!view) return; const old = view; view = null;
    old.read?.abort(); old.controller.abort(); old.api.reserve(false); old.api.closeDialog(old.dialog); resetPresentation(old);
  }
  function resetPresentation(current) {
    const {dialog} = current;
    for (const name of ["identity", "authority", "mode", "thinking", "summary", "verbosity", "confirm-target"]) $("[data-reasoning-" + name + "]", dialog).textContent = "";
    for (const name of ["field", "value"]) { const select = $("[data-reasoning-" + name + "]", dialog); select.replaceChildren(); select.disabled = true; }
    for (const name of ["refresh", "review", "confirm"]) $("[data-reasoning-" + name + "]", dialog).disabled = true;
    $("[data-reasoning-open]", current.api.root).disabled = true;
    $("[data-reasoning-consent]", dialog).checked = false;
    $("[data-reasoning-confirmation]", dialog).hidden = true;
    $("[data-reasoning-notice]", dialog).textContent = "Refresh to inspect the current session.";
  }
  function sameScope() {
    const v = view, a = v?.inspected, s = v?.snapshot;
    return !!a && !!s && a.revision === s.revision && ["project_id", "instance_id", "session_id", "provider", "model", "mode", "permission_mode", "thinking"].every(k => a[k] === s[k]);
  }
  function ready() { return !!view?.ui?.safe && sameScope() && !view.read && !view.committing && !view.uncertain && !view.composing; }
  function option(select, value, label) { const o = document.createElement("option"); o.value = value; o.textContent = label; select.append(o); }
  function fillValues() {
    if (!view) return; const select = $("[data-reasoning-value]", view.dialog), field = $("[data-reasoning-field]", view.dialog).value;
    select.replaceChildren(); for (const value of view.inspected?.[options[field]] || []) option(select, value, value);
    if (view.inspected?.[field] && (view.inspected[options[field]] || []).includes(view.inspected[field])) select.value = view.inspected[field];
  }
  function show(result) {
    const dialog = view.dialog;
    for (const [name, value] of Object.entries({identity: `${result.project_id} / ${result.session_id}`, authority: `${result.provider} / ${result.model} · permissions: ${result.permission_mode}`, mode: result.mode, thinking: result.thinking, summary: result.reasoning_summary, verbosity: result.text_verbosity})) $("[data-reasoning-" + name + "]", dialog).textContent = value;
    const select = $("[data-reasoning-field]", dialog); select.replaceChildren();
    for (const [key, label] of Object.entries(fields)) if (result[options[key]]?.length) option(select, key, label);
    if (!select.options.length) option(select, "", "No supported mutable preferences advertised");
    fillValues();
  }
  function render(snapshot, ui) {
    if (!view) return;
    if (["project_id", "instance_id", "session_id"].some(k => snapshot[k] !== view.api.identity[k])) { dispose(); return; }
    view.snapshot = snapshot; view.ui = ui;
    if (view.inspected && snapshot.revision > view.inspected.revision && !view.committing) { view.inspected = null; cancel(); }
    controls();
  }
  function controls() {
    if (!view) return; const {dialog} = view, enabled = ready(), reviewing = !!view.confirmation;
    const field = $("[data-reasoning-field]", dialog).value, value = $("[data-reasoning-value]", dialog).value;
    const changed = !!view.inspected && value !== view.inspected[field] && view.inspected[options[field]]?.includes(value);
    $("[data-reasoning-open]", view.api.root).disabled = !view.ui?.readable || !!view.committing || !!view.uncertain;
    $("[data-reasoning-refresh]", dialog).disabled = !view.ui?.readable || !!view.read || reviewing || !!view.committing || !!view.uncertain;
    for (const name of ["field", "value"]) $("[data-reasoning-" + name + "]", dialog).disabled = !enabled || reviewing;
    $("[data-reasoning-review]", dialog).disabled = !enabled || !changed || reviewing || !view.inspected?.current_session_available;
    $("[data-reasoning-confirmation]", dialog).hidden = !reviewing;
    $("[data-reasoning-confirm]", dialog).disabled = !enabled || !reviewing || !$("[data-reasoning-consent]", dialog).checked;
    $("[data-reasoning-close]", dialog).disabled = !!view.committing;
    $("[data-reasoning-notice]", dialog).textContent = view.error || (view.read ? "Reading authoritative settings and model capabilities…" : view.committing ? "Applying one confirmed session-only update…" : !sameScope() ? "Refresh to inspect the current idle session. Changes to the conversation invalidate earlier confirmation." : !view.ui?.safe ? "Controls require an idle connected runtime with no other operation, active goal or queue review." : "Current values verified. Changes apply only to this runtime; host and project defaults remain unchanged.");
  }
  async function inspect() {
    if (!view?.ui?.readable || view.read || view.confirmation || view.committing || view.uncertain) return;
    const current = view, read = new AbortController(); current.read = read; current.error = ""; current.inspected = null; controls();
    try {
      const result = await current.api.inspect(read.signal);
      if (view !== current || current.read !== read || read.signal.aborted || !current.dialog.open) return;
      if (!valid(result, current.api.identity) || current.snapshot?.revision > result.revision) throw new Error("Unverified scope");
      current.inspected = result; show(result);
    } catch (_) { if (view === current && !read.signal.aborted) current.error = "Inspection failed or changed scope. Nothing was updated. Refresh explicitly to try inspection again."; }
    finally { if (view === current && current.read === read) { current.read = null; controls(); } }
  }
  function cancel() {
    if (!view || view.committing) return;
    view.read?.abort(); view.read = null; view.confirmation = null; $("[data-reasoning-consent]", view.dialog).checked = false; view.api.reserve(false); controls();
  }
  function close() { if (!view?.committing) { cancel(); view?.api.closeDialog(view.dialog); } }
  function prepare() {
    if (!ready() || view.confirmation) return;
    const field = $("[data-reasoning-field]", view.dialog).value, value = $("[data-reasoning-value]", view.dialog).value, expected = view.inspected;
    if (!expected.current_session_available || !expected[options[field]]?.includes(value) || expected[field] === value) return;
    view.confirmation = {field, value, expected}; view.api.reserve(true); $("[data-reasoning-consent]", view.dialog).checked = false;
    $("[data-reasoning-confirm-target]", view.dialog).textContent = `${fields[field]}: ${expected[field]} → ${value}. Project ${expected.project_id}, session ${expected.session_id}, ${expected.mode} mode. Model ${expected.provider} / ${expected.model}; permissions ${expected.permission_mode}. Branch ${expected.branch_id}, tip ${expected.tip_id || "empty"}. This changes only the current runtime. ${field === "thinking" ? "Only this collaboration mode’s thinking preference changes." : "Other response preferences remain unchanged."} Host and project defaults are not written. No prompt or goal will be started.`;
    controls(); $("[data-reasoning-consent]", view.dialog).focus({preventScroll: true});
  }
  async function commit() {
    if (!ready() || !view.confirmation || !view.dialog.open || !$("[data-reasoning-consent]", view.dialog).checked) return;
    const current = view, confirmation = current.confirmation, expected = confirmation.expected;
    if (facts.some(k => current.inspected[k] !== expected[k]) || current.inspected.revision !== expected.revision) return;
    const payload = {scope: "session", field: confirmation.field, value: confirmation.value, confirm: "session", expected_revision: String(expected.revision)};
    for (const key of facts) if (!["project_id", "instance_id"].includes(key)) payload[key] = expected[key];
    current.committing = true; current.confirmation = null; controls();
    try {
      const result = await current.api.set(payload);
      if (view !== current) return;
      if (!valid(result, current.api.identity) || result.revision <= expected.revision || facts.some(k => result[k] !== (k === confirmation.field ? confirmation.value : expected[k]))) throw new Error("Unverified update");
      current.inspected = result; show(result); current.error = "The worker confirmed the session-only update and its effective current values. No prompt was sent. Refresh before another change.";
    } catch (_) {
      if (view !== current) return;
      current.uncertain = true; current.inspected = null; current.error = "The update outcome is unverified. Do not retry: the runtime preference may have changed. Close this panel and explicitly review runtime recovery. Nothing will be activated or retried automatically.";
    } finally {
      if (view === current) { current.committing = false; current.inspected = null; $("[data-reasoning-consent]", current.dialog).checked = false; current.api.reserve(false); controls(); }
    }
  }
  function click(event) {
    const button = event.target.closest("button"); if (!view || !button || button.disabled) return;
    if (button.matches("[data-reasoning-open]") && view.ui?.readable) { view.api.openDialog(view.dialog, button); inspect(); }
    else if (button.matches("[data-reasoning-close]")) close();
    else if (button.matches("[data-reasoning-refresh]")) inspect();
    else if (button.matches("[data-reasoning-review]")) prepare();
    else if (button.matches("[data-reasoning-cancel]")) cancel();
    else if (button.matches("[data-reasoning-confirm]")) commit();
  }
  window.SnowReasoning = Object.freeze({init, render, dispose, valid});
})();
