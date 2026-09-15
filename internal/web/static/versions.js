/* Read-only, instance-bound version browsing. Only the typed prepared restore
 * callback in app.js can replace active history; previews never render chat. */
(() => {
  "use strict";
  const $ = (selector, scope) => scope?.querySelector(selector);
  const id = (value, empty = false) => typeof value === "string" && (empty || value.length > 0) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
  const cursor = value => typeof value === "string" && value.length <= 4096 && !/[\u0000-\u001f\u007f]/.test(value);
  let view;
  function node(tag, className = "", text = "") { const el = document.createElement(tag); el.className = className; el.textContent = text; return el; }
  function init(api) {
    dispose();
    const dialog = $("#versions-dialog", api.root);
    if (!dialog || api.root.dataset.versionsEnabled !== "true") return;
    const controller = new AbortController();
    view = {api, dialog, controller, page: null, selected: null, preview: null, listCursors: [""], previewCursors: [""], listIndex: 0, previewIndex: 0};
    $("[data-versions-list]", dialog).replaceChildren(); drawPreview();
    api.root.addEventListener("click", onClick, {signal: controller.signal});
    dialog.addEventListener("cancel", event => { event.preventDefault(); close(); }, {signal: controller.signal});
    dialog.addEventListener("close", () => { if (view?.dialog === dialog && view.phase !== "committing") cancelPreparation(); }, {signal: controller.signal});
  }
  function dispose() {
    if (!view) return;
    const old = view; view = null;
    old.operation?.controller.abort(); old.controller.abort(); old.token = "";
    old.api.closeDialog(old.dialog);
  }
  function bound(value) {
    const identity = view?.api.identity;
    return !!value && !!identity && value.project_id === identity.project_id && value.session_id === identity.session_id && value.instance_id === identity.instance_id;
  }
  function validRevision(value) { return Number.isSafeInteger(value) && value >= 0; }
  function validPage(value) {
    if (!bound(value) || !validRevision(value.revision) || !id(value.current_branch_id) || !id(value.current_tip_id, true) || !Array.isArray(value.versions) || value.versions.length > 100 || typeof value.has_more !== "boolean" || !cursor(value.next_cursor) || value.has_more !== !!value.next_cursor) return false;
    const seen = new Set();
    return value.versions.every(item => {
      if (!item || !id(item.branch_id) || !id(item.tip_id, true) || typeof item.name !== "string" || item.name.length > 512 || typeof item.current !== "boolean") return false;
      if (item.current !== (item.branch_id === value.current_branch_id) || item.current && item.tip_id !== value.current_tip_id || seen.has(item.branch_id)) return false; seen.add(item.branch_id); return true;
    });
  }
  function validPreview(value, target) {
    if (!bound(value) || !validRevision(value.revision) || value.branch_id !== target.branch_id || value.tip_id !== target.tip_id || !Array.isArray(value.messages) || value.messages.length > 256 || typeof value.has_more !== "boolean" || !cursor(value.next_cursor) || value.has_more !== !!value.next_cursor) return false;
    let bytes = 0;
    return value.messages.every(message => {
      if (!message || !["user", "assistant", "tool", "system", "tool_activity"].includes(message.role) || typeof message.text !== "string") return false;
      bytes += new TextEncoder().encode(message.text).length;
      return bytes <= 1024 * 1024 && (!message.tools || Array.isArray(message.tools) && message.tools.length <= 128 && message.tools.every(tool => tool && typeof tool.tool === "string" && tool.tool.length <= 256));
    });
  }
  function canRead() { return !!view?.ui?.readable && !view.operation && !view.phase; }
  function canRestore() { return !!view?.ui?.restoreSafe && !view.stale && !!view.selected && !!view.preview && !view.selected.current && !view.operation && view.preview.branch_id === view.selected.branch_id && view.preview.tip_id === view.selected.tip_id; }
  function render(snapshot, ui) { if (view) { view.ui = ui; view.snapshot = snapshot; controls(); } }
  function controls() {
    if (!view) return;
    const {api, dialog, ui, page, preview, phase} = view, busy = !!view.operation;
    const shownPage = page || view.retained?.page, shownPreview = preview || view.retained?.preview, shownSelection = view.selected || view.retained?.selected;
    $("[data-versions-open]", api.root).hidden = !ui?.supported;
    $("[data-versions-open]", api.root).disabled = !ui?.readable;
    $("[data-versions-refresh]", dialog).disabled = !canRead();
    for (const button of dialog.querySelectorAll("[data-version-preview]")) button.disabled = !canRead() || !page;
    for (const [selector, available, verified] of [["[data-versions-more]", shownPage?.has_more, page], ["[data-versions-back]", view.listIndex > 0, page], ["[data-version-preview-more]", shownPreview?.has_more, preview], ["[data-version-preview-back]", view.previewIndex > 0, preview]]) {
      const button = $(selector, dialog); button.hidden = !available; button.disabled = !canRead() || !verified;
    }
    const restore = $("[data-version-restore]", dialog);
    restore.hidden = !shownPreview || !!shownSelection?.current || !!phase; restore.disabled = !canRestore();
    restore.title = ui?.restoreSafe ? "Prepare an explicit conversation-history-only restore" : "Restore requires an idle, connected conversation with no pending/review queue or conflicting action";
    $("[data-version-restore-confirmation]", dialog).hidden = !phase;
    $("[data-version-restore-confirm]", dialog).disabled = phase !== "ready" || !canRestore() || !view.token || Date.now() >= view.expires;
    $("[data-version-restore-cancel]", dialog).disabled = phase === "committing";
    $("[data-versions-status]", dialog).textContent = view.error || (busy ? "Loading a read-only version view…" : !ui?.readable ? "This runtime is unavailable or has changed. No restore will be retried." : view.retained ? "Previous read-only view retained. Select a version to verify it again; restore remains disabled." : !ui.restoreSafe ? "Read-only browsing. Restore is unavailable while work, queued/review items, or another action is pending." : "Read-only preview; the active chat is unchanged.");
    window.SnowHistoryControls?.selectionChanged();
    $("[data-version-restore-status]", dialog).textContent = phase === "preparing" ? "Preparing confirmation only. Nothing has changed." : phase === "ready" ? "Confirm to select this saved history without starting a turn." : phase === "committing" ? "Restoring conversation history…" : "";
  }
  // Only a verified preview and its same-revision branch inventory may bind a
  // new history operation. Return a copy; never expose mutable view authority.
  function selection() {
    const v = view;
    if (!v || v.operation || v.phase || v.stale || !v.dialog.open || !v.selected || !v.preview || !v.page || v.preview.revision !== v.page.revision || v.preview.revision !== v.snapshot?.revision || v.preview.branch_id !== v.selected.branch_id || v.preview.tip_id !== v.selected.tip_id) return null;
    return Object.freeze({project_id: v.page.project_id, instance_id: v.page.instance_id, session_id: v.page.session_id, revision: v.preview.revision, current_branch_id: v.page.current_branch_id, current_tip_id: v.page.current_tip_id, branch_id: v.selected.branch_id, tip_id: v.selected.tip_id, name: v.selected.name});
  }
  function refresh() { return list(0, true); }
  function cancelPreparation() {
    if (!view || view.phase === "committing") return;
    view.operation?.controller.abort(); view.operation = null; view.phase = null; view.token = "";
    view.api.restoreState(null); controls();
    if (view.dialog.open) $("[data-version-restore]", view.dialog)?.focus({preventScroll: true});
  }
  function close() { if (view?.phase !== "committing") { cancelPreparation(); view?.api.closeDialog(view.dialog); } }
  function begin(kind) {
    const current = view, operation = {kind, controller: new AbortController()};
    current.operation?.controller.abort(); current.operation = operation; current.error = ""; controls(); return [current, operation];
  }
  function currentOperation(current, operation) { return view === current && current.operation === operation && !operation.controller.signal.aborted && current.dialog.open; }
  async function list(index = 0, refresh = false) {
    if (!canRead() || !view.dialog.open || index < 0 || index >= 64) return;
    if (refresh) {
      // Retain presentation only. A refresh immediately revokes selection and
      // restore authority, even if the next read fails or returns the same page.
      if (view.page) view.retained = {page: view.page, selected: view.selected || view.retained?.selected, preview: view.preview || view.retained?.preview, previewIndex: view.previewIndex};
      view.stale = true; view.listCursors = [""]; view.listIndex = 0; view.page = null; view.preview = null; view.selected = null; drawPreview();
    }
    const requested = view.listCursors[index]; if (!cursor(requested)) return;
    const [current, operation] = begin("list");
    try {
      const value = await current.api.list(requested, operation.controller.signal);
      if (!currentOperation(current, operation)) return;
      if (!validPage(value) || index > 0 && (value.current_branch_id !== current.page?.current_branch_id || value.current_tip_id !== current.page?.current_tip_id) || value.has_more && current.listCursors.slice(0, index + 1).includes(value.next_cursor)) throw new Error("Unverified versions page");
      current.page = value; if (refresh) current.stale = false; current.listIndex = index; current.listCursors = current.listCursors.slice(0, index + 1);
      if (value.has_more) current.listCursors.push(value.next_cursor);
      drawList();
    } catch (error) { if (currentOperation(current, operation)) { current.stale = true; current.error = "Could not verify this versions page. Nothing changed. Refresh explicitly to read again."; } }
    finally { if (view === current && current.operation === operation) { current.operation = null; controls(); } }
  }
  function drawList() {
    const list = $("[data-versions-list]", view.dialog), keep = new Set();
    view.versionRows ||= new Map();
    let at = list.firstChild;
    for (const item of view.page.versions) {
      const key = JSON.stringify([item.branch_id, item.tip_id]); keep.add(key);
      let button = view.versionRows.get(key);
      if (!button) {
        button = node("button", "version-choice"); button.type = "button"; button.dataset.versionId = item.branch_id; button.dataset.tipId = item.tip_id; button.dataset.versionPreview = "";
        button.append(node("span", "", ""), node("code", "", `Branch: ${item.branch_id}\nTip: ${item.tip_id || "(empty history)"}`)); view.versionRows.set(key, button);
      }
      button.setAttribute("aria-pressed", String(view.selected?.branch_id === item.branch_id && view.selected?.tip_id === item.tip_id));
      const label = `${item.name || "Unnamed version"}${item.current ? " · Current" : ""}`;
      if (button.firstChild.textContent !== label) button.firstChild.textContent = label;
      if (button !== at) {
        if (list.moveBefore && button.isConnected) list.moveBefore(button, at);
        else list.insertBefore(button, at);
      }
      at = button.nextSibling;
    }
    for (const [key, button] of view.versionRows) if (!keep.has(key)) { button.remove(); view.versionRows.delete(key); }
    for (const child of [...list.children]) if (!child.hasAttribute("data-version-preview")) child.remove();
    if (!view.page.versions.length) list.append(node("p", "fine", "No saved versions on this page."));
  }
  async function preview(target, index = 0) {
    if (!canRead() || !target || index < 0 || index >= 64) return;
    if (target !== view.selected) {
      if (target.branch_id !== view.retained?.selected?.branch_id || target.tip_id !== view.retained?.selected?.tip_id) view.retained = null;
      view.selected = target; view.preview = null; view.previewCursors = [""]; view.previewIndex = 0; drawPreview(); drawList();
    }
    const requested = view.previewCursors[index]; if (!cursor(requested)) return;
    const [current, operation] = begin("preview");
    try {
      const value = await current.api.preview(target, requested, operation.controller.signal);
      if (!currentOperation(current, operation)) return;
      if (!validPreview(value, target) || value.has_more && current.previewCursors.slice(0, index + 1).includes(value.next_cursor)) throw new Error("Unverified preview");
      current.preview = value; current.retained = null; current.previewIndex = index; current.previewCursors = current.previewCursors.slice(0, index + 1);
      if (value.has_more) current.previewCursors.push(value.next_cursor);
      drawPreview();
    } catch (error) { if (currentOperation(current, operation)) { current.stale = true; current.error = "Could not verify the selected branch and tip. The active chat is unchanged; refresh explicitly to read again."; } }
    finally { if (view === current && current.operation === operation) { current.operation = null; controls(); } }
  }
  function drawPreview() {
    const {dialog} = view, selected = view.selected || view.retained?.selected, preview = view.preview || view.retained?.preview;
    $("[data-version-preview-title]", dialog).textContent = selected ? `${selected.name || "Unnamed version"} · read-only preview page ${(view.preview ? view.previewIndex : view.retained?.previewIndex ?? view.previewIndex) + 1}` : "Choose a saved version.";
    $("[data-version-tip]", dialog).textContent = selected ? `Branch: ${selected.branch_id}\nTip: ${selected.tip_id || "(empty history)"}` : "";
    const notice = $("[data-version-preview-notice]", dialog);
    notice.hidden = !preview; notice.textContent = preview ? `Only this bounded preview page is shown. Restore selects the complete saved version.${preview.history_truncated ? " Some saved history is omitted from this preview." : ""}${preview.history_tools_truncated ? " Some tool details are omitted." : ""}` : "";
    const signature = JSON.stringify([selected?.branch_id, selected?.tip_id, preview?.messages]);
    if (view.previewSignature === signature) return;
    view.previewSignature = signature;
    const fragment = document.createDocumentFragment();
    for (const message of preview?.messages || []) {
      const row = node("article", "version-preview-message"); row.append(node("strong", "", message.role), node("pre", "", message.text));
      for (const tool of message.tools || []) row.append(node("pre", "", `Tool: ${typeof tool.tool === "string" ? tool.tool.slice(0, 256) : "saved tool"} · ${typeof tool.status === "string" ? tool.status.slice(0, 64) : "saved"}`));
      fragment.append(row);
    }
    $("[data-version-preview-messages]", dialog).replaceChildren(fragment);
  }
  async function prepare() {
    if (!canRestore() || view.phase || !view.dialog.open) return;
    const target = view.selected, origin = view.page, [current, operation] = begin("prepare");
    current.phase = "preparing"; current.api.restoreState("preparing"); controls();
    $("[data-version-restore-cancel]", current.dialog).focus({preventScroll: true});
    $("[data-version-restore-target]", current.dialog).textContent = `Selected branch: ${target.branch_id}; exact tip: ${target.tip_id || "(empty history)"}.`;
    try {
      const value = await current.api.prepare(target, origin, operation.controller.signal);
      if (!currentOperation(current, operation)) return;
      if (!bound(value) || value.branch_id !== target.branch_id || value.tip_id !== target.tip_id || value.current_branch_id !== origin.current_branch_id || value.current_tip_id !== origin.current_tip_id || !id(value.restore_token) || !Number.isFinite(Date.parse(value.expires_at)) || Date.parse(value.expires_at) <= Date.now()) throw new Error("Unverified restore preparation");
      current.token = value.restore_token; current.expires = Date.parse(value.expires_at); current.phase = "ready"; current.api.restoreState("ready");
    } catch (error) {
      if (currentOperation(current, operation)) { current.token = ""; current.phase = null; current.stale = true; current.error = "Restore preparation was rejected or could not be verified. Nothing changed. Refresh before explicitly preparing again."; current.api.restoreState(null); }
    } finally { if (view === current && current.operation === operation) { current.operation = null; controls(); } }
  }
  async function commit() {
    if (!canRestore() || view.phase !== "ready" || !view.token || Date.now() >= view.expires || !view.dialog.open) return;
    const current = view, token = current.token; current.token = ""; current.phase = "committing"; current.api.restoreState("committing");
    current.api.closeDialog(current.dialog);
    const result = await current.api.commit(token);
    if (view !== current) return;
    current.phase = null; current.api.restoreState(null);
    current.error = result ? "Saved conversation version restored. No prompt was replayed." : "Restore outcome needs review. Your draft is kept; nothing will be retried automatically. Review the workspace before continuing.";
    controls();
  }
  function onClick(event) {
    const button = event.target.closest("button"); if (!view || !button) return;
    if (button.matches("[data-versions-open]") && view.ui?.readable) { view.api.openDialog(view.dialog, button); list(0, true); }
    else if (button.matches("[data-versions-close]")) close();
    else if (button.matches("[data-versions-refresh]")) list(0, true);
    else if (button.matches("[data-versions-more]")) list(view.listIndex + 1);
    else if (button.matches("[data-versions-back]")) list(view.listIndex - 1);
    else if (button.matches("[data-version-preview]")) preview(view.page?.versions.find(item => item.branch_id === button.dataset.versionId && item.tip_id === button.dataset.tipId));
    else if (button.matches("[data-version-preview-more]")) preview(view.selected, view.previewIndex + 1);
    else if (button.matches("[data-version-preview-back]")) preview(view.selected, view.previewIndex - 1);
    else if (button.matches("[data-version-restore]")) prepare();
    else if (button.matches("[data-version-restore-cancel]")) cancelPreparation();
    else if (button.matches("[data-version-restore-confirm]")) commit();
  }
  window.SnowVersions = Object.freeze({init, render, dispose, selection, refresh});
})();
