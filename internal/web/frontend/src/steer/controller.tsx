import {createRef} from 'react';
import type {RefObject} from 'react';
import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {SteerPanel} from './SteerPanel.tsx';
import type {Actions, Presentation} from './SteerPanel.tsx';
import {accepted, authority, bindReviewed, blocking as ownsDraft, canDismissUnknown, canSteer as admissible, initialState, randomRequestID, sameIdentity, setText, stale, update, validReceipt} from './model.ts';
import type {API, Identity, Operation, Snapshot, State, Store, UI} from './model.ts';

type View = {
  api: API; identity: Identity; container: HTMLElement; root: Root; state: State; present: boolean;
  dialog: RefObject<HTMLDialogElement | null>; input: RefObject<HTMLTextAreaElement | null>;
  returnFocus: HTMLElement | null; actions: Actions;
};
const stores = new Map<string, Store>();
let view: View | null = null;

export function init(api: API): void {
  dispose();
  const container = api.root.querySelector<HTMLElement>('[data-react-live-panel="steer"]');
  if (!container) return;
  const identity = Object.freeze({project_id: api.root.dataset.project || '', session_id: api.root.dataset.session || '', instance_id: api.root.dataset.instance || ''});
  if (api.session !== identity.session_id || api.identity && !sameIdentity(api.identity, identity)) return;
  const key = JSON.stringify([identity.project_id, identity.session_id, identity.instance_id]);
  const store = stores.get(key) || {text: '', revision: 0};
  stores.delete(key); stores.set(key, store);
  while (stores.size > 16) { const oldest = stores.keys().next(); if (!oldest.done) stores.delete(oldest.value); }
  const current: View = {api, identity, container, root: createRoot(container), state: initialState(store), present: true,
    dialog: createRef(), input: createRef(), returnFocus: null, actions: {
      open: trigger => { if (bound(current)) open(trigger); }, close: () => close(current), closed: () => closed(current),
      review: () => review(current), copy: (request, trigger) => copy(current, request, trigger),
      text: value => { if (bound(current)) { setText(store, value); draw(current); } },
      compose: composing => { if (bound(current)) { current.state.composing = composing; draw(current); } },
      submit: () => { void submit(current); },
    }};
  view = current;
  draw(current);
}
export function dispose(): void {
  if (!view) return;
  const current = view; view = null;
  if (current.state.store.busy) {
    current.state.store.busy = null; current.state.store.unknown = true;
    current.state.store.error = 'The steering result is unknown. Your draft is kept; nothing is retried automatically.';
  }
  // No request cancellation can promise to undo native delivery. Retire only
  // this local waiter; a later instance gets a different tab-memory store.
  current.state.opened = false;
  if (current.dialog.current?.open) {
    if (current.api.closeDialog) current.api.closeDialog(current.dialog.current);
    else current.dialog.current.close();
  }
  flushSync(() => current.root.unmount());
}
function bound(current: View): boolean {
  const {api, identity} = current;
  return view === current && api.root.isConnected && api.root.contains(current.container) && api.session === identity.session_id &&
    api.root.dataset.project === identity.project_id && api.root.dataset.instance === identity.instance_id && api.root.dataset.session === identity.session_id;
}
export function blocking(): boolean { return !!view && ownsDraft(view.state); }
export function canSteer(): boolean { return !!view && bound(view) && admissible(view.state); }
export function render(snapshot: Snapshot | null, ui: UI, present = true): void {
  if (!view) return;
  const current = view;
  // `false` defers DOM publication only. Admission must already reflect this
  // snapshot when the parent computes its other controls in the same stack.
  update(current.state, snapshot, ui, current.api, current.identity);
  current.present = present;
  if (present) draw(current);
}
function presentation(current: View): Presentation {
  const {state: s} = current, {store} = s;
  const changed = stale(s), allowed = bound(current) && authority(s), dismiss = bound(current) && canDismissUnknown(s);
  return {
    text: store.text, busy: !!store.busy, composing: s.composing,
    triggerHidden: !s.projection && !s.invalid && !store.unknown,
    canOpen: bound(current) && (admissible(s) || !!store.unknown && (allowed || dismiss)),
    canSubmit: bound(current) && admissible(s) && !changed && !s.composing && current.api.validText(store.text),
    error: store.error || (changed ? 'The reviewed run changed. Your draft is kept. Review the current run again before sending.' : !allowed && s.opened ? 'Steering is unavailable while the run is stopped, disconnected, needs attention, or another control owns it. Your draft is kept.' : ''),
    reviewHidden: !(store.unknown || changed || store.rejected), canReview: allowed || dismiss, dismiss,
    items: s.projection?.items || [],
  };
}
function draw(current: View): void {
  if (view !== current || !current.present) return;
  // Pass a presentation snapshot, never the mutable admission object. React
  // owns every descendant; native refs only manage modal/focus behavior.
  const state = presentation(current);
  flushSync(() => current.root.render(<SteerPanel state={state} actions={current.actions} dialog={current.dialog} input={current.input} />));
}
function changed(current: View): void {
  if (view !== current) return;
  // Every caller mutates controller state BEFORE this synchronous callback.
  // Parent may immediately read blocking/canSteer and render another snapshot.
  current.api.changed();
  draw(current);
}
function show(current: View, trigger?: HTMLElement | null): boolean {
  const dialog = current.dialog.current;
  if (!dialog) return false;
  current.returnFocus = trigger || current.api.root.ownerDocument.activeElement as HTMLElement | null;
  current.state.opened = true;
  try {
    if (!dialog.open) {
      if (current.api.openDialog) current.api.openDialog(dialog, current.returnFocus);
      else dialog.showModal();
    }
  } catch {
    current.state.opened = false; changed(current); return false;
  }
  return true;
}
export function open(trigger?: HTMLElement | null): boolean {
  if (!view || !bound(view)) return false;
  const current = view, s = current.state;
  if (!admissible(s) && !(s.store.unknown && (authority(s) || canDismissUnknown(s)))) return false;
  if (!s.store.unknown && !bindReviewed(s)) return false;
  if (!s.store.unknown) s.store.error = '';
  if (!show(current, trigger)) return false;
  current.input.current?.focus(); changed(current); return true;
}
function closed(current: View): void {
  if (view !== current || current.dialog.current?.open || !current.state.opened) return;
  current.state.opened = false;
  changed(current);
  restoreFocus(current);
}
function restoreFocus(current: View): void {
  // The parent's native dialog hook handles menu-aware focus restoration. The
  // fallback is for embedders without that hook, not a competing DOM owner.
  if (!current.api.openDialog && current.returnFocus?.isConnected && !current.returnFocus.matches(':disabled')) current.returnFocus.focus({preventScroll: true});
}
function close(current: View): void {
  if (view !== current) return;
  current.state.opened = false;
  if (current.dialog.current?.open) {
    if (current.api.closeDialog) current.api.closeDialog(current.dialog.current);
    else current.dialog.current.close();
  }
  changed(current); restoreFocus(current);
}
function review(current: View): void {
  if (!bound(current)) return;
  const s = current.state, {store} = s;
  if (canDismissUnknown(s)) {
    store.unknown = false; store.rejected = false; store.token = ''; store.baseRevision = 0;
    store.error = 'Draft kept. The earlier steering outcome is still uncertain unless native history confirms it. Dismissal does not mean delivery or discard; nothing is retried automatically.';
    close(current);
  } else if (bindReviewed(s)) {
    store.unknown = false; store.rejected = false;
    store.error = 'Reviewed the current run. Sending again is a new explicit request, not a retry; an uncertain earlier request may already have been delivered.';
    changed(current);
  }
}
function copy(current: View, request: string, trigger: HTMLElement): void {
  if (!bound(current) || current.state.store.busy) return;
  const {store} = current.state, item = current.state.projection?.items.find(item => item.request_id === request);
  if (!item) return;
  // Never replace another draft, never write the composer, and never silently
  // bind historical text to a different root. Review retains the captured scope.
  if (store.text && store.text !== item.text) store.error = 'Your steering draft is kept. Copy the history text manually to avoid replacing it.';
  else { store.text = item.text; store.revision++; }
  if (show(current, trigger)) changed(current);
}
async function submit(current: View): Promise<boolean> {
  const {state: s, api} = current, {store} = s;
  if (!bound(current) || !admissible(s) || s.composing || !api.validText(store.text) || stale(s) || !s.projection) return false;
  const operation: Operation = Object.freeze({identity: current.identity, token: s.projection.live_steer_token, revision: s.projection.revision,
    request: randomRequestID(), text: store.text, draftRevision: store.revision});
  store.busy = operation; store.error = ''; store.rejected = false; changed(current);
  try {
    if (!bound(current) || store.busy !== operation) return false;
    const result = await api.request('steer', {session_id: operation.identity.session_id, live_steer_token: operation.token, steer_revision: String(operation.revision), request_id: operation.request, text: operation.text});
    if (view !== current || store.busy !== operation) return false;
    if (!bound(current) || !validReceipt(result, operation, api)) throw new Error('Unverified steering receipt');
    // A correlated ACK confirms acceptance, NOT delivery. Never install its
    // possibly older projection over SSE, or regrant a completed root's token.
    accepted(store, operation);
    return true;
  } catch {
    if (view !== current || store.busy !== operation) return false;
    store.unknown = true;
    store.error = 'Steering was rejected or its outcome is unknown. Your draft is kept. Review the current run and native history before any new explicit submission; nothing is retried automatically.';
    return false;
  } finally {
    if (view === current && store.busy === operation) { store.busy = null; changed(current); }
  }
}
export const steer = Object.freeze({init, dispose, render, blocking, canSteer, open});
export default steer;
