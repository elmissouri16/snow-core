import { createPortal, flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { Message } from './Message';
import { defaultActions, mergeActions } from './actions';
import type { ActionPresentation, ActionPresentationPatch } from './actions';
import { ToolContents, ToolRow } from './ToolRow';
import { projectActivities, projectMessages, record, sameActivityProjection, sameMessageProjection, text } from './model';
import type { Activity, MessageData, ToolData } from './model';
import { initialActivities, initialMessages } from './initial';
import { scopeKey, scopeOf } from './imageTransport';

type State = {actions: ActionPresentation; root: Root; host: HTMLElement; scope: HTMLElement | null; identity: string; messages: MessageData[]; activities: Activity[]; region: HTMLElement | null; truncated: boolean; slots: Map<string, HTMLElement>; noticeHost: HTMLElement | null; presentation: string; committed: boolean};
const states = new Map<HTMLElement, State>();
// Ephemeral public-only projection for same-document lifecycle (not browser
// storage). A bfcache pagehide/dispose must not erase saved-history content on
// pageshow. Weak keys release replaced workspaces; identity mismatch rejects it.
const retired = new WeakMap<HTMLElement, {identity: string; messages: MessageData[]; activities: Activity[]; truncated: boolean}>();
const standaloneTools = new Map<HTMLDetailsElement, Root>();
function scope(host: HTMLElement) { return host.closest<HTMLElement>('#live-session') || host.closest<HTMLElement>('.catalog-history'); }
function presentation(scope: HTMLElement | null) {
  return JSON.stringify([scope?.dataset.runtime, scope?.dataset.messageEditEnabled, scope?.dataset.messageRegenerateEnabled]);
}
function create(host: HTMLElement): State {
  const owner = scope(host), region = owner?.id === 'live-session' ? owner.querySelector<HTMLElement>('#live-activities') : null;
  const identity = scopeKey(scopeOf(owner)), saved = retired.get(host);
  const restored = saved?.identity === identity ? saved : undefined;
  retired.delete(host);
  const messages = restored?.messages || initialMessages(host), activities = restored?.activities || initialActivities(region);
  const truncated = restored?.truncated ?? region?.querySelector<HTMLElement>('.activity-limit')?.hidden === false;
  // Only explicit public SSR markup is read; all subsequent children are JSX.
  const root = createRoot(host);
  const state: State = {actions: defaultActions(), root, host, scope: owner, identity: scopeKey(scopeOf(owner)), messages, activities, region, truncated, slots: new Map(), noticeHost: null, presentation: presentation(owner), committed: false};
  states.set(host, state);
  if (region) attachRegion(state, region);
  return state;
}
function attachRegion(state: State, region: HTMLElement) {
  if (state.region !== region) { state.noticeHost?.remove(); state.noticeHost = null; }
  state.region = region;
  // The outer legacy stream remains SnowScroll-owned. The notice is global,
  // even when all activities have a chronological marker and fallback is empty.
  if (!state.noticeHost) {
    region.replaceChildren();
    const host = document.createElement('div'); host.style.display = 'contents'; host.dataset.reactActivityNotice = '';
    region.before(host); state.noticeHost = host;
  }
}
function targets(state: State) {
  const groups = new Map<string, HTMLElement>();
  for (const group of state.host.querySelectorAll<HTMLElement>(':scope > [data-runtime-activity-group]')) {
    const list = group.querySelector<HTMLElement>('.activity-list');
    if (group.dataset.messageId && list) groups.set(group.dataset.messageId, list);
  }
  return groups;
}
function move(node: HTMLElement, target: HTMLElement, before: ChildNode | null) {
  if (node.parentNode === target && node === before) return;
  const modern = target as HTMLElement & {moveBefore?: (node: Node, before: Node | null) => void};
  if (modern.moveBefore && node.isConnected && target.isConnected) modern.moveBefore(node, before);
  else target.insertBefore(node, before);
}
function View({state}: {state: State}) {
  const markerIDs = new Set(state.messages.filter(message => message.role === 'tool_activity').map(message => message.id));
  const associated = new Set(state.activities.filter(activity => markerIDs.has(activity.messageID)).map(activity => activity.messageID));
  return <>
    {state.scope?.id !== 'live-session' && !state.messages.length && <div className="empty-state compact"><h2>No displayable messages</h2><p>This page has no displayable messages.</p></div>}
    {state.messages.map(message => message.role === 'tool_activity' ? <section key={`${state.identity}:activity:${message.id}`} className="runtime-tool-group tool-timeline" data-message-id={message.id} data-message-role="tool_activity" data-runtime-activity-group="" role="group" aria-label="Runtime tool activity" hidden={!associated.has(message.id)}><div className="activity-list"/></section> : <Message key={`${state.identity}:message:${message.id}`} message={message} scope={state.scope} actions={state.actions}/>)}
    {state.region && createPortal(<><div className="activity-provenance"><h2 id="tool-timeline-heading" className="sr-only">Unassociated runtime tools</h2><span>Unassociated runtime tools</span></div><div className="activity-list"/></>, state.region)}
    {state.noticeHost && createPortal(<p className="fine activity-limit" hidden={!state.truncated}>Earlier tool activity is omitted from this bounded view.</p>, state.noticeHost)}
    {state.region && state.activities.map(activity => createPortal(<ToolRow data={activity.data}/>, state.slots.get(activity.data.id)!, `${state.identity}:tool:${activity.data.id}`))}
  </>;
}
function commit(state: State) {
  state.committed = true; state.presentation = presentation(state.scope);
  const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const selection = window.getSelection();
  // Numeric endpoints, not cloned live Ranges: changing a retained text node's
  // data also mutates Range offsets. Preserve a reader's selected prefix.
  const selected = selection && !selection.isCollapsed && (state.host.contains(selection.anchorNode) || state.region?.contains(selection.anchorNode)) ? {anchor: selection.anchorNode, start: selection.anchorOffset, focus: selection.focusNode, end: selection.focusOffset} : null;
  const ids = new Set(state.activities.map(activity => activity.data.id));
  for (const activity of state.activities) if (!state.slots.has(activity.data.id)) {
    // Stable portal containers may move between explicit marker/fallback lists;
    // changing a portal's target would remount its disclosure and copy state.
    const slot = document.createElement('div'); slot.style.display = 'contents'; slot.dataset.reactActivitySlot = activity.data.id;
    state.slots.set(activity.data.id, slot);
  }
  // Rescue surviving orphan slots before React removes an evicted marker.
  const markers = new Set(state.messages.filter(message => message.role === 'tool_activity').map(message => message.id));
  const fallbackBefore = state.region?.querySelector<HTMLElement>('.activity-list');
  if (fallbackBefore) for (const [id, slot] of state.slots) {
    const oldGroup = slot.closest<HTMLElement>('[data-runtime-activity-group]');
    if (ids.has(id) && oldGroup && !markers.has(oldGroup.dataset.messageId || '')) move(slot, fallbackBefore, null);
  }
  flushSync(() => state.root.render(<View state={state}/>));
  const groups = targets(state), fallback = state.region?.querySelector<HTMLElement>('.activity-list');
  const positions = new Map<HTMLElement, number>();
  if (fallback) for (const activity of state.activities) {
    const target = groups.get(activity.messageID) || fallback, slot = state.slots.get(activity.data.id)!;
    const position = positions.get(target) || 0;
    move(slot, target, target.childNodes[position] || null); positions.set(target, position + 1);
  }
  for (const [id, slot] of state.slots) if (!ids.has(id)) { slot.remove(); state.slots.delete(id); }
  if (state.region) state.region.hidden = !fallback?.childElementCount;
  if (focused?.isConnected && document.activeElement !== focused) focused.focus({preventScroll: true});
  if (selection && selected?.anchor?.isConnected && selected.focus?.isConnected) {
    const length = (node: Node) => node.nodeType === Node.TEXT_NODE ? node.textContent?.length || 0 : node.childNodes.length;
    const start = Math.min(selected.start, length(selected.anchor)), end = Math.min(selected.end, length(selected.focus));
    if (selection.anchorNode !== selected.anchor || selection.anchorOffset !== start || selection.focusNode !== selected.focus || selection.focusOffset !== end) selection.setBaseAndExtent(selected.anchor, start, selected.focus, end);
  }
}
function ensure(host: HTMLElement) {
  let state = states.get(host);
  if (state && state.identity !== scopeKey(scopeOf(scope(host)))) {
    retire(state); state = undefined;
  }
  return state || create(host);
}
function retire(state: State) {
  retired.set(state.host, {identity: state.identity, messages: state.messages, activities: state.activities, truncated: state.truncated});
  flushSync(() => state.root.unmount());
  for (const slot of state.slots.values()) slot.remove();
  state.noticeHost?.remove(); states.delete(state.host);
}
function render(host: HTMLElement | null, messages: unknown) {
  if (!host || host.closest('[data-react-saved-history]')) return;
  const state = ensure(host), next = projectMessages(messages);
  if (state.committed && state.presentation === presentation(state.scope) && sameMessageProjection(state.messages, next)) return;
  state.messages = next; commit(state);
}
function updateActions(host: HTMLElement | null, partialPresentation: ActionPresentationPatch) {
  if (!host?.isConnected || host.closest('[data-react-saved-history]')) return;
  const state = ensure(host), next = mergeActions(state.actions, partialPresentation);
  if (next === state.actions) return;
  state.actions = next; commit(state);
}
function enhance(scope: ParentNode = document) {
  for (const state of states.values()) if (!state.host.isConnected) retire(state);
  const hosts = new Set<HTMLElement>();
  if (scope instanceof HTMLElement && scope.matches('[data-react-messages], #live-transcript')) hosts.add(scope);
  for (const host of scope.querySelectorAll<HTMLElement>('[data-react-messages], #live-transcript')) hosts.add(host);
  // Cached saved templates had no dedicated marker; accept only a container
  // whose direct children already are public message articles.
  for (const row of scope.querySelectorAll<HTMLElement>('.catalog-history .catalog-message')) if (row.parentElement) hosts.add(row.parentElement);
  // Cold pages import their SSR once into the parent React tree. Navigation
  // callbacks may run before that parent mounts; never create a competing root.
  for (const host of hosts) if (!host.closest('[data-react-saved-history], [data-react-page="workspace-cold"]') && !states.has(host)) commit(ensure(host));
}
function dispose() {
  for (const state of [...states.values()]) retire(state);
  for (const root of standaloneTools.values()) flushSync(() => root.unmount());
  // Unmounting these roots releases their own image jobs. Composite saved
  // history belongs to a parent React tree and must outlive this facade call.
  standaloneTools.clear();
}
function init(scope: ParentNode | null) {
  for (const state of [...states.values()]) if (!state.host.isConnected) retire(state);
  if (scope) enhance(scope);
}
function renderActivities(region: HTMLElement | null, snapshot: unknown, transcript: HTMLElement | null = document.querySelector('#live-transcript')) {
  if (!region || !transcript || transcript.closest('[data-react-saved-history]')) return;
  const state = ensure(transcript), data = record(snapshot), next = projectActivities(data.activities);
  const truncated = !!data.activities_truncated || Array.isArray(data.activities) && data.activities.length > 128;
  const regionChanged = state.region !== region || !state.noticeHost;
  if (regionChanged) attachRegion(state, region);
  if (state.committed && !regionChanged && state.presentation === presentation(state.scope) && state.truncated === truncated && sameActivityProjection(state.activities, next)) return;
  state.activities = next; state.truncated = truncated; commit(state);
}
/** One authoritative runtime snapshot produces one transcript commit. */
function renderSnapshot(region: HTMLElement | null, snapshot: unknown, transcript: HTMLElement | null = document.querySelector('#live-transcript')) {
  if (!region || !transcript || transcript.closest('[data-react-saved-history]')) return;
  const state = ensure(transcript), data = record(snapshot);
  const nextMessages = projectMessages(data.messages), nextActivities = projectActivities(data.activities);
  const truncated = !!data.activities_truncated || Array.isArray(data.activities) && data.activities.length > 128;
  const regionChanged = state.region !== region || !state.noticeHost;
  if (regionChanged) attachRegion(state, region);
  if (state.committed && !regionChanged && state.presentation === presentation(state.scope) && state.truncated === truncated && sameMessageProjection(state.messages, nextMessages) && sameActivityProjection(state.activities, nextActivities)) return;
  state.messages = nextMessages; state.activities = nextActivities; state.truncated = truncated;
  commit(state);
}
// Compatibility for independently owned legacy callers. Transcript/history rows
// use ToolRow directly; this must never be called inside a mounted message root.
function renderToolRow(row: HTMLDetailsElement, value: unknown) {
  if (!row || row.closest('[data-react-messages], #live-transcript, [data-react-saved-history]')) return;
  const raw = record(value);
  const data: ToolData = {id: '', tool: text(raw.tool, 128), status: text(raw.status, 32), label: text(raw.label, 128), summary: text(raw.summary, 1024), output: text(raw.output, 16384), error: !!raw.error, expandable: !!raw.expandable, truncated: !!raw.truncated};
  let root = standaloneTools.get(row);
  if (!root) { root = createRoot(row); standaloneTools.set(row, root); }
  row.dataset.status = data.status; row.dataset.toolName = data.tool || 'Tool';
  row.classList.toggle('activity-error', data.error); row.classList.toggle('activity-no-output', !data.expandable);
  if (!data.expandable) row.open = false;
  flushSync(() => root.render(<ToolContents data={data}/>));
}
export const messages = Object.freeze({render, renderSnapshot, enhance, init, dispose, updateActions});
export const visibility = Object.freeze({renderActivities, renderToolRow});
