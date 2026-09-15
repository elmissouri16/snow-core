import {createRef} from 'react';
import type {RefObject} from 'react';
import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {Panel} from './Panel';
import {inventory, logPage, resultForScope, validRecord} from './model';
import type {ProcessRecord, Scope} from './model';
export type View = Scope & {
  owner: HTMLElement; mount: HTMLElement; root: Root; controller: AbortController; observer: MutationObserver;
  panel: RefObject<HTMLDetailsElement | null>; dialog: RefObject<HTMLDialogElement | null>;
  records: ProcessRecord[]; selected: string; cursor?: number; eof: boolean; output: string; logHeading: string; logStatus: string;
  generation: number; status: string; error: string; busy: boolean; unknown: boolean; confirmation: string; confirmationReturn: HTMLElement | null;
  request: {controller: AbortController; action: string} | null; timer?: ReturnType<typeof setTimeout>;
};
let view: View | null = null;
function same(v: View) {
  return view === v && v.mount.isConnected && v.owner === document.querySelector('#live-session') && v.owner.dataset.project === v.project && v.owner.dataset.instance === v.instance && v.owner.dataset.session === v.session;
}
function scope(v: View) { if (same(v)) return true; if (view === v) dispose(); return false; }
function visible(v: View) { return scope(v) && !!v.panel.current?.open && !!v.dialog.current?.open && document.visibilityState === 'visible' && v.panel.current.getClientRects().length > 0; }
function paint(v: View) { if (same(v)) flushSync(() => v.root.render(<Panel view={v} actions={actions}/>)); }
export function init() {
  dispose();
  const mount = document.querySelector<HTMLElement>('[data-react-processes]'), owner = document.querySelector<HTMLElement>('#live-session');
  if (!mount || !owner || !owner.contains(mount) || !owner.dataset.project || !owner.dataset.instance || !owner.dataset.session) return;
  const controller = new AbortController();
  const v: View = {mount, owner, root: createRoot(mount), project: owner.dataset.project, instance: owner.dataset.instance, session: owner.dataset.session,
    controller, observer: new MutationObserver(() => { if (scope(v) && !visible(v)) suspend(v); }), panel: createRef(), dialog: createRef(),
    generation: 0, records: [], selected: '', eof: false, output: '', logHeading: '', logStatus: '', status: 'Open to read inventory.', error: '', busy: false, unknown: false, confirmation: '', confirmationReturn: null, request: null};
  view = v; paint(v);
  v.observer.observe(owner, {attributes: true, attributeFilter: ['data-project', 'data-instance', 'data-session', 'hidden']});
  // Ancestor replacement and visibility changes retire requests even before a late body resolves.
  const workspace = owner.closest('#workspace'); if (workspace?.parentElement) v.observer.observe(workspace.parentElement, {childList: true});
  document.addEventListener('visibilitychange', () => { if (!scope(v)) return; if (visible(v)) void refresh(v); else suspend(v); }, {signal: controller.signal});
}
export function dispose() {
  const old = view; view = null; if (!old) return;
  clearTimeout(old.timer); old.observer.disconnect(); old.controller.abort(); old.request?.controller.abort();
  if (old.panel.current) old.panel.current.open = false;
  if (old.dialog.current?.open) old.dialog.current.close();
  flushSync(() => old.root.unmount());
}
function suspend(v: View) {
  clearTimeout(v.timer);
  if (v.request && v.request.action !== 'stop') { v.generation++; v.request.controller.abort(); v.request = null; v.busy = false; }
  v.confirmation = ''; paint(v);
}
function schedule(v: View) { clearTimeout(v.timer); if (visible(v) && !v.unknown && !v.confirmation) v.timer = setTimeout(() => void refresh(v), 3000); }
async function request(v: View, action: 'list' | 'logs' | 'stop', fields: Record<string, string> = {}) {
  if (!scope(v)) throw new Error('stale');
  const operation = {controller: new AbortController(), action}; v.request = operation;
  const signal = AbortSignal.any([v.controller.signal, operation.controller.signal, AbortSignal.timeout(12000)]);
  const csrf = document.querySelector<HTMLInputElement>('input[name="csrf"]')?.value || '';
  const response = await fetch(`/projects/${encodeURIComponent(v.project)}/processes/${action}`, {method: 'POST', credentials: 'same-origin', redirect: 'error', cache: 'no-store', signal,
    headers: {'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8', Accept: 'application/json'}, body: new URLSearchParams({csrf, instance_id: v.instance, session_id: v.session, ...fields})});
  if (!response.ok) throw new Error('rejected');
  const text = await response.text();
  if (signal.aborted || !scope(v) || v.request !== operation) throw new Error('stale');
  if (text.length > 256 * 1024) throw new Error('oversized');
  const result = resultForScope(JSON.parse(text), v); if (!result) throw new Error('stale');
  return result;
}
async function refresh(v: View) {
  if (!visible(v) || v.busy || v.confirmation) return;
  // Inventory polling must not transiently disable a focused row every three seconds.
  // Admission still observes busy; reconcile keyed rows once the read settles.
  v.busy = true; const generation = ++v.generation;
  try {
    const result = inventory(await request(v, 'list')); if (!result) throw new Error('invalid');
    if (!visible(v)) return;
    v.records = result.processes;
    v.status = `${v.records.length} managed process${v.records.length === 1 ? '' : 'es'}${result.truncated ? ' · inventory truncated' : ''}`;
    if (v.selected && !v.records.some(p => p.process_id === v.selected)) resetLog(v);
    if (!v.unknown) v.error = '';
  } catch { if (same(v) && v.generation === generation && v.request && visible(v)) v.error = 'Inventory unavailable. Review the current live conversation; no work was started.'; }
  finally { if (same(v) && v.generation === generation && v.request?.action === 'list') { v.request = null; v.busy = false; paint(v); schedule(v); } }
}
function resetLog(v: View) { v.selected = ''; v.cursor = undefined; v.eof = false; v.output = ''; v.logHeading = ''; v.logStatus = ''; }
async function logs(v: View) {
  if (!visible(v) || v.busy || v.eof || v.confirmation || !v.records.some(p => p.process_id === v.selected)) return;
  v.busy = true; const id = v.selected, generation = ++v.generation; paint(v);
  try {
    const result = logPage(await request(v, 'logs', {process_id: id, max_bytes: '32768', ...(v.cursor === undefined ? {} : {cursor: String(v.cursor)})}), id, v.cursor);
    if (!result) throw new Error('invalid'); if (!visible(v)) return;
    v.cursor = result.next_cursor; v.eof = result.eof; v.output = result.output;
    v.logHeading = `${v.records.find(p => p.process_id === id)?.name} · output`;
    v.logStatus = `Cursor ${v.cursor} · ${result.omitted_bytes} bytes omitted${v.eof ? ' · end of output' : ' · more may arrive'}`;
  } catch { if (same(v) && v.generation === generation && v.request && visible(v)) v.error = 'Output unavailable. Select Logs again to read a fresh bounded page.'; }
  finally { if (same(v) && v.generation === generation && v.request?.action === 'logs') { v.request = null; v.busy = false; paint(v); schedule(v); } }
}
async function stop(v: View) {
  const id = v.confirmation;
  if (!visible(v) || v.busy || v.unknown || !v.records.some(p => p.process_id === id && p.status === 'running')) return;
  v.confirmation = ''; v.busy = true; clearTimeout(v.timer); paint(v);
  try {
    const result = await request(v, 'stop', {process_id: id, grace_ms: '2000'});
    if (!scope(v)) return;
    if (!validRecord(result.process) || result.process.process_id !== id) throw new Error('invalid');
    const updated = result.process; v.records = v.records.map(p => p.process_id === id ? updated : p); v.error = '';
  } catch {
    if (same(v)) { v.unknown = true; v.error = 'Stop was not acknowledged; it may have succeeded or been denied by policy. No retry was queued. Refresh inventory to inspect; reopen this panel’s workspace before issuing another Stop.'; }
  } finally { if (same(v)) { v.request = null; v.busy = false; paint(v); v.dialog.current?.querySelector<HTMLButtonElement>('[data-processes-close]')?.focus(); schedule(v); } }
}
const actions = {
  refresh, logs, stop,
  toggle(v: View) { if (!scope(v)) return; clearTimeout(v.timer); if (visible(v)) void refresh(v); else suspend(v); },
  select(v: View, id: string) { if (!visible(v) || v.busy || v.confirmation || !v.records.some(p => p.process_id === id)) return; resetLog(v); v.selected = id; void logs(v); },
  prepare(v: View, id: string, button: HTMLButtonElement) { if (!visible(v) || v.busy || v.unknown || !v.records.some(p => p.process_id === id && p.status === 'running')) return; clearTimeout(v.timer); v.confirmation = id; v.confirmationReturn = button; paint(v); v.dialog.current?.querySelector<HTMLButtonElement>('[data-process-stop-cancel]')?.focus(); },
  cancel(v: View) { if (!scope(v)) return; v.confirmation = ''; paint(v); if (v.confirmationReturn?.isConnected) v.confirmationReturn.focus(); schedule(v); },
};
export type Actions = typeof actions;
export const processes = Object.freeze({init, dispose});
