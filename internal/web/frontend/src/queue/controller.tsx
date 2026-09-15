import {createRef} from "react";
import {flushSync} from "react-dom";
import {createRoot} from "react-dom/client";
import type {Root} from "react-dom/client";
import {QueuePanel} from "./QueuePanel.tsx";
import type {RowRefs} from "./QueuePanel.tsx";
import {blocking as isBlocking, canChange, canEnqueue as mayEnqueue, presentation, sameDraft, validAcknowledgement, validQueue} from "./model.ts";
import type {Action, Admission, Draft, Operation, Presentation, Snapshot, Store, UI} from "./model.ts";
export interface QueueAPI {
  root: HTMLElement; session: string;
  validText: (text: unknown) => boolean;
  changed: () => void;
  draft: () => Draft;
  writeDraft: (text: string, selection?: Draft) => void;
  /** App owns HTTP, CSRF, identity validation, cancellation and applySnapshot. */
  request: (action?: Action, fields?: Record<string, string>) => Promise<Snapshot>;
}
interface View extends Admission {
  api: QueueAPI; root: Root; container: HTMLElement; controller: AbortController;
  snapshotRevision: number; composeRevision: number; lastRaw?: unknown;
  panelRef: ReturnType<typeof createRef<HTMLElement>>; rowRefs: Map<string, RowRefs>;
}
const stores = new Map<string, Store>();
let view: View | null = null;
export function init(api: QueueAPI): void {
  dispose();
  const container = api.root.querySelector<HTMLElement>("[data-react-composer-queue]");
  if (!container || api.root.dataset.queueNextEnabled !== "true") return;
  const key = JSON.stringify([api.root.dataset.project, api.root.dataset.session, api.root.dataset.instance]);
  const store = stores.get(key) || {editors: new Map(), unknown: false};
  stores.delete(key); stores.set(key, store);
  while (stores.size > 16) stores.delete(stores.keys().next().value!);
  const current: View = {api, root: createRoot(container), container, controller: new AbortController(),
    store, queue: null, ui: null, invalid: false, composing: false, snapshotRevision: -1, composeRevision: 0,
    panelRef: createRef<HTMLElement>(), rowRefs: new Map()};
  view = current;
  const options = {signal: current.controller.signal};
  // Only observe the foreign composer: its owner renders it and owns its draft.
  api.root.addEventListener("click", event => {
    if (view !== current || !(event.target instanceof Element)) return;
    const button = event.target.closest("button");
    if (button?.matches("[data-queue-next]")) void enqueue();
  }, options);
  for (const type of ["compositionstart", "compositionend"]) api.root.addEventListener(type, event => {
    if (view !== current || !(event.target instanceof Element) || event.target.id !== "live-prompt") return;
    current.composing = type === "compositionstart"; current.composeRevision++; api.changed();
  }, options);
  draw(current);
}
export function dispose(): void {
  if (!view) return;
  const old = view; view = null;
  old.controller.abort();
  if (old.store.busy) {old.store.busy = false; old.store.unknown = true; old.store.reviewable = false;}
  flushSync(() => old.root.unmount());
}
export function blocking(): boolean {return !!view && isBlocking(view);}
export function canEnqueue(): boolean {return !!view && mayEnqueue(view);}
/** Admission is synchronous even when presentation is intentionally deferred. */
export function render(snapshot: Snapshot | null, ui: UI, present = true): Presentation | null {
  if (!view) return null;
  view.ui = ui;
  if (snapshot && Number.isSafeInteger(snapshot.revision) && snapshot.revision >= view.snapshotRevision) {
    const raw = snapshot.queue;
    if (raw == null) {view.queue = null; view.invalid = false; view.lastRaw = null;}
    else if (raw !== view.lastRaw) {
      view.lastRaw = raw;
      if (!validQueue(raw, view.api.validText)) view.invalid = true;
      else if (!view.queue || raw.token !== view.queue.token || raw.revision >= view.queue.revision) {view.queue = raw; view.invalid = false;}
      else view.invalid = true;
    }
    view.snapshotRevision = snapshot.revision;
    if (view.store.unknown && !view.store.busy && ui.connected && (view.store.unknownRevision == null || snapshot.revision > view.store.unknownRevision)) view.store.reviewable = true;
  }
  if (present) draw(view);
  return presentation(view);
}
function draw(current: View): void {
  if (view !== current) return;
  const focused = document.activeElement;
  const ownedFocus = !!focused && current.container.contains(focused);
  flushSync(() => current.root.render(<QueuePanel current={current} panelRef={current.panelRef} rowRefs={current.rowRefs}
    onAction={(action, id) => {if (view === current) localAction(current, action, id);}}
    onText={(id, text) => {
      if (view !== current) return;
      const editor = current.store.editors.get(id);
      if (editor) {editor.text = text; editor.revision++; draw(current);}
    }} onComposition={(id, composing) => {
      if (view !== current) return;
      const editor = current.store.editors.get(id);
      if (editor) {editor.composing = composing; editor.revision++; current.api.changed();}
    }} />));
  for (const [id, refs] of current.rowRefs) if (!refs.input && !refs.edit) current.rowRefs.delete(id);
  // Restore only queue-owned focus. The parent owns composer focus policy.
  if (ownedFocus && !focused?.isConnected && document.activeElement === document.body && !current.panelRef.current?.hidden) current.panelRef.current?.focus({preventScroll: true});
}
function localAction(current: View, action: string, id = ""): void {
  const {store, api, queue, ui} = current;
  if (action === "reviewed") {
    if (store.unknown && store.reviewable && ui?.connected && !store.busy) {
      store.unknown = false; store.error = "";
      for (const editor of store.editors.values()) if (editor.token === queue?.token) editor.baseRevision = queue.revision;
      api.changed();
    }
    return;
  }
  if (action === "restore") {
    if (store.copiedDraft && !store.busy && !ui?.editing) {api.writeDraft(store.copiedDraft.value, store.copiedDraft); store.copiedDraft = null; api.changed();}
    return;
  }
  const item = queue?.items.find(item => item.id === id), editor = store.editors.get(id);
  if (action === "edit" && canChange(current) && item?.state === "pending" && ui?.status === "running" && queue) {
    store.editors.set(id, {text: item.text, revision: 0, baseRevision: queue.revision, token: queue.token});
    draw(current); current.rowRefs.get(id)?.input?.focus();
  } else if (action === "cancel" && (!store.busy || store.busy.id !== id)) {
    store.editors.delete(id); draw(current); current.rowRefs.get(id)?.edit?.focus();
  } else if (action === "remove") void mutate("queue-remove", id);
  else if (action === "save" && editor) void mutate("queue-update", id, editor.text);
  else if (action === "copy" && !store.busy && !ui?.editing && (item ? ["held", "uncertain"].includes(item.state) : !!editor)) {
    const text = editor?.text ?? item?.text;
    if (typeof text !== "string") return;
    store.copiedDraft ||= api.draft(); api.writeDraft(text); api.changed();
  }
}
async function mutate(action: Action, id: string, text?: string): Promise<boolean> {
  if (!view || !canChange(view) || action === "queue-enqueue" && !mayEnqueue(view)) return false;
  const current = view, {api, store, queue} = current;
  if (!queue) return false;
  const item = queue.items.find(item => item.id === id);
  if (action !== "queue-enqueue" && (!item || !(item.state === "pending" && current.ui?.status === "running" || action === "queue-remove" && ["held", "uncertain"].includes(item.state)))) return false;
  if (action !== "queue-remove" && !api.validText(text)) {
    store.error = "Enter a nonblank message of at most 64 KiB, without null or malformed Unicode characters."; api.changed(); return false;
  }
  const editor = store.editors.get(id), editorRevision = editor?.revision;
  if (action === "queue-remove" && editor || action === "queue-update" && editor?.composing) return false;
  const revision = action === "queue-update" ? editor?.baseRevision : queue.revision;
  const token = action === "queue-update" ? editor?.token : queue.token;
  if (revision == null || !Number.isSafeInteger(revision) || token !== queue.token) return false;
  const focus = document.activeElement instanceof HTMLElement && current.container.contains(document.activeElement) ? document.activeElement : null;
  const operation: Operation = {action, id, focus, revision, token};
  store.busy = operation; store.error = ""; store.reviewable = false; api.changed();
  try {
    const fields: Record<string, string> = {session_id: api.session, queue_token: token, queue_revision: String(revision)};
    if (id) fields.item_id = id;
    if (action !== "queue-remove") fields.text = text!;
    const result = await api.request(action, fields);
    if (view !== current || store.busy !== operation) return false;
    if (!validAcknowledgement(result.queue, operation, current.queue, api.validText)) throw new Error("Unverified queue acknowledgement");
    if (action === "queue-update" && editor) {
      const editingNow = current.rowRefs.get(id)?.input === document.activeElement;
      if (!editingNow && !editor.composing && editor.revision === editorRevision && editor.text === text) store.editors.delete(id);
      else editor.baseRevision = result.queue.revision;
    }
    if (action === "queue-remove") store.editors.delete(id);
    operation.verified = true; return true;
  } catch {
    if (view !== current || store.busy !== operation) return false;
    store.unknown = true; store.unknownRevision = current.snapshotRevision;
    store.error = "Queue request outcome needs review. Your text is kept. Nothing will be retried automatically.";
    if (editor) editor.error = "Your edit is kept. Review the current queue, then explicitly Save again if appropriate.";
    return false;
  } finally {
    if (view === current && store.busy === operation) {
      store.busy = false; api.changed();
      if (operation.verified && focus?.isConnected && document.activeElement === document.body && focus.getClientRects().length && !focus.matches(":disabled") && !focus.closest("[inert]")) focus.focus({preventScroll: true});
      // Read-only reconciliation is not evidence for an automatic retry.
      if (store.unknown) {
        try {await api.request(); if (view === current && current.ui?.connected && !current.invalid) store.reviewable = true;} catch { /* Connection guards remain authoritative. */ }
        if (view === current) api.changed();
      }
    }
  }
}
export async function enqueue(): Promise<void> {
  if (!view || !mayEnqueue(view)) return;
  const current = view, draft = current.api.draft(), composition = current.composeRevision;
  if (await mutate("queue-enqueue", "", draft.value)) {
    if (view !== current) return;
    if (!current.composing && current.composeRevision === composition && sameDraft(draft, current.api.draft())) current.api.writeDraft("");
    current.api.changed();
  }
}
export const queue = Object.freeze({init, dispose, render, blocking, canEnqueue, enqueue});
export default queue;
