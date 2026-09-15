(() => {
  "use strict";
  const $ = (selector, scope = document) => scope.querySelector(selector);
  const root = document.documentElement;
  const narrow = matchMedia("(max-width: 767px)");
  const uncertain = new Map(); // Outcome guards survive workspace navigation, in memory only.
  const maxDrafts = 16;
  const editDrafts = new Map(); // Prepared authority and original selection stay in tab memory only.
  const reuseDrafts = new Map(); // Original drafts retained while editing a message copy.
  const drafts = new Map(); // Tab memory only; never place prompts in URLs or localStorage.
  let navReturn, folderRequest, folderState, live, pollTimer;
  let navRestore = true, sidebarCollapsed = false;
  let pageAway = false, homeNavigationForm;
  const composingWorkspaceDrafts = new WeakSet();
  const registeringProjects = new WeakSet();
  let homeDraft = {text: "", project: "", name: "", pending: false};
  let workspaceIntent;
  const visitedSessions = new Map();
  const dialogReturns = new WeakMap(), boundDialogs = new WeakSet();
  root.dataset.theme = "dark";
  try {
    const theme = localStorage.getItem("snow-manager-theme");
    if (theme === "light" || theme === "dark") root.dataset.theme = theme;
  } catch (_) { /* Preferences are optional. */ }

  function matchesWorkspaceDraft(prompt) {
    return !homeDraft.text || ((!homeDraft.project || homeDraft.project === prompt.dataset.draftProject) && (homeDraft.targetSession === undefined || homeDraft.targetSession === prompt.dataset.draftSession));
  }
  function syncWorkspaceDraft() {
    const prompt = $("#workspace-prompt");
    if (!prompt) return false;
    const matches = matchesWorkspaceDraft(prompt);
    if (matches && homeDraft.text) {
      homeDraft.project = prompt.dataset.draftProject;
      homeDraft.targetSession = prompt.dataset.draftSession;
      homeDraft.name = prompt.dataset.draftName;
    }
    window.SnowWorkspace?.updateDraft({workspaceEnabled: matches,
      ...(matches && !composingWorkspaceDrafts.has(prompt) ? {workspaceText: homeDraft.text} : {})});
    return matches && !!homeDraft.text;
  }
  function syncHomeDraft() {
    const workspaceDraftVisible = syncWorkspaceDraft();
    const prompt = $("#home-prompt");
    const host = $(".runtime-activation") || $("#live-composer-normal") || $("#add-project");
    const draftURL = homeDraft.project && homeDraft.targetSession !== undefined ? projectLocation(homeDraft.project) + (homeDraft.targetSession ? "&session=" + encodeURIComponent(homeDraft.targetSession) : "&new=1") : "/";
    window.SnowWorkspace?.updateDraft({
      ...(!prompt || !composingWorkspaceDrafts.has(prompt) ? {text: homeDraft.text} : {}),
      homeEnabled: true, name: homeDraft.name, pending: !!homeNavigationForm && homeNavigationForm === $("#home-composer"),
      notice: !homeDraft.text || prompt || workspaceDraftVisible || !host ? null : {
        text: homeDraft.text,
        explanation: live ? "Your startup draft is kept. Clear the session draft to use it here, or return to edit it." : `Your draft is kept for ${homeDraft.name || "another session"}. Return to edit it, or discard it before writing a different draft.`,
        url: draftURL, useVisible: !!live && homeDraft.pending && homeDraft.project === live.project,
        useDisabled: !!$("#live-prompt")?.value || !live || live.unknown || editDrafts.has(live.key) || reuseDrafts.has(live.key)
      }});
  }
  function takeHomeDraft(explicit = false) {
    const prompt = $("#live-prompt");
    const ack = homeDraft.ack;
    if (!explicit && (!ack || ack.project !== live?.project || ack.session !== live?.session || ack.instance !== live?.instance)) return false;
    if (!live || !homeDraft.pending || homeDraft.project !== live.project || !homeDraft.text || !prompt || prompt.value || live.unknown || editDrafts.has(live.key) || reuseDrafts.has(live.key)) return false;
    if (!window.SnowLiveView?.updateDraft(homeDraft.text)) return false;
    live.draftRevision++; saveDraft();
    homeDraft = {text: "", project: "", name: "", pending: false};
    return true;
  }
  async function continueHome() {
    const form = $("#home-composer");
    if (form.dataset.pending || homeNavigationForm === form) return;
    if (!window.htmx) { window.SnowWorkspace?.updateDraft({privacy: "Workspace navigation is unavailable. Copy your draft before reloading."}); return; }
    homeDraft.text = $("#home-prompt").value;
    if (new TextEncoder().encode(homeDraft.text).length > 65536) { window.SnowWorkspace?.updateDraft({privacy: "Shorten your draft to at most 64 KiB before continuing."}); return; }
    if (!homeDraft.project) { $(".workspace-picker > summary")?.click(); return; }
    homeDraft.pending = true; homeNavigationForm = form;
    const button = $(".home-send"); window.SnowWorkspace?.updateDraft({pending: true});
    try {
      await window.htmx.ajax("GET", projectLocation(homeDraft.project), {source: button, target: "#workspace", swap: "outerHTML"});
    } catch (_) {
      if (form.isConnected) window.SnowWorkspace?.updateDraft({privacy: "Could not open this workspace. Your draft is kept; try again."});
    } finally {
      if (homeNavigationForm === form) homeNavigationForm = null;
      if (form.isConnected) window.SnowWorkspace?.updateDraft({pending: false});
    }
  }
  // Select locally before HTMX's target-level listener can navigate away.
  document.addEventListener("click", event => {
    if (!(event.target instanceof Element) || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || !$("#home-prompt")) return;
    const picker = event.target.closest(".workspace-picker > summary");
    if (picker) { event.preventDefault(); event.stopPropagation(); window.SnowShell?.openWorkspacePicker(picker); return; }
    if (event.target.closest(".picker-add")) { homeDraft.project = ""; homeDraft.name = ""; homeDraft.pending = false; delete homeDraft.ack; delete homeDraft.targetSession; return; }
    const link = event.target.closest("[data-home-project]");
    if (!link) return;
    event.preventDefault(); event.stopPropagation();
    if ($("#home-composer")?.dataset.pending) return;
    if (link.dataset.homeProjectAvailable !== "true") { window.SnowWorkspace?.updateDraft({privacy: "This folder is unavailable. Choose another workspace or restore its folder."}); return; }
    homeDraft.project = link.dataset.homeProject; homeDraft.name = link.dataset.homeProjectName; homeDraft.pending = false; delete homeDraft.ack; delete homeDraft.targetSession;
    window.SnowMenus?.close({restoreFocus: true}); syncHomeDraft();
    window.SnowWorkspace?.updateDraft({privacy: "Continue to start or resume in this workspace. Nothing has been sent."});
  }, {capture: true});
  function finishWorkspaceOpening(region = $("#live-session")) {
    if (region !== $("#live-session")) return;
    if (region?.dataset.workspaceOpening === "true") {
      region.hidden = false; delete region.dataset.workspaceOpening;
    }
    window.SnowWorkspace?.updateOpening?.({visible: false, minHeight: 0});
  }
  function workspaceFlowError(message) {
    finishWorkspaceOpening();
    if (live) { liveError(message); return; }
    window.SnowWorkspace?.updateFlowError(message);
  }
  async function consumeWorkspaceIntent() {
    const intent = workspaceIntent;
    if (!intent || !live || live.project !== intent.project || !live.connected) return;
    workspaceIntent = null; // One explicit click, consumed once; never replayed.
    if (intent.session === live.session) { finishWorkspaceOpening(); return; }
    if (intent.instance && intent.instance !== live.instance) { workspaceFlowError("This workspace changed. Select the session again to review its current state."); return; }
    const group = [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === intent.project);
    const row = [...(group?.querySelectorAll("[data-shell-session]") || [])].find(item => item.dataset.shellSession === intent.session);
    const trigger = intent.trigger?.isConnected ? intent.trigger : row?.querySelector("a") || group?.querySelector("[data-shell-project-new]") || $("[data-session-menu]");
    requestNav(false, false);
    if (["running", "permission", "input"].includes(live.status)) finishWorkspaceOpening();
    const region = $("#live-session");
    let accepted;
    try { accepted = await window.SnowConversation?.select({...intent, instance: live.instance, trigger}); }
    finally { finishWorkspaceOpening(region); }
    if (!accepted && live?.project === intent.project && !live.actionError) workspaceFlowError("This session cannot be opened yet. Review any pending work or connection warning, then select it again.");
  }
  async function selectWorkspaceSession(detail, isNew = false) {
    const {project, trigger} = detail || {}, session = isNew ? "" : detail?.session;
    if (typeof project !== "string" || !project || project.length > 128 || typeof session !== "string" || session.length > 128 || !window.htmx) return;
    const intent = {...detail, project, session};
    workspaceIntent = intent;
    if (live?.project === project) { void consumeWorkspaceIntent(); return; }
    try {
      intent.url = projectLocation(project) + (isNew ? "&new=1" : "");
      await window.htmx.ajax("GET", intent.url, {source: trigger, target: "#workspace", swap: "outerHTML", push: "true"});
      if (workspaceIntent !== intent) return;
      if (!live || live.project !== project) {
        workspaceIntent = null;
        if (!isNew) workspaceFlowError("The workspace is no longer live. Select the saved session again to read or resume it.");
      } else void consumeWorkspaceIntent();
    } catch (_) {
      if (workspaceIntent === intent) { workspaceIntent = null; workspaceFlowError("Could not open this workspace. Nothing was switched; select it again to retry."); }
    }
  }
  document.addEventListener("snow:session-deleted", event => {
    const {project, session} = event.detail || {};
    if (typeof project !== "string" || typeof session !== "string" || !session) return;
    if (visitedSessions.get(project) === session) visitedSessions.delete(project);
    if (homeDraft.project === project && homeDraft.targetSession === session) {
      homeDraft.targetSession = ""; delete homeDraft.ack;
      syncHomeDraft();
    }
  });
  document.addEventListener("snow:session-select", event => { void selectWorkspaceSession(event.detail); });
  document.addEventListener("snow:session-new", event => { void selectWorkspaceSession(event.detail, true); });
  document.body.addEventListener("htmx:beforeRequest", event => {
    if (event.detail?.target?.id === "workspace" && workspaceIntent && event.detail.elt !== workspaceIntent.trigger) workspaceIntent = null;
  });
  async function navigateWorkspace(href, source, event) {
    if (!window.htmx?.ajax || typeof href !== "string" || href.length > 8192) return false;
    let target;
    try { target = new URL(href, location.href); } catch (_) { return false; }
    if (target.origin !== location.origin || target.pathname !== "/" || target.username || target.password) return false;
    if (source && (!(source instanceof Element) || !source.isConnected)) return false;
    source ||= $("#project-navigation");
    if (!source?.isConnected || !$("#workspace")) return false;
    // Only a workspace heading link inherits this tab's last visited session.
    // Explicit session, New, settings, pagination and draft links keep their URL.
    if (source.matches("#project-navigation .project-link")) {
      const project = source.closest("[data-sidebar-project]")?.dataset.sidebarProject;
      const session = visitedSessions.get(project);
      if (session && target.searchParams.get("project") === project && !target.searchParams.has("session") && !target.searchParams.has("new") && !target.searchParams.has("inspect")) target.searchParams.set("last_session", session);
    }
    workspaceIntent = null;
    window.SnowShell?.preemptInventory();
    try {
      // Bundled HTMX 2.0.10 supports context.push; retain a link's explicit
      // fragment/false override, otherwise push the resolved request URL.
      // Synchronization is inherited from JSX's transport-only hx-sync attrs.
      await window.htmx.ajax("GET", target.pathname + target.search + target.hash, {
        source, event, target: "#workspace", swap: "outerHTML", push: source.getAttribute("hx-push-url") || "true"
      });
      return true;
    } catch (_) { workspaceFlowError("Could not open this workspace. Your draft is kept."); return false; }
  }
  document.addEventListener("snow:shell-navigate", event => {
    void navigateWorkspace(event.detail?.href, event.detail?.source);
  });
  // Capture prevents a previously processed SSR link's target listener from
  // dispatching a second request. React roots themselves are never processed.
  document.addEventListener("click", event => {
    if (event.defaultPrevented || !(event.target instanceof Element) || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
    const link = event.target.closest('a[hx-get][hx-target="#workspace"]');
    if (!link || !link.closest('[data-react-page], [data-react-workspace], [data-react-live-panel], [data-react-inspection], #home-draft-notice') || link.hasAttribute("download") || link.target && link.target !== "_self") return;
    event.preventDefault(); event.stopPropagation();
    void navigateWorkspace(link.getAttribute("hx-get"), link, event);
  }, {capture: true});
  function syncThemeChoices() {
    window.SnowShell?.setTheme(root.dataset.theme);
  }
  function syncSidebarCollapse() {
    const collapsed = sidebarCollapsed && !narrow.matches;
    root.dataset.sidebarCollapsed = String(collapsed);
    $("#workspace")?.classList.toggle("sidebar-collapsed", collapsed);
    document.querySelectorAll('#workspace-content [data-sidebar-collapse], .topbar [data-sidebar-collapse]').forEach(button => {
      button.setAttribute("aria-expanded", String(!collapsed));
      button.setAttribute("aria-label", collapsed ? "Expand sidebar" : "Collapse sidebar");
    });
  }
  document.addEventListener("snow:shell-command", event => {
    const command = event.detail;
    if (command?.type === "theme") setTheme(command.theme);
    else if (command?.type === "collapse" && typeof command.collapsed === "boolean") { sidebarCollapsed = command.collapsed; syncSidebarCollapse(); }
    else if (command?.type === "navigation" && typeof command.open === "boolean") setNav(command.open, navRestore);
  });
  function requestNav(open, restore = true) {
    const previous = navRestore; navRestore = restore;
    try { window.SnowShell?.navigation(open); }
    finally { navRestore = previous; }
  }
  function setTheme(theme) {
    if (theme !== "light" && theme !== "dark") return;
    root.dataset.theme = theme;
    try { localStorage.setItem("snow-manager-theme", theme); } catch (_) { /* Preferences are optional. */ }
    syncThemeChoices();
  }
  function csrf() { return $('input[name="csrf"]')?.value || ""; }
  function focusable(scope) {
    return [...scope.querySelectorAll('a[href],button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),summary,[tabindex="0"]')].filter(node => !node.hidden && node.getClientRects().length);
  }
  function trap(event, scope) {
    if (event.key !== "Tab") return;
    const nodes = focusable(scope), first = nodes[0], last = nodes.at(-1);
    if (!first) { event.preventDefault(); return; }
    if (event.shiftKey && (document.activeElement === first || !scope.contains(document.activeElement))) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && (document.activeElement === last || !scope.contains(document.activeElement))) { event.preventDefault(); first.focus(); }
  }
  function setNav(open, restore = true) {
    const nav = $("#project-navigation");
    open = open && narrow.matches;
    if (open) navReturn = document.activeElement;
    $(".topbar [data-nav-toggle]")?.setAttribute("aria-expanded", String(open));
    const content = $("#workspace-content"), topbar = $(".topbar");
    if (content) content.inert = open;
    if (topbar) topbar.inert = open;
    if (open) requestAnimationFrame(() => {
      if (nav?.isConnected && nav === $("#project-navigation") && nav.classList.contains("nav-open")) $("[data-nav-close]", nav)?.focus();
    });
    else {
      if (restore && navReturn?.isConnected) navReturn.focus();
      navReturn = null;
    }
  }
  function openDialog(dialog, trigger) {
    // Menu aliases focus their surviving launcher before forwarding the click.
    // Composer-origin actions return to their own icon, not the header menu.
    if (trigger?.closest(".manager-menu-enabled") || document.activeElement?.matches("[data-session-menu]")) trigger = $("[data-session-menu]");
    bindDialog(dialog);
    dialogReturns.set(dialog, trigger || document.activeElement);
    dialog.showModal();
    // Native dialog supplies inert background, focus containment and Escape.
  }
  function closeDialog(dialog) { if (dialog?.open) dialog.close(); }
  function bindDialog(dialog) {
    // React can replace SSR dialog nodes after navigation. Bind the actual
    // native dialog on opening, not only the discarded initial fallback.
    if (dialog.id === "settings-dialog" || boundDialogs.has(dialog)) return;
    boundDialogs.add(dialog);
    if (dialog.id === "message-regenerate-dialog") dialog.addEventListener("cancel", event => {
      event.preventDefault(); cancelRegeneration();
    });
    dialog.addEventListener("close", () => {
      // A queued native close from a retired React dialog cannot act on the
      // next workspace's operation or steal its focus.
      if (!dialog.isConnected) { dialogReturns.delete(dialog); return; }
      if (dialog.id === "message-regenerate-dialog" && !dialog.open && ["preparing", "ready", "failed", "stale"].includes(live?.regeneration?.phase)) cancelRegeneration(false);
      if (dialog.id === "folder-picker" && !dialog.open) folderRequest?.abort();
      if (dialog.id === "processes-dialog" && !dialog.open) $("#managed-processes", dialog).open = false;
      const target = dialogReturns.get(dialog);
      dialogReturns.delete(dialog);
      if (target?.isConnected) target.focus();
    });
  }
  function rememberWorkspaceSession(project, session) {
    if (!project || !session || visitedSessions.get(project) === session) return;
    visitedSessions.delete(project); visitedSessions.set(project, session);
    while (visitedSessions.size > 100) visitedSessions.delete(visitedSessions.keys().next().value);
  }
  function navigation() {
    // Module arrival is independent of classic defer order. Initialize only
    // after React view facades exist, always against the current workspace.
    if (!window.SnowReactReady) return;
    syncThemeChoices();
    rememberWorkspaceSession($("#workspace")?.dataset.project, $("#workspace")?.dataset.session);
    syncSidebarCollapse();
    requestNav(false, false);
    document.querySelectorAll("dialog").forEach(bindDialog);
    setupLive();
    syncHomeDraft();
    window.SnowInspection?.init(); window.SnowProcesses?.init();
    renderSavedMessages();
    const locationState = new URL(location.href);
    if (locationState.searchParams.get("inspect") === "project") {
      inspectProject(locationState.searchParams.get("project"), locationState.hash === "#remove-project");
    }
  }
  // HTMX owns idle-setting POSTs, timing and request lifecycle. The existing
  // snapshot reconciler owns the result: never swap JSON into the conversation,
  // process response HTML/redirect headers, or serialize the prompt form.
  async function settingsRequest(url, fields, signal) {
    const source = $("#live-settings-transport"), endpoint = new URL(url, location.href);
    if (!source || !window.htmx?.ajax || signal.aborted || endpoint.origin !== location.origin) throw new Error("Settings transport unavailable");
    let value, failure, xhr;
    const maxBytes = 4 * 1024 * 1024;
    const abort = () => xhr?.abort();
    const before = event => {
      if (event.detail.elt !== source) return;
      xhr = event.detail.xhr;
      xhr.addEventListener("progress", event => { if (event.loaded > maxBytes) xhr.abort(); });
      if (signal.aborted) { event.preventDefault(); xhr.abort(); }
    };
    source.addEventListener("htmx:beforeRequest", before);
    signal.addEventListener("abort", abort, {once: true});
    try {
      await window.htmx.ajax("POST", endpoint.href, {
        source, target: source, swap: "none", values: fields,
        headers: {Accept: "application/json"},
        handler: (_element, response) => {
          const reply = response.xhr;
          if (reply.status < 200 || reply.status >= 300) { failure = new Error("Settings request failed"); failure.status = reply.status; return; }
          try {
            if (signal.aborted || reply.responseURL !== endpoint.href || !/^application\/json(?:;|$)/i.test(reply.getResponseHeader("Content-Type") || "") || reply.responseText.length > maxBytes || new TextEncoder().encode(reply.responseText).length > maxBytes) throw new Error("Unverified settings response");
            value = JSON.parse(reply.responseText);
          } catch (error) { failure = error; }
        }
      });
      if (failure) throw failure;
      if (!value || signal.aborted) throw new Error("Settings outcome unavailable");
      return value;
    } finally {
      source.removeEventListener("htmx:beforeRequest", before);
      signal.removeEventListener("abort", abort);
    }
  }
  async function request(url, body, signal, timeout = 10000, maxBytes = 0) {
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (signal?.aborted) controller.abort();
    signal?.addEventListener("abort", abort, {once: true});
    const timer = setTimeout(abort, timeout);
    try {
      const response = await fetch(url, {
        method: body ? "POST" : "GET", credentials: "same-origin", cache: "no-store", redirect: "error",
        headers: body ? {"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", "Accept": "application/json"} : {"Accept": "application/json"},
        body: body ? new URLSearchParams(body) : undefined, signal: controller.signal
      });
      if (!response.ok) { const error = new Error("request failed"); error.status = response.status; throw error; }
      let value;
      if (maxBytes) {
        const reader = response.body.getReader(), chunks = []; let size = 0;
        try {
          while (true) {
            const {done, value: chunk} = await reader.read(); if (done) break;
            size += chunk.byteLength;
            if (size > maxBytes) { controller.abort(); throw new Error("Panel response too large"); }
            chunks.push(chunk);
          }
          const bytes = new Uint8Array(size); let offset = 0;
          for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
          value = JSON.parse(new TextDecoder("utf-8", {fatal: true}).decode(bytes));
        } finally { reader.releaseLock(); }
      } else value = await response.json();
      if (!value || typeof value !== "object") throw new Error("invalid response");
      return value;
    } finally { clearTimeout(timer); signal?.removeEventListener("abort", abort); }
  }

  async function browseFolders(path = "", offset = 0, append = false) {
    folderRequest?.abort();
    const controller = new AbortController(); folderRequest = controller;
    const dialog = $("#folder-picker");
    if (!dialog?.open) return;
    window.SnowWorkspace?.updateFolder({error: "", status: "Reading host folders…", busy: true, canSelect: false});
    try {
      const data = await request("/projects/folders", {csrf: csrf(), path, offset: String(offset)}, controller.signal, 8000);
      if (controller.signal.aborted || !dialog.open || folderRequest !== controller) return;
      if (typeof data.path !== "string" || typeof data.parent !== "string" || !Array.isArray(data.folders)) throw new Error("invalid folders");
      const folders = data.folders.slice(0, 256).filter(folder => typeof folder.name === "string" && typeof folder.path === "string").map(folder => ({name: folder.name, path: folder.path}));
      folderState = {...data, folders: append ? [...(folderState?.folders || []), ...folders] : folders};
      window.SnowWorkspace?.updateFolder({path: data.path, parent: data.parent, folders: folderState.folders,
        canSelect: true, hasMore: !!data.has_more,
        status: data.limited ? "Listing limit reached. Enter a full host path manually if your folder is not shown." : folderState.folders.length ? `${folderState.folders.length} folders shown. Open a folder to browse inside.` : "No subfolders in this batch. You can select this folder."});
    } catch (_) {
      if (controller.signal.aborted || folderRequest !== controller || !dialog.open) return;
      folderState = null;
      window.SnowWorkspace?.updateFolder({error: "Could not read host folders.", status: "Try Home, or cancel and enter an absolute host path manually.", hasMore: false, parent: "", canSelect: false});
    } finally {
      if (folderRequest === controller && dialog.open) window.SnowWorkspace?.updateFolder({busy: false});
    }
  }

  function liveError(message) {
    window.SnowLiveView?.updateChrome({error: message});
  }
  function runtimeURL(action = "") { return `/projects/${encodeURIComponent(live.project)}/runtime${action ? "/" + action : ""}`; }
  function saveDraft() {
    if (live && $("#live-prompt")) {
      drafts.delete(live.key); drafts.set(live.key, $("#live-prompt").value);
      while (drafts.size > maxDrafts) {
        const oldest = drafts.keys().next().value;
        drafts.delete(oldest); reuseDrafts.delete(oldest); editDrafts.delete(oldest); window.SnowComposerContext?.forget(oldest);
      }
    }
  }
  function renderSavedMessages() {
    window.SnowMessages?.enhance(document);
  }
  function renderMessages(snapshot) {
    const stream = $("#live-stream"), transcript = $("#live-transcript");
    const nearBottom = !window.SnowScroll && stream.scrollHeight - stream.scrollTop - stream.clientHeight < 100;
    const messages = Array.isArray(snapshot.messages) ? snapshot.messages : [];
    const focusedActivity = document.activeElement?.closest("[data-activity-id]") ? document.activeElement : null;
    window.SnowMessages?.render(transcript, messages);
    // Mount chronological markers before routing their public rows. Saved tool
    // enhancement remains independent of runtime event-step associations.
    window.SnowMessages?.enhance(transcript);
    window.SnowVisibility?.renderActivities($("#live-activities"), snapshot, transcript);
    if (focusedActivity?.isConnected && document.activeElement !== focusedActivity) focusedActivity.focus({preventScroll: true});
    $("#live-empty").hidden = messages.length > 0 || (snapshot.activities || []).length > 0;
    $("#live-history-notice").hidden = !snapshot.history_truncated;
    if (!window.SnowScroll) {
      if (nearBottom) { stream.scrollTop = stream.scrollHeight; $("#jump-latest").hidden = true; }
      else $("#jump-latest").hidden = false;
    }
  }
  function renderAttention(snapshot) {
    window.SnowAttention?.render(snapshot, attentionState());
  }
  const activeStatus = status => ["running", "permission", "input"].includes(status);
  function mutationSafe(panelOwner) {
    return !!live && live.connected && !live.action && !live.cancel && !live.metadata && !live.invalidInstance && !live.unknown && !live.snapshot?.cancel_requested && (!live.snapshot?.goal?.running || panelOwner === "attention") && (!compactionActive() || panelOwner === "attention") && !window.SnowQueue?.blocking() && (panelOwner === "steer" || !window.SnowSteer?.blocking()) && (!live.panelMutation || live.panelMutation === panelOwner);
  }
  function canCloseFailed() {
    // A bound failed worker has no running-turn authority. Explicit Close is
    // recovery, not a retry; an unknown Stop result must not trap this runtime.
    return !!live && live.connected && !live.invalidInstance && live.status === "failed" && !live.action && !live.metadata;
  }
  const goalBlocksHistory = () => !!live?.snapshot?.goal?.goal_id && !["complete", "budget_limited"].includes(live.snapshot.goal.status);
  const compactionActive = () => ["pending", "running"].includes(live?.snapshot?.compaction?.state);
  const settingsAction = action => ["mode", "permission-mode", "model", "rename"].includes(action);
  const streamingAdmission = action => ["prompt", "prompt-content", "goal-start", "goal-resume", "compaction-start", "steer"].includes(action);
  const contextDraftBusy = () => !!(window.SnowComposerContext?.hasAttachments() || window.SnowComposerContext?.pending());
  function uncertainSteerStopMatches() {
    const scope = live?.unknownSteerStop, snapshot = live?.snapshot;
    return !!scope && !!snapshot && typeof scope.cancel_token === "string" && !!scope.cancel_token &&
      scope.project_id === live.project && scope.instance_id === live.instance && scope.session_id === live.session &&
      scope.project_id === snapshot.project_id && scope.instance_id === snapshot.instance_id && scope.session_id === snapshot.session_id &&
      scope.cancel_token === snapshot.cancel_token;
  }
  function canStop() {
    if (!live || !live.connected || live.invalidInstance || (live.unknown && !live.snapshot?.goal?.running && !compactionActive() && !uncertainSteerStopMatches()) || live.cancel || (live.metadata && !compactionActive()) || (!activeStatus(live.status) && !live.snapshot?.goal?.running && !compactionActive()) || live.snapshot?.cancel_requested) return false;
    // Only a bound authoritative snapshot grants authority, never local Sending.
    if (live.turnCancel) return typeof live.snapshot?.cancel_token === "string" && !!live.snapshot.cancel_token && (!live.action || streamingAdmission(live.actionName) || compactionActive());
    return !!live.snapshot && !live.action;
  }
  function attentionTakeover(region, active) {
    if (!region?.isConnected || region !== $("#live-session")) return;
    const attention = $("#live-attention", region), seat = $("#live-composer-seat", region), normal = $("#live-composer-normal", region);
    if (attention) attention.hidden = !active;
    if (seat) seat.dataset.attention = String(active);
    if (normal) { normal.hidden = active; normal.inert = active; }
  }
  function attentionState() {
    return {safe: mutationSafe("attention"), busy: !!(live?.action || live?.cancel), canStop: canStop(), stopping: !!live?.cancel || !!live?.snapshot?.cancel_requested};
  }
  function updateReuse(ready) {
    window.SnowMessages?.updateActions($("#live-transcript"), {reuse: {
      hidden: live.messageEdit, disabled: !ready || reuseDrafts.has(live.key),
      title: "Use a copy as a new prompt. Saved history stays unchanged."
    }});
    const reuse = reuseDrafts.get(live.key);
    window.SnowLiveView?.updateControls({reuse: {visible: !!reuse, disabled: !!live.action,
      text: reuse?.sent ? "Copy sent as a new message. Your earlier draft is still kept; Cancel restores it." : "Using a copy as a new prompt. Send adds a new message to this conversation; saved history stays unchanged. Your earlier draft is kept."}});
  }
  function reuseMessage(button) {
    if (!mutationSafe() || contextDraftBusy() || live.regeneration || live.messageEdit || live.status !== "idle" || reuseDrafts.has(live.key)) return;
    const row = button.closest('[data-message-role="user"]'), prompt = $("#live-prompt");
    if (!row?._snowReusable || !row.closest("#live-transcript") || typeof row._snowCopyText !== "string") return;
    reuseDrafts.set(live.key, {draft: prompt.value, start: prompt.selectionStart, end: prompt.selectionEnd, direction: prompt.selectionDirection});
    window.SnowScroll?.beforeUpdate();
    if (!window.SnowLiveView?.updateDraft(row._snowCopyText)) { reuseDrafts.delete(live.key); return; }
    live.draftRevision++; saveDraft(); updateControls();
    window.SnowScroll?.afterUpdate();
    prompt.focus(); prompt.setSelectionRange(prompt.value.length, prompt.value.length);
  }
  function cancelReuse() {
    if (!live || live.action) return;
    const reuse = reuseDrafts.get(live.key), prompt = $("#live-prompt");
    if (!reuse) return;
    window.SnowScroll?.beforeUpdate();
    if (!window.SnowLiveView?.updateDraft(reuse.draft)) return;
    live.draftRevision++; reuseDrafts.delete(live.key);
    saveDraft(); updateControls(); window.SnowScroll?.afterUpdate(); prompt.focus(); prompt.setSelectionRange(reuse.start, reuse.end, reuse.direction);
  }
  function validEditText(text) {
    // URLSearchParams would silently replace lone UTF-16 surrogates. Reject
    // malformed Unicode and NUL before dispatch instead of altering user text.
    return typeof text === "string" && text.length <= 65536 && !!text.trim() && !/[\u0000\uD800-\uDFFF]/u.test(text) && new TextEncoder().encode(text).length <= 65536;
  }
  function updateEdit(ready) {
    const edit = editDrafts.get(live.key);
    window.SnowMessages?.updateActions($("#live-transcript"), {edit: {
      hidden: !live.messageEdit, disabled: !ready || !!edit, messageID: edit?.messageID || ""
    }});
    if (!edit) { window.SnowLiveView?.updateControls({edit: {visible: false, text: "", disabled: false}}); return; }
    const status = edit.phase === "preparing" ? "Loading the full message. Nothing has changed yet." :
      edit.phase === "unknown" ? "Edit outcome unknown. Nothing will retry. Your text and earlier draft are kept; review the conversation before continuing." :
      edit.phase === "committing" ? "Resending edited message… Your text and earlier draft are kept." :
      edit.phase === "completed" ? "Edited message resent. Your newer typing is kept. Cancel restores your earlier draft." :
      edit.instance !== live.instance ? "This edit belongs to an older runtime. Cancel to restore your earlier draft; nothing will be sent." :
      "Editing message: resending replaces the following conversation. Your earlier draft is kept.";
    window.SnowLiveView?.updateControls({edit: {visible: true, text: status, disabled: edit.phase === "committing"},
      ...(edit.phase !== "ready" || edit.instance !== live.instance ? {sendDisabled: true} : {})});
  }
  async function prepareEdit(button) {
    if (!mutationSafe() || contextDraftBusy() || goalBlocksHistory() || live.regeneration || !live.messageEdit || live.status !== "idle" || editDrafts.has(live.key)) return;
    const row = button.closest('#live-transcript [data-message-role="user"]'), prompt = $("#live-prompt");
    if (!row?._snowEditable || !row.dataset.messageId) return;
    const current = live, controller = current.controller, revision = current.draftRevision;
    const edit = {phase: "preparing", instance: current.instance, messageID: row.dataset.messageId,
      draft: prompt.value, draftRevision: revision, start: prompt.selectionStart, end: prompt.selectionEnd, direction: prompt.selectionDirection};
    editDrafts.set(current.key, edit);
    current.action = true; current.actionName = "message-edit-prepare"; current.actionError = ""; updateControls();
    try {
      // Prepare is read-only. Only its full authoritative text may enter the
      // editor; the displayed, sanitized and possibly truncated row is not input.
      const result = await request(runtimeURL("message-edit-prepare"), {csrf: csrf(), instance_id: edit.instance, message_id: edit.messageID}, controller.signal, 15000);
      if (live !== current || current.controller !== controller || controller.signal.aborted || editDrafts.get(current.key) !== edit) return;
      if (result.project_id !== current.project || result.session_id !== current.session || result.instance_id !== edit.instance || result.message_id !== edit.messageID ||
          typeof result.edit_token !== "string" || !result.edit_token || !validEditText(result.text)) throw new Error("Unverified prepared message");
      // Do not overwrite typing (even typing followed by undo) during the read.
      if (revision !== current.draftRevision || prompt.value !== edit.draft) throw new Error("Draft changed during prepare");
      edit.token = result.edit_token; edit.phase = "ready";
      window.SnowScroll?.beforeUpdate();
      if (!window.SnowLiveView?.updateDraft(result.text)) throw new Error("Draft is composing");
      current.draftRevision++; saveDraft();
      prompt.focus(); prompt.setSelectionRange(prompt.value.length, prompt.value.length);
      window.SnowScroll?.afterUpdate();
    } catch (_) {
      if (live === current && editDrafts.get(current.key) === edit) {
        editDrafts.delete(current.key);
        current.actionError = "Could not load this message for editing. Your draft and conversation are unchanged. Nothing was sent or retried.";
        liveError(current.actionError);
      }
    } finally {
      if (live === current && current.controller === controller) {
        current.action = false; current.actionName = "";
        const pending = current.pendingSnapshot; current.pendingSnapshot = null;
        if (pending) applySnapshot(pending);
        updateControls();
      }
    }
  }
  function cancelEdit() {
    if (!live) return;
    const edit = editDrafts.get(live.key), prompt = $("#live-prompt");
    if (!edit || edit.phase === "committing") return;
    // Canceling a pending read does not roll back text typed during that read.
    if (edit.phase !== "preparing") {
      window.SnowScroll?.beforeUpdate();
      if (!window.SnowLiveView?.updateDraft(edit.draft)) return;
      live.draftRevision++; saveDraft();
      prompt.focus(); prompt.setSelectionRange(edit.start, edit.end, edit.direction);
      window.SnowScroll?.afterUpdate();
    } else {
      prompt.focus();
      if (prompt.value === edit.draft && live.draftRevision === edit.draftRevision) prompt.setSelectionRange(edit.start, edit.end, edit.direction);
    }
    editDrafts.delete(live.key);
    updateControls();
  }
  function updateRegeneration(ready) {
    const operation = live.regeneration, dialog = $("#message-regenerate-dialog");
    window.SnowLiveView?.updateControls({regenerate: {
      visible: !!operation && ["committing", "unknown"].includes(operation.phase),
      text: operation?.phase === "unknown" ? "Regeneration outcome unknown. Your draft is kept. Review the conversation; nothing will retry." : "Regenerating reply… Your draft is kept; nothing is queued.",
      dismiss: operation?.phase === "unknown"}});
    window.SnowMessages?.updateActions($("#live-transcript"), {
      regenerate: {hidden: !live.messageRegenerate, disabled: !ready || !!operation || editDrafts.has(live.key) || reuseDrafts.has(live.key)},
      historicalDisabled: !!operation && !!dialog
    });
    if (!operation || !dialog) return;
    // Preparing/confirming regeneration never lends authority to the composer
    // or another historical action. Tools/permissions still use their own gates.
    window.SnowLiveView?.updateControls({sendDisabled: true});
    if (["preparing", "ready"].includes(operation.phase) && (operation.instance !== live.instance || live.invalidInstance || live.status !== "idle" || !live.snapshot?.messages?.some(message => message.id === operation.messageID && message.can_regenerate === true))) {
      operation.phase = "stale"; operation.token = "";
    }
    const labels = {
      preparing: "Preparing confirmation… Nothing has changed yet.",
      ready: "Ready to regenerate. Nothing changes until you confirm.",
      committing: "Regenerating… Your draft is kept. Nothing will be retried.",
      failed: "Could not prepare regeneration. Your conversation and draft are unchanged. Cancel to return; nothing was sent or retried.",
      stale: "This confirmation is no longer current. Cancel and review the conversation; nothing will be sent.",
      unknown: "Regeneration outcome unknown. Your draft is kept. Cancel and review the conversation before continuing; nothing will retry."
    };
    window.SnowLiveView?.updateControls({dialog: {
      status: operation.phase === "ready" && !ready ? "Updates unavailable. Confirmation is disabled; your draft is kept." : labels[operation.phase],
      confirmDisabled: !ready || operation.phase !== "ready" || !operation.token,
      cancelDisabled: operation.phase === "committing"}});
  }
  async function prepareRegeneration(button) {
    if (!mutationSafe() || goalBlocksHistory() || !live.messageRegenerate || live.status !== "idle" || live.regeneration || editDrafts.has(live.key) || reuseDrafts.has(live.key)) return;
    const row = button.closest('#live-transcript [data-message-role="assistant"]'), dialog = $("#message-regenerate-dialog");
    if (!row?._snowRegeneratable || !row.dataset.messageId || !dialog) return;
    const current = live, controller = current.controller;
    const operation = {phase: "preparing", instance: current.instance, messageID: row.dataset.messageId, sourceControl: button};
    current.regeneration = operation; current.action = true; current.actionName = "message-regenerate-prepare"; current.actionError = "";
    openDialog(dialog, button); updateControls();
    try {
      const result = await request(runtimeURL("message-regenerate-prepare"), {csrf: csrf(), instance_id: operation.instance, message_id: operation.messageID}, controller.signal, 15000);
      if (live !== current || current.controller !== controller || controller.signal.aborted || current.regeneration !== operation) return;
      if (result.project_id !== current.project || result.session_id !== current.session || result.instance_id !== operation.instance || result.message_id !== operation.messageID || typeof result.edit_token !== "string" || !result.edit_token) throw new Error("Unverified regeneration preparation");
      operation.token = result.edit_token; operation.phase = "ready";
      // No source text is fetched into, or inferred from, the composer. Its
      // value, selection and concurrent typing are untouched at every phase.
    } catch (_) {
      if (live === current && current.regeneration === operation) operation.phase = "failed";
    } finally {
      if (live === current && current.controller === controller) {
        current.action = false; current.actionName = "";
        const pending = current.pendingSnapshot; current.pendingSnapshot = null;
        if (pending) applySnapshot(pending);
        updateControls();
      }
    }
  }
  function cancelRegeneration(close = true) {
    if (!live || live.regeneration?.phase === "committing") return;
    live.regeneration = null;
    if (close) closeDialog($("#message-regenerate-dialog"));
    updateControls();
  }
  async function commitRegeneration() {
    if (!mutationSafe() || live.status !== "idle") return;
    const current = live, operation = current.regeneration;
    if (!operation || operation.phase !== "ready" || operation.instance !== current.instance || !operation.token || !$("#message-regenerate-dialog")?.open) return;
    const token = operation.token; operation.token = ""; operation.phase = "committing";
    closeDialog($("#message-regenerate-dialog"));
    const result = await runtimeAction("message-regenerate-commit", {edit_token: token, confirm: "regenerate"});
    if (live !== current || current.regeneration !== operation) return;
    if (result) {
      current.regeneration = null; closeDialog($("#message-regenerate-dialog"));
      saveDraft(); window.SnowScroll?.follow();
    } else operation.phase = "unknown";
    updateControls();
  }
  function applySnapshot(snapshot) {
    if (!live || !$("#live-session") || snapshot.project_id !== live.project || typeof snapshot.status !== "string") return;
    if (snapshot.instance_id !== live.instance || snapshot.session_id !== live.session) {
      finishWorkspaceOpening();
      live.invalidInstance = true; live.connected = false; live.controller.abort(); disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose();
      live.connection = "Session changed";
      // Let the seat owner release its takeover as well as its DOM. Directly
      // clearing the card would leave the preserved normal draft hidden.
      window.SnowAttention?.render({input: null, permission: null}, attentionState());
      liveError("This runtime was replaced in another browser. Your draft is kept, but controls are disabled. Use Review / reload workspace to inspect the new session before continuing.");
      updateControls(); return;
    }
    if (!Number.isSafeInteger(snapshot.revision) || snapshot.revision < live.revision || snapshot.revision < live.minRevision) return;
    live.revision = Number(snapshot.revision); live.status = snapshot.status; live.connected = true; live.connection = "Live"; live.snapshot = snapshot;
    if (snapshot.error) finishWorkspaceOpening();
    if (["failed", "closing"].includes(snapshot.status)) { finishWorkspaceOpening(); disposeLivePanels(); }
    if (live.cancel && snapshot.status === "failed") {
      live.cancel = null;
      live.unknown = true; live.reviewable = false;
      live.actionError = "The worker failed before cancellation was confirmed. Nothing will be retried. Close this runtime to review saved history before explicitly reopening.";
    }
    // A newer authoritative token also proves the old turn is over when an
    // intermediate idle SSE snapshot was coalesced away. Retire only that
    // operation: its eventual HTTP result has no authority over this turn.
    if (live.cancel && live.turnCancel && activeStatus(snapshot.status) && snapshot.revision > live.cancel.revision &&
        typeof snapshot.cancel_token === "string" && snapshot.cancel_token && snapshot.cancel_token !== live.cancel.token && (!live.cancel.goalRunID || snapshot.goal?.goal_run_id !== live.cancel.goalRunID)) {
      live.cancel = null;
      if (!live.action && !live.unknown) uncertain.delete(live.project + ":" + live.instance);
    }
    if (live.cancel && snapshot.status === "idle" && !snapshot.goal?.running && !["pending", "running"].includes(snapshot.compaction?.state) && snapshot.revision > live.cancel.revision && !snapshot.cancel_requested) {
      live.cancel.idleObserved = true;
      if (!live.cancel.pending) {
        live.cancel = null;
        if (!live.action && !live.unknown) uncertain.delete(live.project + ":" + live.instance);
      }
    }
    if (live.unknown && !live.action && !live.cancel && snapshot.status === "idle" && !snapshot.goal?.running && !["pending", "running"].includes(snapshot.compaction?.state)) live.reviewable = true;
    liveError(snapshot.error || live.actionError || "");
    const recoveryState = snapshot.recovery?.state;
    window.SnowLiveView?.updateChrome({
      canceled: snapshot.status === "idle" && !snapshot.cancel_requested && recoveryState === "canceled",
      recoveryVisible: snapshot.status === "failed" || snapshot.status === "idle" && ["admission_unknown", "admitted"].includes(recoveryState),
      recoveryMessage: ({admission_unknown: "Prompt admission is unknown. Review saved history before explicitly continuing.", admitted: "Prompt was admitted, but completion was not observed. Admission does not prove it was saved.", completed: "Prompt completion was observed before the worker stopped.", failed: "Prompt failure was observed before the worker stopped.", canceled: "Prompt cancellation was observed before the worker stopped.", rejected: "Prompt was not accepted. No retry was queued."})[recoveryState] || "The worker stopped. Review the saved conversation before explicitly continuing."
    });
    const toolsNotice = $("#live-history-tools-notice");
    if (toolsNotice) toolsNotice.hidden = !snapshot.history_tools_truncated;
    window.SnowScroll?.beforeUpdate();
    renderMessages(snapshot); renderAttention(snapshot); updateControls();
    window.SnowSidebarSessions?.syncCurrent($("#live-session"));
    window.SnowInspection?.refresh?.({project_id: live.project, instance_id: live.instance, session_id: live.session, provider: snapshot.provider, model: snapshot.model});
    window.SnowScroll?.afterUpdate();
  }
  function updateControls() {
    if (!live) return;
    const stopping = !!live.cancel || !!live.snapshot?.cancel_requested;
    const setting = live.action && settingsAction(live.actionName);
    const switching = live.action && live.actionName === "switch";
    $("#live-session").toggleAttribute("data-refreshing", live.connected && !live.invalidInstance && !live.unknown && live.status === "idle" && !!(live.metadata || setting));
    const compacting = live.panelMutation === "compaction" || live.actionName === "compaction-start" || compactionActive();
    // Goals and compaction are not ordinary queueable turns. Keep their status
    // distinct so Queue next never flashes into the composer footer. Independent
    // held-item Copy/Remove controls remain available.
    const queueUI = {connected: live.connected && !live.invalidInstance, status: live.snapshot?.goal?.running || window.SnowGoals?.composerState().enabled ? "goal-running" : compacting ? "compacting" : live.status, busy: !!(live.action || live.cancel || live.metadata || live.panelMutation), unknown: live.unknown, stopping, editing: editDrafts.has(live.key) || !!live.regeneration || reuseDrafts.has(live.key) || contextDraftBusy()};
    window.SnowQueue?.render(live.snapshot, queueUI, false);
    const safe = mutationSafe();
    const ready = live.status === "idle" && safe;
    const activeTurn = activeStatus(live.status) || !!live.snapshot?.goal?.running || compactionActive();
    const showStop = activeTurn && !!live.snapshot || stopping;
    const goalOperation = !!live.snapshot?.goal?.running || ["goal-start", "goal-resume"].includes(live.actionName) || !!live.cancel?.goalRunID;
    const sending = ["prompt", "prompt-content", "message-edit-commit", "message-regenerate-commit"].includes(live.actionName);
    const stopLabel = stopping ? "Stopping…" : goalOperation ? "Stop goal run" : compactionActive() ? "Stop compaction" : "Stop";
    const stopTitle = goalOperation ? "Stop the whole goal run, including gaps between turns" : compactionActive() ? "Stop the whole manual compaction operation" : stopping ? "Stopping: waiting for this turn to stop" : "Stop the current turn";
    live.stopView = {showStop, canStop: canStop(), stopLabel, stopTitle};
    window.SnowGoals?.render(live.snapshot, {...live.stopView, supported: live.snapshot?.goal != null, readable: live.connected && !live.invalidInstance && (!live.action || streamingAdmission(live.actionName)) && !["failed", "closing"].includes(live.status), safe: mutationSafe("goals") && live.status === "idle" && !live.snapshot?.queue?.items?.length && !editDrafts.has(live.key) && !reuseDrafts.has(live.key) && !live.regeneration});
    const goalComposer = window.SnowGoals?.composerState();
    window.SnowLiveView?.updateChrome({connection: live.connection || "Connecting…", connected: live.connected && !live.invalidInstance,
      reload: live.invalidInstance || live.connection === "Login required", unknown: live.unknown,
      reviewDisabled: !live.unknown || !live.connected || !live.reviewable || !!live.action || !!live.cancel || !!live.invalidInstance});
    window.SnowLiveView?.updateControls({...live.stopView,
      turnVisible: live.status === "running" && live.connected && !live.invalidInstance && !compacting,
      sendDisabled: !ready || !!window.SnowComposerContext?.pending() || !!goalComposer?.enabled && !goalComposer.available, sending: sending || !!goalComposer?.busy,
      goalMode: !!goalComposer?.enabled,
      sendLabel: goalComposer?.busy ? "Starting goal" : goalComposer?.enabled ? "Start goal run" : sending ? "Sending message" : editDrafts.has(live.key) ? "Send edited message" : "Send message",
      status: live.unknown ? "Review the unknown request outcome before sending" : !live.connected ? "Updates unavailable · your draft is kept" : stopping ? (goalOperation ? "Stopping the goal run · nothing else will be sent" : "Stopping this turn · nothing else will be sent") : setting ? "Updating setting…" : live.metadata ? "Refreshing choices…" : live.action ? "Sending request…" : ready ? "Ready for your next message" : "Turn in progress · your draft is kept; nothing is queued",
      // This flag keeps routine status screen-reader-only, not admission-safe.
      // Compaction already owns inline progress and the shared Stop button;
      // expanding a second footer moves the entire composer on every phase.
      // Connection and uncertain-outcome warnings must still remain visible.
      statusIdle: !live.unknown && live.connected && (compacting || live.status === "idle" && !activeTurn && (!live.action || setting || switching) && !stopping)});
    updateReuse(ready && !contextDraftBusy()); updateEdit(ready && !goalBlocksHistory() && !contextDraftBusy()); updateRegeneration(ready && !goalBlocksHistory());
    window.SnowComposerContext?.render({safe: ready, editable: !editDrafts.has(live.key) && !reuseDrafts.has(live.key) && !live.regeneration && !live.invalidInstance && !live.snapshot?.permission && !live.snapshot?.input,
      readable: ready && !live.snapshot?.permission && !live.snapshot?.input, status: live.status});
    window.SnowAttention?.render(null, attentionState());
    window.SnowConversation?.render(live.snapshot, {closeDisabled: !safe && !canCloseFailed(), inspectorExpanded: !!$("#project-inspector") && !$("#project-inspector").hidden, safe: safe && !editDrafts.has(live.key) && !live.regeneration, connected: live.connected, verified: live.connected && !live.invalidInstance && !live.unknown, busy: live.action, setting: setting && !live.unknown, invalid: live.invalidInstance, status: live.status});
    const queueView = window.SnowQueue?.render(live.snapshot, queueUI);
    if (queueView) window.SnowLiveView?.updateControls({queue: {enabled: true, hidden: queueView.hidden, disabled: queueView.disabled, label: queueView.label, title: queueView.title, hint: queueView.hint}, ...(queueView.state !== null ? {status: queueView.state} : {})});
    renderLivePanels();
    rememberWorkspaceSession(live.project, live.session);
    if ($("#workspace")?.dataset.session !== live.session) $("#workspace").dataset.session = live.session;
    void consumeWorkspaceIntent();
    window.SnowVersions?.render(live.snapshot, {...live.stopView, supported: live.snapshot?.versions_enabled === true, readable: live.snapshot?.versions_enabled === true && live.connected && !live.invalidInstance && !live.action && !live.cancel && !live.metadata && !["failed", "closing"].includes(live.status), restoreSafe: live.snapshot?.versions_enabled === true && !goalBlocksHistory() && mutationSafe("versions") && live.status === "idle" && !editDrafts.has(live.key) && !reuseDrafts.has(live.key) && !live.regeneration && !live.snapshot?.queue?.items?.length});
  }
  function connectionState(current, kind) {
    if (live !== current || current.invalidInstance) return;
    finishWorkspaceOpening();
    current.connected = false;
    if (workspaceIntent?.project === current.project) workspaceIntent = null;
    if (kind === "closed" || kind === "replaced") {
      current.invalidInstance = true; current.controller.abort(); disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose();
      current.connection = kind === "closed" ? "Runtime closed" : "Session changed";
      liveError(kind === "closed" ? "This runtime was closed. Your draft is kept. Review the workspace before explicitly activating again." : "The session changed in another browser. Your draft is kept. Review the workspace before continuing.");
    } else if (kind === "auth_required") {
      current.connection = "Login required"; current.controller.abort(); disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose();
      liveError("Browser access expired or was revoked. Your draft is kept; sign in again before reviewing this workspace.");
    } else {
      current.connection = kind === "paused" ? "Updates paused" : "Reconnecting…";
      if (kind !== "paused") liveError("Live updates disconnected. Your draft is kept and no action will be retried.");
    }
    updateControls();
  }
  function startUpdates() {
    const current = live;
    if (!current || current.invalidInstance || current.controller.signal.aborted) return;
    if (current.subscription) return;
    if (!window.SnowStream || $("#live-session")?.dataset.streaming !== "true" || current.pollFallback) { poll(); return; }
    const controller = current.controller, instance = current.instance;
    current.subscription = window.SnowStream.open({
      url: runtimeURL("events") + "?instance_id=" + encodeURIComponent(instance), signal: controller.signal,
      snapshot: snapshot => {
        if (live !== current || current.controller !== controller || current.instance !== instance) return;
        // An in-flight switch/action owns its authoritative response. Retain only
        // the newest read, then reconcile after it settles; never replay a POST.
        if (current.action && !streamingAdmission(current.actionName)) current.pendingSnapshot = snapshot;
        else applySnapshot(snapshot);
      },
      state: kind => {
        if (live !== current || current.controller !== controller) return;
        // Own close/switch may end this read before its POST response arrives.
        // Only that response grants replacement authority. Re-read after settle.
        if (current.action && !settingsAction(current.actionName) && !["message-edit-prepare", "message-regenerate-prepare"].includes(current.actionName) && ["closed", "replaced"].includes(kind)) return;
        connectionState(current, kind);
      },
      fallback: () => {
        if (live !== current || current.controller !== controller) return;
        current.subscription = null; current.pollFallback = true; poll();
      }
    });
  }
  async function poll() {
    clearTimeout(pollTimer);
    const current = live;
    if (!current || document.hidden || current.polling || current.invalidInstance) return;
    if (current.action && !streamingAdmission(current.actionName)) { pollTimer = setTimeout(poll, 2000); return; }
    const controller = current.controller, instance = current.instance;
    current.polling = true;
    try {
      const snapshot = await request(runtimeURL(), null, controller.signal, 8000);
      if (live === current && current.controller === controller && current.instance === instance && (!current.action || streamingAdmission(current.actionName))) applySnapshot(snapshot);
    } catch (error) {
      if (live === current && current.controller === controller && !controller.signal.aborted) {
        connectionState(current, [401, 403].includes(error.status) ? "auth_required" : error.status === 404 ? "closed" : "reconnecting");
      }
    } finally { if (current.controller === controller) { current.polling = false; if (live === current && !controller.signal.aborted) pollTimer = setTimeout(poll, 2000); } }
  }
  function setupLive() {
    saveDraft();
    disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose();
    clearTimeout(pollTimer); window.SnowGoals?.dispose(); window.SnowVersions?.dispose(); window.SnowQueue?.dispose(); live?.controller.abort(); live = null;
    window.SnowMessages?.dispose(); window.SnowScroll?.dispose(); window.SnowWidth?.dispose(); window.SnowAttention?.dispose();
    window.SnowConversation?.dispose(); window.SnowLiveView?.dispose();
    const region = $('#live-session[data-runtime="true"]');
    if (!region) return;
    window.SnowLiveView?.init(region);
    const key = region.dataset.project + ":" + region.dataset.session;
    live = {messageRegenerate: region.dataset.messageRegenerateEnabled === "true", messageEdit: region.dataset.messageEditEnabled === "true", turnCancel: region.dataset.turnCancel === "true", draftRevision: 0, project: region.dataset.project, session: region.dataset.session, instance: region.dataset.instance, key, unknown: uncertain.has(region.dataset.project + ":" + region.dataset.instance), connection: "Connecting…", minRevision: 0, status: region.dataset.status, revision: Number(region.dataset.revision), attention: "", connected: false, action: false, controller: new AbortController()};
    if (workspaceIntent?.project === live.project && !workspaceIntent.instance) workspaceIntent.instance = live.instance;
    if (!live.unknown && workspaceIntent?.project === live.project && workspaceIntent.session !== live.session) {
      // This owner is mounted only to validate the deliberate selection. Do not
      // paint its unrelated history or draft while the guarded switch settles.
      // Keep its geometry; errors and Stop confirmation release this cover.
      region.dataset.workspaceOpening = "true";
      const height = region.getBoundingClientRect().height;
      region.hidden = true;
      window.SnowWorkspace?.updateOpening?.({visible: true, minHeight: height});
    }
    if (drafts.has(key)) window.SnowLiveView?.updateDraft(drafts.get(key));
    takeHomeDraft();
    if (!window.SnowScroll) $("#live-stream").scrollTop = $("#live-stream").scrollHeight;
    window.SnowWidth?.init(region);
    window.SnowMessages?.init(region);
    window.SnowAttention?.init({root: region, onLayout: () => window.SnowScroll?.onLayout(), onTakeover: active => attentionTakeover(region, active)});
    window.SnowConversation?.init(region, {
      action: runtimeAction,
      sessions: async target => {
        const current = live;
        if (!current || !mutationSafe()) throw new Error("Unavailable");
        const instance = current.instance, sessions = [];
        current.metadata = true; updateControls();
        try {
          let offset = 0;
          for (let page = 0; page < 4; page++) {
            const data = await request(`/projects/${encodeURIComponent(current.project)}/sidebar-sessions?offset=${offset}`, null, current.controller.signal, 12000, 512 * 1024);
            if (live !== current || current.invalidInstance || current.instance !== instance || data.instance_id !== instance || data.project_id !== current.project || !Array.isArray(data.sessions) || data.sessions.length > 25) throw new Error("Session changed");
            sessions.push(...data.sessions);
            if (!data.available || !data.has_more || sessions.some(item => item.session_id === target)) return {...data, sessions};
            if (!Number.isInteger(data.next_offset) || data.next_offset <= offset || data.next_offset >= 100) throw new Error("Invalid session page");
            offset = data.next_offset;
          }
          throw new Error("Session unavailable");
        } finally { if (live === current) { current.metadata = false; updateControls(); } }
      },
      choices: async () => {
        const current = live;
        if (!current || !mutationSafe()) throw new Error("Unavailable");
        current.metadata = true; updateControls();
        try {
          const data = await settingsRequest(runtimeURL("choices"), {csrf: csrf(), instance_id: current.instance}, current.controller.signal);
          if (live !== current || current.invalidInstance || data.instance_id !== current.instance || data.project_id !== current.project) throw new Error("Session changed");
          return data;
        } finally { if (live === current) { current.metadata = false; updateControls(); } }
      },
      openDialog,
      closeDialog
    });
    setupQueue(); setupVersions(); setupGoals(); setupLivePanels(); setupComposerContext(); updateControls();
    // Establish reader ownership only after the synchronous presentation roots
    // have their initial geometry; mounting chrome is not a manual scroll.
    window.SnowScroll?.init(region, key);
    startUpdates();
  }
  function setupComposerContext() {
    const current = live, controller = current.controller, instance = current.instance, session = current.session;
    const valid = () => live === current && !current.invalidInstance && current.controller === controller && current.instance === instance && current.session === session && !controller.signal.aborted;
    window.SnowComposerContext?.init({root: $("#live-session"), key: current.key, instance,
      replaceText: (value, start, end) => {
        if (!valid()) return false;
        const prompt = $("#live-prompt");
        const next = prompt.value.slice(0, start) + value + prompt.value.slice(end);
        if (!window.SnowLiveView?.updateDraft(next)) return false;
        const caret = start + value.length;
        prompt.setSelectionRange(caret, caret);
        current.draftRevision++; saveDraft(); return true;
      },
      focusPrompt: () => { if (valid()) $("#live-prompt")?.focus({preventScroll: true}); },
      suggestions: state => { if (valid()) window.SnowLiveView?.updateSuggestions(state); },
      changed: () => { if (valid()) { saveDraft(); updateControls(); window.SnowScroll?.afterUpdate(); } },
      error: message => { if (valid()) liveError(message); },
      request: async (kind, fields) => {
        if (!valid() || !mutationSafe() || current.status !== "idle") throw new Error("Context is unavailable in this runtime state");
        if (!["files", "file", "skills"].includes(kind)) throw new Error("Unsupported context request");
        const url = kind === "skills" ? runtimeURL("skills") : `/projects/${encodeURIComponent(current.project)}/inspect/${kind}`;
        const body = kind === "skills" ? {csrf: csrf(), instance_id: instance} : {...fields, csrf: csrf()};
        const data = await request(url, body, controller.signal, 15000);
        if (!valid()) throw new Error("Composer workspace changed");
        if (kind === "skills" && (data.project_id !== current.project || data.instance_id !== instance || data.session_id !== session)) throw new Error("Unverified skills response");
        return data;
      }
    });
  }
  function setupQueue() {
    const current = live;
    window.SnowQueue?.init({
      root: $("#live-session"), session: current.session, validText: validEditText, changed: updateControls,
      draft: () => { const prompt = $("#live-prompt"); return {value: prompt.value, revision: current.draftRevision, start: prompt.selectionStart, end: prompt.selectionEnd, direction: prompt.selectionDirection}; },
      writeDraft: (text, selection) => {
        if (live !== current) return;
        const prompt = $("#live-prompt");
        if (!window.SnowLiveView?.updateDraft(text)) return false;
        current.draftRevision++; saveDraft();
        if (selection) { prompt.focus({preventScroll: true}); prompt.setSelectionRange(selection.start, selection.end, selection.direction); }
      },
      request: async (action = "", fields = {}) => {
        const controller = current.controller, instance = current.instance;
        if (live !== current || current.invalidInstance || controller.signal.aborted) throw new Error("Queue workspace changed");
        const result = await request(runtimeURL(action), action ? {...fields, csrf: csrf(), instance_id: instance} : null, controller.signal, 15000);
        if (live !== current || current.controller !== controller || current.instance !== instance || controller.signal.aborted) throw new Error("Queue workspace changed");
        if (result.project_id !== current.project || result.session_id !== current.session || result.instance_id !== instance || !Number.isSafeInteger(result.revision) || typeof result.status !== "string") throw new Error("Unverified queue response");
        if (action && (!result.queue || result.queue.token !== fields.queue_token || !Number.isSafeInteger(result.queue.revision) || result.queue.revision <= Number(fields.queue_revision))) throw new Error("Unverified queue acknowledgement");
        applySnapshot(result); return result;
      }
    });
  }
  // Panels receive typed callbacks only. Identity is captured once so even a
  // delayed read cannot retarget a replacement worker sharing the same DOM.
  function panelIdentity(current) {
    return Object.freeze({project_id: current.project, session_id: current.session, instance_id: current.instance});
  }
  async function panelRead(current, identity, action, fields, signal) {
    const controller = current.controller;
    if (live !== current || current.project !== identity.project_id || current.session !== identity.session_id || current.instance !== identity.instance_id || current.invalidInstance || controller.signal.aborted || signal.aborted) throw new Error("Panel runtime changed");
    const data = await request(runtimeURL(action), {...fields, ...{csrf: csrf(), instance_id: identity.instance_id, session_id: identity.session_id}}, AbortSignal.any([controller.signal, signal]), 15000, 4 * 1024 * 1024);
    if (live !== current || current.controller !== controller || current.instance !== identity.instance_id || controller.signal.aborted || signal.aborted || data.project_id !== identity.project_id || data.session_id !== identity.session_id || data.instance_id !== identity.instance_id) throw new Error("Unverified panel response");
    return data;
  }
  function setupVersions() {
    const current = live, identity = panelIdentity(current);
    current.panelMutation = null;
    window.SnowVersions?.init({
      root: $("#live-session"), identity, openDialog, closeDialog,
      list: (cursor, signal) => panelRead(current, identity, "versions-list", cursor ? {cursor} : {}, signal),
      preview: (target, cursor, signal) => panelRead(current, identity, "version-preview", {branch_id: target.branch_id, tip_id: target.tip_id, ...(cursor ? {cursor} : {})}, signal),
      prepare: (target, origin, signal) => panelRead(current, identity, "version-restore-prepare", {branch_id: target.branch_id, tip_id: target.tip_id, current_branch_id: origin.current_branch_id, current_tip_id: origin.current_tip_id}, signal),
      commit: token => live === current && current.instance === identity.instance_id && current.panelMutation === "versions" ? runtimeAction("version-restore-commit", {session_id: identity.session_id, restore_token: token}) : Promise.resolve(false),
      restoreState: phase => { if (live === current && current.instance === identity.instance_id) { if (phase ? !current.panelMutation || current.panelMutation === "versions" : current.panelMutation === "versions") current.panelMutation = phase ? "versions" : null; updateControls(); } }
    });
  }
  function setupGoals() {
    const current = live, identity = panelIdentity(current);
    window.SnowGoals?.init({root: $("#live-session"), identity, openDialog, closeDialog, validText: validEditText,
      changed: () => { if (live === current) updateControls(); },
      focusPrompt: () => { if (live === current) $("#live-prompt")?.focus({preventScroll: true}); },
      notice: message => { if (live === current) liveError(message); },
      inspect: async (branch, signal) => {
        const result = await panelRead(current, identity, "goal-inspect", {branch_id: branch}, signal);
        if (!window.SnowGoals.validGoal(result.goal, identity.session_id) || result.goal.branch_id !== branch || !Number.isSafeInteger(result.revision) || result.revision <= 0) throw new Error("Unverified goal inspection");
        applySnapshot(result); return result;
      },
      reserve: active => {
        if (live !== current || current.instance !== identity.instance_id || current.session !== identity.session_id || current.invalidInstance) return false;
        if (active && current.panelMutation && current.panelMutation !== "goals") return false;
        if (active || current.panelMutation === "goals") current.panelMutation = active ? "goals" : null;
        updateControls(); return true;
      },
      run: (action, fields, expectedRevision) => live === current && current.instance === identity.instance_id && Number.isSafeInteger(expectedRevision) && expectedRevision > 0 && expectedRevision === current.revision && current.panelMutation === "goals" && ["goal-start", "goal-resume"].includes(action) ? runtimeAction(action, {...fields, expected_revision: String(expectedRevision)}) : Promise.resolve(false)
    });
  }
  // A controller's immutable scope outlives its reads, never the workspace.
  // This token also fences mutations, whose HTTP waiters are intentionally not
  // aborted by dialog close or read subscription replacement.
  function panelCurrent(current, identity, lifetime) {
    return live === current && !current.invalidInstance && current.panels === lifetime && !lifetime.signal.aborted &&
      current.project === identity.project_id && current.instance === identity.instance_id && current.session === identity.session_id;
  }
  function disposeLivePanels() {
    window.SnowComposerContext?.dispose();
    if (live?.panels) { live.panels.abort(); live.panels = null; }
    window.SnowReasoning?.dispose(); window.SnowHistoryControls?.dispose(); window.SnowCompaction?.dispose(); window.SnowSteer?.dispose();
    if (live && ["reasoning", "history-controls", "compaction", "steer"].includes(live.panelMutation)) live.panelMutation = null;
  }
  function renderLivePanels() {
    if (!live?.panels || !live.snapshot) return;
    const s = live.snapshot, readable = live.connected && !live.invalidInstance && !["failed", "closing"].includes(live.status);
    const editing = editDrafts.has(live.key) || reuseDrafts.has(live.key) || !!live.regeneration;
    const idle = owner => mutationSafe(owner) && live.status === "idle" && !goalBlocksHistory() && !editing && !s.queue?.items?.length && !s.permission && !s.input;
    window.SnowReasoning?.render(s, {readable: readable && s.reasoning_enabled === true && !live.action && !live.cancel && !live.metadata, safe: idle("reasoning") && s.reasoning_enabled === true});
    window.SnowHistoryControls?.render(s, {supported: s.history_control_enabled === true, safe: idle("history-controls") && s.history_control_enabled === true});
    window.SnowCompaction?.render(s, {...live.stopView, supported: s.compaction_enabled === true, readable: readable && s.compaction_enabled === true, safe: idle("compaction") && s.compaction_enabled === true});
    window.SnowSteer?.render(s, {connected: readable, status: s.goal?.running || compactionActive() || s.permission || s.input ? "unavailable" : live.status,
      busy: !!(live.action || live.cancel || live.metadata || live.panelMutation && live.panelMutation !== "steer" || window.SnowQueue?.blocking()),
      unknown: live.unknown, stopping: !!live.cancel || !!s.cancel_requested, editing});
  }
  function setupLivePanels() {
    disposeLivePanels();
    const current = live, identity = panelIdentity(current), lifetime = new AbortController(); current.panels = lifetime;
    const bound = () => panelCurrent(current, identity, lifetime);
    const reserve = owner => active => {
      if (!bound()) return false;
      if (active && current.panelMutation && current.panelMutation !== owner) return false;
      if (active || current.panelMutation === owner) current.panelMutation = active ? owner : null;
      updateControls(); return true;
    };
    const base = {root: $("#live-session"), identity, openDialog, closeDialog};
    window.SnowReasoning?.init({...base, reserve: reserve("reasoning"),
      inspect: async signal => {
        if (!bound()) throw new Error("Retired panel");
        const result = await panelRead(current, identity, "reasoning-inspect", {}, AbortSignal.any([signal, lifetime.signal]));
        if (!bound() || !window.SnowReasoning.valid(result, identity)) throw new Error("Unverified reasoning inspection");
        // A preference DTO is not a runtime snapshot. Fetch real authority if
        // inspection advanced the revision; never manufacture status/history.
        if (result.revision > current.revision) await readPanelSnapshot(current, identity, lifetime, signal);
        if (!bound() || current.revision < result.revision) throw new Error("Reasoning snapshot not yet verified");
        return result;
      },
      set: fields => panelMutation(current, identity, lifetime, "reasoning", "reasoning-set", fields)
    });
    window.SnowHistoryControls?.init({...base, reserve: reserve("history-controls"),
      mutate: (action, fields) => panelMutation(current, identity, lifetime, "history-controls", action, fields),
      refreshInventory: () => { if (bound()) refreshPanelInventory(current, identity, lifetime); }
    });
    window.SnowCompaction?.init({...base, reserve: reserve("compaction"),
      commit: fields => panelMutation(current, identity, lifetime, "compaction", "compaction-start", fields)
    });
    window.SnowSteer?.init({...base, session: identity.session_id, validText: validEditText,
      changed: () => { if (bound()) reserve("steer")(!!window.SnowSteer?.blocking()); },
      request: (action, fields) => panelMutation(current, identity, lifetime, "steer", action, fields)
    });
  }
  async function readPanelSnapshot(current, identity, lifetime, signal = lifetime.signal) {
    const controller = current.controller;
    if (!panelCurrent(current, identity, lifetime)) throw new Error("Retired panel");
    const snapshot = await request(`/projects/${encodeURIComponent(identity.project_id)}/runtime`, null, AbortSignal.any([signal, lifetime.signal, controller.signal]), 8000, 4 * 1024 * 1024);
    if (!panelCurrent(current, identity, lifetime) || current.controller !== controller || snapshot.project_id !== identity.project_id || snapshot.instance_id !== identity.instance_id || snapshot.session_id !== identity.session_id || !Number.isSafeInteger(snapshot.revision) || typeof snapshot.status !== "string") throw new Error("Unverified runtime read");
    applySnapshot(snapshot); return snapshot;
  }
  async function panelMutation(current, identity, lifetime, owner, action, fields) {
    const actions = {reasoning: ["reasoning-set"], "history-controls": ["history-branch-fork", "history-session-fork", "history-branch-rename"], compaction: ["compaction-start"], steer: ["steer"]};
    if (!panelCurrent(current, identity, lifetime) || !actions[owner]?.includes(action) || current.panelMutation !== owner || !mutationSafe(owner) || fields.session_id !== identity.session_id) return false;
    const capability = {reasoning: "reasoning_enabled", "history-controls": "history_control_enabled", compaction: "compaction_enabled"}[owner];
    if (capability && current.snapshot?.[capability] !== true) return false;
    if (owner === "steer") {
      const steer = current.snapshot?.steer;
      if (current.status !== "running" || compactionActive() || current.snapshot?.goal?.running || current.snapshot?.permission || current.snapshot?.input || !steer?.can_steer || !steer.live_steer_token || fields.live_steer_token !== steer.live_steer_token || fields.steer_revision !== String(steer.revision)) return false;
    } else if (current.status !== "idle" || goalBlocksHistory() || current.snapshot?.queue?.items?.length || !Number.isSafeInteger(current.revision) || current.revision <= 0 || fields.expected_revision !== String(current.revision)) return false;
    if (editDrafts.has(current.key) || reuseDrafts.has(current.key) || current.regeneration) return false;
    // Branch fork uses exactly the restore replacement path: fresh instance,
    // old read retirement, same-session draft, then normal subsequent SSE.
    if (action === "history-branch-fork") return runtimeAction(action, {...fields});
    // Preserve only this already-authoritative root's Stop capability if the
    // steering receipt is lost. A later root/instance must not inherit it.
    const steerStopScope = owner === "steer" && typeof current.snapshot?.cancel_token === "string" && current.snapshot.cancel_token &&
      current.snapshot.project_id === identity.project_id && current.snapshot.instance_id === identity.instance_id && current.snapshot.session_id === identity.session_id
      ? Object.freeze({...identity, cancel_token: current.snapshot.cancel_token}) : null;
    const guardKey = identity.project_id + ":" + identity.instance_id;
    current.action = true; current.actionName = action; current.actionError = ""; current.reviewable = false;
    uncertain.set(guardKey, true); updateControls();
    try {
      const result = await request(`/projects/${encodeURIComponent(identity.project_id)}/runtime/${action}`, {...fields, csrf: csrf(), instance_id: identity.instance_id}, undefined, 15000, 4 * 1024 * 1024);
      if (!panelCurrent(current, identity, lifetime)) return false;
      if (result.project_id !== identity.project_id || result.instance_id !== identity.instance_id || result.session_id !== identity.session_id || !Number.isSafeInteger(result.revision) || result.revision <= 0) throw new Error("Unverified panel receipt");
      if (owner === "reasoning") {
        if (!window.SnowReasoning.valid(result, identity) || result.revision <= Number(fields.expected_revision) || result[fields.field] !== fields.value || ["branch_id", "tip_id", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity"].some(key => result[key] !== (key === fields.field ? fields.value : fields[key]))) throw new Error("Unverified session preference update");
        await readPanelSnapshot(current, identity, lifetime);
        if (current.revision < result.revision) throw new Error("Updated runtime not yet verified");
      } else if (owner === "history-controls") {
        if (result.revision < Number(fields.expected_revision) || result.branch_id !== fields.branch_id || result.tip_id !== fields.tip_id || result.name !== fields.name || action === "history-session-fork" && (typeof result.child_session_id !== "string" || !result.child_session_id || result.child_session_id.length > 256 || /[\u0000-\u001f\u007f]/.test(result.child_session_id) || result.child_session_id === identity.session_id)) throw new Error("Unverified history metadata");
        // Metadata is not replacement authority. Read the same live instance.
        await readPanelSnapshot(current, identity, lifetime);
        if (current.revision < result.revision) throw new Error("Updated runtime not yet verified");
      } else {
        if (typeof result.status !== "string") throw new Error("Missing runtime receipt");
        if (owner === "compaction" && !window.SnowCompaction.validACK(result, identity, fields)) throw new Error("Unverified compaction receipt");
        if (owner === "steer") {
          const ack = result.steer_ack;
          if (!ack || ack.request_id !== fields.request_id || ack.live_steer_token !== fields.live_steer_token || typeof ack.item_id !== "string" || !ack.item_id || ack.item_id.length > 256 || /[\u0000-\u001f\u007f]/.test(ack.item_id) || ack.status !== "accepted") throw new Error("Unverified steering receipt");
        }
        // ACKs may be clone-only snapshots older than fast completion/delivery.
        // Never downgrade the latest SSE state, including equal revisions.
        if (result.revision > current.revision) applySnapshot(result);
      }
      if (!panelCurrent(current, identity, lifetime)) return false;
      if (owner === "steer") current.unknownSteerStop = null;
      if (!current.cancel && !current.unknown) uncertain.delete(guardKey);
      return result;
    } catch (error) {
      if (panelCurrent(current, identity, lifetime)) {
        current.unknown = true; current.reviewable = false;
        if (owner === "steer") current.unknownSteerStop = steerStopScope;
        current.actionError = "Panel request outcome unknown. Your draft is kept. Read the current state and review before any new explicit action; nothing is retried automatically.";
        liveError(current.actionError);
        if ([401, 403].includes(error.status)) connectionState(current, "auth_required");
      }
      return false;
    } finally {
      if (live === current && current.instance === identity.instance_id && current.session === identity.session_id) {
        current.action = false; current.actionName = ""; current.pendingSnapshot = null;
        updateControls();
        // Keep root cancellation usable during compaction/steering. Reconcile
        // through a bounded read, not subscription teardown or another POST.
        if (panelCurrent(current, identity, lifetime)) readPanelSnapshot(current, identity, lifetime).catch(() => {});
      }
    }
  }
  async function refreshPanelInventory(current, identity, lifetime) {
    const panel = $("[data-history-controls]", $("#live-session")); if (!panel) return;
    if (!panelCurrent(current, identity, lifetime)) return;
    if (current.action || current.cancel || current.metadata) { window.SnowHistoryControls?.renderInventory({text: "Refresh saved conversations after the current action finishes.", rows: []}); return; }
    current.metadata = true; updateControls();
    window.SnowHistoryControls?.renderInventory({text: "Refreshing saved conversations…", rows: []});
    try {
      const result = await request(`/projects/${encodeURIComponent(identity.project_id)}/runtime/choices`, {csrf: csrf(), instance_id: identity.instance_id}, lifetime.signal, 15000, 2 * 1024 * 1024);
      if (!panelCurrent(current, identity, lifetime)) return;
      if (result.project_id !== identity.project_id || result.instance_id !== identity.instance_id || result.sessions_available !== true || !Array.isArray(result.sessions) || result.sessions.length > 1000) throw new Error("Unverified inventory");
      const rows = [];
      for (const session of result.sessions) {
        if (typeof session.session_id !== "string" || !session.session_id || session.session_id.length > 256 || typeof session.name !== "string" || session.name.length > 512) throw new Error("Unverified conversation");
        if (session.session_id === identity.session_id) continue;
        rows.push({name: session.name, url: projectLocation(identity.project_id) + "&session=" + encodeURIComponent(session.session_id)});
      }
      window.SnowHistoryControls?.renderInventory({text: (result.sessions_truncated ? "Bounded saved-conversation inventory refreshed; some entries are omitted. " : "Saved conversations refreshed. ") + "Opening one is a separate explicit action; the current conversation is unchanged.", rows});
    } catch (_) { if (panelCurrent(current, identity, lifetime)) window.SnowHistoryControls?.renderInventory({text: "Saved-conversation inventory could not be refreshed. The detached child is not opened automatically. Reload the workspace to inspect saved conversations.", rows: []}); }
    finally { if (live === current && current.instance === identity.instance_id && current.session === identity.session_id) { current.metadata = false; updateControls(); } }
  }
  function validGoalRunACK(result, current, fields, action, revision) {
    const goal = result.goal, receipt = result.goal_run_ack;
    // Native creation trims Unicode White_Space (not JavaScript's FEFF-aware trim).
    if (action === "goal-start" && (!goal || typeof fields.objective !== "string" || goal.objective !== fields.objective.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "") || (goal.token_budget ?? null) !== (fields.token_budget == null ? null : Number(fields.token_budget)) || goal.goal_id === fields.expected_goal_id)) return false;
    return result.project_id === current.project && result.instance_id === current.instance && result.session_id === current.session && Number.isSafeInteger(result.revision) && result.revision > revision && window.SnowGoals?.validGoal(goal, current.session) && goal.branch_id === fields.branch_id && !!receipt && receipt.session_id === current.session && receipt.branch_id === fields.branch_id && typeof receipt.goal_run_id === "string" && !!receipt.goal_run_id && receipt.goal_run_id.length <= 256 && !/[\u0000-\u001f\u007f]/.test(receipt.goal_run_id) && receipt.goal_id === goal.goal_id && !!goal.goal_id && (action !== "goal-resume" || goal.goal_id === fields.expected_goal_id) && (!goal.running || goal.goal_run_id === receipt.goal_run_id && typeof result.cancel_token === "string" && !!result.cancel_token) && ["idle", "running", "permission", "input"].includes(result.status);
  }
  async function runtimeAction(action, fields = {}) {
    const forking = action === "history-branch-fork", restoring = action === "version-restore-commit" || forking, goalRun = ["goal-start", "goal-resume"].includes(action);
    if (!mutationSafe(forking ? "history-controls" : restoring ? "versions" : goalRun ? "goals" : ["permission", "input"].includes(action) ? "attention" : undefined) && !(action === "close" && canCloseFailed())) return false;
    const current = live, initialRevision = live.revision, guardKey = current.project + ":" + current.instance;
    const editing = action === "message-edit-commit", regenerating = action === "message-regenerate-commit", replacing = editing || regenerating || restoring;
    if (replacing && goalBlocksHistory()) return false;
    if (current.regeneration && !regenerating && !["close", "permission", "input"].includes(action)) return false;
    if (editDrafts.has(current.key) && !editing && !["close", "permission", "input"].includes(action)) return false;
    if (action === "switch" || replacing) {
      current.controller.abort(); current.subscription = null; current.pendingSnapshot = null;
      current.controller = new AbortController(); current.polling = false; clearTimeout(pollTimer);
      // Retire old reads now, not only after success. Failed transitions retain
      // this session's attachment draft and need callbacks bound to its new read
      // lifetime after explicit unknown-outcome review.
      setupComposerContext();
    }
    const dispatchController = current.controller, dispatchInstance = current.instance;
    const setting = settingsAction(action);
    let settingReconciled = false, switchReconciled = false;
    current.action = true; current.actionName = action; current.actionError = ""; current.reviewable = false;
    uncertain.set(guardKey, true); updateControls();
    try {
      const values = {...fields, csrf: csrf(), instance_id: current.instance};
      const result = setting ? await settingsRequest(runtimeURL(action), values, dispatchController.signal) : await request(runtimeURL(action), values, undefined, 15000, forking ? 4 * 1024 * 1024 : 0);
      if (live !== current || current.invalidInstance || current.instance !== dispatchInstance || current.controller !== dispatchController || dispatchController.signal.aborted) return false;
      if (goalRun && !validGoalRunACK(result, current, fields, action, initialRevision)) throw new Error("Unverified goal run acknowledgement");
      if (action === "switch" || replacing) {
        if (result.project_id !== current.project || typeof result.instance_id !== "string" || !result.instance_id || result.instance_id === current.instance || typeof result.session_id !== "string" || !result.session_id || typeof result.status !== "string" || action === "switch" && fields.session_id && result.session_id !== fields.session_id) throw new Error("Invalid switch response");
        if (action === "switch" && (!Number.isSafeInteger(result.revision) || result.revision < 0 || !Array.isArray(result.messages) || result.status !== "idle" || result.permission || result.input || result.cancel_requested)) throw new Error("Unverified switch snapshot");
        if (replacing && (result.session_id !== current.session || !Number.isSafeInteger(result.revision) || result.revision < 0 || !Array.isArray(result.messages))) throw new Error("Invalid edit response");
        if (restoring && (result.status !== "idle" || result.permission || result.input || result.cancel_requested || result.queue?.items?.length)) throw new Error("Restore did not return an idle history-only snapshot");
        saveDraft(); disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose(); window.SnowScroll?.dispose(); window.SnowAttention?.dispose();
        current.controller.abort(); clearTimeout(pollTimer); current.subscription = null; current.pendingSnapshot = null;
        current.controller = new AbortController(); current.polling = false;
        current.instance = result.instance_id; current.session = result.session_id;
        current.key = current.project + ":" + current.session;
        current.invalidInstance = false; current.revision = 0; current.minRevision = 0; current.attention = "";
        const region = $("#live-session");
        region.dataset.instance = current.instance; region.dataset.session = current.session;
        window.SnowProcesses?.init();
        window.SnowInspection?.init();
        window.SnowInspection?.refresh?.({project_id: current.project, instance_id: current.instance, session_id: current.session, provider: result.provider, model: result.model});
        $('input[name="instance_id"]', $("#live-composer")).value = current.instance;
        if (!replacing) window.SnowLiveView?.updateDraft(drafts.get(current.key) || "");
        window.SnowAttention?.init({root: region, onLayout: () => window.SnowScroll?.onLayout(), onTakeover: active => attentionTakeover(region, active)});
        setupQueue(); setupVersions(); setupGoals(); setupLivePanels(); setupComposerContext(); applySnapshot(result);
        // Regeneration removes the confirmation's return control. Once that
        // removal is authoritative, repair only a stranded body focus; never
        // move focus away from concurrent typing or another surviving control.
        if (regenerating && current.regeneration?.sourceControl && !current.regeneration.sourceControl.isConnected && document.activeElement === document.body) {
          const prompt = $("#live-prompt");
          if (prompt?.isConnected && prompt.getClientRects().length && !prompt.closest("[inert]")) {
            const {selectionStart: start, selectionEnd: end, selectionDirection: direction} = prompt;
            prompt.focus({preventScroll: true});
            if (prompt.selectionStart !== start || prompt.selectionEnd !== end || prompt.selectionDirection !== direction) prompt.setSelectionRange(start, end, direction);
          }
        }
        history.replaceState(history.state, "", projectLocation(current.project) + "&live=1");
        window.SnowScroll?.init(region, current.key);
        switchReconciled = action === "switch" && current.connected && !current.unknown;
      } else {
        if (setting && (!result.project_id || result.status !== "idle" || result.permission || result.input || !Number.isSafeInteger(result.revision) || result.revision <= initialRevision)) throw new Error("Unverified settings snapshot");
        if (action === "model" && (result.provider !== fields.provider || result.model !== fields.model)) throw new Error("Unverified model response");
        if (action === "rename" && result.session_name !== fields.name.trim()) throw new Error("Unverified name response");
        if (action === "mode" && (result.mode !== fields.mode || !["default", "plan"].includes(result.mode) || result.status !== "idle" || result.permission || result.input || !Number.isSafeInteger(result.revision) || result.revision <= initialRevision || !result.project_id)) throw new Error("Unverified collaboration mode response");
        if (action === "permission-mode" && (!result.project_id || result.permission_mode !== fields.mode || result.status !== "idle" || result.permission || result.input || result.session_id !== current.session || result.instance_id !== current.instance || !Number.isSafeInteger(result.revision) || result.revision <= initialRevision)) throw new Error("Unverified permission policy response");
        if (result.project_id) {
          if (result.project_id !== current.project || result.instance_id !== current.instance || result.session_id !== current.session || !Number.isSafeInteger(result.revision) || typeof result.status !== "string") throw new Error("Mismatched action response");
        } else if (result.success !== true) throw new Error("Unverified action acknowledgement");
        if (["prompt", "prompt-content", "permission", "input"].includes(action)) current.minRevision = initialRevision + 1;
        // This is a verified idle setting snapshot, not prompt admission. Keep
        // the healthy stream; a real transport loss still takes reconciliation.
        settingReconciled = setting && current.connected && !current.unknown;
        if (result.project_id) applySnapshot(result);
      }
      if (!current.cancel && !current.unknown) uncertain.delete(guardKey);
      return result;
    } catch (error) {
      if (live === current) {
        current.unknown = true; current.reviewable = false;
        current.actionError = "Request outcome unknown. Review the conversation before trying again. Nothing will be retried automatically.";
        if ([401, 403].includes(error.status)) { current.connected = false; current.connection = "Login required"; }
        liveError(current.actionError);
      }
      return false;
    } finally {
      if (live === current) {
        const pending = current.pendingSnapshot;
        current.action = false; current.actionName = ""; current.pendingSnapshot = null;
        if (switchReconciled && !current.invalidInstance && !current.unknown) {
          // Switch returns the complete, instance-bound idle history snapshot.
          // Keep that verified seat stable while subscribing to its new nonce;
          // a genuine transport failure still takes the ordinary error path.
          updateControls(); clearTimeout(pollTimer); startUpdates();
        } else if (settingReconciled && !current.invalidInstance && !dispatchController.signal.aborted && !current.unknown) {
          // Reconcile any later SSE revision, including replacement identity,
          // without reconnecting, refetching history or replaying the action.
          if (pending) applySnapshot(pending);
          updateControls();
          if (!current.subscription && !current.invalidInstance) { clearTimeout(pollTimer); pollTimer = setTimeout(startUpdates, 2000); }
        } else {
          current.subscription?.close(); current.subscription = null;
          // A command acknowledgement is not a current runtime snapshot. In
          // particular a fast turn may already be idle, while a delayed POST may
          // have outlived several streaming updates. Keep mutation controls inert
          // until the fresh bound read reconciles the authoritative state.
          current.connected = false;
          if (!current.invalidInstance && current.connection !== "Login required") current.connection = "Synchronizing…";
          updateControls(); clearTimeout(pollTimer); pollTimer = setTimeout(startUpdates, 0);
        }
      }
    }
  }
  async function cancelTurn() {
    if (!canStop()) return false;
    const current = live, instance = current.instance, token = current.snapshot.cancel_token;
    const operation = {token, goalRunID: current.snapshot.goal?.goal_run_id || "", revision: current.revision, pending: true, idleObserved: false};
    const guardKey = current.project + ":" + instance;
    current.cancel = operation; current.reviewable = false;
    uncertain.set(guardKey, true); updateControls();
    try {
      const result = await request(runtimeURL(current.turnCancel ? "cancel" : "abort"), {
        csrf: csrf(), instance_id: instance, ...(current.turnCancel ? {cancel_token: token} : {})
      }, current.controller.signal, 15000);
      if (live !== current || current.instance !== instance || current.cancel !== operation || current.invalidInstance) return false;
      if (result.project_id) {
        if (result.project_id !== current.project || result.instance_id !== instance || result.session_id !== current.session ||
            !Number.isSafeInteger(result.revision) || result.revision < operation.revision || typeof result.status !== "string" ||
            (current.turnCancel && activeStatus(result.status) && (result.cancel_token !== token || result.cancel_requested !== true))) {
          throw new Error("Unverified cancellation response");
        }
        applySnapshot(result);
      } else if (result.success !== true || Object.keys(result).length !== 1) {
        throw new Error("Unverified cancellation acknowledgement");
      }
      // An acknowledgement alone never synthesizes idle or releases the guard.
      return true;
    } catch (error) {
      if (live === current && current.cancel === operation) {
        current.unknown = true; current.reviewable = false;
        current.actionError = "Stop outcome unknown. Nothing will be retried. Wait for a current idle snapshot and review the conversation before sending again.";
        if ([401, 403].includes(error.status)) { current.connected = false; current.connection = "Login required"; }
        liveError(current.actionError);
      }
      return false;
    } finally {
      operation.pending = false;
      if (live === current && current.cancel === operation) {
        if (operation.idleObserved) {
          current.cancel = null;
          if (current.unknown && current.connected && !current.action && !current.invalidInstance) current.reviewable = true;
        }
        if (!current.action && !current.cancel && !current.unknown) uncertain.delete(guardKey);
        updateControls();
        // Re-read only. Never retry cancellation or abort the prompt fetch.
        if (!current.subscription) { clearTimeout(pollTimer); pollTimer = setTimeout(startUpdates, 0); }
      }
    }
  }
  function reloadWorkspace(project, session = "") {
    saveDraft();
    const location = projectLocation(project) + (session ? `&session=${encodeURIComponent(session)}` : "&live=1");
    if (window.htmx) window.htmx.ajax("GET", location, {target: "#workspace", swap: "outerHTML"});
    else liveError("Workspace navigation is unavailable. Copy any unsent draft before reloading this page.");
  }
  function projectLocation(project) { return `/?view=projects&project=${encodeURIComponent(project)}`; }
  function setInspector(open) {
    const panel = $("#project-inspector");
    if (!panel) return;
    panel.hidden = !open;
    const trigger = $('.workspace-heading [data-inspector-toggle]');
    if (live) updateControls();
    else window.SnowWorkspace?.updateInspector?.(open);
    if (open) {
      window.SnowInspection?.opened(); $("[data-inspector-toggle]", panel)?.focus();
      if (narrow.matches) {
        panel.setAttribute("role", "dialog"); panel.setAttribute("aria-modal", "true");
        for (const selector of [".conversation-pane", ".workspace-heading", ".topbar"]) { const node = $(selector); if (node) node.inert = true; }
      }
    } else {
      panel.removeAttribute("role"); panel.removeAttribute("aria-modal");
      for (const selector of [".conversation-pane", ".workspace-heading", ".topbar"]) { const node = $(selector); if (node) node.inert = false; }
      trigger?.focus();
    }
  }
  function inspectProject(project, remove = false) {
    const panel = $("#project-inspector");
    if (!panel || !project || panel.dataset.project !== project) return;
    requestNav(false, false);
    // Select before opening so this presentation-only entrypoint cannot start
    // the Files tab's lazy filesystem read while routing to Project settings.
    if (!window.SnowInspection?.select("project", project)) return;
    setInspector(true);
    const removal = $(".remove-project", panel);
    if (remove && removal) { removal.open = true; $("summary", removal)?.focus(); }
    else $("#inspection-tab-project", panel)?.focus();
  }
  document.addEventListener("snow:inspect-project", event => {
    inspectProject(event.detail?.project, event.detail?.remove === true);
  });

  document.addEventListener("click", async event => {
    const button = event.target.closest("button,[data-folder-open]");
    if (!button) return;
    if (button.hasAttribute("data-home-draft-discard")) { homeDraft = {text: "", project: "", name: "", pending: false}; syncHomeDraft(); return; }
    if (button.hasAttribute("data-home-draft-use")) { if (takeHomeDraft(true)) { syncHomeDraft(); updateControls(); $("#live-prompt")?.focus(); } return; }
    if (button.matches("[data-sidebar-restore], [data-sidebar-collapse]") && !button.closest("#project-navigation")) window.SnowShell?.collapse();
    else if (button.matches("#theme-toggle")) setTheme(root.dataset.theme === "dark" ? "light" : "dark");
    else if (button.matches("[data-nav-toggle]")) requestNav(true);
    else if (button.matches("[data-nav-close]")) requestNav(false);
    else if (button.matches("[data-folder-open]")) { folderState = null; window.SnowWorkspace?.updateFolder({folders: [], path: "", parent: "", hasMore: false, canSelect: false, busy: false, error: "", status: ""}); openDialog($("#folder-picker"), button); browseFolders($("#project-path")?.value.trim() || ""); }
    else if (button.matches("[data-folder-close]")) closeDialog($("#folder-picker"));
    else if (button.matches("[data-folder-home]")) browseFolders();
    else if (button.matches("[data-folder-up]") && folderState) browseFolders(folderState.parent);
    else if (button.matches("[data-folder-path]")) browseFolders(button.dataset.folderPath);
    else if (button.matches("[data-folder-more]") && folderState) browseFolders(folderState.path, folderState.next_offset, true);
    else if (button.matches("[data-folder-select]") && folderState) { window.SnowWorkspace?.setProjectPath(folderState.path); closeDialog($("#folder-picker")); $("#project-path").focus(); }
    else if (button.matches("[data-inspector-toggle]")) {
      const panel = $("#project-inspector"); if (panel) setInspector(panel.hidden);
    } else if (button.matches("#jump-latest")) { if (window.SnowScroll) window.SnowScroll.follow(); else { const stream = $("#live-stream"); stream.scrollTop = stream.scrollHeight; button.hidden = true; } }
    else if (button.matches("[data-runtime-reload]")) reloadWorkspace(live.project);
    else if (button.matches("[data-runtime-reviewed]")) {
      if (live?.unknown && live.connected && live.reviewable && !live.action && !live.cancel && !live.invalidInstance) {
        uncertain.delete(live.project + ":" + live.instance); live.unknown = false; live.unknownSteerStop = null; live.actionError = ""; liveError(live.snapshot?.error || ""); updateControls();
      }
    }
    else if (button.matches("[data-runtime-abort]")) {
      // Send and Stop share a position. The continuation of a Send multi-click
      // must not become a fresh cancellation decision when that button changes.
      // Single clicks and keyboard/programmatic activation (detail 0) still stop.
      if (event.detail > 1) { event.preventDefault(); return; }
      await cancelTurn();
    }
    else if (button.matches("[data-message-regenerate]")) await prepareRegeneration(button);
    else if (button.matches("[data-message-regenerate-cancel], [data-message-regenerate-dismiss]")) cancelRegeneration();
    else if (button.matches("[data-message-regenerate-confirm]")) await commitRegeneration();
    else if (button.matches("[data-message-edit]")) await prepareEdit(button);
    else if (button.matches("[data-message-edit-cancel]")) cancelEdit();
    else if (button.matches("[data-message-reuse]")) reuseMessage(button);
    else if (button.matches("[data-message-reuse-cancel]")) cancelReuse();
    else if (button.matches("[data-processes-open]")) {
      openDialog($("#processes-dialog"), button);
      $("#managed-processes").open = true;
    }
    else if (button.matches("[data-processes-close]")) closeDialog($("#processes-dialog"));
    else if (button.matches("[data-runtime-close]")) openDialog($("#runtime-close-dialog"), button);
    else if (button.matches("[data-runtime-close-cancel]")) closeDialog($("#runtime-close-dialog"));
    else if (button.matches("[data-runtime-close-confirm]")) {
      const project = live?.project, session = live?.session; closeDialog($("#runtime-close-dialog"));
      if (await runtimeAction("close")) reloadWorkspace(project, session);
    } else if (button.matches("[data-permission]")) { await runtimeAction("permission", {request_id: button.dataset.requestId, decision: button.dataset.permission}); }
  });
  async function registerProject(form, event) {
    if (registeringProjects.has(form) || !form.isConnected) return;
    if (!window.htmx?.ajax) { workspaceFlowError("Workspace registration is unavailable. Your draft is kept; copy it before reloading."); return; }
    // Registration returns redirected HTML, not a JSON admission receipt.
    // Capture the existing csrf/path/name controls before any busy-state change.
    const fields = new FormData(form);
    registeringProjects.add(form);
    try {
      await window.htmx.ajax("POST", "/projects/add", {
        source: form, event, values: fields, target: "#workspace", select: "#workspace", swap: "outerHTML", push: "true"
      });
    } catch (_) { /* Existing HTMX error events own feedback; never replay registration. */ }
    finally { registeringProjects.delete(form); }
  }
  // A capture-only registration owner also suppresses an obsolete SSR HTMX
  // target listener. Other forms still reach their existing React/delegated owners.
  document.addEventListener("submit", event => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || form.id !== "add-project-form") return;
    event.preventDefault(); event.stopPropagation();
    void registerProject(form, event);
  }, {capture: true});
  document.addEventListener("submit", async event => {
    const form = event.target;
    if (form.id === "home-composer") {
      event.preventDefault(); await continueHome();
    } else if (form.matches("[data-runtime-open]")) {
      event.preventDefault(); if (form.dataset.pending) return;
      form.dataset.pending = "true";
      const workspace = $("#workspace"), project = workspace.dataset.project, targetSession = $('input[name="session_id"]', form)?.value || "";
      const draftOwner = homeDraft;
      const handoff = !!homeDraft.text && (!homeDraft.project || homeDraft.project === project) && (homeDraft.targetSession === undefined || homeDraft.targetSession === targetSession);
      if (handoff && new TextEncoder().encode(homeDraft.text).length > 65536) { window.SnowWorkspace?.updateActivation({busy: false, error: "Shorten your draft to at most 64 KiB before starting."}); delete form.dataset.pending; return; }
      // Capture the exact explicit trust/skills choices before React disables
      // busy controls: disabled inputs are omitted by native FormData.
      const fields = new URLSearchParams(new FormData(form));
      window.SnowWorkspace?.updateActivation({busy: true, error: ""});
      window.SnowShell?.preemptInventory();
      try {
        const snapshot = await request(form.action, fields, undefined, 15000);
        if (!form.isConnected) return;
        if (snapshot.project_id !== project || project !== $("#workspace")?.dataset.project || typeof snapshot.session_id !== "string" || !snapshot.session_id || typeof snapshot.instance_id !== "string" || !snapshot.instance_id || targetSession && snapshot.session_id !== targetSession) throw new Error("invalid response");
        if (handoff && homeDraft === draftOwner) {
          homeDraft.project = project; homeDraft.pending = true;
          homeDraft.ack = {project, session: snapshot.session_id, instance: snapshot.instance_id};
        }
        // An admitted start may finish after navigation; retain its receipt but
        // never pull a newer workspace view back to the old form.
        if (!form.isConnected || workspace !== $("#workspace")) return;
        await window.htmx.ajax("GET", projectLocation(snapshot.project_id) + "&live=1", {source: form, target: "#workspace", swap: "outerHTML"});
      } catch (_) {
        delete form.dataset.pending;
        if (form.isConnected && workspace === $("#workspace")) window.SnowWorkspace?.updateActivation({busy: false, error: "Could not confirm activation. No retry was queued. Refresh to check whether the runtime opened, or check the provider configuration in the terminal."});
      }
    } else if (form.id === "live-composer") {
      event.preventDefault(); if (!mutationSafe() || live.regeneration || live.status !== "idle") return;
      const prompt = $("#live-prompt"), value = prompt.value, current = live, draftRevision = live.draftRevision, reuse = reuseDrafts.get(live.key);
      if (window.SnowComposerContext?.pending()) { liveError("Wait for context loading to finish before sending."); return; }
      if (new TextEncoder().encode(value).length > 65536) { liveError("Message is too large. Shorten it to at most 64 KiB and try again."); return; }
      if (/[\u0000\uD800-\uDFFF]/u.test(value)) { liveError("Message contains invalid text. Remove null or incomplete Unicode characters before sending."); return; }
      let context;
      try { context = window.SnowComposerContext?.capture(value); }
      catch (error) { liveError(error.message || "Could not prepare attachments. Nothing was sent."); return; }
      if (!value.trim() && !context?.hasContent) return;
      const edit = editDrafts.get(current.key);
      if (window.SnowGoals?.composerState().enabled) {
        if (edit || reuse || context?.hasContent) { liveError("Goal objectives use plain composer text. Remove attachments or finish the edit first, or turn Goal off to send a message."); return; }
        const unchanged = () => live === current && prompt.isConnected && current.draftRevision === draftRevision && prompt.value === value;
        if (await window.SnowGoals.submit(value, unchanged)) {
          if (live !== current || !prompt.isConnected) return;
          if (unchanged() && window.SnowLiveView?.updateDraft("")) current.draftRevision++;
          saveDraft(); window.SnowScroll?.follow(); updateControls();
        }
        return;
      }
      if (edit && context?.hasContent) { liveError("Attachments cannot be used with Edit & resend. Cancel the edit first."); return; }
      if (edit) {
        if (!validEditText(value)) { liveError("Message contains invalid text. Remove null or incomplete Unicode characters before sending."); return; }
        // Never route an edit through normal prompt, including failed or stale
        // prepares/commits. A consumed token is never retried by this browser.
        if (edit.phase !== "ready" || edit.instance !== current.instance || !edit.token) return;
        const token = edit.token; edit.token = ""; edit.phase = "committing";
        const result = await runtimeAction("message-edit-commit", {edit_token: token, text: value});
        if (live !== current || !prompt.isConnected) {
          edit.phase = "unknown"; return;
        }
        if (result) {
          // The server snapshot alone replaces the active path. Preserve any
          // concurrent typing and its earlier draft until explicit Cancel.
          if (current.draftRevision === draftRevision && prompt.value === value && window.SnowLiveView?.updateDraft(edit.draft)) {
            current.draftRevision++;
            editDrafts.delete(current.key);
          } else {
            edit.phase = "completed"; edit.instance = current.instance;
          }
          saveDraft(); window.SnowScroll?.follow();
        } else edit.phase = "unknown";
        updateControls(); return;
      }
      const action = context?.hasContent ? "prompt-content" : "prompt";
      const fields = context?.hasContent ? {text: context.text, content: context.content} : {text: value};
      if (context?.hasContent && new URLSearchParams({...fields, csrf: csrf(), instance_id: current.instance}).toString().length > 4 * 1024 * 1024) {
        liveError("Encoded attachments exceed the 4 MiB request limit. Remove or reduce a file before sending."); return;
      }
      if (await runtimeAction(action, fields)) {
        if (live !== current || !prompt.isConnected) return;
        if (context?.hasContent) window.SnowComposerContext?.accepted(context);
        if (reuse && reuseDrafts.get(current.key) === reuse) reuse.sent = true;
        // Value equality alone misses typing-and-undo while admission is pending.
        if (current.draftRevision === draftRevision && prompt.value === value && window.SnowLiveView?.updateDraft(reuse?.draft || "")) {
          reuseDrafts.delete(current.key);
        }
        saveDraft(); window.SnowScroll?.follow(); updateControls();
      }
    } else if (form.matches("[data-runtime-input]")) {
      event.preventDefault(); const answers = [];
      for (const field of form.querySelectorAll("fieldset")) {
        const selected = $('input[type="radio"]:checked', field), free = $("textarea", field);
        const answer = selected && !selected.dataset.other ? selected.value : free?.value || "";
        if (!answer.trim()) { if (free) { free.setCustomValidity("Enter an answer before sending."); free.reportValidity(); } return; }
        answers.push({id: field.dataset.questionId, answer});
      }
      await runtimeAction("input", {request_id: form.dataset.requestId, answers: JSON.stringify(answers)});
    }
  });
  document.addEventListener("compositionstart", event => {
    if (event.target.id === "live-prompt" && live) live.draftRevision++;
    if (["home-prompt", "workspace-prompt"].includes(event.target.id)) composingWorkspaceDrafts.add(event.target);
  });
  document.addEventListener("compositionend", event => {
    if (!["home-prompt", "workspace-prompt"].includes(event.target.id)) return;
    composingWorkspaceDrafts.delete(event.target);
    // The following native input remains the canonical draft writer.
  });
  document.addEventListener("snow:workspace-mounted", () => {
    syncHomeDraft();
  });
  document.addEventListener("input", event => {
    if (event.target.id === "home-prompt") { homeDraft.text = event.target.value; homeDraft.pending = false; delete homeDraft.ack; delete homeDraft.targetSession; }
    if (event.target.id === "workspace-prompt" && !event.target.disabled && matchesWorkspaceDraft(event.target)) {
      const prompt = event.target;
      homeDraft = {text: prompt.value, project: prompt.dataset.draftProject, name: prompt.dataset.draftName, targetSession: prompt.dataset.draftSession, pending: true};
    }
    if (event.target.id === "live-prompt") { if (live) live.draftRevision++; saveDraft(); syncHomeDraft(); }
    if (event.target.closest("[data-runtime-input]")) {
      event.target.setCustomValidity?.("");
    }
  });
  document.addEventListener("keydown", event => {
    const dialog = $("dialog[open]");
    if (dialog) { trap(event, dialog); return; }
    const nav = $(".sidebar.nav-open");
    if (nav) { if (event.key === "Escape") { event.preventDefault(); requestNav(false); } else trap(event, nav); return; }
    const inspector = $('#project-inspector[aria-modal="true"]');
    if (inspector) { if (event.key === "Escape") $("[data-inspector-toggle]", inspector).click(); else trap(event, inspector); return; }
    if (event.target.id === "live-prompt" && event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.isComposing) { event.preventDefault(); if (!$("#live-send").disabled) $("#live-composer").requestSubmit(); else if (window.SnowQueue?.canEnqueue()) window.SnowQueue.enqueue(); }
  });
  narrow.addEventListener("change", () => { syncSidebarCollapse(); requestNav(false, false); const panel = $("#project-inspector"); if (panel && !panel.hidden) $("[data-inspector-toggle]", panel).click(); });
  document.addEventListener("visibilitychange", () => { if (!document.hidden && live && !live.subscription) { clearTimeout(pollTimer); pollTimer = setTimeout(startUpdates, 0); } });
  document.body.addEventListener("htmx:beforeCleanupElement", event => {
    // Retire only a committed replacement. beforeSwap can still be canceled by
    // a later listener; destroying its React views there would blank a live owner.
    if (event.detail.elt?.id === "workspace") { saveDraft(); disposeLivePanels(); window.SnowGoals?.dispose(); window.SnowVersions?.dispose(); window.SnowQueue?.dispose(); live?.controller.abort(); clearTimeout(pollTimer); folderRequest?.abort(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose(); window.SnowConversation?.dispose(); window.SnowMessages?.dispose(); window.SnowScroll?.dispose(); window.SnowWidth?.dispose(); window.SnowAttention?.dispose(); window.SnowLiveView?.dispose(); live = null; window.SnowWorkspace?.reset(); requestNav(false, false); }
  });
  document.body.addEventListener("htmx:afterSwap", event => {
    syncThemeChoices();
    if (event.detail.target?.id === "workspace") {
      navigation();
      if (narrow.matches || !$("#project-navigation")?.contains(document.activeElement)) $("#workspace-content")?.focus({preventScroll: true});
    }
    if ($("#connection-error")) $("#connection-error").hidden = true;
  });
  for (const name of ["htmx:sendError", "htmx:timeout", "htmx:responseError"]) document.body.addEventListener(name, event => {
    // Settings own an explicit unknown-outcome notice, not the navigation
    // banner claiming nothing was sent and suggesting a retry.
    if (event.detail?.elt?.id === "live-settings-transport") return;
    if ($("#connection-error")) $("#connection-error").hidden = false;
  });
  window.addEventListener("pagehide", () => { pageAway = true; saveDraft(); disposeLivePanels(); window.SnowProcesses?.dispose(); window.SnowInspection?.dispose(); window.SnowGoals?.dispose(); window.SnowVersions?.dispose(); window.SnowQueue?.dispose(); live?.controller.abort(); clearTimeout(pollTimer); window.SnowMessages?.dispose(); window.SnowScroll?.dispose(); window.SnowWidth?.dispose(); window.SnowAttention?.dispose(); window.SnowConversation?.dispose(); window.SnowLiveView?.dispose(); live = null; });
  document.body.addEventListener("htmx:historyRestore", navigation);
  document.addEventListener("snow:react-ready", navigation);
  window.addEventListener("pageshow", event => {
    if (!pageAway && !event.persisted) return;
    pageAway = false;
    navigation();
  });
  navigation();
})();
