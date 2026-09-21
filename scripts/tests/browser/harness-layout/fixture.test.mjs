import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const source = readFileSync(new URL('./fixture.js', import.meta.url), 'utf8');
function fixture({roots = 1, snapshot = null, name = "home", groups = []} = {}) {
  const listeners = new Map();
  const window = {__harnessConfig: {name, snapshot}, addEventListener(type, fn) { listeners.set(type, fn); }, setTimeout};
  const document = {
    querySelector(selector) {
      assert.equal(selector, '[data-react-page="shell"]');
      return {dataset: {reactProps: JSON.stringify({projects: groups.map(group => ({id: group.dataset.sidebarProject})), sessions: [], project: ''})}};
    },
    querySelectorAll(selector) { if (selector === '[data-sidebar-project]') return groups; assert.equal(selector, '[data-browser-inventory]'); return Array(roots).fill({}); }
  };
  vm.runInNewContext(source, {window, document, navigator: {}, localStorage: {setItem() {}}, location: {href: 'http://127.0.0.1:12345/home.html', origin: 'http://127.0.0.1:12345'}, structuredClone, URL, URLSearchParams, Response});
  return {state: window.harnessFixture, fetch: window.fetch, window, ready: () => listeners.get('DOMContentLoaded')?.()};
}
const options = () => ({credentials: 'same-origin', cache: 'no-store', redirect: 'error', headers: {Accept: 'application/json'}});

test('exact public startup GET is bounded, retained, and classified separately', async () => {
  const f = fixture();
  assert.equal(f.state.inventoryComplete(), false, 'missing startup read is not accepted');
  const response = await f.fetch('/access/browsers', options());
  assert.equal(response.status, 200);
  const data = await response.json();
  assert.equal(data.limit, 8); assert.equal(data.browsers.length, 1);
  assert.match(data.browsers[0].id, /^browser_[a-f0-9]{32}$/);
  assert.deepEqual(Object.keys(data.browsers[0]).sort(), ['created', 'current', 'expires', 'id', 'label', 'last_used']);
  assert.equal(f.state.requests.length, 1); assert.equal(f.state.requests[0].method, 'GET');
  assert.equal(f.state.requests[0].kind, 'browser-inventory');
  assert.equal(f.state.nonInventoryRequests().length, 0);
  assert.equal(f.state.inventoryComplete(), true); assert.equal(f.state.errors.length, 0);
  assert.equal((await f.fetch('/access/browsers', options())).status, 409, 'reopening/refresh is not silently permitted');
  assert.equal(f.state.requests.length, 2); assert.equal(f.state.nonInventoryRequests().length, 1);
  assert.equal(f.state.errors.length, 1);
});

test('no inventory root permits no inventory read, including unactivated login pages', async () => {
  const f = fixture({roots: 0});
  assert.equal(f.state.inventoryComplete(), true);
  assert.equal((await f.fetch('/access/browsers', options())).status, 409);
  assert.equal(f.state.nonInventoryRequests().length, 1); assert.equal(f.state.errors.length, 1);
});

test('each mounted inventory root has exactly one read, never an unlimited GET exemption', async () => {
  const f = fixture({roots: 2});
  assert.equal((await f.fetch('/access/browsers', options())).status, 200);
  assert.equal(f.state.inventoryComplete(), false);
  assert.equal((await f.fetch('/access/browsers', options())).status, 200);
  assert.equal(f.state.inventoryComplete(), true);
  assert.equal((await f.fetch('/access/browsers', options())).status, 409);
});

for (const [label, path, patch] of [
  ['POST', '/access/browsers', {method: 'POST'}],
  ['query', '/access/browsers?offset=0', {}],
  ['fragment', '/access/browsers#anything', {}],
  ['body', '/access/browsers', {body: 'csrf=fictional'}],
  ['null body', '/access/browsers', {body: null}],
  ['credential header', '/access/browsers', {headers: {Accept: 'application/json', Authorization: 'fictional'}}],
  ['wrong accept', '/access/browsers', {headers: {Accept: 'text/html'}}],
  ['wrong credentials', '/access/browsers', {credentials: 'omit'}],
  ['cached read', '/access/browsers', {cache: 'default'}],
  ['redirect', '/access/browsers', {redirect: 'follow'}],
  ['external origin', 'https://example.invalid/access/browsers', {}],
  ['trailing path', '/access/browsers/anything', {}],
]) test(`strict startup inventory refuses ${label}`, async () => {
  const f = fixture();
  assert.equal((await f.fetch(path, {...options(), ...patch})).status, 409);
  assert.equal(f.state.nonInventoryRequests().length, 1);
  assert.equal(f.state.inventoryComplete(), false); assert.equal(f.state.errors.length, 1);
});

for (const snapshot of [null, {project_id: '00000000-0000-4000-8000-000000000001', instance_id: 'fixture-instance'}]) {
  test(`other reads/mutations fail closed with ${snapshot ? 'active' : 'inactive'} fixture`, async () => {
    const f = fixture({snapshot});
    assert.equal((await f.fetch('/access/browsers', options())).status, 200);
    const forbidden = [
      ['/operations?offset=0', 'GET'], ['/activity', 'GET'], ['/settings/host', 'GET'],
      ['/settings/providers', 'GET'], ['/settings/providers/opencode-go/api-key', 'GET'],
      ['/access/browsers/browser_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/revoke', 'POST'],
      ['/access/revoke-all', 'POST'], ['/projects/00000000-0000-4000-8000-000000000001/runtime/open', 'POST'],
      ['/operations/create', 'POST'],
    ];
    for (const [path, method] of forbidden) {
      assert.equal((await f.fetch(path, {...options(), method, ...(method === 'POST' ? {body: 'csrf=fixture-only-not-a-credential&instance_id=fixture-instance'} : {})})).status, 409, `${method} ${path}`);
    }
    assert.equal(f.state.nonInventoryRequests().length, forbidden.length);
    assert.equal(f.state.requests.length, forbidden.length + 1);
    assert.equal(f.state.errors.length, forbidden.length);
  });
}

const runtimeSnapshot = {project_id: '00000000-0000-4000-8000-000000000001', instance_id: 'fixture-instance', session_id: 'fixture-session', revision: 1,
  goal: {branch_id: 'fixture-branch'}, provider: 'host-provider', model: 'host-model', mode: 'default', permission_mode: 'ask', thinking: 'off'};
const scope = {csrf: 'fixture-only-not-a-credential', instance_id: 'fixture-instance', session_id: 'fixture-session'};
const postOptions = fields => ({...options(), method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8', Accept: 'application/json'}, body: new URLSearchParams(fields)});
const publicReads = [
  ['runtime/versions-list', {}], ['runtime/version-preview', {branch_id: 'fixture-branch-1', tip_id: 'fixture-tip-1'}],
  ['runtime/goal-inspect', {branch_id: 'fixture-branch'}], ['runtime/reasoning-inspect', {}], ['processes/list', {}],
  ['processes/logs', {process_id: 'proc_' + 'a'.repeat(32), max_bytes: '32768'}]
];
test('enabled runtime fixture accepts only exact bounded public read DTOs', async () => {
  const f = fixture({name: 'runtime-panels', snapshot: runtimeSnapshot});
  for (const [action, fields] of publicReads) {
    const response = await f.fetch('/projects/00000000-0000-4000-8000-000000000001/' + action, postOptions({...scope, ...fields}));
    assert.equal(response.status, 200, action);
    const body = await response.json();
    assert.equal(body.project_id, runtimeSnapshot.project_id); assert.equal(body.instance_id, runtimeSnapshot.instance_id);
    assert.equal(body.session_id || body.result?.session_id, runtimeSnapshot.session_id);
  }
  assert.equal(f.state.requests.filter(r => r.kind === 'runtime-panel-read').length, publicReads.length);
  assert.equal(f.state.errors.length, 0);
});
for (const [action, fields] of publicReads) test(`runtime read ${action} rejects changed scope, extra fields and transport`, async () => {
  const f = fixture({name: 'runtime-panels', snapshot: runtimeSnapshot});
  for (const patch of [{csrf: 'wrong'}, {instance_id: 'stale'}, {session_id: 'another'}, {authority: 'extra'}]) {
    assert.equal((await f.fetch('/projects/00000000-0000-4000-8000-000000000001/' + action, postOptions({...scope, ...fields, ...patch}))).status, 409);
  }
  const duplicate = postOptions({...scope, ...fields}); duplicate.body.append('csrf', scope.csrf);
  for (const [suffix, options] of [['', duplicate], ['?extra=true', postOptions({...scope, ...fields})], ['#fragment', postOptions({...scope, ...fields})], ['', {...postOptions({...scope, ...fields}), cache: 'default'}]]) {
    assert.equal((await f.fetch('/projects/00000000-0000-4000-8000-000000000001/' + action + suffix, options)).status, 409);
  }
  assert.equal(f.state.requests.every(r => !r.kind), true);
  assert.equal(f.state.errors.length, f.state.requests.length);
});
test('enabled runtime fixture never grants mutation authority, including legacy allowActions', async () => {
  const f = fixture({name: 'runtime-panels', snapshot: runtimeSnapshot}); f.state.allowActions = true;
  for (const action of ['runtime/prompt', 'runtime/input', 'runtime/abort', 'runtime/permission', 'runtime/open', 'runtime/close', 'runtime/choices',
    'runtime/version-restore-prepare', 'runtime/version-restore-commit', 'runtime/goal-start', 'runtime/goal-resume', 'runtime/compaction-start',
    'runtime/reasoning-set', 'runtime/steer', 'runtime/history-branch-fork', 'runtime/queue-next', 'processes/stop', 'processes/start']) {
    assert.equal((await f.fetch('/projects/00000000-0000-4000-8000-000000000001/' + action, postOptions(scope))).status, 409, action);
  }
  assert.equal(f.state.errors.length, f.state.requests.length);
});
test('unsupported runtime fixture refuses even known panel reads', async () => {
  const f = fixture({name: 'runtime-unsupported', snapshot: runtimeSnapshot});
  for (const [action, fields] of publicReads) assert.equal((await f.fetch('/projects/00000000-0000-4000-8000-000000000001/' + action, postOptions({...scope, ...fields}))).status, 409);
});

test('layout native discovery uses the same scoped public fixture as other requests', async () => {
  const f = fixture({name: 'chat', snapshot: {project_id: '00000000-0000-4000-8000-000000000001', instance_id: 'instance-one', session_id: 'session-one'}});
  const url = 'http://127.0.0.1:12345/projects/00000000-0000-4000-8000-000000000001/runtime/choices';
  const send = values => f.fetch(url, postOptions(values));
  let response = await send({csrf: 'fixture-only-not-a-credential', instance_id: 'instance-one'});
  assert.equal(response.status, 200);
  assert.equal(response.headers.get('Content-Type'), 'application/json');
  assert.equal(response.url, url, 'synthetic response retains exact intercepted URL for production admission');
  assert.equal(response.redirected, false);
  assert.equal((await response.json()).models.length, 30);
  assert.equal(f.state.requests.length, 1);
  response = await send({csrf: 'fixture-only-not-a-credential', instance_id: 'retired-instance'});
  assert.equal(response.status, 409, 'native transport cannot bypass instance admission');
});

test('layout native discovery preserves failures and runtime read allowlist', async () => {
  for (const name of ['model-unavailable', 'runtime-goals']) {
    const f = fixture({name, snapshot: {project_id: '00000000-0000-4000-8000-000000000001', instance_id: 'instance-one', session_id: 'session-one'}});
    const response = await f.fetch('http://127.0.0.1:12345/projects/00000000-0000-4000-8000-000000000001/runtime/choices',
      postOptions({csrf: 'fixture-only-not-a-credential', instance_id: 'instance-one'}));
    assert.equal(response.status, name === 'model-unavailable' ? 503 : 409);
    assert.equal(f.state.requests.length, 1);
  }
});


function sidebarGroup(project, count = 35) {
  return {dataset: {sidebarProject: project}, querySelectorAll(selector) {
    assert.equal(selector, '[data-shell-session]');
    return Array.from({length: count}, (_, index) => ({dataset: {shellSession: `saved-${index}`}, querySelector(selector) {
      assert.equal(selector, 'a span'); return {textContent: `Saved conversation ${index + 1}`};
    }}));
  }};
}
test('cold sidebar inventories page metadata from real exported rows without inventing a runtime', async () => {
  const group = sidebarGroup('cold-project'), f = fixture({groups: [group]});
  const first = await (await f.fetch('/projects/cold-project/sidebar-sessions?offset=0', options())).json();
  assert.equal(first.instance_id, ''); assert.equal(first.available, true); assert.equal(first.sessions.length, 25);
  assert.equal(first.next_offset, 25); assert.equal(first.has_more, true); assert.equal(first.sessions[24].name, 'Saved conversation 25');
  group.querySelectorAll = () => assert.fail('Paging must retain initial exported inventory after production DOM reconciliation');
  const second = await (await f.fetch('/projects/cold-project/sidebar-sessions?offset=25', options())).json();
  assert.equal(second.sessions.length, 10); assert.equal(second.sessions[9].name, 'Saved conversation 35');
  assert.equal(second.next_offset, 0); assert.equal(second.has_more, false);
  assert.equal(f.state.nonInventoryRequests().length, 0); assert.equal(f.state.requests.length, 2); assert.equal(f.state.errors.length, 0);
});
test('live sidebar inventory is an explicit session-only read, never panel or provider discovery', async () => {
  const f = fixture({name: 'runtime-unsupported', groups: [sidebarGroup('live-project')], snapshot: {project_id: 'live-project', instance_id: 'instance-one', session_id: 'session-one'}});
  const data = await (await f.fetch('/projects/live-project/sidebar-sessions', options())).json();
  assert.equal(data.instance_id, 'instance-one'); assert.equal(data.sessions[0].session_id, 'session-one');
  assert.equal(f.state.requests[0].kind, 'sidebar-inventory'); assert.equal(f.state.errors.length, 0);
});
for (const [label, path, patch] of [
  ['unknown workspace', '/projects/unknown/sidebar-sessions', {}],
  ['mutation', '/projects/cold-project/sidebar-sessions', {method: 'POST'}],
  ['body', '/projects/cold-project/sidebar-sessions', {body: 'activate=yes'}],
  ['negative page', '/projects/cold-project/sidebar-sessions?offset=-1', {}],
  ['duplicate page', '/projects/cold-project/sidebar-sessions?offset=0&offset=25', {}],
  ['discovery flag', '/projects/cold-project/sidebar-sessions?models=true', {}],
  ['fragment', '/projects/cold-project/sidebar-sessions#anything', {}],
  ['extra authorization header', '/projects/cold-project/sidebar-sessions', {headers: {Accept: 'application/json', Authorization: 'fixture'}}],
]) test(`sidebar read allowlist rejects ${label}`, async () => {
  const f = fixture({groups: [sidebarGroup('cold-project')]});
  assert.equal((await f.fetch(path, {...options(), ...patch})).status, 409);
  assert.equal(f.state.nonInventoryRequests().length, 1); assert.equal(f.state.errors.length, 1);
});
