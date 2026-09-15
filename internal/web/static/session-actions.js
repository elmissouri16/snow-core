(() => {
  "use strict";
  // Explicit saved-session deletion only. No activation, switching or prompt transport.
  let dialog, current;
  const $ = (selector, scope = document) => scope.querySelector(selector);
  function node(tag, className, text) {
    const result = document.createElement(tag);
    if (className) result.className = className;
    if (text !== undefined) result.textContent = text;
    return result;
  }
  function set(element, name, value) { if (element.getAttribute(name) !== value) element.setAttribute(name, value); }
  function group(project) { return [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === project); }
  function close() {
    if (current?.pending) return;
    const previous = current; current = null;
    dialog?.close();
    const fallback = group(previous?.project)?.querySelector("[data-workspace-toggle]");
    const trigger = previous?.trigger;
    (trigger?.isConnected ? (!trigger.hidden && !trigger.disabled ? trigger : trigger.closest("[data-shell-session]")?.querySelector("a")) : fallback)?.focus({preventScroll: true});
  }
  function ensureDialog() {
    if (dialog) return;
    dialog = node("dialog", "folder-dialog session-delete-dialog"); dialog.id = "session-delete-dialog";
    dialog.setAttribute("aria-labelledby", "session-delete-title");
    const heading = node("div", "dialog-heading"), title = node("h2", "", "Delete session?"); title.id = "session-delete-title";
    const cancel = node("button", "quiet", "Cancel"); cancel.type = "button"; cancel.dataset.deleteCancel = "";
    cancel.addEventListener("click", close); heading.append(title, cancel);
    const name = node("p", "session-delete-name"); name.dataset.deleteName = "";
    const warning = node("p", "fine", "Permanently delete this saved conversation and its managed private session data. This cannot be undone. Workspace files are not deleted. Unsent drafts remain in this tab.");
    warning.id = "session-delete-warning"; dialog.setAttribute("aria-describedby", warning.id);
    const label = node("label", "checkbox-label"), checkbox = node("input"); checkbox.type = "checkbox"; checkbox.dataset.deleteConfirm = "";
    label.append(checkbox, node("span", "", "I understand this permanently deletes the saved session."));
    const error = node("p", "error"); error.dataset.deleteError = ""; error.setAttribute("role", "alert"); error.hidden = true;
    const footer = node("div", "dialog-actions"), submit = node("button", "button danger", "Delete session"); submit.type = "button"; submit.dataset.deleteSubmit = ""; submit.disabled = true;
    checkbox.addEventListener("change", () => { submit.disabled = !checkbox.checked || !current || current.pending || current.submitted; });
    submit.addEventListener("click", () => { void remove(); });
    footer.append(submit); dialog.append(heading, name, warning, label, error, footer);
    dialog.addEventListener("cancel", event => { event.preventDefault(); close(); });
    document.body.append(dialog);
  }
  function canDelete(trigger) {
    return trigger?.isConnected && !trigger.disabled && trigger.dataset.deleteSupported === "true" && trigger.dataset.deleteAvailable === "true" && trigger.dataset.deleteActive !== "true";
  }
  function open(trigger) {
    if (!canDelete(trigger) || current?.pending) return;
    const {project, session, instance, sessionName} = trigger.dataset;
    if (!project || !session || typeof instance !== "string") return;
    ensureDialog();
    window.SnowMenus?.close({restoreFocus: false});
    current = {project, session, instance, trigger, root: $("#workspace"), pending: false, submitted: false};
    $("[data-delete-name]", dialog).textContent = sessionName || "Untitled session";
    $("[data-delete-confirm]", dialog).checked = false;
    $("[data-delete-confirm]", dialog).disabled = false;
    $("[data-delete-submit]", dialog).disabled = true;
    $("[data-delete-submit]", dialog).textContent = "Delete session";
    $("[data-delete-cancel]", dialog).disabled = false;
    $("[data-delete-cancel]", dialog).textContent = "Cancel";
    $("[data-delete-error]", dialog).hidden = true;
    dialog.showModal(); $("[data-delete-cancel]", dialog).focus();
  }
  async function responseText(response) {
    const reader = response.body.getReader(), decoder = new TextDecoder("utf-8", {fatal: true});
    let size = 0, text = "";
    try {
      while (true) {
        const {done, value} = await reader.read(); if (done) break;
        size += value.length; if (size > 65536) throw new Error("Response too large");
        text += decoder.decode(value, {stream: true});
      }
      return text + decoder.decode();
    } finally { await reader.cancel(); reader.releaseLock(); }
  }
  async function remove() {
    const attempt = current;
    if (!attempt || attempt.pending || attempt.submitted || !dialog.open || !$("[data-delete-confirm]", dialog).checked) return;
    const csrf = $('input[name="csrf"]')?.value;
    if (!csrf) return;
    // A confirmation from a retired workspace cannot authorize a fresh action.
    if (attempt.root !== $("#workspace") || !canDelete(attempt.trigger) || attempt.trigger.dataset.project !== attempt.project || attempt.trigger.dataset.session !== attempt.session || attempt.trigger.dataset.instance !== attempt.instance) { close(); return; }
    attempt.pending = true; attempt.submitted = true;
    $("[data-delete-submit]", dialog).disabled = true; $("[data-delete-submit]", dialog).textContent = "Deleting…";
    $("[data-delete-confirm]", dialog).disabled = true; $("[data-delete-cancel]", dialog).disabled = true;
    const controller = new AbortController(), timer = setTimeout(() => controller.abort(), 15000);
    let failure = "Deletion could not be confirmed. It may have completed. Refresh the session list before trying again; no automatic retry was sent.";
    try {
      const response = await fetch(`/projects/${encodeURIComponent(attempt.project)}/sessions/${encodeURIComponent(attempt.session)}/delete`, {
        method: "POST", credentials: "same-origin", signal: controller.signal,
        headers: {Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded"},
        body: new URLSearchParams({csrf, confirm: "delete", instance_id: attempt.instance})
      });
      const text = await responseText(response);
      if (!response.ok) {
        // Only fixed-text application errors are shown; proxy HTML is not rendered.
        if ([400, 401, 403, 404, 409].includes(response.status) && response.headers.get("content-type")?.startsWith("text/plain") && text.length <= 1024) failure = text.trim() + " No automatic retry was sent.";
        throw new Error("Deletion not confirmed");
      }
      const receipt = JSON.parse(text);
      if (receipt.project_id !== attempt.project || receipt.session_id !== attempt.session || receipt.instance_id !== attempt.instance || receipt.deleted !== true) throw new Error("Invalid receipt");
      document.dispatchEvent(new CustomEvent("snow:session-deleted", {detail: {project: attempt.project, session: attempt.session, instance: attempt.instance}}));
      window.SnowSidebarSessions?.deleted(attempt.project, attempt.session);
      attempt.pending = false;
      close();
      if (attempt.root === $("#workspace") && attempt.root.dataset.project === attempt.project && attempt.root.dataset.session === attempt.session && !$('#live-session[data-runtime="true"]')) {
        // Explicit deletion of the viewed cold session selects an empty state,
        // never another session, worker activation or a prompt send.
        await window.htmx?.ajax("GET", "/?" + new URLSearchParams({view: "projects", project: attempt.project, new: "1"}), {source: group(attempt.project)?.querySelector("[data-shell-project-new]"), target: "#workspace", swap: "outerHTML"});
      }
    } catch (_) {
      window.SnowSidebarSessions?.invalidate(attempt.project);
      if (current === attempt) {
        attempt.pending = false;
        const error = $("[data-delete-error]", dialog); error.textContent = failure; error.hidden = false;
        $("[data-delete-cancel]", dialog).disabled = false; $("[data-delete-cancel]", dialog).textContent = "Close";
        $("[data-delete-submit]", dialog).textContent = "Delete session";
      }
    } finally { clearTimeout(timer); }
  }
  function renderRow(row, {project, session, name, instance, active, available, supported}) {
    let button = $("[data-shell-session-menu]", row);
    if (!button && !supported) return;
    if (!button) {
      button = node("button", "quiet shell-session-more", "⋯"); button.type = "button"; button.dataset.shellSessionMenu = "";
      row.append(button);
    }
    for (const [key, value] of Object.entries({"data-project": project, "data-session": session, "data-instance": instance, "data-session-name": name || "Untitled session",
      "data-delete-supported": String(supported), "data-delete-available": String(available), "data-delete-active": String(active),
      "aria-label": `Session actions for ${name || "Untitled session"}`, "aria-haspopup": "menu", "title": "Session actions"})) set(button, key, value);
    if (supported) { if (button.hidden) button.hidden = false; if (button.disabled) button.disabled = false; }
    else {
      const live = $('#live-session[data-runtime="true"]');
      const rename = $('[data-workflow-rename]') || $('[data-workflow-rename-form] button[type="submit"]');
      const keepRename = live?.dataset.project === project && live?.dataset.session === session && !!rename;
      if (!keepRename) {
        const expanded = button.getAttribute("aria-expanded") === "true";
        const restore = expanded || document.activeElement === button;
        if (expanded) window.SnowMenus?.close({restoreFocus: false});
        if (!button.hidden) button.hidden = true;
        if (!button.disabled) button.disabled = true;
        if (restore) $("a", row)?.focus({preventScroll: true});
      } else {
        if (button.hidden) button.hidden = false;
        if (button.disabled !== rename.disabled) button.disabled = rename.disabled;
      }
    }
  }
  function appendMenu(panel, trigger) {
    if (trigger.dataset.deleteSupported !== "true") return;
    const captured = {root: $("#workspace"), project: trigger.dataset.project, session: trigger.dataset.session, instance: trigger.dataset.instance};
    const action = node("button", "snow-menu-row danger", "Delete session"); action.type = "button";
    action.setAttribute("role", "menuitem"); action.dataset.sessionDelete = ""; action.disabled = !canDelete(trigger);
    if (trigger.dataset.deleteActive === "true") action.title = "Switch to another session or close this workspace before deleting the active session";
    action.addEventListener("click", () => {
      if (action.disabled || !action.isConnected || captured.root !== $("#workspace") || !canDelete(trigger) ||
          captured.project !== trigger.dataset.project || captured.session !== trigger.dataset.session || captured.instance !== trigger.dataset.instance) return;
      open(trigger);
    });
    panel.append(action);
  }
  document.body.addEventListener("htmx:beforeSwap", event => {
    if (current && !current.pending && event.detail?.target?.id === "workspace" && event.detail.shouldSwap !== false) close();
  });
  window.SnowSessionActions = Object.freeze({renderRow, appendMenu});
})();
