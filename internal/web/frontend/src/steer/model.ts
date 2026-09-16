/** Public current-run DTOs only. The opaque token binds native TurnID/root epoch;
 * those private IDs never enter this controller or a browser request. */
export type Identity = Readonly<{project_id: string; instance_id: string; session_id: string}>;
export type Status = 'accepted' | 'delivered' | 'discarded' | 'uncertain';
export type Item = {request_id: string; item_id?: string; text: string; status: Status};
export type Projection = {live_steer_token: string; revision: number; can_steer: boolean; items: Item[]};
export type Snapshot = {revision: number; steer?: unknown; project_id?: string; instance_id?: string; session_id?: string};
export type UI = {connected: boolean; busy: boolean; unknown: boolean; stopping: boolean; editing: boolean; status: string};
export type Operation = Readonly<{identity: Identity; token: string; revision: number; request: string; text: string; draftRevision: number}>;
export type Store = {text: string; revision: number; token?: string; baseRevision?: number; busy?: Operation | null; unknown?: boolean; error?: string; rejected?: boolean};
export type State = {store: Store; ui: UI | null; projection: Projection | null; snapshotRevision: number; invalid: boolean; composing: boolean; opened: boolean};
export type Fields = {session_id: string; live_steer_token: string; steer_revision: string; request_id: string; text: string};
export type API = {
  root: HTMLElement; identity?: Identity; session: string; validText: (text: unknown) => boolean;
  changed: () => void; request: (action: 'steer', fields: Fields) => Promise<unknown>;
  openDialog?: (dialog: HTMLDialogElement, trigger?: HTMLElement | null) => void;
  closeDialog?: (dialog: HTMLDialogElement) => void;
};
const encoder = new TextEncoder();
const statuses = new Set<unknown>(['accepted', 'delivered', 'discarded', 'uncertain']);
export const labels: Record<Status, string> = {
  accepted: 'Accepted · awaiting native delivery', delivered: 'Delivered · confirmed by native run',
  discarded: 'Discarded · confirmed by native run', uncertain: 'Outcome uncertain · may still be delivered; review before submitting again',
};
export function record(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
export function validID(id: unknown): id is string {
  return typeof id === 'string' && id.length > 0 && id.trim() === id && encoder.encode(id).length <= 256 && !/[\x00-\x1f\x7f]/.test(id);
}
export function randomRequestID(source: Pick<Crypto, 'getRandomValues'> & Partial<Pick<Crypto, 'randomUUID'>> = globalThis.crypto): string {
  if (typeof source.randomUUID === 'function') return source.randomUUID();
  const value = new Uint8Array(16);
  source.getRandomValues(value);
  value[6] = (value[6] & 0x0f) | 0x40;
  value[8] = (value[8] & 0x3f) | 0x80;
  const hex = Array.from(value, byte => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export function validProjection(raw: unknown, api: Pick<API, 'validText'>): raw is Projection {
  if (!record(raw) || typeof raw.live_steer_token !== 'string' || (raw.live_steer_token !== '' && !validID(raw.live_steer_token)) || !Number.isSafeInteger(raw.revision) || (raw.revision as number) < 0 || typeof raw.can_steer !== 'boolean' || !Array.isArray(raw.items) || raw.items.length > 8) return false;
  if (raw.can_steer && (!validID(raw.live_steer_token) || (raw.revision as number) <= 0)) return false;
  let bytes = 0; const requests = new Set<string>(), items = new Set<string>();
  for (const item of raw.items) {
    if (!record(item) || !validID(item.request_id) || requests.has(item.request_id) || !statuses.has(item.status) || typeof item.text !== 'string' || !api.validText(item.text)) return false;
    if (item.item_id !== undefined && item.item_id !== '' && (!validID(item.item_id) || items.has(item.item_id))) return false;
    if (item.status !== 'uncertain' && !validID(item.item_id)) return false;
    requests.add(item.request_id); if (typeof item.item_id === 'string' && item.item_id) items.add(item.item_id);
    bytes += encoder.encode(item.text).length;
  }
  return bytes <= 256 * 1024;
}
export function sameIdentity(raw: unknown, identity: Identity): boolean {
  return record(raw) && raw.project_id === identity.project_id && raw.instance_id === identity.instance_id && raw.session_id === identity.session_id;
}
export function initialState(store: Store): State {
  return {store, ui: null, projection: null, snapshotRevision: -1, invalid: false, composing: false, opened: false};
}
/** Admission updates synchronously, even when the caller defers presentation. */
export function update(state: State, snapshot: Snapshot | null, ui: UI, api: Pick<API, 'validText'>, identity: Identity): void {
  state.ui = {...ui};
  if (!snapshot || !Number.isSafeInteger(snapshot.revision) || snapshot.revision < state.snapshotRevision) return;
  state.snapshotRevision = snapshot.revision;
  const mismatched = ['project_id', 'instance_id', 'session_id'].some(key => {
    const field = key as keyof Identity;
    return snapshot[field] !== undefined && snapshot[field] !== identity[field];
  });
  state.invalid = mismatched || snapshot.steer != null && !validProjection(snapshot.steer, api);
  state.projection = !state.invalid && validProjection(snapshot.steer, api)
    ? {...snapshot.steer, items: snapshot.steer.items.map(item => ({...item}))} : null;
}
export function blocking(state: State): boolean { return !!(state.opened || state.store.busy || state.store.unknown); }
export function authority(state: State): boolean {
  const ui = state.ui;
  return !!ui && ui.connected && !ui.busy && !ui.unknown && !ui.stopping && !ui.editing && ui.status === 'running' && !state.invalid && state.projection?.can_steer === true && !state.store.busy;
}
export function canSteer(state: State): boolean { return authority(state) && !state.store.unknown; }
export function canDismissUnknown(state: State): boolean {
  const ui = state.ui;
  // Local draft ownership only: the parent's independent uncertainty/Stop fence
  // is not cleared, nor does an idle review grant mutation authority.
  return !!ui && ui.connected && !ui.busy && !ui.stopping && !ui.editing && ui.status === 'idle' && !state.invalid && !!state.store.unknown && !state.store.busy;
}
export function stale(state: State): boolean {
  return !!state.projection && (!state.store.token || state.store.token !== state.projection.live_steer_token || state.store.baseRevision !== state.projection.revision);
}
export function bindReviewed(state: State): boolean {
  if (!authority(state) || !state.projection) return false;
  state.store.token = state.projection.live_steer_token; state.store.baseRevision = state.projection.revision;
  return true;
}
export function setText(store: Store, text: string): void {
  if (encoder.encode(text).length > 64 * 1024) store.error = 'Steering is limited to 64 KiB; your previous draft is kept.';
  else { store.text = text; store.revision++; }
}
export function validReceipt(raw: unknown, operation: Operation, api: Pick<API, 'validText'>): boolean {
  if (!record(raw) || !sameIdentity(raw, operation.identity) || !record(raw.steer_ack) || !Number.isSafeInteger(raw.revision) || (raw.revision as number) <= 0) return false;
  const ack = raw.steer_ack;
  return ack.live_steer_token === operation.token && ack.request_id === operation.request && validID(ack.item_id) && ack.status === 'accepted' && validProjection(raw.steer, api);
}
export function accepted(store: Store, operation: Operation): void {
  store.error = 'Accepted by the native run. Acceptance is not delivery; see the steering history for its confirmed outcome.';
  if (store.revision === operation.draftRevision && store.text === operation.text) { store.text = ''; store.revision++; }
  store.unknown = false;
}
