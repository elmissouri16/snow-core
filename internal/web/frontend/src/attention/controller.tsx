import {createRef} from 'react';
import type {FormEvent, KeyboardEvent, MouseEvent} from 'react';
import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {Panel} from './Panel.tsx';
import {identifier, invalidAnswer, permissionBlocked, record, requestKey, validInput} from './model.ts';
import type {Answer, Draft, Snapshot, State} from './model.ts';
export interface Options {
  root?: HTMLElement; project?: string; session?: string; instance?: string; nonce?: string;
  onLayout?: () => void; onTakeover?: (takingOver: boolean) => void;
}
interface View {
  api: Options; root: HTMLElement; region: HTMLElement; seat: HTMLElement; react: Root; controller: AbortController;
  identity: (string | undefined)[]; scope: string; key: string; kind: 'input' | 'permission'; pending: unknown; turn: string;
  draft: Draft | null; state: State; feedback: string; composing: boolean;
  form: ReturnType<typeof createRef<HTMLFormElement>>; body: ReturnType<typeof createRef<HTMLDivElement>>;
  observer?: ResizeObserver; layoutFrame: number; layoutMetrics: number[] | null; renderKey: string; takingOver: boolean;
}
const stores = new Map<string, Draft>(), MAX_STORES = 8, MAX_AGE = 30 * 60 * 1000;
const pageNonce = globalThis.crypto?.randomUUID?.() || String(Date.now());
let view: View | null = null;
function trimStores() {
  for (const [key, value] of stores) if (Date.now() - value.updated > MAX_AGE) stores.delete(key);
  while (stores.size > MAX_STORES) stores.delete(stores.keys().next().value!);
}
function save(current: View) {
  if (!current.draft) return;
  current.draft.updated = Date.now(); stores.delete(current.scope); stores.set(current.scope, current.draft); trimStores();
}
export function init(api: Options = {}): void {
  dispose();
  const root = api.root || document.querySelector<HTMLElement>('#live-session[data-runtime="true"]');
  const region = root?.querySelector<HTMLElement>('[data-react-attention]'), seat = root?.querySelector<HTMLElement>('#live-composer-seat');
  if (!root || !region || !seat) return;
  const identity = [api.project ?? root.dataset.project, api.session ?? root.dataset.session, api.instance ?? root.dataset.instance];
  for (const [key, value] of stores) if (value.identity[0] === identity[0] && value.identity[1] === identity[1] && value.identity[2] !== identity[2]) stores.delete(key);
  trimStores();
  const current: View = {api, root, region, seat, react: createRoot(region), controller: new AbortController(), identity,
    scope: JSON.stringify([api.nonce ?? pageNonce, ...identity]), key: '', kind: 'input', pending: null, turn: '', draft: null,
    state: {}, feedback: '', composing: false, form: createRef(), body: createRef(), layoutFrame: 0, layoutMetrics: null, renderKey: '', takingOver: false};
  view = current;
  const options = {signal: current.controller.signal}, schedule = () => layout(current);
  window.addEventListener('resize', schedule, options);
  window.visualViewport?.addEventListener('resize', schedule, options);
  window.visualViewport?.addEventListener('scroll', schedule, options);
  if (globalThis.ResizeObserver) {
    current.observer = new ResizeObserver(schedule); current.observer.observe(root);
    for (const child of root.children) if (child !== seat && child.id !== 'live-stream' && child.tagName !== 'DIALOG') current.observer.observe(child);
  }
  draw(current); takeover(current, false);
}
/** Parent owns visibility of all three outer seat boundaries and the normal draft. */
function takeover(current: View, active: boolean) {
  if (!active) {current.layoutMetrics = null; current.root.classList.remove('attention-compact', 'attention-constrained');}
  current.api.onTakeover?.(active);
}
export function render(snapshot: Snapshot | null, state: State = {}): {takingOver: boolean} {
  if (!view) return {takingOver: false};
  const current = view;
  current.state = {...state, safe: state.safe === true && !state.busy};
  if (snapshot) {
    const matches = [snapshot.project_id, snapshot.session_id, snapshot.instance_id].every((id, index) => id === undefined || id === current.identity[index]);
    if (!matches) {
      stores.delete(current.scope); current.state.safe = false;
      // Foreign snapshots neither replace the readable request nor grant consent.
    } else {
      if (typeof snapshot.cancel_token === 'string') current.turn = snapshot.cancel_token;
      const pending = snapshot.permission || snapshot.input;
      if (!pending) {
        stores.delete(current.scope); current.pending = null; current.draft = null; current.key = ''; current.feedback = ''; current.composing = false;
      } else {
        const kind = snapshot.permission ? 'permission' : 'input', key = requestKey(kind, pending, current.turn);
        if (key !== current.key) {
          const prior = stores.get(current.scope);
          current.draft = prior?.key === key ? prior : {key, identity: current.identity, page: 0, collapsed: false, answers: Object.create(null), updated: Date.now()};
          current.key = key; current.kind = kind; current.feedback = ''; current.composing = false;
          save(current);
        }
        current.pending = pending;
      }
    }
  }
  const takingOver = !!current.pending;
  const changed = current.renderKey !== presentationKey(current);
  if (changed) draw(current);
  if (current.takingOver !== takingOver) {current.takingOver = takingOver; takeover(current, takingOver);}
  if (changed) {if (takingOver) layout(current); else current.api.onLayout?.();}
  return {takingOver};
}
function presentationKey(current: View): string {
  return JSON.stringify([current.key, !!current.pending, current.state.safe === true, current.state.busy === true, current.state.canStop === true, current.state.stopping === true]);
}
function alive(current: View, key: string): boolean {return view === current && current.key === key && current.region.isConnected;}
function draw(current: View) {
  if (view !== current) return;
  current.renderKey = presentationKey(current);
  const key = current.key;
  const ready = () => alive(current, key) && current.state.safe === true;
  flushSync(() => current.react.render(current.pending && current.draft ? <Panel key={key} kind={current.kind} pending={current.pending}
    draft={current.draft} state={current.state} feedback={current.feedback} formRef={current.form} bodyRef={current.body}
    onGuard={event => guard(current, key, event)}
    onSubmit={event => submit(current, key, event)}
    onPage={delta => {
      if (!ready() || !current.draft || !validInput(current.pending)) return;
      current.draft.page = Math.max(0, Math.min(current.pending.questions.length - 1, current.draft.page + delta));
      current.feedback = ''; showPage(current, true);
    }}
    onCollapse={() => {if (ready() && current.draft) {current.draft.collapsed = !current.draft.collapsed; showPage(current, false);}}}
    onContinue={() => {if (ready()) advance(current);}}
    onAnswer={(id, change, advancePage, focusCustom) => {if (ready()) changeAnswer(current, id, change, advancePage, focusCustom);}}
    onKeydown={event => {if (alive(current, key)) keydown(current, event);}}
    onComposition={active => {if (alive(current, key)) current.composing = active;}}
  /> : null));
}
function stopEvent(event: FormEvent | MouseEvent) {event.preventDefault(); event.stopPropagation(); event.nativeEvent.stopImmediatePropagation();}
function guard(current: View, key: string, event: MouseEvent<HTMLElement>) {
  const target = event.target instanceof Element ? event.target.closest<HTMLButtonElement>('button') : null;
  if (!target) return;
  const stopping = target.hasAttribute('data-runtime-abort');
  if (!alive(current, key) || target.disabled || (stopping ? !current.state.canStop : !current.state.safe) ||
    (target.dataset.permission === 'allow' && permissionBlocked(current.pending)) ||
    (target.hasAttribute('data-permission') && (!record(current.pending) || !identifier(current.pending.id) || target.dataset.requestId !== current.pending.id))) stopEvent(event);
}
function submit(current: View, key: string, event: FormEvent<HTMLFormElement>) {
  if (!alive(current, key) || event.currentTarget !== current.form.current || !validate(current, true)) stopEvent(event);
  // Valid events intentionally bubble to the ONE app submit delegate. No POST here.
}
function focusPage(current: View, custom = false) {
  const field = current.form.current?.querySelector<HTMLElement>('fieldset:not([hidden])');
  const target = custom ? field?.querySelector<HTMLTextAreaElement>('textarea') :
    field?.querySelector<HTMLInputElement>('input:checked') || field?.querySelector<HTMLInputElement | HTMLTextAreaElement>('input, textarea');
  target?.focus({preventScroll: true});
}
function showPage(current: View, focus: boolean) {
  save(current); draw(current);
  if (current.body.current) current.body.current.scrollTop = 0;
  if (focus && !current.draft?.collapsed) focusPage(current);
  layout(current);
}
function changeAnswer(current: View, id: string, change: Partial<Answer>, advancePage = false, focusCustom = false) {
  if (!current.draft || !validInput(current.pending) || !current.pending.questions.some(q => q.id === id)) return;
  current.draft.answers[id] = {...(current.draft.answers[id] || {selected: null, custom: ''}), ...change};
  current.feedback = ''; save(current);
  if (advancePage && current.draft.page < current.pending.questions.length - 1) {current.draft.page++; showPage(current, true);}
  else {draw(current); if (focusCustom) focusPage(current, true); layout(current);}
}
function validate(current: View, all = false): boolean {
  if (!current.state.safe || !current.draft || !validInput(current.pending)) return false;
  const invalid = invalidAnswer(current.pending.questions, current.draft, all);
  if (invalid < 0) {current.feedback = ''; draw(current); return true;}
  current.draft.page = invalid; current.draft.collapsed = false;
  current.feedback = all ? 'Answer every question before submitting.' : 'Choose an option or enter an answer to continue.';
  showPage(current, true); return false;
}
function advance(current: View) {
  if (!validate(current) || !current.draft || !validInput(current.pending)) return;
  if (current.draft.page < current.pending.questions.length - 1) {current.draft.page++; showPage(current, true);}
  else {
    const button = current.form.current?.querySelector<HTMLButtonElement>('[data-attention-continue]');
    if (button) current.form.current?.requestSubmit(button);
  }
}
function keydown(current: View, event: KeyboardEvent<HTMLTextAreaElement>) {
  if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229 || current.composing) return;
  event.preventDefault(); if (current.state.safe) advance(current);
}
/** Observer delivery only schedules work; layout writes happen in a frame. */
function layout(current: View) {
  if (view !== current || !current.pending || current.layoutFrame) return;
  current.layoutFrame = requestAnimationFrame(() => {
    current.layoutFrame = 0;
    if (view === current && current.pending && !current.region.hidden) measureLayout(current);
  });
}
function measureLayout(current: View) {
  const {root, seat} = current, rect = root.getBoundingClientRect();
  const viewportBottom = window.visualViewport ? window.visualViewport.offsetTop + window.visualViewport.height : window.innerHeight;
  let remaining = Math.min(rect.bottom, viewportBottom) - rect.top;
  for (const child of root.children) {
    if (child === seat || child.id === 'live-stream') continue;
    const style = getComputedStyle(child);
    if (style.display === 'none' || style.position === 'absolute' || style.position === 'fixed') continue;
    remaining -= child.getBoundingClientRect().height + (parseFloat(style.marginTop) || 0) + (parseFloat(style.marginBottom) || 0);
  }
  const compact = remaining < 180 || (window.visualViewport?.height || window.innerHeight) <= 420;
  const compactChanged = root.classList.contains('attention-compact') !== compact;
  if (compactChanged) root.classList.toggle('attention-compact', compact);
  const stream = root.querySelector('#live-stream');
  if (stream) {
    const style = getComputedStyle(stream);
    for (const property of ['paddingTop', 'paddingBottom', 'borderTopWidth', 'borderBottomWidth'] as const) remaining -= parseFloat(style[property]) || 0;
  }
  const constrained = remaining < 96, constrainedChanged = root.classList.contains('attention-constrained') !== constrained;
  if (constrainedChanged) root.classList.toggle('attention-constrained', constrained);
  const height = Math.max(96, Math.floor(remaining)), previousHeight = parseFloat(seat.style.getPropertyValue('--attention-seat-height'));
  if (!Number.isFinite(previousHeight) || Math.abs(previousHeight - height) > 0.5) seat.style.setProperty('--attention-seat-height', height + 'px');
  const bounds = seat.getBoundingClientRect(), metrics = [bounds.top, bounds.width, bounds.height, rect.height];
  const changed = !current.layoutMetrics || metrics.some((value, index) => Math.abs(value - current.layoutMetrics![index]) > 0.5);
  if (changed || compactChanged || constrainedChanged) {current.layoutMetrics = metrics; current.api.onLayout?.();}
}
export function dispose({clearDrafts = false} = {}): void {
  if (view) {
    const current = view;
    save(current); current.controller.abort(); current.observer?.disconnect();
    if (current.layoutFrame) cancelAnimationFrame(current.layoutFrame);
    current.seat.style.removeProperty('--attention-seat-height');
    takeover(current, false); view = null; flushSync(() => current.react.unmount());
  }
  if (clearDrafts) stores.clear(); else trimStores();
}
