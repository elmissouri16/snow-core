/* Explicit inspected-scope goal admission. No automatic resume, prompt text
 * tunneling, generic RPC command, or frontend goal/tool loop exists here. */
(() => {
  "use strict";
  const $ = (selector, scope) => scope?.querySelector(selector), drafts = new Map();
  const statuses = new Set(["none", "active", "paused", "blocked", "usage_limited", "budget_limited", "complete"]);
  const terminal = goal => ["complete", "budget_limited"].includes(goal?.status);
  const id = (value, empty = false) => typeof value === "string" && (empty || !!value) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
  const natural = value => Number.isSafeInteger(value) && value >= 0;
  let view;
  function validGoal(goal, session) {
    return !!goal && goal.session_id === session && id(goal.branch_id) && id(goal.tip_id, true) && id(goal.goal_id, true) && id(goal.goal_run_id, true) && typeof goal.objective === "string" && new TextEncoder().encode(goal.objective).length <= 65536 && typeof goal.blocked_reason === "string" && new TextEncoder().encode(goal.blocked_reason).length <= 32768 && statuses.has(goal.status) && (goal.goal_id ? goal.status !== "none" : goal.status === "none" && !goal.running) && typeof goal.deferred === "boolean" && typeof goal.running === "boolean" && (!goal.running || !!goal.goal_run_id) && natural(goal.tokens_used) && (goal.token_budget == null || natural(goal.token_budget) && goal.token_budget > 0) && (goal.budget_remaining == null || natural(goal.budget_remaining)) && (goal.estimated_costs == null || Array.isArray(goal.estimated_costs) && goal.estimated_costs.length <= 32);
  }
  function init(api) {
    dispose(); const dialog = $("#goals-dialog", api.root);
    if (!dialog || api.root.dataset.goalsEnabled !== "true") return;
    const key = JSON.stringify([api.identity.project_id, api.identity.session_id]), draft = drafts.get(key) || {objective: "", budget: ""};
    drafts.delete(key); drafts.set(key, draft); while (drafts.size > 16) drafts.delete(drafts.keys().next().value);
    const controller = new AbortController();
    view = {api, dialog, controller, draft, inspected: null};
    $("[data-goal-objective-draft]", dialog).value = draft.objective; $("[data-goal-token-budget]", dialog).value = draft.budget;
    $("[data-goal-consent]", dialog).checked = false;
    api.root.addEventListener("click", onClick, {signal: controller.signal});
    dialog.addEventListener("input", event => {
      if (event.target.matches("[data-goal-objective-draft]")) draft.objective = event.target.value;
      if (event.target.matches("[data-goal-token-budget]")) draft.budget = event.target.value;
      controls();
    }, {signal: controller.signal});
    dialog.addEventListener("change", controls, {signal: controller.signal});
    dialog.addEventListener("cancel", event => { event.preventDefault(); close(); }, {signal: controller.signal});
    dialog.addEventListener("close", () => { if (view?.dialog === dialog && !view.committing) cancel(); }, {signal: controller.signal});
    for (const name of ["compositionstart", "compositionend"]) dialog.addEventListener(name, () => { view.composing = name === "compositionstart"; controls(); }, {signal: controller.signal});
  }
  function dispose() {
    if (!view) return; const old = view; view = null;
    old.read?.controller.abort(); old.controller.abort(); old.api.closeDialog(old.dialog);
  }
  function facts(snapshot) {
    return {provider: snapshot.provider, model: snapshot.model, permission: snapshot.permission_mode, mode: snapshot.mode, thinking: snapshot.thinking};
  }
  function sameScope() {
    const inspected = view?.inspected, goal = view?.goal;
    return !!inspected && Number.isSafeInteger(inspected.revision) && inspected.revision > 0 && inspected.revision === view.snapshot?.revision && !!goal && ["session_id", "branch_id", "tip_id", "goal_id", "goal_run_id", "running", "status", "deferred", "objective", "tokens_used", "token_budget", "budget_remaining"].every(key => inspected.goal[key] === goal[key]) && JSON.stringify(inspected.facts) === JSON.stringify(facts(view.snapshot));
  }
  function ready() { return !!view?.ui?.safe && !view.invalid && !view.read && !view.committing && !view.composing && sameScope() && !view.goal.running && view.snapshot.mode === "default" && ["ask", "allow", "deny"].includes(view.snapshot.permission_mode) && !!view.snapshot.provider && !!view.snapshot.model; }
  function budget() {
    const text = view.draft.budget; if (!text) return null;
    if (!/^[1-9][0-9]*$/.test(text) || !Number.isSafeInteger(Number(text))) return false;
    return text;
  }
  function objectiveValid() { return view.api.validText(view.draft.objective) && [...view.draft.objective].length <= 32768; }
  function render(snapshot, ui) {
    if (!view) return;
    if (view.goal?.running && !snapshot?.goal?.running) view.inspected = null;
    view.snapshot = snapshot; view.ui = ui; view.invalid = snapshot?.goal != null && !validGoal(snapshot.goal, view.api.identity.session_id);
    view.goal = view.invalid ? null : snapshot?.goal;
    controls();
  }
  function controls() {
    if (!view) return;
    const {api, dialog, goal, ui, draft, confirmation} = view, safe = ready();
    const trigger = $("[data-goals-open]", api.root); trigger.hidden = !ui?.supported; trigger.disabled = !ui?.readable || view.invalid;
    $("[data-goal-status-badge]", api.root).textContent = goal?.running ? "· Running" : goal?.goal_id ? `· ${goal.status.replaceAll("_", " ")}` : "";
    const summary = $("[data-live-goal-status]", api.root); summary.hidden = !goal?.goal_id;
    $("[data-goal-summary-label]", api.root).textContent = goal?.running ? "Goal run active · Stop cancels the whole run" : goal?.goal_id ? `Goal ${goal.status.replaceAll("_", " ")}${goal.deferred ? " · deferred" : ""}` : "";
    $("[data-goal-summary-usage]", api.root).textContent = goal ? `${goal.tokens_used.toLocaleString()} tokens used${goal.budget_remaining == null ? " · no token budget" : ` · ${goal.budget_remaining.toLocaleString()} tokens remaining`}` : "";
    $("[data-goal-inspect]", dialog).disabled = !ui?.readable || !!view.read || !!confirmation || view.committing;
    $("[data-goal-scope]", dialog).textContent = view.inspected ? `Session: ${view.inspected.goal.session_id}\nBranch: ${view.inspected.goal.branch_id}\nTip: ${view.inspected.goal.tip_id || "(empty history)"}\nGoal: ${view.inspected.goal.goal_id || "(confirmed absent)"}${sameScope() ? "" : "\nChanged: refresh inspection before continuing."}` : "Not inspected. Refresh before authorizing a goal run.";
    $("[data-goal-objective]", dialog).textContent = goal?.objective || "No saved goal";
    $("[data-goal-state]", dialog).textContent = goal ? `${goal.status.replaceAll("_", " ")}${goal.running ? " · run active" : " · no active run"}` : "Unavailable";
    $("[data-goal-deferred]", dialog).textContent = goal?.deferred ? "Yes · not automatically scheduled" : "No";
    for (const [selector, value] of [["tokens", goal?.tokens_used], ["budget", goal?.token_budget], ["remaining", goal?.budget_remaining]]) $("[data-goal-" + selector + "]", dialog).textContent = value == null ? "No token budget" : value.toLocaleString();
    $("[data-goal-authority]", dialog).textContent = view.snapshot ? `${view.snapshot.provider || "Unknown"} / ${view.snapshot.model || "Unknown"} · ${view.snapshot.permission_mode || "unknown permissions"} · ${view.snapshot.mode || "unknown mode"}` : "Unknown";
    const blocked = $("[data-goal-blocked]", dialog); blocked.hidden = !goal?.blocked_reason; blocked.textContent = goal?.blocked_reason || "";
    const create = !goal?.goal_id || terminal(goal), resume = !!goal?.goal_id && !terminal(goal);
    $("[data-goal-create-fields]", dialog).hidden = !create || !!confirmation;
    const startButton = $("[data-goal-start]", dialog), resumeButton = $("[data-goal-resume]", dialog);
    startButton.hidden = !create || !!confirmation; startButton.disabled = !safe || !objectiveValid() || budget() === false;
    resumeButton.hidden = !resume || !!confirmation; resumeButton.disabled = !safe || goal?.budget_remaining === 0;
    $("[data-goal-confirmation]", dialog).hidden = !confirmation;
    $("[data-goal-confirm]", dialog).disabled = !safe || !confirmation || !$("[data-goal-consent]", dialog).checked;
    $("[data-goal-confirm]", dialog).textContent = confirmation?.action === "goal-resume" ? "Resume goal run" : "Start goal run";
    $("[data-goal-cancel]", dialog).disabled = !!view.committing;
    $("[data-goal-notice]", dialog).textContent = view.error || (view.invalid ? "Could not verify the goal projection. Controls are disabled." : view.read ? "Inspecting the exact saved goal; no work is being started…" : view.committing ? "Requesting one goal run…" : goal?.running ? "One goal run is active across serial turns. No frontend loop or automatic restart is scheduled." : create && budget() === false ? "Enter a positive whole-token budget no larger than 9007199254740991, or leave it empty." : create && draft.objective && !objectiveValid() ? "The objective must be valid text within 32,768 Unicode characters and 64 KiB." : view.snapshot?.mode === "plan" ? "Goal execution is unavailable in Plan mode. Inspection is read-only." : !sameScope() ? "Refresh the inspected scope before authorizing a goal run." : !ui?.safe ? "Goal execution needs an idle connected runtime without pending/review queue items or another action." : "Inspection is read-only. Nothing starts until you explicitly authorize and confirm.");
  }
  async function inspect() {
    if (!view?.ui?.readable || view.invalid || view.read || view.confirmation || view.committing || !view.goal) return;
    const current = view, branch = current.goal.branch_id, operation = {controller: new AbortController()};
    current.read = operation; current.error = ""; controls();
    try {
      const result = await current.api.inspect(branch, operation.controller.signal);
      if (view !== current || current.read !== operation || operation.controller.signal.aborted || !current.dialog.open) return;
      if (!validGoal(result.goal, current.api.identity.session_id) || result.goal.branch_id !== branch || !Number.isSafeInteger(result.revision) || result.revision <= 0) throw new Error("Goal scope changed");
      current.inspected = {goal: result.goal, facts: facts(result), revision: result.revision};
    } catch (error) { if (view === current && current.read === operation && !operation.controller.signal.aborted) { current.inspected = null; current.error = "Goal inspection failed or changed scope. Nothing started. Refresh explicitly to inspect again."; } }
    finally { if (view === current && current.read === operation) { current.read = null; controls(); } }
  }
  function cancel() {
    if (!view || view.committing) return;
    const focused = document.activeElement;
    view.read?.controller.abort(); view.read = null; view.confirmation = null; $("[data-goal-consent]", view.dialog).checked = false;
    view.api.reserve(false); controls();
    if (view.dialog.open && focused?.closest("[data-goal-confirmation]")) $(view.goal?.goal_id && !terminal(view.goal) ? "[data-goal-resume]" : "[data-goal-start]", view.dialog)?.focus({preventScroll: true});
  }
  function close() { if (!view?.committing) { cancel(); view?.api.closeDialog(view.dialog); } }
  function prepare(action) {
    if (!ready() || view.confirmation) return;
    const goal = view.inspected.goal, create = action === "goal-start";
    if (create ? goal.goal_id && !terminal(goal) || !objectiveValid() || budget() === false : !goal.goal_id || terminal(goal) || goal.budget_remaining === 0) return;
    const fields = {session_id: goal.session_id, branch_id: goal.branch_id, expected_tip_id: goal.tip_id, expected_goal_id: goal.goal_id};
    if (create) { fields.objective = view.draft.objective; if (budget() != null) fields.token_budget = budget(); }
    view.confirmation = {action, fields, expectedRevision: view.inspected.revision}; view.api.reserve(true);
    $("[data-goal-consent]", view.dialog).checked = false;
    $("[data-goal-confirm-target]", view.dialog).textContent = `${create ? "Create" : "Resume"} on branch ${goal.branch_id}, exact tip ${goal.tip_id || "(empty history)"}, ${goal.goal_id ? "reviewed goal " + goal.goal_id : "reviewed goal absence"}.${create ? " Objective: " + fields.objective + (fields.token_budget ? " Token budget: " + fields.token_budget + "." : " No token budget.") : " Existing objective and budget remain unchanged."}`;
    controls(); $("[data-goal-consent]", view.dialog).focus({preventScroll: true});
  }
  async function commit() {
    if (!ready() || !view.confirmation || !$("[data-goal-consent]", view.dialog).checked || !view.dialog.open) return;
    const current = view, confirmation = current.confirmation; current.confirmation = null; current.committing = true;
    current.api.closeDialog(current.dialog);
    const result = await current.api.run(confirmation.action, confirmation.fields, confirmation.expectedRevision);
    if (view !== current) return;
    current.committing = false; current.inspected = null; current.api.reserve(false);
    current.error = result ? "The goal run was admitted. Follow its current status; admission does not mean the goal is complete." : "Goal run outcome needs review. Your objective and message draft are kept. Nothing will be retried automatically.";
    controls();
  }
  function onClick(event) {
    const button = event.target.closest("button"); if (!view || !button) return;
    if (button.matches("[data-goals-open]") && view.ui?.readable && !view.invalid) { view.api.openDialog(view.dialog, button); inspect(); }
    else if (button.matches("[data-goals-close]")) close();
    else if (button.matches("[data-goal-inspect]")) inspect();
    else if (button.matches("[data-goal-start]")) prepare("goal-start");
    else if (button.matches("[data-goal-resume]")) prepare("goal-resume");
    else if (button.matches("[data-goal-cancel]")) cancel();
    else if (button.matches("[data-goal-confirm]")) commit();
  }
  window.SnowGoals = Object.freeze({init, render, dispose, validGoal});
})();
