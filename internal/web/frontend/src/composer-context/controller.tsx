import {createRef} from 'react';
import type {CSSProperties} from 'react';
import {createPortal, flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import {ContextPanel, ContextTools} from './Panel.tsx';
import type {Actions, Presentation, Refs} from './Panel.tsx';
import {TEXT_LIMIT, PROMPT_LIMIT, IMAGE_LIMIT, bytes, validText, imageType, base64, labelText, privacyNotice, releasePreview, previewURL} from './model.ts';
import type {API, Authority, Choice, Controller, Directory, Draft, FileResponse, Item, ListingResponse, Query, SkillsResponse, Snapshot} from './types.ts';
// Canonical synchronous state remains outside React. Retained drafts never leave tab memory.
const drafts = new Map<string, Draft>();
let sequence = 0, active: Controller | null = null;
function retire(draft: Draft) {
  draft.owner = null;
  for (const item of draft.items) {
    releasePreview(item); item.previewFailed = false;
    if (item.state === "pending") {
      item.state = "error"; item.error = "Read interrupted. Remove this attachment and add it again."; item.version++;
    }
  }
  draft.revision++;
}
export function forget(key: string) {
  const draft = drafts.get(key); if (draft) retire(draft);
  drafts.delete(key);
}
export function init({root, key, instance, request, changed = () => {}, error = () => {}, replaceText, focusPrompt = () => {}, suggestions = () => {}}: API): Controller {
  active?.dispose(); active = null;
  const find = (selector: string) => root.querySelector<HTMLElement>(selector);
  const promptNode = root.querySelector<HTMLTextAreaElement>("#live-prompt");
  const containerNode = find("[data-composer-context-root]"), toolsNode = find("[data-composer-context-tools-root]");
  if (!promptNode || !containerNode || !toolsNode) throw new Error("Composer context markup is incomplete");
  const prompt = promptNode, container = containerNode, tools = toolsNode;
  const view = createRoot(container);
  const refs: Refs = {input: createRef(), attach: createRef(), trigger: createRef(), popup: createRef(), menu: createRef()};
  const draft: Draft = drafts.get(key) || {items: [], revision: 0};
  if (drafts.has(key)) { retire(draft); drafts.delete(key); }
  const owner = {}; draft.owner = owner; drafts.set(key, draft);
  while (drafts.size > 16) forget(drafts.keys().next().value!);
  const listeners = new AbortController(), options = {signal: listeners.signal};
  let disposed = false, authority: Authority = {safe: false, editable: false, readable: false};
  let queryRevision = 0, query: Query | null = null, rows: Choice[] = [], selected = 0, directory: Directory | null = null, skillsPromise: Promise<unknown> | null = null;
  let localError = "", composing = false, reading = 0, pendingBytes = 0, discoveries = 0, positionObserver: ResizeObserver | undefined;
  let plusMenu = false, popupVisible = false, message = "", heading = false;
  let popupStyle: CSSProperties = {}, menuStyle: CSSProperties = {};
  let lastSuggestions = "";
  const alive = () => !disposed && draft.owner === owner && drafts.get(key) === draft;
  const canRemove = () => alive() && authority.editable && !prompt.closest("[inert], [hidden]");
  const canAdd = () => canRemove() && authority.safe;
  const canRead = () => canAdd() && authority.readable;
  const pending = () => alive() && (reading > 0 || discoveries > 0 || draft.items.some(item => item.state === "pending"));
  const hasAttachments = () => alive() && draft.items.length > 0;
  function close() {
    queryRevision++; query = null; rows = []; popupVisible = false; message = "";
    publish();
  }
  function closePlusMenu(restoreFocus = false) {
    if (!plusMenu) return;
    plusMenu = false; publish();
    if (restoreFocus) refs.trigger.current?.focus({preventScroll: true});
  }
  function openPlusMenu() {
    if (!canRead()) return;
    if (plusMenu) { closePlusMenu(true); return; }
    close(); plusMenu = true; publish(); positionMenu();
    refs.menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({preventScroll: true});
  }
  function report(message: string) { if (!alive()) return; localError = message; paint(); error(message); changed(); }
  function notify() { if (alive()) { draft.revision++; paint(); changed(); } }
  function publish() {
    if (!alive()) return;
    const imageNotice = draft.items.some(item => item.kind === "image") ? " Images need a vision-capable model." : "";
    const notice = [localError, draft.items.length ? `On Send: shared with provider and saved in chat.${imageNotice}` : "", !authority.editable && draft.items.length ? "Attachments are kept; finish or cancel the current operation to change them." : ""].filter(Boolean).join(" ");
    const state: Presentation = {items: draft.items.map(item => ({...item, url: previewURL(item)})), canRemove: canRemove(), canAttach: canAdd() && draft.items.length < 8,
      canRead: canRead(), canFiles: canRead() && draft.items.length < 8, notice, privacy: draft.items.length ? privacyNotice : "", error: !!localError,
      popupVisible, rows: [...rows], selected, message, heading, marker: query?.marker || "", menuOpen: plusMenu, popupStyle, menuStyle};
    const actions: Actions = {
      remove: shown => { const item = draft.items.find(item => item.id === shown.id); if (!canRemove() || !item) return;
        releasePreview(item); draft.items = draft.items.filter(candidate => candidate !== item); localError = ""; notify();
        if (!root.ownerDocument.activeElement || root.ownerDocument.activeElement === root.ownerDocument.body) refs.attach.current?.focus({preventScroll: true}); },
      previewError: (shown, url, image) => { const item = draft.items.find(item => item.id === shown.id);
        if (!alive() || !item || item.previewURL !== url || image.getAttribute('src') !== url || !container.contains(image)) return;
        releasePreview(item); item.previewFailed = true; paint(); },
      choose: choice => { if (query && validQuery(query) && rows.includes(choice) && !choice.disabled) choice.choose(); },
      addFiles, attach: () => { if (canAdd()) refs.input.current?.click(); }, menu: openPlusMenu,
      marker: marker => { if (!plusMenu || !canRead() || marker === '@' && draft.items.length >= 8) return; closePlusMenu(); insertMarker(marker); },
      menuKey: event => {
        if (!plusMenu || event.nativeEvent.isComposing) return;
        if (event.key === 'Escape' || event.key === 'Tab') { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); } closePlusMenu(true); return; }
        if (event.key === 'PageDown' || event.key === 'PageUp') {
          event.preventDefault();
          const content = refs.menu.current?.querySelector<HTMLElement>('.snow-menu-content');
          if (content) content.scrollTop += (event.key === 'PageUp' ? -1 : 1) * content.clientHeight;
          return;
        }
        if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
        event.preventDefault(); event.stopPropagation();
        const buttons = [...(refs.menu.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])];
        const index = buttons.indexOf(root.ownerDocument.activeElement as HTMLButtonElement);
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : event.key === 'ArrowUp' && index < 0 ? buttons.length - 1 : (index + (event.key === 'ArrowUp' ? -1 : 1) + buttons.length) % buttons.length;
        const row = buttons[next]; row?.focus({preventScroll: true});
        const content = refs.menu.current?.querySelector<HTMLElement>('.snow-menu-content');
        if (row && content) {
          const bounds = content.getBoundingClientRect(), rect = row.getBoundingClientRect();
          if (rect.top < bounds.top) content.scrollTop -= bounds.top - rect.top;
          else if (rect.bottom > bounds.bottom) content.scrollTop += rect.bottom - bounds.bottom;
        }
      },
    };
    const focusedElement = root.ownerDocument.activeElement;
    const focusedItem = focusedElement && container.contains(focusedElement) ? focusedElement.getAttribute("data-context-item-id") : null;
    flushSync(() => view.render(<><ContextPanel state={state} actions={actions} refs={refs} />{createPortal(<ContextTools state={state} actions={actions} refs={refs} />, tools)}</>));
    if (focusedItem) {
      const target = [...container.querySelectorAll<HTMLButtonElement>("[data-context-item-id]")].find(button => button.dataset.contextItemId === focusedItem) || refs.attach.current;
      if (target && target !== root.ownerDocument.activeElement) target.focus({preventScroll: true});
    }
    const suggestionState = {controls: 'composer-mentions', expanded: popupVisible, activeDescendant: popupVisible ? rows[selected]?.id : undefined};
    const signature = JSON.stringify(suggestionState);
    if (signature !== lastSuggestions) { lastSuggestions = signature; suggestions(suggestionState); }
  }
  function paint() { if (alive()) { publish(); position(); positionMenu(); } }
  function positionMenu() {
    const panel = refs.menu.current, trigger = refs.trigger.current;
    if (!plusMenu || !panel || !trigger) return;
    const viewport = window.visualViewport, rect = trigger.getBoundingClientRect();
    const left = viewport?.offsetLeft || 0, top = viewport?.offsetTop || 0;
    const width = viewport?.width || window.innerWidth, height = viewport?.height || window.innerHeight;
    const minWidth = Math.max(0, Math.min(240, width - 24)), maxWidth = Math.max(0, Math.min(420, width - 24));
    const maxHeight = Math.max(0, Math.min(360, height - 96));
    menuStyle = {minWidth, maxWidth, maxHeight, left: Math.max(left + 12, Math.min(rect.left, left + width - panel.offsetWidth - 12)), top: Math.max(top + 12, Math.min(rect.top - 8 - panel.offsetHeight, top + height - panel.offsetHeight - 12))};
    publish();
  }
  function render(next: Partial<Authority>) {
    authority = {...authority, ...next};
    if (!canRead()) { close(); closePlusMenu(); }
    paint();
  }
  function reserve(label: string, size = 0) {
    if (!canAdd()) return null;
    if (draft.items.length >= 8) { report("At most 8 attachments can be added. Remove one before adding another."); return null; }
    if (!validText(label) || bytes(label) > 4096) { report("Attachment name is invalid or too long."); return null; }
    if (!Number.isSafeInteger(size) || size < 0 || size > IMAGE_LIMIT || pendingBytes + size > IMAGE_LIMIT || reading >= 8) {
      report("Attachments being read must total at most 2 MiB. Wait for current reads or choose a smaller file."); return null;
    }
    const item: Item = {id: ++sequence, version: 0, label, state: "pending", size};
    draft.items.push(item); reading++; pendingBytes += size; localError = ""; notify(); return item;
  }
  const owns = (item: Item) => alive() && draft.items.includes(item) && item.state === "pending";
  function complete(item: Item, content: Partial<Item>) {
    if (!owns(item)) return false;
    const candidate: Item = {...item, ...content, state: "ready"};
    const ready = draft.items.filter(other => other !== item && other.state === "ready");
    if (ready.reduce((sum, other) => sum + bytes(labelText(other)), bytes(labelText(candidate))) > TEXT_LIMIT) throw new Error("Attachment text and labels must total at most 64 KiB.");
    if (ready.reduce((sum, other) => sum + (other.kind === "image" ? other.size : 0), candidate.kind === "image" ? candidate.size : 0) > IMAGE_LIMIT) throw new Error("Image attachments must total at most 2 MiB.");
    Object.assign(item, candidate); item.version++; notify(); return true;
  }
  function failed(item: Item, message: string) {
    if (!owns(item)) return;
    item.state = "error"; item.error = message; item.version++; notify();
  }
  function finished(size: number) {
    reading--; pendingBytes -= size;
    if (alive()) { paint(); changed(); }
  }
  async function localFile(file: File) {
    const item = reserve(file.name || "Pasted image", file.size); if (!item) return;
    try {
      const data = new Uint8Array(await file.arrayBuffer());
      if (!owns(item)) return;
      if (data.length !== file.size || data.length > IMAGE_LIMIT) throw new Error("File changed or exceeds the attachment size limit.");
      const mime = imageType(data);
      if (mime) complete(item, {kind: "image", mime, data: base64(data), size: data.length});
      else {
        if (/^image\//i.test(file.type || "") || String.fromCharCode(...data.subarray(0, 5)) === "%PDF-") throw new Error("Only PNG, JPEG, GIF, WebP images or UTF-8 text can be attached; PDFs are not supported.");
        if (data.length > TEXT_LIMIT) throw new Error("Text attachments must total at most 64 KiB.");
        let text;
        try { text = new TextDecoder("utf-8", {fatal: true}).decode(data); }
        catch (_) { throw new Error("This file is not valid UTF-8 text or a supported image."); }
        if (!validText(text)) throw new Error("Text attachments cannot contain NUL bytes or invalid Unicode.");
        complete(item, {kind: "text", text, size: data.length});
      }
    } catch (reason) { failed(item, (reason instanceof Error ? reason.message : "") || "Unable to read attachment. Remove it and try again."); }
    finally { finished(file.size); }
  }
  function addFiles(list: ArrayLike<File>) {
    if (!canAdd()) return;
    // Do not retain an unbounded browser FileList or start unbounded reads.
    for (let i = 0; i < Math.min(list.length, 8); i++) localFile(list[i]);
    if (list.length > 8) report("Only the first 8 files were considered. At most 8 attachments are allowed.");
  }
  function token(): Query | null {
    if (prompt.selectionStart !== prompt.selectionEnd) return null;
    const caret = prompt.selectionStart, before = prompt.value.slice(0, caret);
    const match = /(?:^|\s)([@$])(?:"([^"\n]*)|([^\s"@$]*))$/.exec(before);
    if (!match) return null;
    const marker = match[1], rawValue = match[2] ?? match[3];
    let value = rawValue;
    if (match[2] !== undefined) {
      try { value = JSON.parse(`"${rawValue}"`); } catch (_) { return null; }
    }
    return {marker, value, revision: 0, path: ".", filter: "", start: caret - rawValue.length - 1 - (match[2] !== undefined ? 1 : 0), end: caret, text: prompt.value, caret};
  }
  const validQuery = (current: Query | null): current is Query => !!current && alive() && canRead() && query === current && current.revision === queryRevision && prompt.value === current.text && prompt.selectionStart === current.caret && prompt.selectionEnd === current.caret;
  function replace(current: Query, value: string, reopen = false) {
    if (!validQuery(current)) return false;
    if (!replaceText?.(value, current.start, current.end)) return false;
    close(); inspectToken(); focusPrompt();
    if (!reopen) close();
    return true;
  }
  function select(index: number) {
    selected = index; publish();
    const popup = refs.popup.current, row = popup?.querySelectorAll<HTMLElement>('[role="option"]')[selected];
    if (row && popup) {
      if (row.offsetTop < popup.scrollTop) popup.scrollTop = row.offsetTop;
      else if (row.offsetTop + row.offsetHeight > popup.scrollTop + popup.clientHeight) popup.scrollTop = row.offsetTop + row.offsetHeight - popup.clientHeight;
    }
  }
  function position() {
    const popup = refs.popup.current;
    if (!popupVisible || !popup) return;
    const form = (find("#live-composer") || root).getBoundingClientRect();
    const inputBounds = prompt.getBoundingClientRect(), viewport = window.visualViewport;
    const margin = 8, gap = 8;
    const left = (viewport?.offsetLeft || 0) + margin, top = (viewport?.offsetTop || 0) + margin;
    const width = Math.max(0, (viewport?.width || window.innerWidth) - margin * 2);
    const height = Math.max(0, (viewport?.height || window.innerHeight) - margin * 2), bottom = top + height;
    const minimum = Math.min(120, height), aboveForm = form.top - gap - top;
    const aboveInput = inputBounds.top - gap - top, belowInput = bottom - inputBounds.bottom - gap;
    let clearance, anchor, below = false;
    // Prefer above the composer. Short screens may need an input-adjacent
    // placement; retain the textarea rather than shrinking options to slivers.
    if (aboveForm >= minimum) { clearance = aboveForm; anchor = form.top - gap; }
    else if (aboveInput >= minimum) { clearance = aboveInput; anchor = inputBounds.top - gap; }
    else if (belowInput >= minimum) { clearance = belowInput; anchor = inputBounds.bottom + gap; below = true; }
    else { clearance = height; anchor = bottom; }
    const popupWidth = Math.min(Math.max(0, form.width), width);
    popupStyle = {...popupStyle, width: `${popupWidth}px`, maxHeight: `${Math.min(320, Math.max(0, clearance))}px`, left: `${Math.max(left, Math.min(form.left, left + width - popupWidth))}px`};
    publish();
    const actualHeight = Math.min(popup.offsetHeight, clearance, 320);
    popupStyle = {...popupStyle, top: `${Math.max(top, Math.min(below ? anchor : anchor - actualHeight, bottom - actualHeight))}px`};
    publish();
    select(selected);
  }
  function show(current: Query, choices: Choice[], note = "", isHeading = false) {
    if (!validQuery(current)) return;
    rows = choices.map(choice => ({...choice, id: `composer-context-option-${++sequence}`}));
    selected = 0; popupVisible = true; message = note; heading = isHeading;
    publish(); select(0); position();
  }
  function validPath(path: unknown): path is string {
    return typeof path === "string" && bytes(path) <= 4096 && validText(path) && !/[\x00-\x1f\x7f\\:]/.test(path) && path.split("/").every(part => part && part !== "." && part !== "..");
  }
  function fileChoices(current: Query, listing: Directory) {
    if (!validQuery(current)) return;
    const choices: Choice[] = listing.entries.filter(entry => entry.name.toLocaleLowerCase().includes(current.filter.toLocaleLowerCase())).map(entry => ({
      label: entry.name, title: entry.path, folder: entry.kind === "directory",
      choose: () => {
        if (entry.kind === "directory") replace(current, `@${JSON.stringify(entry.path + "/").replaceAll("$", "\\u0024").slice(0, -1)}`, true);
        else projectFile(current, entry.path);
      }
    }));
    if (listing.hasMore && listing.offset < 4096 && listing.entries.length < 4096) choices.push({label: "More files…", choose: () => loadFiles(current, listing.offset, listing)});
    show(current, choices, listing.limited ? "Directory listing is limited to 4,096 entries. Type a folder path to narrow it." : choices.length ? "Files & folders" : "No matching files in this folder.", !listing.limited && choices.length > 0);
  }
  async function loadFiles(current: Query, offset = 0, previous: Directory | null = null) {
    if (!validQuery(current) || current.loading) return;
    current.loading = true; discoveries++; changed(); show(current, [], "Loading files…");
    try {
      const response = (await request("files", {path: current.path, offset})) as ListingResponse;
      if (!validQuery(current)) return;
      if (!response || response.path !== current.path || !Array.isArray(response.entries) || response.entries.length > 256) throw new Error("Invalid directory listing.");
      const entries = response.entries.filter(entry => entry && ["file", "directory"].includes(entry.kind) && validPath(entry.path) && typeof entry.name === "string" && entry.name === entry.path.split("/").at(-1) && !entry.path.includes('"') && (current.path === "." ? !entry.path.includes("/") : entry.path.slice(0, entry.path.lastIndexOf("/")) === current.path));
      const merged = [...(previous?.entries || []), ...entries].slice(0, 4096);
      directory = {path: current.path, entries: merged, offset: response.next_offset, hasMore: response.has_more === true && Number.isSafeInteger(response.next_offset) && response.next_offset > offset, limited: response.limited === true || merged.length >= 4096};
      fileChoices(current, directory);
    } catch (_) { if (validQuery(current)) show(current, [], "Could not list this folder. Edit the @path to try again."); }
    finally { current.loading = false; discoveries--; if (alive()) changed(); }
  }
  async function projectFile(current: Query, path: string) {
    if (!validQuery(current) || current.reading) return;
    const item = reserve(path); if (!item) return;
    current.reading = true; show(current, [], "Reading selected file…");
    try {
      const response = (await request("file", {path})) as FileResponse;
      if (!owns(item)) return;
      if (!validQuery(current)) throw new Error("Selection changed during the read. Remove this attachment and select the file again.");
      if (!response || response.path !== path || response.truncated !== false) throw new Error("This file preview is truncated or unavailable. Partial files are not attached.");
      if (!validText(response.text) || !Number.isSafeInteger(response.size) || response.size < 0 || response.size > TEXT_LIMIT || bytes(response.text) > TEXT_LIMIT) throw new Error("Selected file must be UTF-8 text without NUL bytes, at most 64 KiB.");
      if (complete(item, {kind: "text", text: response.text, size: response.size})) replace(current, `@${JSON.stringify(path).replaceAll("$", "\\u0024")} `);
    } catch (reason) { failed(item, (reason instanceof Error ? reason.message : "") || "Could not read selected file."); if (validQuery(current)) close(); }
    finally { current.reading = false; finished(0); }
  }
  async function loadSkills(current: Query) {
    if (!validQuery(current) || current.skillsLoading) return;
    current.skillsLoading = true; discoveries++; changed();
    show(current, [], "Loading installed skills…");
    try {
      // Failed discoveries stay cached until the explicit Retry choice.
      // Typing and reopening never cause a hidden discovery loop.
      if (!skillsPromise) {
        if (draft.skillCatalog?.instance === instance) skillsPromise = draft.skillCatalog.promise;
        else {
          skillsPromise = Promise.resolve().then(() => request("skills", {}));
          draft.skillCatalog = {instance, promise: skillsPromise};
        }
      }
      const response = (await skillsPromise) as SkillsResponse;
      if (!validQuery(current)) return;
      if (!response || response.instance_id !== instance || !Array.isArray(response.skills) || response.skills.length > 4096) throw new Error("Invalid installed skill catalog.");
      if (response.enabled !== true) { show(current, [], "Close this runtime, enable installed skills in Settings → Workspaces, then start again."); return; }
      const choices = response.skills.filter(skill => skill && typeof skill.name === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(skill.name) && skill.name.toLowerCase().includes(current.value.toLowerCase())).slice(0, 256).map(skill => ({
        label: `$${skill.name}`, description: skill.enabled === true ? String(skill.description || "").slice(0, 512) : `Disabled${typeof skill.disabled_by === "string" ? `: ${skill.disabled_by.slice(0, 128)}` : ""}`,
        fullDescription: skill.enabled === true ? String(skill.description || "") : `Disabled${typeof skill.disabled_by === "string" ? `: ${skill.disabled_by}` : ""}`,
        disabled: skill.enabled !== true, choose: () => replace(current, `$${skill.name} `)
      }));
      show(current, choices, response.limited || choices.length === 256 ? "Catalog is limited. Type a skill name to narrow results." : choices.length ? "Skills" : "No matching enabled installed skills.", !response.limited && choices.length > 0 && choices.length < 256);
    } catch (_) {
      if (validQuery(current)) show(current, [{label: "Retry loading skills", retrySkills: true, choose: () => {
        if (!validQuery(current) || current.skillsLoading) return;
        if (draft.skillCatalog?.promise === skillsPromise) delete draft.skillCatalog;
        skillsPromise = null;
        loadSkills(current);
      }}], "Installed skills could not be loaded for this runtime. Nothing was activated. Retry only when you choose.");
    }
    finally { current.skillsLoading = false; discoveries--; if (alive()) changed(); }
  }
  function inspectToken() {
    close();
    if (!canRead() || composing) return;
    const current = token(); if (!current) return;
    current.revision = queryRevision; query = current;
    if (current.marker === "$") { loadSkills(current); return; }
    const slash = current.value.lastIndexOf("/");
    current.path = slash < 0 ? "." : current.value.slice(0, slash);
    current.filter = current.value.slice(slash + 1);
    if (current.path !== "." && !validPath(current.path)) { close(); return; }
    if (directory?.path === current.path) fileChoices(current, directory); else loadFiles(current);
  }
  function insertMarker(marker: string) {
    if (!canRead()) return;
    const start = prompt.selectionStart;
    if (replaceText?.(`${start && !/\s/.test(prompt.value[start - 1]) ? " " : ""}${marker}`, start, prompt.selectionEnd)) { focusPrompt(); inspectToken(); }
  }
  function capture(text = prompt!.value): Snapshot {
    if (!alive()) throw new Error("This attachment draft is no longer active.");
    if (pending()) throw new Error("Wait for attachment reads to finish before sending.");
    if (draft.items.some(item => item.state === "error")) throw new Error("Remove failed attachments before sending; they have not been silently omitted.");
    if (!validText(text)) throw new Error("Prompt must be valid Unicode without NUL bytes.");
    const items = draft.items.map(item => Object.freeze({id: item.id, version: item.version}));
    const fallback = text.trim() ? text : items.length ? "Please review the attached files." : text;
    // RPC prepends the separate Message field. Content contains attachments only,
    // and untrusted filenames must never become explicit skill-activation text.
    const blocks = [];
    for (const item of draft.items) {
      blocks.push({type: "text", text: labelText(item)});
      if (item.kind === "image") blocks.push({type: "image", mime_type: item.mime, data: item.data});
    }
    if (blocks.reduce((sum, block) => sum + (block.type === "text" ? bytes(block.text || "") : 0), bytes(fallback)) > PROMPT_LIMIT) throw new Error("Prompt and attachment text must total at most 128 KiB, including labels.");
    return Object.freeze({revision: draft.revision, items: Object.freeze(items), content: JSON.stringify(blocks), text: fallback, hasContent: items.length > 0, key, owner});
  }
  function accepted(snapshot: Snapshot) {
    if (!alive() || snapshot?.key !== key || snapshot.owner !== owner) return;
    draft.items = draft.items.filter(item => {
      if (!snapshot.items.some(sent => sent.id === item.id && sent.version === item.version)) return true;
      releasePreview(item); return false;
    });
    localError = ""; notify();
  }
  const reposition = () => { position(); positionMenu(); };
  window.addEventListener?.("resize", reposition, options);
  window.addEventListener?.("scroll", reposition, {...options, capture: true});
  window.visualViewport?.addEventListener("resize", reposition, options);
  window.visualViewport?.addEventListener("scroll", reposition, options);
  if (window.ResizeObserver) {
    positionObserver = new ResizeObserver(reposition);
    positionObserver.observe(find("#live-composer") || root);
  }
  prompt.addEventListener("input", inspectToken, options);
  prompt.addEventListener("compositionstart", () => { composing = true; close(); }, options);
  prompt.addEventListener("compositionend", () => { composing = false; inspectToken(); }, options);
  prompt.addEventListener("click", () => { if (query && !validQuery(query)) close(); }, options);
  prompt.addEventListener("blur", close, options);
  prompt.addEventListener("keyup", () => { if (query && !validQuery(query)) close(); }, options);
  prompt.addEventListener("keydown", event => {
    if (event.isComposing || composing || event.ctrlKey || event.metaKey || event.altKey || !popupVisible) return;
    if (!validQuery(query)) { close(); return; }
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close(); }
    else if (["ArrowDown", "ArrowUp"].includes(event.key) && rows.length) {
      event.preventDefault(); event.stopPropagation(); select((selected + (event.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length);
    } else if (event.key === "Enter" && rows.length) {
      event.preventDefault(); event.stopPropagation(); if (!rows[selected].disabled) rows[selected].choose();
    } else if (event.key === "Tab") close();
  }, options);
  const composer = find("#live-composer") || root;
  composer.addEventListener("dragover", event => {
    if ([...(event.dataTransfer?.types || [])].includes("Files")) { event.preventDefault(); event.dataTransfer!.dropEffect = canAdd() ? "copy" : "none"; }
  }, options);
  composer.addEventListener("drop", event => { if (event.dataTransfer?.files?.length) { event.preventDefault(); addFiles(event.dataTransfer.files); } }, options);
  composer.addEventListener("paste", event => {
    const images = [...(event.clipboardData?.items || [])].filter(item => item.kind === "file" && /^image\//.test(item.type)).slice(0, 8).map(item => item.getAsFile()).filter((file): file is File => !!file);
    if (images.length) { event.preventDefault(); addFiles(images); }
  }, options);
  function dispose() {
    if (disposed) return;
    close(); closePlusMenu(); listeners.abort(); positionObserver?.disconnect();
    if (alive()) retire(draft);
    disposed = true;
    flushSync(() => view.unmount());
    suggestions({expanded: false});
  }
  root.ownerDocument.addEventListener('snow:navigation-before-swap', () => closePlusMenu(false), options);
  root.ownerDocument.addEventListener('pointerdown', event => {
    if (plusMenu && event.target instanceof Node && !refs.menu.current?.contains(event.target) && !refs.trigger.current?.contains(event.target)) closePlusMenu(false);
  }, options);
  root.ownerDocument.addEventListener('focusin', event => {
    if (plusMenu && event.target instanceof Node && !refs.menu.current?.contains(event.target) && !refs.trigger.current?.contains(event.target)) closePlusMenu(false);
  }, options);
  close(); paint();
  active = Object.freeze({render, capture, accepted, hasAttachments, pending, dispose});
  return active;
}
export const composerContext = Object.freeze({
init, forget,
render: (state: Partial<Authority>) => active?.render(state),
capture: (text?: string) => { if (!active) throw new Error("Composer context is unavailable."); return active.capture(text); },
accepted: (snapshot: Snapshot) => active?.accepted(snapshot),
hasAttachments: () => active?.hasAttachments() || false,
pending: () => active?.pending() || false,
dispose: () => { active?.dispose(); active = null; }
});
