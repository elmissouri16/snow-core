import test from 'node:test';
import assert from 'node:assert/strict';
import {accepted, authority, bindReviewed, blocking, canDismissUnknown, canSteer, initialState, randomRequestID, sameIdentity, setText, stale, update, validID, validProjection, validReceipt} from './model.ts';
import type {Identity, Item, Operation, Projection, Snapshot, Store, UI} from './model.ts';

const identity: Identity = {project_id: 'project', instance_id: 'instance', session_id: 'session'};
const api = {validText: (text: unknown) => typeof text === 'string' && text.trim() !== '' && !text.includes('\0') && new TextEncoder().encode(text).length <= 65536};
const ui: UI = {connected: true, busy: false, unknown: false, stopping: false, editing: false, status: 'running'};
const projection = (): Projection => ({live_steer_token: 'opaque-root', revision: 1, can_steer: true, items: []});
const item = (status: Item['status'] = 'accepted'): Item => ({request_id: 'nonce', item_id: 'native-item', text: 'literal\ncorrection', status});
const snapshot = (revision = 1, steer: unknown = projection()): Snapshot => ({...identity, revision, steer});
const operation: Operation = {identity, token: 'opaque-root', revision: 1, request: 'nonce', text: 'literal\ncorrection', draftRevision: 2};
const receipt = () => ({...identity, revision: 2, steer: {...projection(), revision: 2, items: [item()]}, steer_ack: {live_steer_token: 'opaque-root', request_id: 'nonce', item_id: 'native-item', status: 'accepted'}});
function state(store: Store = {text: 'literal\ncorrection', revision: 2}) {
  const value = initialState(store); update(value, snapshot(), ui, api, identity); return value;
}

test('request IDs remain available when randomUUID is unavailable on private-IP HTTP', () => {
  const id = randomRequestID({getRandomValues: value => {
    const bytes = value as unknown as Uint8Array;
    for (let i = 0; i < bytes.length; i++) bytes[i] = i;
    return value;
  }});
  assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$/);
});

test('bounded projections accept only native public states and distinct correlated IDs', () => {
  for (const status of ['accepted', 'delivered', 'discarded', 'uncertain'] as const) assert.equal(validProjection({...projection(), items: [item(status)]}, api), true);
  const unknown = {request_id: 'nonce', text: 'literal', status: 'uncertain'};
  assert.equal(validProjection({...projection(), items: [unknown]}, api), true);
  for (const invalid of [
    {...projection(), revision: -1}, {...projection(), revision: Number.MAX_SAFE_INTEGER + 1}, {...projection(), revision: 0},
    {...projection(), live_steer_token: ''}, {...projection(), can_steer: 'true'}, {...projection(), items: {}},
    {...projection(), items: [item(), item()]}, {...projection(), items: [item(), {...item(), request_id: 'other'}]},
    {...projection(), items: [{...item(), item_id: ''}]}, {...projection(), items: [{...unknown, item_id: {}}]},
    {...projection(), items: [{...item(), status: 'sending'}]}, {...projection(), items: [{...item(), text: '\0bad'}]},
    {...projection(), items: [{...item(), text: ' '}]}, {...projection(), items: [{...item(), request_id: ''}]},
  ]) assert.equal(validProjection(invalid, api), false, JSON.stringify(invalid));
  assert.equal(validProjection({live_steer_token: '', revision: 0, can_steer: false, items: []}, api), true);
  assert.equal(validProjection({...projection(), items: Array.from({length: 9}, (_, i) => ({...unknown, request_id: String(i)}))}, api), false);
});

test('UTF-8 aggregate and draft limits are bytes; malformed IDs fail closed', () => {
  const full = Array.from({length: 4}, (_, i) => ({request_id: String(i), text: 'é'.repeat(32768), status: 'uncertain'}));
  assert.equal(validProjection({...projection(), items: full}, api), true);
  assert.equal(validProjection({...projection(), items: [...full, {request_id: 'extra', text: 'x', status: 'uncertain'}]}, api), false);
  for (const id of ['', ' x', 'x\t', 'x\n', 'x\x01', 'x\x7f', 'é'.repeat(129)]) assert.equal(validID(id), false);
  assert.equal(validID('é'.repeat(128)), true);
  const draft = {text: 'partial\ndraft', revision: 1};
  setText(draft, 'é'.repeat(32769)); assert.equal(draft.text, 'partial\ndraft'); assert.equal(draft.revision, 1);
  setText(draft, 'é'.repeat(32768)); assert.equal(draft.text.length, 32768); assert.equal(draft.revision, 2);
});

test('admission is synchronous and local dialog/busy/uncertainty ownership is independent of publication', () => {
  const s = state();
  assert.equal(canSteer(s), true); assert.equal(blocking(s), false);
  s.opened = true; assert.equal(blocking(s), true); assert.equal(canSteer(s), true);
  s.store.busy = operation; assert.equal(canSteer(s), false); assert.equal(blocking(s), true);
  s.opened = false; assert.equal(blocking(s), true);
  s.store.busy = null; s.store.unknown = true; assert.equal(canSteer(s), false); assert.equal(authority(s), true); assert.equal(blocking(s), true);
  for (const patch of [{connected: false}, {busy: true}, {unknown: true}, {stopping: true}, {editing: true}, {status: 'permission'}, {status: 'input'}, {status: 'unavailable'}, {status: 'idle'}]) {
    update(s, snapshot(), {...ui, ...patch}, api, identity); assert.equal(canSteer(s), false); assert.equal(authority(s), false);
  }
});

test('review captures the exact root and revision, not a later run or projection', () => {
  const s = state();
  assert.equal(stale(s), true); assert.equal(bindReviewed(s), true); assert.equal(stale(s), false);
  update(s, snapshot(2, {...projection(), revision: 2}), ui, api, identity);
  assert.equal(stale(s), true); assert.equal(s.store.baseRevision, 1);
  assert.equal(bindReviewed(s), true); assert.equal(stale(s), false);
  update(s, snapshot(3, {...projection(), live_steer_token: 'next-root', revision: 3}), ui, api, identity);
  assert.equal(stale(s), true); assert.equal(s.store.token, 'opaque-root'); assert.equal(s.store.text, 'literal\ncorrection');
  update(s, snapshot(4, {live_steer_token: '', revision: 4, can_steer: false, items: []}), {...ui, status: 'idle'}, api, identity);
  assert.equal(bindReviewed(s), false); assert.equal(canSteer(s), false);
});

test('lower snapshots cannot overwrite delivery; malformed or foreign owners disable admission', () => {
  const s = state();
  const delivered = {...projection(), revision: 3, items: [item('delivered')]};
  update(s, snapshot(3, delivered), ui, api, identity);
  delivered.items[0].status = 'accepted';
  assert.equal(s.projection?.items[0].status, 'delivered', 'projection does not alias caller mutations');
  update(s, snapshot(2, {...projection(), revision: 2, items: [item()]}), ui, api, identity);
  assert.equal(s.projection?.items[0].status, 'delivered');
  for (const key of ['project_id', 'instance_id', 'session_id'] as const) {
    update(s, {...snapshot(4), [key]: 'replacement'}, ui, api, identity);
    assert.equal(canSteer(s), false); assert.equal(s.invalid, true);
  }
  update(s, snapshot(5, {...projection(), items: [{...item(), status: 'not-native'}]}), ui, api, identity);
  assert.equal(s.invalid, true); assert.equal(s.projection, null);
});

test('receipt requires exact owner/token/nonce, accepted native item, and a bounded projection', () => {
  assert.equal(validReceipt(receipt(), operation, api), true);
  for (const key of ['project_id', 'instance_id', 'session_id'] as const) {
    assert.equal(validReceipt({...receipt(), [key]: 'replacement'}, operation, api), false);
    assert.equal(sameIdentity({...identity, [key]: 'replacement'}, identity), false);
  }
  for (const patch of [{live_steer_token: 'next-root'}, {request_id: 'other-request'}, {item_id: ''}, {item_id: 'bad\nitem'}, {status: 'delivered'}]) assert.equal(validReceipt({...receipt(), steer_ack: {...receipt().steer_ack, ...patch}}, operation, api), false);
  for (const invalid of [false, null, {...receipt(), revision: 0}, {...receipt(), steer: null}, {...receipt(), steer_ack: null}]) assert.equal(validReceipt(invalid, operation, api), false);
});

test('native terminal state may retire the token before receipt without reinstating authority', () => {
  for (const status of ['delivered', 'discarded'] as const) {
    const s = state(); bindReviewed(s); s.store.busy = operation;
    const completed = {live_steer_token: '', revision: 3, can_steer: false, items: [item(status)]};
    update(s, snapshot(3, completed), {...ui, status: 'idle'}, api, identity);
    assert.equal(validReceipt({...receipt(), revision: 3, steer: completed}, operation, api), true);
    accepted(s.store, operation); s.store.busy = null;
    assert.equal(s.store.text, ''); assert.equal(canSteer(s), false);
    assert.equal(s.projection?.items[0].status, status); assert.equal(s.projection?.live_steer_token, '');
  }
});

test('only the unchanged submitted draft clears; partial edits and reviewed newer run survive an old ACK', () => {
  const s = state(); bindReviewed(s);
  setText(s.store, 'new partial draft');
  update(s, snapshot(5, {...projection(), live_steer_token: 'next-root', revision: 5}), ui, api, identity);
  accepted(s.store, operation);
  assert.equal(s.store.text, 'new partial draft'); assert.equal(s.projection?.live_steer_token, 'next-root'); assert.equal(stale(s), true);
  const sameTextNewRevision = {...s.store, text: operation.text};
  accepted(sameTextNewRevision, operation); assert.equal(sameTextNewRevision.text, operation.text);
});

test('idle local dismissal preserves the independent global unknown fence and never grants Stop or sends', () => {
  const s = state(); s.store.unknown = true;
  update(s, snapshot(2, {live_steer_token: '', revision: 2, can_steer: false, items: [{request_id: 'nonce', text: operation.text, status: 'uncertain'}]}), {...ui, unknown: true, status: 'idle'}, api, identity);
  assert.equal(canDismissUnknown(s), true); assert.equal(canSteer(s), false); assert.equal(blocking(s), true);
  s.store.unknown = false; s.store.token = ''; s.store.baseRevision = 0;
  assert.equal(blocking(s), false); assert.equal(s.ui?.unknown, true); assert.equal(canSteer(s), false);
  assert.equal(s.store.text, operation.text); assert.equal(s.projection?.items[0].status, 'uncertain');
  for (const patch of [{connected: false}, {busy: true}, {stopping: true}, {editing: true}]) {
    s.store.unknown = true; update(s, snapshot(2), {...ui, status: 'idle', ...patch}, api, identity); assert.equal(canDismissUnknown(s), false);
  }
});
