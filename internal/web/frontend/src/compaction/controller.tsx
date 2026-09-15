import {flushSync} from "react-dom";
import {createRoot} from "react-dom/client";
import type {Root} from "react-dom/client";
import {ManualCompaction} from "./ManualCompaction.tsx";
import {idleSafe, scope, validACK, validCompaction} from "./model.ts";
import type {Identity, Scope, Snapshot, UI} from "./model.ts";

export interface CompactionAPI {
  root: HTMLElement; identity: Identity;
  reserve: (active: boolean) => unknown;
  commit: (fields: Scope) => Promise<unknown>;
}
interface View {
  api: CompactionAPI; root: Root; launcher: HTMLElement; controller: AbortController;
  snapshot: Snapshot | null; ui: UI | null;
  invalid: boolean; committing: boolean; notice: string; dismissed: boolean;
}
let view: View | null = null;
function canStart(current: View): boolean {
  return view === current && !current.committing && !!current.ui?.supported && !!current.ui.readable &&
    idleSafe(current.snapshot, current.ui, current.invalid) && !!scope(current.snapshot, current.api.identity.session_id);
}
function controls(current = view): void {
  if (!current || current !== view) return;
  const c = current.snapshot?.compaction;
  flushSync(() => current.root.render(<ManualCompaction launcher={current.launcher}
    compaction={validCompaction(c, current.api.identity.session_id) ? c : null}
    invalid={current.invalid} notice={current.notice} dismissed={current.dismissed} supported={!!current.ui?.supported}
    canStart={canStart(current)} committing={current.committing} stopping={current.ui?.stopLabel === "Stopping…"} />));
}
export function init(api: CompactionAPI): void {
  dispose();
  const container = api.root.querySelector<HTMLElement>('[data-react-live-panel="compaction"]');
  const launcher = api.root.querySelector<HTMLElement>('[data-compaction-launcher]');
  if (!container || !launcher) return;
  // One owner renders the composer icon and inline status. No second dialog,
  // confirmation or independent mutation path is needed for an explicit click.
  const current: View = {api, root: createRoot(container), launcher, controller: new AbortController(), snapshot: null, ui: null, invalid: false, committing: false, notice: "", dismissed: false};
  view = current; controls(current);
  api.root.addEventListener("click", event => {
    if (!(event.target instanceof Element)) return;
    if (event.target.closest("button[data-compaction-open]")) void commit(current);
    if (event.target.closest("button[data-compaction-dismiss]")) {
      current.dismissed = true; controls(current);
      current.launcher.querySelector<HTMLButtonElement>('[data-compaction-open]')?.focus({preventScroll: true});
    }
  }, {signal: current.controller.signal});
}
export function dispose(): void {
  if (!view) return;
  const old = view; view = null;
  old.controller.abort();
  old.api.reserve(false);
  flushSync(() => old.root.unmount());
}
export function render(snapshot: Snapshot | null, ui: UI): void {
  if (!view) return;
  if (snapshot?.compaction !== view.snapshot?.compaction && validCompaction(snapshot?.compaction, view.api.identity.session_id) &&
      (!validCompaction(view.snapshot?.compaction, view.api.identity.session_id) || snapshot.compaction.request_id !== view.snapshot.compaction.request_id)) view.dismissed = false;
  view.snapshot = snapshot; view.ui = ui;
  view.invalid = snapshot?.compaction != null && !validCompaction(snapshot.compaction, view.api.identity.session_id);
  controls();
}
async function commit(current: View): Promise<void> {
  if (!canStart(current)) return;
  // Capture the current session/branch/tip/revision on this click. The existing
  // serial admission layer and server recheck them before starting provider work.
  const fields = scope(current.snapshot, current.api.identity.session_id);
  if (!fields) return;
  current.committing = true; current.notice = ""; current.dismissed = false;
  if (current.api.reserve(true) === false) { current.committing = false; controls(current); return; }
  controls(current);
  let result: unknown = false;
  try { result = await current.api.commit(fields); } catch { /* Reconcile read-only; never retry. */ }
  if (view !== current) return;
  current.committing = false;
  current.api.reserve(false);
  // An ACK is not completion and cannot overwrite a faster terminal snapshot.
  current.notice = result ? "" : "Could not confirm compaction. Check the current status before trying again. Nothing was retried.";
  controls(current);
}
export {validCompaction, validACK};
export const compaction = Object.freeze({init, render, dispose, validCompaction, validACK});
export default compaction;
