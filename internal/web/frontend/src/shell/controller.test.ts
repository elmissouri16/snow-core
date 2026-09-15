import test from 'node:test';
import assert from 'node:assert/strict';
import {ShellController} from './controller.ts';
import type {Group} from './controller.ts';
import {LIMITS} from './model.ts';
import type {ShellBootstrap} from './model.ts';
const project = '12345678-1234-1234-1234-123456789012';
const launcher = (instance = 'instance') => ({isConnected: true, dataset: {project, session: 'next', instance, deleteSupported: 'true', deleteAvailable: 'true', deleteActive: 'false'}} as unknown as HTMLElement);
const state = (): Group => ({project, expanded: false, rows: [{session_id: 'old', name: 'Old'}, {session_id: 'next', name: 'Next'}], instance: 'instance', loaded: true, available: true, deleteSupported: true, activeSession: 'old', loading: false, error: '', nextOffset: 0, hasMore: false, truncated: false, pages: 0});
function controller() {
  const c = new ShellController(); c.mounted = true; c.snapshot.groups.set(project, state()); return c;
}
test('live selection never rewrites session row IDs while inventory is delayed', () => {
  const c = controller();
  c.updateLive({project, session: 'next', instance: 'instance', title: 'Renamed next', renameAvailable: true, renameDisabled: false, newDisabled: false});
  const rows = c.snapshot.groups.get(project)!.rows;
  assert.deepEqual(rows.map(row => row.session_id), ['old', 'next']);
  assert.equal(rows[0].name, 'Old'); assert.equal(rows[1].name, 'Renamed next');
  c.updateLive({...c.snapshot.live!, session: 'new', title: 'New'});
  assert.deepEqual(c.snapshot.groups.get(project)!.rows.map(row => row.session_id), ['new', 'old', 'next']);
});
test('latest revoked capability, detached trigger, retired owner and active session invalidate captured confirmation', () => {
  const c = controller(), trigger = launcher();
  const attempt = {project, session: 'next', instance: 'instance', owner: c.owner, trigger};
  assert.equal(c.eligible(attempt), true);
  c.group(project, {deleteSupported: false}); assert.equal(c.eligible(attempt), false);
  c.group(project, {deleteSupported: true});
  assert.equal(c.eligible({...attempt, trigger: {isConnected: false} as HTMLElement}), false);
  assert.equal(c.eligible({...attempt, owner: {}}), false);
  assert.equal(c.eligible({...attempt, instance: 'stale'}), false);
  c.group(project, {activeSession: 'next'}); assert.equal(c.eligible(attempt), false);
  c.group(project, {activeSession: 'old'});
  c.updateLive({project, session: 'next', instance: 'instance', title: 'Next', renameAvailable: true, renameDisabled: false, newDisabled: false});
  c.group(project, {activeSession: 'old'}); // stale inventory cannot authorize live deletion
  assert.equal(c.eligible(attempt), false);
});
test('explicit Start preempts inventory synchronously without retry', () => {
  const c = controller(), request = new AbortController();
  c.requests.set(project, request); c.group(project, {loading: true});
  c.preemptInventory();
  assert.equal(request.signal.aborted, true); assert.equal(c.requests.size, 0);
  assert.equal(c.snapshot.groups.get(project)!.loading, false);
  assert.equal(c.snapshot.groups.get(project)!.deleteSupported, false);
});
test('independent branch visibility/filter state stays in tab memory', () => {
  const c = controller(), other = '87654321-1234-1234-1234-123456789012';
  c.snapshot.groups.set(other, {...state(), project: other});
  c.toggle(project);
  assert.equal(c.snapshot.groups.get(project)!.expanded, true);
  assert.equal(c.snapshot.groups.get(other)!.expanded, false);
  c.publish({query: 'workspace', pinnedOnly: true}); c.syncVisibility(true);
  assert.equal(c.snapshot.groups.get(project)!.expanded, true);
  assert.equal(c.snapshot.query, 'workspace'); assert.equal(c.snapshot.pinnedOnly, true);
});
test('bounded inventory rejects a 26th page and 101st project read without transport', async () => {
  const c = controller(); c.group(project, {pages: LIMITS.pages});
  await c.load(project); assert.equal(c.requests.size, 0); assert.match(c.snapshot.groups.get(project)!.error, /limit/);
  c.group(project, {pages: 0}); c.reads = LIMITS.reads;
  await c.load(project); assert.equal(c.requests.size, 0); assert.match(c.snapshot.groups.get(project)!.error, /limit/);
});
test('unconfirmed and repeated deletion submissions never dispatch', async () => {
  const c = controller();
  c.snapshot.deletion = {project, session: 'next', instance: 'instance', owner: c.owner, trigger: {isConnected: true} as HTMLElement, name: 'Next', pending: false, submitted: true, error: ''};
  await c.remove(false); await c.remove(true);
  assert.equal(c.snapshot.deletion.submitted, true);
});

test('cold deletion captures empty owner nonce, admits once and keeps UNKNOWN non-retryable', async t => {
  const c = controller(); c.snapshot.bootstrap = {csrf: 'csrf'} as ShellBootstrap;
  c.group(project, {instance: '', activeSession: ''});
  const trigger = launcher('');
  c.snapshot.deletion = {project, session: 'next', instance: '', owner: c.owner, trigger, name: 'Next', pending: false, submitted: false, error: ''};
  let calls = 0, body = '';
  let finish!: (value: Response) => void;
  t.mock.method(globalThis, 'fetch', (_url: unknown, options: RequestInit) => {
    calls++; body = String(options.body); return new Promise<Response>(resolve => { finish = resolve; });
  });
  await c.remove(false); assert.equal(calls, 0);
  const pending = c.remove(true);
  await c.remove(true);
  assert.equal(calls, 1);
  assert.equal(new URLSearchParams(body).get('instance_id'), '');
  assert.equal(new URLSearchParams(body).get('confirm'), 'delete');
  finish(new Response('<html>proxy error</html>', {status: 502, headers: {'Content-Type': 'text/html'}}));
  await pending;
  assert.match(c.snapshot.deletion!.error, /may have completed/);
  assert.doesNotMatch(c.snapshot.deletion!.error, /proxy/);
  assert.equal(c.snapshot.deletion!.submitted, true);
  assert.equal(c.snapshot.deletion!.pending, false);
  await c.remove(true); assert.equal(calls, 1);
});

test('latest inventory capability loss is exact and preempted late responses cannot restore it', async t => {
  const c = controller();
  const response = {project_id: project, instance_id: 'instance', sessions: [{session_id: 'old', name: 'Old'}, {session_id: 'next', name: 'Next'}], available: true, active_session_id: 'old', delete_supported: false};
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify(response)));
  await c.load(project);
  assert.equal(c.snapshot.groups.get(project)!.deleteSupported, false);
  let finish!: (value: Response) => void;
  t.mock.method(globalThis, 'fetch', () => new Promise<Response>(resolve => { finish = resolve; }));
  const pending = c.load(project);
  c.preemptInventory();
  finish(new Response(JSON.stringify({...response, delete_supported: true})));
  await pending;
  assert.equal(c.snapshot.groups.get(project)!.deleteSupported, false);
  assert.equal(c.snapshot.groups.get(project)!.loading, false);
});
