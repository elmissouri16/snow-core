(() => {
  "use strict";
  /** @typedef {{provider:string,id:string,name:string,context_window:number}} ModelChoice */
  /** @typedef {{session_id:string,name:string,active:boolean,updated_at:number}} SessionChoice */
  let state;
  const $ = selector => document.querySelector(selector);
  const text = (selector, value) => { const node = $(selector); if (node && node.textContent !== value) node.textContent = value; };
  const activeTurn = status => ["running", "permission", "input"].includes(status);
  const canChange = s => !!s?.controls.safe && !s.loading && s.snapshot?.status === "idle";
  const policies = Object.freeze({ask: "Ask", deny: "Deny", allow: "Allow"});
  const canSetPolicy = s => canChange(s) && Object.hasOwn(policies, s.snapshot?.permission_mode) && !s.snapshot.permission && !s.snapshot.input && !["admitted", "admission_unknown"].includes(s.snapshot.recovery?.state);
  function cancelPolicy(s) {
    s.policyTarget = null;
    const dialog = $("#workflow-permission-dialog");
    if (dialog) { s.hooks.closeDialog(dialog); dialog.querySelector("input").checked = false; }
  }
  const canSwitch = s => !!s?.controls.safe && !s.loading && (s.snapshot?.status === "idle" || activeTurn(s.snapshot?.status));
  const number = value => Number.isFinite(value) && value >= 0 ? value.toLocaleString() : "Unknown";
  function node(tag, className, value) { const el = document.createElement(tag); el.className = className; if (value !== undefined) el.textContent = value; return el; }
  const iconPaths = {
    new: "M9 18H5l-3 3V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v5M18 14v8m-4-4h8",
    rename: "m16 3 5 5-12 12H4v-5L16 3Z M13 6l5 5",
    versions: "M3 4v5h5M3 9a9 9 0 1 1 0 6M12 7v5l3 2",
    goals: "M4 22V3m0 1h15l-3 4 3 4H4",
    processes: "M4 4h16v16H4V4Z m3 4 4 4-4 4m6 0h4",
    reasoning: "M9 18h6m-6 3h6M8 14a6 6 0 1 1 8 0c-1 1-1 2-1 4H9c0-2 0-3-1-4Z",
    compaction: "M4 3h16M4 21h16M12 5v14m-4-10 4 4 4-4m-8 6 4-4 4 4",
    steer: "M12 21V3m-5 5 5-5 5 5M5 21v-4a5 5 0 0 1 5-5h2",
    refresh: "M20 7V3m0 4h-4M4 17v4m0-4h4M20 7a9 9 0 0 0-16 1m0 9a9 9 0 0 0 16-1",
    back: "m14 6-6 6 6 6", next: "m10 6 6 6-6 6", check: "m5 12 4 4L19 6"
  };
  function icon(name) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    for (const [key, value] of Object.entries({class: "icon", viewBox: "0 0 24 24", width: "16", height: "16", fill: "none", stroke: "currentColor", "stroke-width": "1.6", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", focusable: "false"})) svg.setAttribute(key, value);
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path"); path.setAttribute("d", iconPaths[name]); svg.append(path);
    return svg;
  }
  function row(label, {key = label, disabled = false, checked, value, glyph = key === "back" ? "back" : undefined, next = false, action} = {}) {
    const button = node("button", "snow-menu-row"); button.type = "button"; button.dataset.menuKey = key;
    button.setAttribute("role", checked === undefined ? "menuitem" : "menuitemradio");
    if (checked !== undefined) button.setAttribute("aria-checked", String(checked));
    button.disabled = disabled; button.title = label + (value ? ` · ${value}` : "");
    if (glyph) button.append(icon(glyph));
    button.append(node("span", "snow-menu-row-label", label));
    if (value) button.append(node("span", "snow-menu-row-value", value));
    if (next) button.append(icon("next"));
    if (checked !== undefined) { const check = node("span", "snow-menu-check"); check.setAttribute("aria-hidden", "true"); const mark = icon("check"); mark.style.visibility = checked ? "visible" : "hidden"; check.append(mark); button.append(check); }
    button._snowMenuAction = action;
    button.addEventListener("click", () => { if (!button.disabled) button._snowMenuAction?.(button); });
    return button;
  }
  const note = value => node("p", "snow-menu-note", value);
  const separator = () => { const el = node("div", "snow-menu-separator"); el.setAttribute("role", "separator"); return el; };
  function currentModel(s) { return s.choices?.models.find(item => item.provider === s.snapshot?.provider && item.id === s.snapshot?.model)?.name || s.snapshot?.model || "Model unknown"; }
  function dismiss(s, restoreFocus = true) { if (s.menu) window.SnowMenus.close({restoreFocus}); }
  function valid(s, instance) { return state === s && s.root.isConnected && s.snapshot?.instance_id === instance; }
  function pane(s, name) { if (!s.menu) return; s.menu.pane = name; s.menu.signature = ""; paintMenu(s); s.menu.panel.querySelector("button:not(:disabled)")?.focus(); }
  function telemetryView(snapshot) {
    const telemetry = snapshot?.telemetry, cost = window.SnowCosts?.presentation(telemetry);
    return {
      contextLabel: telemetry?.context_available && telemetry.estimated === false ? "Last reported input" : "Estimated context",
      context: telemetry?.context_available ? `${number(telemetry.context_tokens)} tokens / ${telemetry.context_window > 0 ? number(telemetry.context_window) : "unknown window"}` : "Unknown",
      usage: telemetry?.available ? `${number(telemetry.input_tokens)} in · ${number(telemetry.output_tokens)} out · ${number(telemetry.total_tokens)} total` : "Unknown",
      cost: cost?.known ? cost.value : "Unknown", knownCost: !!cost?.known
    };
  }
  function telemetryContent(s, view) {
    const fragment = document.createDocumentFragment();
    fragment.append(node("h2", "", "Context & usage"));
    const dl = node("dl", "");
    if (s.menu.pane === "telemetry-details") {
      fragment.append(row("Back", {key: "back", action: () => pane(s, "root")}),
        note("Context estimates are approximate; last reported input is measured. An unknown context window cannot give a usage percentage. Unknown values are not zero."));
      window.SnowCosts?.render(dl, s.snapshot?.telemetry);
      fragment.append(dl);
      return fragment;
    }
    for (const [label, value, hook] of [
      [view.contextLabel, view.context, "workflowContext"],
      ["Usage", view.usage, "workflowUsage"],
      ["Cost estimate", view.cost, "workflowCost"]
    ]) {
      const pair = node("div", ""), dt = node("dt", "", label), dd = node("dd", "", value);
      pair.dataset.menuKey = hook; dd.dataset[hook] = "";
      if (hook === "workflowContext") dt.dataset.workflowContextLabel = "";
      if (hook === "workflowCost") dd.dataset.known = String(view.knownCost);
      pair.append(dt, dd); dl.append(pair);
    }
    fragment.append(dl, row("Details", {key: "telemetry-details", next: true, action: () => pane(s, "telemetry-details")}));
    return fragment;
  }
  // Menu rows delegate to resident feature owners; they never acquire runtime
  // authority or duplicate requests. Hidden is capability state, not CSS layout.
  function runtimeActions(s) {
    return [
      ["versions", "Conversation versions", "[data-versions-open]"],
      ["goals", "Thread goal", "[data-goals-open]"],
      ["processes", "Managed processes", "[data-processes-open]"],
      ["reasoning", "Thinking & response", "[data-reasoning-open]"],
      ["compaction", "Compact context…", "[data-compaction-open]"],
      ["steer", "Steer current run…", "[data-steer-open]"]
    ].map(([key, label, selector]) => ({key, label, source: s.root.querySelector(selector)}))
      .filter(item => item.source && !item.source.hidden);
  }
  function menuContent(s, telemetry) {
    const fragment = document.createDocumentFragment(), menu = s.menu, instance = s.snapshot?.instance_id;
    const idle = canChange(s), switchable = canSwitch(s), snapshot = s.snapshot;
    const mutate = async (kind, fields) => {
      if (!valid(s, instance) || !canChange(s)) return;
      dismiss(s); await s.hooks.action(kind, fields);
    };
    const loadRow = sessions => {
      fragment.append(row(s.loading ? "Loading host choices…" : sessions ? "Load conversations & models" : "Load host models", {key: "load", glyph: "refresh", disabled: !idle, action: () => load(s)}));
      const status = note(s.loadError || (s.loading ? "Contacting the host…" : "Loading may contact provider model discovery."));
      status.dataset.workflowLoadStatus = ""; status.setAttribute("role", "status"); fragment.append(status);
    };
    if (menu.kind === "telemetry") return telemetryContent(s, telemetry);
    if (menu.kind === "permission-policy") {
      fragment.append(node("p", "snow-menu-group-label", menu.pane === "permission-help" ? "Permission details" : "Session permissions"));
      if (menu.pane === "permission-help") {
        fragment.append(row("Back", {key: "back", action: () => pane(s, "root")}),
          note("Ask requests approval when required. Deny rejects non-read tools without asking. Allow skips permission prompts. Read-risk tools do not require approval."),
          note("These are approval policies, not sandbox modes. Tools run with the host account’s privileges. Changes affect this session only; existing session decisions still apply in Ask."));
        return fragment;
      }
      for (const [mode, label] of Object.entries(policies)) fragment.append(row(label, {
        key: mode, checked: snapshot?.permission_mode === mode, disabled: !canSetPolicy(s),
        action: () => {
          if (!valid(s, instance) || !canSetPolicy(s)) return;
          if (snapshot.permission_mode === mode) { dismiss(s); return; }
          if (mode !== "allow") { mutate("permission-mode", {mode, session_id: snapshot.session_id}); return; }
          const trigger = menu.trigger;
          dismiss(s);
          s.policyTarget = {instance, session: snapshot.session_id, previous: snapshot.permission_mode};
          const dialog = $("#workflow-permission-dialog");
          dialog.querySelector("input").checked = false;
          dialog.querySelector("[data-policy-confirm]").disabled = true;
          s.hooks.openDialog(dialog, trigger);
        }
      }));
      fragment.append(note("Session only · not a sandbox."), row("Details", {key: "permission-help", action: () => pane(s, "permission-help")}));
      if (!canSetPolicy(s)) fragment.append(note("Policy changes require a connected, verified, idle session."));
      return fragment;
    }
    if (menu.kind === "mode") {
      fragment.append(node("p", "snow-menu-group-label", "Collaboration mode"));
      for (const [mode, label] of [["default", "Default"], ["plan", "Plan Mode"]]) fragment.append(row(label, {
        key: mode, checked: snapshot?.mode === mode, disabled: !idle || !["default", "plan"].includes(snapshot?.mode),
        action: () => { if (snapshot.mode === mode) dismiss(s); else mutate("mode", {mode}); }
      }));
      fragment.append(note(snapshot?.mode === "plan" ? "Plan Mode: investigate and plan, not implement." : snapshot?.mode === "default" ? "Default mode is active in the runtime." : "Authoritative mode is unavailable."));
      return fragment;
    }
    if (menu.kind === "session") {
      if (menu.pane === "root") {
        fragment.append(row("New conversation", {key: "new", glyph: "new", disabled: !switchable, action: () => { if (valid(s, instance)) switchTo(s, "", menu.trigger); }}));
        fragment.append(row("Rename conversation", {key: "rename", glyph: "rename", disabled: !idle, action: () => {
          if (valid(s, instance)) openRename(s, menu.trigger);
        }}));
        fragment.append(separator(), row("Switch conversation", {key: "sessions", next: true, disabled: !switchable, action: () => pane(s, "sessions")}));
        const actions = runtimeActions(s);
        if (actions.length) fragment.append(separator());
        for (const {key, label, source} of actions) fragment.append(row(label, {key, glyph: key, disabled: source.disabled, action: () => {
          if (!valid(s, instance) || !source.isConnected || source.disabled || source.hidden) return;
          dismiss(s);
          source.click();
        }}));
      } else {
        fragment.append(row("Conversations", {key: "back", action: () => pane(s, "root")}));
        if (!s.choices) loadRow(true);
        else {
          const groups = node("div", "snow-menu-groups"), sessions = s.choices.sessions;
          if (!sessions.some(item => item.session_id === snapshot.session_id)) groups.append(row(snapshot.session_name || "Untitled conversation", {key: "current", checked: true, action: () => dismiss(s)}));
          for (const item of sessions) groups.append(row(item.name || "Untitled conversation", {key: item.session_id, checked: item.session_id === snapshot.session_id, disabled: !switchable, action: () => { if (valid(s, instance)) switchTo(s, item.session_id, menu.trigger); }}));
          fragment.append(groups);
          if (s.choices.sessions_available === false) fragment.append(note("Saved conversations are unavailable on this worker."));
          else if (!sessions.length) fragment.append(note("No other saved conversations."));
          if (s.choices.sessions_truncated) fragment.append(note("Some conversations are omitted from this bounded list."));
        }
      }
      return fragment;
    }
    const header = node("div", "snow-menu-header model-search-header");
    const label = node("label", "sr-only", "Search models by name, ID or provider"); label.htmlFor = menu.search.id;
    // Describe the live input without detaching it during refresh. Its native
    // focus, selection, search value and input listener belong to this menu.
    header.append(label, menu.search.isConnected ? menu.search.cloneNode(true) : menu.search); fragment.append(header);
    const status = note(s.loading ? "Loading host models…" : s.loadError || (!s.choices ? "Model discovery requires a connected, verified, idle session." : ""));
    status.dataset.workflowLoadStatus = ""; status.setAttribute("role", "status");
    fragment.append(status);
    if (!s.choices) {
      const actions = node("div", "snow-menu-footer"); actions.setAttribute("role", "menu"); actions.setAttribute("aria-label", "Model discovery");
      actions.append(row(s.loading ? "Loading models…" : s.loadError ? "Retry loading models" : "Refresh models", {key: "load", glyph: "refresh", disabled: !idle, action: () => load(s)})); fragment.append(actions);
      return fragment;
    }
    const query = (menu.query || "").trim().toLocaleLowerCase();
    const models = s.choices.models.filter(item => [item.provider, item.id, item.name || ""].some(value => value.toLocaleLowerCase().includes(query)));
    const groups = node("div", "snow-menu-groups"); groups.setAttribute("role", "menu"); groups.setAttribute("aria-label", "Available models");
    for (const provider of [...new Set(models.map(item => item.provider))]) {
      const group = node("div", "snow-menu-group"); group.setAttribute("role", "group"); group.setAttribute("aria-label", provider);
      group.append(node("p", "snow-menu-group-label", provider));
      for (const item of models.filter(item => item.provider === provider)) {
        const checked = item.provider === snapshot.provider && item.id === snapshot.model;
        const button = row(item.name || item.id, {key: JSON.stringify([item.provider, item.id]), checked, disabled: !idle, action: () => {
          if (!valid(s, instance) || !canChange(s) || !s.choices.models.some(model => model.provider === item.provider && model.id === item.id)) return;
          if (s.snapshot.provider === item.provider && s.snapshot.model === item.id) dismiss(s);
          else mutate("model", {provider: item.provider, model: item.id});
        }});
        button.dataset.modelProvider = item.provider; button.dataset.modelId = item.id;
        button.title = `${item.name || item.id} · ${item.provider} / ${item.id}`;
        group.append(button);
      }
      groups.append(group);
    }
    fragment.append(groups);
    if (!models.length) {
      const empty = note(query ? "No models match your search." : "No host models discovered."); empty.setAttribute("role", "status"); fragment.append(empty);
    }
    if (s.choices.models_partial) fragment.append(note("Some provider discovery was unavailable."));
    if (s.choices.models_truncated) fragment.append(note("Some models are omitted from this bounded list."));
    const footer = node("div", "snow-menu-footer"); footer.setAttribute("role", "menu"); footer.setAttribute("aria-label", "Model discovery");
    footer.append(row(s.loading ? "Refreshing models…" : s.loadError ? "Retry loading models" : "Refresh models", {key: "load", glyph: "refresh", disabled: !idle, action: () => load(s)}));
    fragment.append(footer);
    return fragment;
  }
  function paintMenu(s) {
    const menu = s.menu;
    if (!menu) return;
    const telemetry = menu.kind === "telemetry" ? telemetryView(s.snapshot) : null;
    const busy = String(!telemetry && s.loading);
    if (menu.panel.getAttribute("aria-busy") !== busy) menu.panel.setAttribute("aria-busy", busy);
    // Read-only metrics do not depend on model inventories or mutation locks.
    // Compare their displayed values before allocating/reconciling any DOM.
    const signature = telemetry ? JSON.stringify([menu.pane, s.snapshot?.instance_id, s.snapshot?.session_id, telemetry]) : JSON.stringify([menu.kind, menu.pane, menu.query, s.snapshot?.instance_id, s.snapshot?.session_id, s.snapshot?.session_name, s.snapshot?.provider, s.snapshot?.model, s.snapshot?.mode, s.snapshot?.permission_mode, menu.kind === "session" ? runtimeActions(s).map(({key, source}) => [key, source.disabled]) : null, canChange(s), canSwitch(s), s.loading, s.loadError, s.choices]);
    if (signature === menu.signature) return;
    menu.signature = signature;
    const focused = menu.panel.contains(document.activeElement), searchFocused = document.activeElement === menu.search, key = document.activeElement?.dataset.menuKey;
    window.SnowMenus.reconcile(menu.panel, menuContent(s, telemetry));
    if (focused && (!menu.panel.contains(document.activeElement) || document.activeElement.disabled)) (searchFocused ? menu.search : [...menu.panel.querySelectorAll("button:not(:disabled)")].find(el => el.dataset.menuKey === key) || menu.search || menu.panel.querySelector("button:not(:disabled)") || menu.panel).focus({preventScroll: true});
    window.SnowMenus.reposition();
  }
  function showMenu(s, kind, trigger) {
    if (!window.SnowMenus) return;
    if (s.menu?.trigger === trigger) { dismiss(s); return; }
    window.SnowMenus.close({restoreFocus: false});
    const panel = node("div", `conversation-task-menu${kind === "telemetry" ? " telemetry-menu" : ""}`);
    panel.setAttribute("aria-label", {model: "Model selection", session: "Conversation actions", mode: "Collaboration mode", "permission-policy": "Session permissions", telemetry: "Context and usage"}[kind]);
    if (kind === "telemetry" || kind === "model") panel.setAttribute("role", "dialog");
    s.menu = {kind, trigger, panel, pane: "root", signature: "", query: ""};
    if (kind === "model") {
      panel.classList.add("model-picker-menu");
      const search = node("input", "model-search"); search.type = "search"; search.id = "model-picker-search";
      search.placeholder = "Search models…"; search.maxLength = 256; search.autocomplete = "off"; search.spellcheck = false;
      search.dataset.modelSearch = ""; search.dataset.menuAutofocus = "";
      s.menu.search = search;
      search.addEventListener("input", () => {
        if (s.menu?.search !== search) return;
        s.menu.query = search.value; paintMenu(s);
        const content = panel.querySelector(".snow-menu-content"); if (content) content.scrollTop = 0;
      });
    }
    paintMenu(s);
    window.SnowMenus.open({trigger, panel, placement: kind === "session" ? "bottom-end" : "top-end", onClose: () => { s.menu = null; }, onBack: () => {
      if (!s.menu || s.menu.pane === "root") return false;
      pane(s, "root"); return true;
    }});
    if (kind === "model" && !s.choices && !s.loading) void load(s);
  }
  function render(snapshot, controls = {}) {
    const s = state;
    if (!s || !s.root.isConnected) return;
    s.controls = controls;
    if (snapshot) {
      if (s.snapshot && s.snapshot.instance_id !== snapshot.instance_id) {
        // Choices and in-flight metadata are bound to the previous runtime nonce.
        s.choices = null; s.sessionChoices = null; s.loading = false; s.generation++; s.loadError = ""; s.switchTarget = null;
        dismiss(s, false); s.hooks.closeDialog($("#workflow-switch-dialog")); s.hooks.closeDialog($("#workflow-rename-dialog"));
      }
      s.snapshot = snapshot;
    }
    if (!s.snapshot) return;
    const idle = canChange(s), mode = s.snapshot.mode;
    const policyTrigger = $("[data-permission-policy-menu]");
    if (policyTrigger) {
      const label = s.controls.verified && ["idle", "running", "permission", "input"].includes(s.snapshot.status) && Object.hasOwn(policies, s.snapshot.permission_mode) ? policies[s.snapshot.permission_mode] : "Unknown";
      text("[data-permission-policy-label]", label);
      policyTrigger.setAttribute("aria-label", `Session permissions: ${label}`);
      policyTrigger.title = `Session permissions: ${label} · not a sandbox`;
      policyTrigger.disabled = !canSetPolicy(s);
      if (!canSetPolicy(s) && s.menu?.kind === "permission-policy") dismiss(s, false);
      if (s.policyTarget && (!canSetPolicy(s) || s.policyTarget.instance !== s.snapshot.instance_id || s.policyTarget.session !== s.snapshot.session_id || s.policyTarget.previous !== s.snapshot.permission_mode)) cancelPolicy(s);
    }
    const modeLabel = mode === "plan" ? "Plan Mode" : mode === "default" ? "Default" : "Mode unknown";
    text("[data-mode-label]", modeLabel);
    text("[data-mode-short]", mode === "plan" ? "Plan" : mode === "default" ? "Default" : "Mode");
    $("[data-mode-menu]").setAttribute("aria-label", `Collaboration mode: ${modeLabel}`);
    $("[data-mode-menu]").title = `Collaboration mode: ${modeLabel}`;
    text("#live-model", currentModel(s));
    const modelTrigger = $("[data-model-menu]"); modelTrigger.title = `${s.snapshot.provider || "Unknown provider"} / ${s.snapshot.model || "Unknown model"}`;
    modelTrigger.setAttribute("aria-label", `Choose model: ${currentModel(s)}`);
    $("[data-workflow-switch-confirm]").disabled = !canSwitch(s);
    $('[data-workflow-rename-form] button[type="submit"]').disabled = !idle;
    const telemetry = s.snapshot.telemetry;
    const known = telemetry?.context_available && Number.isFinite(telemetry.context_tokens) && telemetry.context_tokens >= 0 && Number.isFinite(telemetry.context_window) && telemetry.context_window > 0;
    const percent = known ? Math.max(0, Math.min(100, telemetry.context_tokens / telemetry.context_window * 100)) : 0;
    const meter = $("[data-telemetry-menu]"); meter.dataset.unknown = String(!known);
    meter.setAttribute("aria-label", known ? `Context and usage: ${Math.round(percent)}% context used` : "Context and usage: context unknown");
    meter.querySelector(".context-ring-value").style.strokeDasharray = `${percent} 100`;
    paintMenu(s);
  }
  async function load(s) {
    if (state !== s || !canChange(s)) return;
    s.loading = true; s.loadError = ""; const generation = ++s.generation, instance = s.snapshot.instance_id;
    paintMenu(s);
    try {
      const choices = await s.hooks.choices();
      if (!valid(s, instance) || generation !== s.generation || choices.instance_id !== instance) return;
      if (!Array.isArray(choices.models) || !Array.isArray(choices.sessions)) throw new Error("Invalid choices");
      s.choices = {...choices,
        models: choices.models.filter(item => typeof item?.provider === "string" && item.provider && typeof item.id === "string" && item.id),
        sessions: choices.sessions.filter(item => typeof item?.session_id === "string" && item.session_id)};
      s.sessionChoices = s.choices.sessions;
    } catch (_) {
      if (state === s && generation === s.generation) s.loadError = "Host choices unavailable. Try again.";
    } finally { if (state === s && generation === s.generation) { s.loading = false; render(s.snapshot, s.controls); } }
  }
  async function switchTo(s, sessionID, trigger, sidebarTarget = false) {
    if (!canSwitch(s)) return false;
    if (sessionID === s.snapshot.session_id) { dismiss(s); return true; }
    if (sessionID && !sidebarTarget && !(s.sessionChoices || s.choices?.sessions)?.some(item => item.session_id === sessionID)) return false;
    dismiss(s);
    if (activeTurn(s.snapshot.status)) {
      s.switchTarget = {sessionID, instance: s.snapshot.instance_id};
      s.hooks.openDialog($("#workflow-switch-dialog"), trigger);
      return true;
    }
    return await s.hooks.action("switch", {session_id: sessionID});
  }
  function openRename(s, trigger) {
    if (state !== s || !s?.root.isConnected || !canChange(s) || !trigger?.isConnected) return;
    dismiss(s, false); s.renameInstance = s.snapshot.instance_id;
    $("#workflow-name").value = s.snapshot.session_name || "";
    s.hooks.openDialog($("#workflow-rename-dialog"), trigger);
  }
  function init(root, hooks) {
    dispose();
    if (!root || !$("[data-model-menu]")) return;
    const s = state = {root, hooks, controls: {}, snapshot: null, choices: null, loading: false, loadError: "", generation: 0, listeners: new AbortController(), menu: null};
    const options = {signal: s.listeners.signal};
    s.actionObserver = new MutationObserver(() => { if (state === s && s.menu?.kind === "session") paintMenu(s); });
    const actions = root.querySelector(".live-manager-controls");
    if (actions) s.actionObserver.observe(actions, {subtree: true, attributes: true, attributeFilter: ["disabled", "hidden"]});
    document.addEventListener("click", async event => {
      const button = event.target.closest("button");
      if (!button || state !== s) return;
      for (const kind of ["model", "session", "mode", "telemetry", "permission-policy"]) if (button.matches(`[data-${kind}-menu]`)) { showMenu(s, kind, button); return; }
      if (button.matches("[data-policy-cancel]")) { cancelPolicy(s); return; }
      if (button.matches("[data-policy-confirm]")) {
        const target = s.policyTarget, dialog = $("#workflow-permission-dialog");
        if (!target || !dialog.open || !dialog.querySelector("input").checked || !canSetPolicy(s) || target.instance !== s.snapshot.instance_id || target.session !== s.snapshot.session_id || target.previous !== s.snapshot.permission_mode) return;
        cancelPolicy(s);
        await hooks.action("permission-mode", {mode: "allow", session_id: target.session, confirm_allow: "allow"});
        return;
      }
      if (button.matches("[data-workflow-cancel]")) { s.switchTarget = null; hooks.closeDialog(button.closest("dialog")); }
      else if (button.matches("[data-workflow-switch-confirm]")) {
        const target = s.switchTarget;
        if (!$("#workflow-switch-dialog").open || !canSwitch(s) || !target || target.instance !== s.snapshot.instance_id) return;
        s.switchTarget = null; hooks.closeDialog($("#workflow-switch-dialog"));
        await hooks.action("switch", {session_id: target.sessionID, confirm_stop: "stop"});
      }
    }, options);
    document.addEventListener("submit", async event => {
      if (!event.target.matches("[data-workflow-rename-form]")) return;
      event.preventDefault();
      if (state !== s || !canChange(s) || s.renameInstance !== s.snapshot.instance_id || !$("#workflow-rename-dialog").open) return;
      const name = $("#workflow-name").value.trim();
      if (!name || new TextEncoder().encode(name).length > 256) { $("#workflow-name").setCustomValidity("Enter a name of at most 256 UTF-8 bytes."); $("#workflow-name").reportValidity(); return; }
      await hooks.action("rename", {name});
      // An uncertain mutation remains globally visible, never a retry loop.
      if (state === s) hooks.closeDialog($("#workflow-rename-dialog"));
    }, options);
    $("#workflow-name").addEventListener("input", event => event.target.setCustomValidity(""), options);
    const policyDialog = $("#workflow-permission-dialog");
    if (policyDialog) {
      policyDialog.querySelector("input").addEventListener("change", event => { policyDialog.querySelector("[data-policy-confirm]").disabled = !event.target.checked || !canSetPolicy(s); }, options);
      policyDialog.addEventListener("close", () => { s.policyTarget = null; policyDialog.querySelector("input").checked = false; }, options);
    }
  }
  function dispose() { if (state) { dismiss(state, false); state.listeners.abort(); state.actionObserver?.disconnect(); state.generation++; } state = null; }
  // Sidebar selection shares this owner's admission and confirmation path.
  // Session-only inventory must never trigger model discovery.
  async function select({project, session = "", instance, trigger}) {
    const s = state;
    if (!s || !canSwitch(s) || s.snapshot.project_id !== project || (instance && s.snapshot.instance_id !== instance)) return false;
    const currentInstance = s.snapshot.instance_id;
    if (session && session !== s.snapshot.session_id) {
      // A displayed, instance-bound row is navigation intent, not membership
      // authority. Switch re-lists/validates the target server-side and preempts
      // background inventory; an extra frontend read would race those reads.
      const sidebarTarget = trigger?.dataset.project === project && trigger.dataset.instance === currentInstance && trigger.closest("[data-shell-session]")?.dataset.shellSession === session;
      if (sidebarTarget) return await switchTo(s, session, trigger, true);
      if (activeTurn(s.snapshot.status)) return false;
      if (!s.hooks.sessions) return false;
      s.loading = true;
      try {
        const inventory = await s.hooks.sessions(session);
        if (!valid(s, currentInstance) || inventory.project_id !== project || inventory.instance_id !== currentInstance || !inventory.available || !Array.isArray(inventory.sessions)) return false;
        // Keep models independently unloaded when only sessions were requested.
        s.sessionChoices = inventory.sessions;
      } catch (_) { return false; }
      finally { if (state === s) { s.loading = false; render(s.snapshot, s.controls); } }
      if (!canSwitch(s) || !s.sessionChoices.some(item => item.session_id === session)) return false;
    }
    if (!valid(s, currentInstance)) return false;
    return await switchTo(s, session, trigger);
  }
  window.SnowConversation = Object.freeze({init, render, dispose, select, rename: trigger => openRename(state, trigger)});
})();
