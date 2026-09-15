(() => {
  "use strict";
  const $ = (selector, scope = document) => scope.querySelector(selector);
  function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }
  function text(value, limit = 16384) { return typeof value === "string" ? value.slice(0, limit) : ""; }
  function setText(node, value) { if (node.textContent !== value) node.textContent = value; }
  // Local line icons and plain text only: previews never interpret host markup.
  function icon(kind) {
    const paths = {
      folder: "M2 5h5l2 2h13v12H2z",
      file: "M6 2h8l4 4v16H6z M14 2v5h4",
      tool: "m8 6-6 6 6 6 M16 6l6 6-6 6 M14 4l-4 16",
      chevron: "m9 5 7 7-7 7",
      search: "M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6",
      terminal: "m4 6 6 6-6 6 M13 18h7",
      edit: "m4 16 12-12 4 4-12 12-5 1z M13 7l4 4"
    };
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("class", "inspection-icon"); svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor"); svg.setAttribute("stroke-width", "1.5");
    const path = document.createElementNS(svg.namespaceURI, "path"); path.setAttribute("d", paths[kind] || paths.file);
    svg.append(path); return svg;
  }
  function pathLabel(node, value) {
    const safe = text(value, 4096), slash = safe.lastIndexOf("/"), name = safe.slice(slash + 1), dot = name.lastIndexOf(".");
    node.title = safe; node.replaceChildren();
    if (slash >= 0) node.append(element("span", "inspection-directory", safe.slice(0, slash + 1)));
    const leaf = element("span", "inspection-basename");
    leaf.append(element("span", "inspection-stem", dot > 0 ? name.slice(0, dot) : name));
    if (dot > 0) leaf.append(element("span", "inspection-extension", name.slice(dot)));
    node.append(leaf);
  }
  function renderDiff(node, value) {
    const content = text(value, 65536), fragment = document.createDocumentFragment();
    // Preserve every character, including final newline; colours are an aid,
    // not a replacement for the literal +/- and unified-diff headers.
    let offset = 0, count = 0;
    for (const match of content.matchAll(/[^\n]*\n|[^\n]+$/g)) {
      if (count++ === 4096) {
        fragment.append(element("span", "inspection-diff-line inspection-diff-context", content.slice(offset)));
        break;
      }
      const line = match[0]; offset += line.length;
      const kind = /^(diff |index |--- |\+\+\+ )/.test(line) ? "meta" : line.startsWith("@@") ? "hunk" : line.startsWith("+") ? "added" : line.startsWith("-") ? "removed" : "context";
      fragment.append(element("span", "inspection-diff-line inspection-diff-" + kind, line));
    }
    node.replaceChildren(fragment);
  }
  const statuses = Object.freeze({pending: "Pending", queued: "Queued", running: "Running", waiting: "Waiting", permission: "Approval needed", completed: "Completed", complete: "Completed", success: "Completed", done: "Completed", failed: "Failed", error: "Failed", denied: "Rejected", rejected: "Rejected", failure: "Failed", cancelled: "Cancelled", canceled: "Cancelled", interrupted: "Interrupted", unknown: "Outcome unknown", aborted: "Cancelled"});
  const toolKinds = Object.freeze({
    glob: ["search", "Find files"], grep: ["search", "Search"],
    read: ["file", "Read"], bash: ["terminal", "Run"],
    write: ["edit", "Write"], edit: ["edit", "Edit"]
  });
  // Shared by live activity and saved, authoritatively associated history.
  // Presentation consumes only public projections, never arguments or markup.
  function renderToolRow(row, data) {
    const summary = $("summary", row), wireName = text(data.tool, 128) || "Tool";
    const [kind, title] = Object.hasOwn(toolKinds, wireName) ? toolKinds[wireName] : ["tool", wireName];
    if (!row._snowToolRow) {
      const leading = element("span", "activity-leading"), glyph = icon(kind), chevron = icon("chevron");
      glyph.classList.add("activity-kind"); chevron.classList.add("activity-chevron");
      leading.append(glyph, chevron);
      const name = $(".activity-tool", row) || element("span", "activity-tool");
      const status = $(".activity-status", row) || element("span", "activity-status");
      status.classList.remove("pill");
      const separator = element("span", "activity-separator"); separator.setAttribute("aria-hidden", "true");
      summary.replaceChildren(leading, name, separator, element("span", "activity-summary"), status);
      summary.addEventListener("click", event => { if (row.classList.contains("activity-no-output")) event.preventDefault(); });
      row._snowToolRow = true;
    }
    row.dataset.toolName = wireName;
    const glyph = $(".activity-kind", row);
    if (glyph.dataset.kind !== kind) {
      $("path", glyph).setAttribute("d", $("path", icon(kind)).getAttribute("d")); glyph.dataset.kind = kind;
    }
    const name = $(".activity-tool", row);
    setText(name, title); name.title = wireName;
    const publicSummary = text(data.summary, 1024);
    const normalized = value => value.trim().replace(/\s+/g, " ").toLowerCase();
    const duplicate = [wireName, title].some(value => normalized(value) === normalized(publicSummary));
    const detail = duplicate ? "" : publicSummary;
    summary.setAttribute("aria-label", `${title === wireName ? wireName : `${title} (${wireName})`}${detail ? ` · ${detail}` : ""} · ${data.label}`);
    setText($(".activity-summary", row), detail); $(".activity-summary", row).title = detail;
    $(".activity-summary", row).hidden = !detail; $(".activity-separator", row).hidden = !detail;
    row.dataset.status = data.status;
    row.classList.toggle("activity-error", !!data.error);
    const status = $(".activity-status", row);
    setText(status, data.label);
    status.classList.toggle("activity-status-quiet", !data.error && ["completed", "complete", "success", "done"].includes(data.status));
    row.classList.toggle("activity-no-output", !data.expandable);
    summary.setAttribute("aria-disabled", String(!data.expandable)); summary.tabIndex = data.expandable ? 0 : -1;
    $(".activity-body", row).hidden = !data.expandable;
    if (!data.expandable) row.open = false;
    setText($(".activity-output", row), data.output);
    $(".activity-truncated", row).hidden = !data.truncated;
    // Never replace the keyed details/summary or set open on an expandable row.
  }
  // Retain only the last bounded projection. A marker can be evicted before
  // activity reconciliation; surviving orphan rows still keep their disclosure
  // identity when moved into the truthful, unassociated fallback.
  const activityRows = new WeakMap();
  function renderActivities(region, snapshot, transcript = document.querySelector("#live-transcript")) {
    if (!region) return;
    const source = Array.isArray(snapshot.activities) ? snapshot.activities : [];
    const activities = source.slice(-128), list = $(".activity-list", region);
    const groups = new Map();
    for (const group of transcript?.querySelectorAll(":scope > [data-runtime-activity-group]") || []) {
      const key = group.dataset.messageId;
      if (key && !groups.has(key)) groups.set(key, group);
    }
    const lists = [list, ...[...groups.values()].map(group => $(".activity-list", group))];
    const existing = new Map(activityRows.get(region) || []);
    for (const target of lists) for (const row of target.children) {
      if (row.dataset.activityId && !existing.has(row.dataset.activityId)) existing.set(row.dataset.activityId, row);
    }
    const selected = [], keep = new Set();
    for (const [index, activity] of activities.entries()) {
      if (!activity || typeof activity !== "object") continue;
      const key = text(activity.id, 256) || `activity-${index}`;
      if (keep.has(key)) continue;
      keep.add(key);
      // Full explicit marker identity only. Orphans and old servers without a
      // binding remain separate; an assistant's ID is never a fallback owner.
      const group = typeof activity.message_id === "string" ? groups.get(activity.message_id) : null;
      selected.push({key, activity, target: group ? $(".activity-list", group) : list});
    }
    for (const [key, row] of existing) if (!keep.has(key)) row.remove();
    const retained = new Map(), positions = new Map();
    const focused = document.activeElement;
    for (const {key, activity, target} of selected) {
      let row = existing.get(key);
      if (!row) {
        row = element("details", "tool-activity"); row.dataset.activityId = key;
        const summary = element("summary");
        const body = element("div", "activity-body");
        body.append(element("pre", "activity-output"), element("p", "fine activity-truncated", "Tool output truncated for bounded display."));
        row.append(summary, body);
      }
      const status = text(activity.status, 32), output = text(activity.output);
      const error = !!activity.is_error || ["error", "failed", "failure", "denied", "rejected"].includes(status);
      renderToolRow(row, {
        tool: activity.tool, status, error,
        label: status === "unknown" ? "Outcome unknown" : activity.is_error ? "Failed" : Object.hasOwn(statuses, status) ? statuses[status] : "Status unavailable",
        summary: activity.summary, output, expandable: !!output || !!activity.truncated,
        truncated: !!activity.truncated || (activity.output || "").length > 16384 || (activity.summary || "").length > 1024
      });
      const position = positions.get(target) || 0, at = target.children[position];
      if (at !== row) {
        if (target.moveBefore && row.isConnected && target.isConnected) target.moveBefore(row, at || null);
        else target.insertBefore(row, at || null);
      }
      positions.set(target, position + 1); retained.set(key, row);
    }
    activityRows.set(region, retained);
    for (const group of groups.values()) group.hidden = !$(".activity-list", group).children.length;
    const unassociated = !!list.children.length;
    const provenance = $(".activity-provenance", region);
    if (provenance) {
      const heading = $("h2", provenance), label = $("span", provenance);
      if (heading) setText(heading, "Unassociated runtime tools");
      if (label) setText(label, "Unassociated runtime tools");
    }
    // Count truncation is global: never imply it belongs to a particular step
    // or hide it with an empty fallback when all retained rows are associated.
    const stream = region.closest("#live-stream") || region.parentElement;
    const notice = $(".activity-limit", stream);
    const truncated = !!snapshot.activities_truncated || source.length > 128;
    if (notice) {
      if (region.contains(notice) && !unassociated && truncated) region.before(notice);
      notice.hidden = !truncated;
    }
    region.hidden = !unassociated;
    if (focused?.isConnected && document.activeElement !== focused) focused.focus({preventScroll: true});
  }
  window.SnowVisibility = {renderActivities, renderToolRow};

  let inspection;
  const loaded = new WeakSet();
  const routes = Object.freeze({files: "inspect/files", file: "inspect/file", changes: "inspect/changes", diff: "inspect/diff"});
  async function inspect(state, action, fields, channel) {
    state.requests[channel]?.abort();
    const controller = new AbortController(); state.requests[channel] = controller;
    const timer = setTimeout(() => controller.abort(), 10000);
    try {
      if (!routes[action]) throw new Error("Inspection is not available in this build.");
      const response = await fetch(`/projects/${encodeURIComponent(state.project)}/${routes[action]}`, {
        method: "POST", credentials: "same-origin", cache: "no-store", redirect: "error",
        headers: {"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8", Accept: "application/json"},
        body: new URLSearchParams({csrf: $('input[name="csrf"]', state.panel).value, ...fields}), signal: controller.signal
      });
      if (!response.ok) throw new Error("Inspection unavailable. The path may be protected, unsupported, too large, or changed. Refresh to try again.");
      const data = await response.json();
      if (inspection !== state || !state.panel.isConnected || state.requests[channel] !== controller) return null;
      return data;
    } catch (error) {
      if (inspection !== state || !state.panel.isConnected || state.requests[channel] !== controller) return null;
      throw new Error(controller.signal.aborted ? "Inspection timed out. Refresh to try again." : error.message);
    } finally { clearTimeout(timer); }
  }
  function notice(state, type, message, failed = false) {
    const error = $(`[data-${type}-error]`, state.panel), status = $(`[data-${type}-status]`, state.panel);
    error.hidden = !failed; error.textContent = failed ? message : "";
    status.textContent = failed ? "" : message;
    status.dataset.state = failed ? "error" : message.startsWith("Reading") ? "loading" : "ready";
  }
  const fileOrder = new Intl.Collator(undefined, {numeric: true, sensitivity: "base"});
  function rowButton(name, kind, folder = false) {
    const row = element("li"), button = element("button"); button.type = "button";
    const label = element("span", "inspection-entry-name"); pathLabel(label, name);
    button.title = text(name, 4096) + " · " + kind;
    button.append(icon(folder ? "folder" : "file"), label, element("span", "inspection-entry-kind", kind));
    if (folder) button.append(icon("chevron"));
    row.append(button); return {row, button};
  }
  function breadcrumbs(state, path) {
    const trail = $("[data-inspection-path]", state.panel); trail.replaceChildren(); trail.title = path;
    const parts = path === "." ? [] : text(path, 4096).split("/").filter(part => part && part !== ".");
    for (const [index, name] of ["Project root", ...parts].entries()) {
      if (index) trail.append(element("span", "inspection-crumb-separator", "/"));
      const button = element("button", "inspection-crumb", name); button.type = "button";
      button.dataset.inspectionCrumb = index ? parts.slice(0, index).join("/") : ".";
      button.title = button.dataset.inspectionCrumb;
      if (index === parts.length) { button.setAttribute("aria-current", "location"); button.disabled = true; }
      trail.append(button);
    }
    trail.scrollLeft = trail.scrollWidth;
  }
  function resetFile(state) {
    state.fileGeneration = (state.fileGeneration || 0) + 1;
    state.requests.file?.abort(); delete state.requests.file;
    $("[data-file-preview]", state.panel).hidden = true;
    state.panel.querySelectorAll("[data-inspection-entry]").forEach(node => { node.removeAttribute("aria-current"); node.removeAttribute("aria-busy"); });
  }
  async function files(state, path = ".", offset = 0, append = false) {
    notice(state, "files", "Reading project files…");
    const generation = (state.filesGeneration || 0) + 1; state.filesGeneration = generation;
    state.filesPending = true;
    const list = $("[data-files-list]", state.panel), more = $("[data-files-more]", state.panel);
    resetFile(state);
    list.querySelectorAll("button").forEach(button => button.disabled = true);
    list.setAttribute("aria-busy", "true"); more.disabled = true;
    $("[data-inspection-up]", state.panel).disabled = true;
    state.panel.querySelectorAll("[data-inspection-crumb]").forEach(button => button.disabled = true);
    try {
      const data = await inspect(state, "files", {path, offset: String(offset)}, "files");
      if (!data || state.filesGeneration !== generation) return;
      if (typeof data.path !== "string" || !Array.isArray(data.entries)) throw new Error("Unexpected file listing. Refresh to try again.");
      state.path = data.path; state.next = data.next_offset; state.filesLoaded = true;
      if (!append) list.replaceChildren();
      for (const entry of data.entries.slice(0, 256)) {
        if (typeof entry.path !== "string" || !["file", "directory"].includes(entry.kind) || list.children.length >= 4096) continue;
        const {row, button} = rowButton(entry.name, entry.kind === "directory" ? "Folder" : "File", entry.kind === "directory");
        button.dataset.inspectionEntry = entry.path; button.dataset.entryKind = entry.kind;
        list.append(row);
      }
      // Display order is independent of the server's authoritative page offset.
      list.append(...[...list.children].sort((left, right) => {
        const a = $("button", left), b = $("button", right);
        return Number(b.dataset.entryKind === "directory") - Number(a.dataset.entryKind === "directory") || fileOrder.compare(a.dataset.inspectionEntry, b.dataset.inspectionEntry);
      }));
      list.querySelectorAll("button").forEach(button => { button.disabled = false; button.dataset.filesGeneration = String(generation); });
      breadcrumbs(state, data.path);
      $("[data-inspection-up]", state.panel).disabled = data.path === ".";
      more.hidden = !data.has_more || list.children.length >= 4096;
      notice(state, "files", data.limited ? "Listing limit reached. Refresh or open a subfolder to inspect more." : list.children.length ? `${list.children.length} entries shown. Protected names, links and special files are omitted.` : "No visible files in this folder. Protected names, links and special files are omitted.");
    } catch (error) { notice(state, "files", error.message + (list.children.length ? " Previously listed rows are stale until refresh succeeds." : ""), true); }
    finally {
      if (inspection === state && state.filesGeneration === generation) {
        state.filesPending = false; list.setAttribute("aria-busy", "false"); more.disabled = false;
        $("[data-inspection-up]", state.panel).disabled = state.path === ".";
        state.panel.querySelectorAll("[data-inspection-crumb]").forEach(button => button.disabled = button.hasAttribute("aria-current"));
      }
    }
  }
  async function file(state, path, button) {
    const generation = state.filesGeneration;
    if (state.filesPending || state.tab !== "files" || button.disabled || button.dataset.filesGeneration !== String(generation) || !$("[data-files-list]", state.panel).contains(button)) return;
    resetFile(state);
    const selection = state.fileGeneration;
    const current = () => inspection === state && state.filesGeneration === generation && state.fileGeneration === selection && !state.filesPending && state.tab === "files" && state.panel.contains(button);
    button.setAttribute("aria-busy", "true");
    const preview = $("[data-file-preview]", state.panel); preview.hidden = true;
    notice(state, "files", "Reading text preview…");
    try {
      const data = await inspect(state, "file", {path}, "file");
      if (!data || !current()) return;
      if (typeof data.text !== "string" || typeof data.path !== "string") throw new Error("Unexpected file preview. Refresh to try again.");
      pathLabel($("[data-file-title]", state.panel), data.path);
      $("[data-file-content]", state.panel).textContent = text(data.text, 65536);
      $("[data-file-notice]", state.panel).textContent = data.truncated || data.text.length > 65536 ? "Preview truncated to 64 KiB. The original file is unchanged." : data.text ? "Read-only UTF-8 text preview." : "This file is empty.";
      preview.hidden = false;
      state.panel.querySelectorAll("[data-inspection-entry]").forEach(node => node.removeAttribute("aria-current"));
      button.setAttribute("aria-current", "true");
      notice(state, "files", "Preview loaded. Files on disk are unchanged.");
    } catch (error) { if (current()) notice(state, "files", error.message, true); }
    finally { if (current()) button.removeAttribute("aria-busy"); }
  }
  function resetDiff(state) {
    state.diffGeneration = (state.diffGeneration || 0) + 1;
    state.requests.diff?.abort(); delete state.requests.diff;
    $("[data-diff-preview]", state.panel).hidden = true;
    state.panel.querySelectorAll("[data-inspection-change]").forEach(node => { node.removeAttribute("aria-current"); node.removeAttribute("aria-busy"); });
  }
  async function changes(state) {
    notice(state, "changes", "Reading working changes…");
    const generation = (state.changesGeneration || 0) + 1; state.changesGeneration = generation;
    state.changesPending = true;
    const list = $("[data-changes-list]", state.panel); list.setAttribute("aria-busy", "true");
    resetDiff(state);
    // Old rows remain readable during refresh, but no longer own valid selections.
    list.querySelectorAll("button").forEach(button => button.disabled = true);
    try {
      const data = await inspect(state, "changes", {}, "changes");
      if (!data || state.changesGeneration !== generation) return;
      if (typeof data.available !== "boolean" || !Array.isArray(data.changes)) throw new Error("Unexpected changes response. Refresh to try again.");
      state.changesLoaded = true; list.replaceChildren();
      if (!data.available) { notice(state, "changes", text(data.reason, 2048) || "Git changes are unavailable for this project."); return; }
      for (const change of data.changes.slice(0, 256)) {
        if (typeof change.path !== "string" || !["staged", "unstaged", "untracked"].includes(change.kind)) continue;
        const kind = {staged: "Staged", unstaged: "Unstaged", untracked: "Untracked"}[change.kind];
        const {row, button} = rowButton(change.path, kind + (change.status ? " · " + text(change.status, 32) : ""));
        button.dataset.inspectionChange = change.path; button.dataset.changeKind = change.kind;
        button.dataset.changesGeneration = String(generation); list.append(row);
      }
      notice(state, "changes", data.limited || data.changes.length > 256 ? "Change listing truncated. Only a bounded set is shown." : list.children.length ? `${list.children.length} changes shown. Select a file for a read-only diff.` : "No working changes reported.");
    } catch (error) { notice(state, "changes", error.message + (list.children.length ? " Previously listed rows are stale until refresh succeeds." : ""), true); }
    finally {
      if (inspection === state && state.changesGeneration === generation) {
        state.changesPending = false; list.setAttribute("aria-busy", "false");
      }
    }
  }
  async function diff(state, path, kind, button) {
    const generation = state.changesGeneration;
    if (state.changesPending || button.disabled || state.tab !== "changes" || button.dataset.changesGeneration !== String(generation) || !$("[data-changes-list]", state.panel).contains(button)) return;
    resetDiff(state);
    button.setAttribute("aria-busy", "true");
    const selection = state.diffGeneration;
    const current = () => inspection === state && state.changesGeneration === generation && state.diffGeneration === selection && !state.changesPending && state.tab === "changes" && state.panel.contains(button);
    const preview = $("[data-diff-preview]", state.panel);
    notice(state, "changes", "Reading diff preview…");
    try {
      const data = await inspect(state, "diff", {path, kind}, "diff");
      if (!data || !current()) return;
      if (!data.available) { notice(state, "changes", text(data.reason, 2048) || "A diff is unavailable for this file."); return; }
      if (typeof data.text !== "string") throw new Error("Unexpected diff response. Refresh to try again.");
      pathLabel($("[data-diff-title]", state.panel), data.path || path);
      renderDiff($("[data-diff-content]", state.panel), data.text);
      $("[data-diff-notice]", state.panel).textContent = data.truncated || data.text.length > 65536 ? "Diff truncated for bounded display." : data.text ? "Read-only diff · " + kind : "No text diff is available for this file.";
      preview.hidden = false;
      state.panel.querySelectorAll("[data-inspection-change]").forEach(node => node.removeAttribute("aria-current")); button.setAttribute("aria-current", "true");
      notice(state, "changes", "Diff loaded. No files or Git state were changed.");
    } catch (error) { if (current()) notice(state, "changes", error.message, true); }
    finally { if (current()) button.removeAttribute("aria-busy"); }
  }
  function selectTab(state, name, focus = false) {
    if (!state) return;
    if (state.tab === "files" && name !== "files") {
      resetFile(state);
      const status = $("[data-files-status]", state.panel);
      if (["Reading text preview…", "Preview loaded. Files on disk are unchanged."].includes(status.textContent)) notice(state, "files", "Select a file for a read-only preview.");
    }
    if (state.tab === "changes" && name !== "changes") {
      resetDiff(state);
      const status = $("[data-changes-status]", state.panel);
      if (["Reading diff preview…", "Diff loaded. No files or Git state were changed."].includes(status.textContent)) notice(state, "changes", "Select a file for a read-only diff.");
    }
    state.tab = name;
    for (const tab of state.panel.querySelectorAll("[data-inspection-tab]")) {
      const selected = tab.dataset.inspectionTab === name;
      tab.setAttribute("aria-selected", String(selected)); tab.tabIndex = selected ? 0 : -1;
      $("#" + tab.getAttribute("aria-controls"), state.panel).hidden = !selected;
      if (selected && focus) tab.focus();
    }
    if (state.panel.hidden || name === "project") return;
    if (state.panel.dataset.available !== "true") { notice(state, name, "Project folder is missing or its identity changed. Re-register the folder before inspecting it.", true); return; }
    if (name === "files" && !state.filesLoaded) files(state);
    else if (name === "changes" && !state.changesLoaded) changes(state);
  }
  function init() {
    const panel = $("#project-inspector");
    if (inspection?.panel === panel) return;
    if (inspection) for (const request of Object.values(inspection.requests)) request.abort();
    inspection = panel ? {panel, project: panel.dataset.project, requests: {}, path: ".", tab: "files"} : null;
    if (!panel || loaded.has(panel)) return;
    loaded.add(panel);
    panel.addEventListener("click", event => {
      const state = inspection, button = event.target.closest("button");
      if (!state || state.panel !== panel || !button || button.disabled) return;
      if (button.dataset.inspectionTab) selectTab(state, button.dataset.inspectionTab);
      else if (panel.dataset.available !== "true") return;
      else if (button.dataset.inspectionRefresh === "files") files(state, state.path);
      else if (button.dataset.inspectionRefresh === "changes") changes(state);
      else if (state.changesPending && button.hasAttribute("data-inspection-change")) return;
      else if (state.filesPending && button.matches("[data-inspection-up],[data-files-more],[data-inspection-entry],[data-inspection-crumb]")) return;
      else if (button.hasAttribute("data-inspection-crumb")) files(state, button.dataset.inspectionCrumb);
      else if (button.hasAttribute("data-inspection-up")) files(state, state.path.includes("/") ? state.path.slice(0, state.path.lastIndexOf("/")) : ".");
      else if (button.hasAttribute("data-files-more")) files(state, state.path, state.next, true);
      else if (button.hasAttribute("data-inspection-entry")) {
        if (button.dataset.entryKind === "directory") files(state, button.dataset.inspectionEntry);
        else file(state, button.dataset.inspectionEntry, button);
      } else if (button.hasAttribute("data-inspection-change")) diff(state, button.dataset.inspectionChange, button.dataset.changeKind, button);
    });
    panel.addEventListener("keydown", event => {
      const tab = event.target.closest("[data-inspection-tab]");
      if (!tab || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const names = ["files", "changes", "project"], current = names.indexOf(tab.dataset.inspectionTab);
      const index = event.key === "Home" ? 0 : event.key === "End" ? 2 : (current + (event.key === "ArrowRight" ? 1 : 2)) % 3;
      selectTab(inspection, names[index], true);
    });
  }
  function opened() { if (inspection) selectTab(inspection, inspection.tab); }
  function select(name, project) {
    if (!inspection || inspection.project !== project || !["files", "changes", "project"].includes(name)) return false;
    selectTab(inspection, name);
    return true;
  }
  function dispose() {
    if (inspection) for (const request of Object.values(inspection.requests)) request.abort();
    inspection = null;
  }
  window.SnowInspection = {init, opened, select, dispose};
})();
