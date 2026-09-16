// Phase-two native React parity. All HTTP below is fake, allowlisted, and recording-only.
import {csrf, projectID, malicious} from './fixture.mjs';

const otherID = 'browser_' + '1'.repeat(32), currentID = 'browser_' + '2'.repeat(32);
const browserRows = () => [
  {id: otherID, label: malicious, current: false, created: '2026-07-01T00:00:00Z', last_used: '2026-07-02T00:00:00Z', expires: '2026-07-31T00:00:00Z'},
  {id: currentID, label: 'Fixture current browser', current: true, created: '2026-07-01T00:00:00Z', last_used: '2026-07-02T00:00:00Z', expires: '2026-07-31T00:00:00Z'},
];
const defaults = (scope = 'global', project = '', revision = 'revision-fixture-1') => ({
  scope, project_id: project, revision, applies_to: 'future_runtime', availability: 'not_network_verified',
  [scope]: {
    provider_model: {explicit: null, effective: {provider: 'openai-compatible', model: 'fixture-model'}, source: 'builtin'},
    thinking: {explicit: null, effective: 'off', source: 'builtin'},
    ...(scope === 'global' ? {reasoning_summary: {explicit: null, effective: 'off', source: 'builtin'}, text_verbosity: {explicit: null, effective: 'medium', source: 'builtin'}} : {}),
  },
});
export function settingsTransport(state) {
  state.resetSettings = () => { state.hostProps = null; state.settingsNext = []; state.settingsAborts = 0; state.inventory = []; };
  state.resetSettings();
  state.handleSettings = async (request, response, record, url) => {
    const path = url.pathname;
    if (path === '/login' && request.method === 'GET') { response.writeHead(200, {'Content-Type': 'text/html'}); response.end('<!doctype html><title>Fixture login</title><h1>Fixture pairing screen</h1>'); return true; }
    if (!['/settings/host', '/settings/providers', '/access/browsers'].includes(path) && !/^\/access\/browsers\/browser_[a-f0-9]{32}\/revoke$/.test(path)) return false;
    const exact = (fields, keys) => {
      if (fields.size !== keys.length || JSON.stringify([...fields.keys()].sort()) !== JSON.stringify(keys.sort())) throw Error('Unexpected settings field set');
    };
    let fields;
    if (request.method === 'POST') {
      let body = ''; for await (const chunk of request) { body += chunk; if (body.length > 16384) throw Error('Settings request exceeds body bound'); }
      fields = new URLSearchParams(body); body = '';
      if (request.headers['content-type'] !== 'application/x-www-form-urlencoded' || fields.get('csrf') !== csrf || request.headers.origin !== `http://${request.headers.host}`) throw Error('Settings POST content type, CSRF or same-origin mismatch');
      if (path === '/settings/host') {
        const scope = fields.get('scope'), keys = ['csrf', 'scope', 'expected_revision'];
        if (!['global', 'project'].includes(scope) || fields.get('expected_revision') !== 'revision-fixture-1') throw Error('Host scope/revision mismatch');
        if (scope === 'project') { keys.push('project'); if (fields.get('project') !== projectID) throw Error('Host project mismatch'); }
        for (const name of scope === 'global' ? ['provider_model', 'thinking', 'reasoning_summary', 'text_verbosity'] : ['provider_model', 'thinking']) {
          const op = fields.get(name + '_op'); if (op === null) continue;
          if (!['set', 'reset'].includes(op)) throw Error('Invalid host operation'); keys.push(name + '_op');
          if (op === 'set') keys.push(...(name === 'provider_model' ? ['provider', 'model'] : [name]));
        }
        exact(fields, keys);
      } else if (path.endsWith('/revoke')) {
        exact(fields, ['csrf', 'confirm']); if (fields.get('confirm') !== 'revoke') throw Error('Browser revoke contract mismatch');
      } else throw Error('Forbidden settings mutation');
      record.fields = Object.fromEntries(fields);
    } else if (request.method !== 'GET') throw Error('Forbidden settings method');
    if (request.headers.accept !== 'application/json') throw Error('Settings JSON Accept required');
    let value;
    if (path === '/settings/host') value = defaults(fields?.get('scope') || url.searchParams.get('scope'), fields?.get('project') || url.searchParams.get('project') || '');
    else if (path === '/settings/providers') value = {checked_locally: true, providers: [{provider_id: 'openai-compatible', state: 'unavailable', reason: 'credential_missing', checked_locally: true}]};
    else if (path === '/access/browsers') value = {limit: 8, browsers: state.inventory};
    else if (path.endsWith('/revoke')) value = {revoked_id: path.split('/')[3], signed_out: path.includes(currentID)};
    else throw Error('Unexpected settings path');
    const i = state.settingsNext.findIndex(next => (!next.path || next.path === path) && (!next.method || next.method === request.method));
    const behavior = i < 0 ? {} : state.settingsNext.splice(i, 1)[0];
    response.on('close', () => { if (!response.writableFinished) state.settingsAborts++; });
    const body = behavior.raw ?? JSON.stringify(behavior.value ?? value);
    const send = () => {
      if (response.destroyed) return;
      response.writeHead(behavior.status || 200, {'Content-Type': 'application/json', 'Cache-Control': 'no-store', ...behavior.headers});
      if (behavior.holdBody) { const split = Math.max(1, body.length >> 1); response.write(body.slice(0, split)); state.held.push(() => response.end(body.slice(split))); }
      else response.end(body);
    };
    if (behavior.holdHeaders) state.held.push(send); else send();
    return true;
  };
}

export async function settingsChecks(t) {
  const {page, wait, until, evaluate, click, type, assert, report, state, delay} = t;
  await t.send('Emulation.setDeviceMetricsOverride', {width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false});
  const requests = (path, method) => state.requests.filter(row => row.path === path && (!method || row.method === method));
  const open = async (section = 'general', setup) => {
    await page(section === 'access' ? 'browser-access' : 'host-settings', {setup});
    await click('[data-settings-open]'); await wait('document.querySelector("#settings-dialog").open');
    if (section !== 'general') await click(`[data-settings-section="${section}"]`);
  };
  const change = async (selector, value) => evaluate(`(() => {const node=document.querySelector(${JSON.stringify(selector)});node.value=${JSON.stringify(value)};node.dispatchEvent(new Event('change',{bubbles:true}));})()`);
  const load = async () => { await click('[data-host-load]'); await wait('document.querySelectorAll(".host-default-row").length>0 && !document.querySelector("[data-host-save]").disabled'); };
  await report('React host-settings mount is read-free; capabilities and local provider status', async () => {
    await open(); await delay(150);
    assert(await evaluate('document.querySelector("[data-react-page=shell]").dataset.reactMounted==="true" && document.querySelector("[data-host-api-key]")===null'), 'Committed real shell contains host settings without removed API-key controls');
    assert(!state.requests.some(row => row.path.startsWith('/settings/')), 'Opening General reads no host settings or credentials');
    assert(await evaluate('!Array.from(document.scripts).some(s=>/\\/(host-settings|host-api-key|browser-access)\\.js$/.test(s.src))'), 'No retired settings/API-key/browser-access scripts loaded');
    await click('[data-host-providers-load]');
    await wait('document.querySelector("[data-host-providers-list]").textContent.includes("not network verified")');
    assert(requests('/settings/providers', 'GET').length === 1, 'Provider status requires one explicit local-only GET');
    await open('general', state => { state.hostProps = {csrf, enabled: false, projects: []}; });
    assert(await evaluate('document.querySelector("[data-host-load]").disabled'), 'Server-disabled host settings cannot be enabled by UI');
    assert(!state.requests.some(row => row.path.startsWith('/settings/')), 'Disabled settings produce no settings traffic');
  });
  await report('Host defaults exact conditional bodies, stable draft controls, conflict consumes write grant', async () => {
    await open(); await load();
    assert(requests('/settings/host', 'GET').length === 1 && requests('/settings/host')[0].query === '?scope=global', 'Explicit global read uses exact scope URL');
    await evaluate('window.keptOperation=document.querySelector(".host-default-row select");void 0');
    await change('.host-default-row:nth-child(1) select', 'set');
    await type('.host-default-row:nth-child(1) input:nth-of-type(1)', 'openai-compatible');
    // Inputs live inside labels; select the second input by row-local collection.
    await evaluate('document.querySelectorAll(".host-default-row")[0].querySelectorAll("input")[1].focus();document.querySelectorAll(".host-default-row")[0].querySelectorAll("input")[1].select()');
    await t.send('Input.insertText', {text: 'changed-fixture-model'});
    await change('.host-default-row:nth-child(2) select', 'reset');
    assert(await evaluate('keptOperation===document.querySelector(".host-default-row select")'), 'React edits preserve operation-control identity');
    state.settingsNext.push({path: '/settings/host', method: 'POST', status: 409});
    await click('[data-host-save]'); await until(() => requests('/settings/host', 'POST').length === 1, 'Host conditional save');
    await wait('document.querySelector("[data-host-status]").textContent.includes("could not be confirmed")');
    const posted = requests('/settings/host', 'POST')[0].fields;
    assert(JSON.stringify(posted) === JSON.stringify({csrf, scope: 'global', expected_revision: 'revision-fixture-1', provider_model_op: 'set', provider: 'openai-compatible', model: 'changed-fixture-model', thinking_op: 'reset'}), 'Unchanged fields omitted; provider/model atomic, reset carries no value');
    assert(await evaluate('document.querySelector("[data-host-save]").disabled'), 'Conflict consumes save authority pending explicit reload');
    await delay(100); assert(requests('/settings/host', 'POST').length === 1, 'Conflict never retries a mutation');
    await change('[data-host-scope]', 'project'); await change('[data-host-project]', projectID); await load();
    assert(requests('/settings/host', 'GET').at(-1).query === `?scope=project&project=${projectID}`, 'Project read includes exact registered identity');
    assert(await evaluate('document.querySelectorAll(".host-default-row").length===2'), 'Project scope excludes unsupported global-only fields');
    await change('.host-default-row:nth-child(2) select', 'set');
    await change('.host-default-row:nth-child(2) label:last-child select', 'high');
    await click('[data-host-save]'); await until(() => requests('/settings/host', 'POST').length === 2, 'Project settings POST');
    await wait('document.querySelector("[data-host-status]").textContent.startsWith("Saved")');
    assert(JSON.stringify(requests('/settings/host', 'POST')[1].fields) === JSON.stringify({csrf, scope: 'project', expected_revision: 'revision-fixture-1', project: projectID, thinking_op: 'set', thinking: 'high'}), 'Project mutation contains only supported edited field and exact revision');
  });
  await report('Host defaults malformed/auth response and unmount fence stale reads', async () => {
    await open(); state.settingsNext.push({path: '/settings/host', value: defaults('project', projectID)}); await click('[data-host-load]');
    await wait('document.querySelector("[data-host-status]").textContent.startsWith("Unable")');
    assert(await evaluate('document.querySelector("[data-host-save]").disabled && document.querySelectorAll(".host-default-row").length===0'), 'Wrong-scope DTO cannot grant write authority');
    state.settingsNext.push({path: '/settings/host', status: 401}); await click('[data-host-load]');
    await wait('!document.querySelector("[data-host-load]").disabled');
    assert(await evaluate('document.querySelector("[data-host-save]").disabled'), 'Authentication failure remains read-only');
    state.settingsNext.push({path: '/settings/host', holdBody: true}); await click('[data-host-load]'); await until(() => state.held.length > 0, 'Pending host body');
    await click('[data-settings-close]');
    await click('a.sidebar-utility[href="/?view=organization"]');
    await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    await until(() => state.settingsAborts > 0, 'Host unmount aborts body'); state.release();
    assert(!requests('/settings/host', 'POST').length, 'Stale read/unmount never triggers settings mutation');
  });
  await report('React browser inventory validates public rows, native confirmation focus and strict one-browser revoke', async () => {
    await open('access', state => { state.inventory = browserRows(); });
    await wait('document.querySelectorAll("[data-browser-id]").length===2');
    assert(requests('/access/browsers', 'GET').length === 1, 'One initial read per committed browser root');
    assert(await evaluate(`document.querySelector('[data-browser-id="${otherID}"] strong').textContent===${JSON.stringify(malicious)} && !document.querySelector('[data-browser-inventory] img')`), 'Untrusted browser labels remain text');
    await click(`[data-browser-id="${otherID}"] button`);
    assert(await evaluate('document.activeElement===document.querySelector("[data-browser-cancel]")'), 'Selecting revoke focuses safe Cancel');
    await click('[data-browser-cancel]');
    assert(await evaluate('document.activeElement===document.querySelector("[data-browser-refresh]") && document.querySelector("[data-browser-confirm]").hidden'), 'Cancel hides confirmation and restores Refresh focus');
    await click(`[data-browser-id="${otherID}"] button`);
    state.settingsNext.push({path: `/access/browsers/${otherID}/revoke`, holdHeaders: true});
    await evaluate('document.querySelector("[data-browser-revoke]").click();document.querySelector("[data-browser-revoke]").click()');
    await until(() => requests(`/access/browsers/${otherID}/revoke`, 'POST').length === 1, 'One explicit browser revoke');
    assert(JSON.stringify(requests(`/access/browsers/${otherID}/revoke`, 'POST')[0].fields) === JSON.stringify({csrf, confirm: 'revoke'}), 'One-browser exact CSRF/confirm POST, duplicate event fenced');
    state.release(); await wait(`!document.querySelector('[data-browser-id="${otherID}"]')`);
    assert(await evaluate(`!!document.querySelector('[data-browser-id="${currentID}"]')`), 'Receipt removes only the selected browser');
  });
  await report('Browser malformed metadata and uncertain receipt discard authority until explicit refresh', async () => {
    await open('access', state => { state.inventory = browserRows(); }); await wait('document.querySelectorAll("[data-browser-id]").length===2');
    state.settingsNext.push({path: '/access/browsers', value: {limit: 8, browsers: [browserRows()[0], browserRows()[0]]}});
    await click('[data-browser-refresh]'); await wait('document.querySelector("[data-browser-status]").textContent.startsWith("Unable")');
    assert(await evaluate('document.querySelectorAll("[data-browser-id]").length===0'), 'Duplicate identity rejects complete inventory');
    await click('[data-browser-refresh]'); await wait('document.querySelectorAll("[data-browser-id]").length===2');
    await click(`[data-browser-id="${otherID}"] button`);
    state.settingsNext.push({path: `/access/browsers/${otherID}/revoke`, value: {revoked_id: currentID, signed_out: false}});
    await click('[data-browser-revoke]'); await wait('document.querySelector("[data-browser-status]").textContent.includes("Could not confirm")');
    assert(await evaluate('document.querySelectorAll("[data-browser-id]").length===2 && Array.from(document.querySelectorAll("[data-browser-id] button")).every(b=>b.disabled)'), 'Mismatched receipt removes no unrelated row and consumes authority');
    await delay(150); assert(requests(`/access/browsers/${otherID}/revoke`, 'POST').length === 1, 'Unknown revoke outcome never retries');
  });
  await report('Rejected native navigation preserves browser React root but retires confirmation authority', async () => {
    await open('access', state => { state.inventory = browserRows(); }); await wait('document.querySelectorAll("[data-browser-id]").length===2');
    await click(`[data-browser-id="${otherID}"] button`);
    state.invalidNextNavigation = true;
    await evaluate('window.keptBrowserRoot=document.querySelector("[data-react-page=shell]");window.browserNavigationDone=false;document.addEventListener("snow:navigation-end",()=>window.browserNavigationDone=true,{once:true})');
    await click('[data-settings-close]'); await click('a.sidebar-utility[href="/?view=organization"]');
    await wait('window.browserNavigationDone');
    await click('[data-settings-open]'); await click('[data-settings-section="access"]');
    assert(await evaluate('keptBrowserRoot===document.querySelector("[data-react-page=shell]") && keptBrowserRoot.dataset.reactMounted==="true"'), 'Rejected ancestor swap preserves browser root/listeners');
    assert(await evaluate('document.querySelector("[data-browser-confirm]").hidden && Array.from(document.querySelectorAll("[data-browser-id] button")).every(b=>b.disabled)'), 'Navigation clears stale selection and row mutation authority');
    assert(requests('/access/browsers', 'GET').length === 1 && !state.requests.some(row => row.method === 'POST'), 'Rejected browser navigation does not auto-refresh or revoke');
    await click('[data-browser-refresh]'); await wait('!document.querySelector("[data-browser-id] button").disabled');
    assert(requests('/access/browsers', 'GET').length === 2, 'Explicit refresh restores one read owner after rejected swap');
  });
  await report('Browser pending revoke interrupted by navigation never replays; HTTP 401 routes to pairing', async () => {
    await open('access', state => { state.inventory = browserRows(); }); await wait('document.querySelectorAll("[data-browser-id]").length===2');
    await click(`[data-browser-id="${otherID}"] button`); state.settingsNext.push({path: `/access/browsers/${otherID}/revoke`, holdBody: true}); await click('[data-browser-revoke]');
    await until(() => state.held.length > 0, 'Held revoke receipt'); await click('[data-settings-close]');
    await click('a.sidebar-utility[href="/?view=organization"]'); await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    await until(() => state.settingsAborts > 0, 'Navigation aborts pending browser receipt'); state.release();
    assert(requests(`/access/browsers/${otherID}/revoke`, 'POST').length === 1, 'Departure does not replay unknown revocation');
    await open('access', state => { state.inventory = browserRows(); }); await wait('document.querySelectorAll("[data-browser-id]").length===2');
    state.settingsNext.push({path: '/access/browsers', status: 401}); await click('[data-browser-refresh]');
    await wait('location.pathname==="/login"');
    assert(requests('/login', 'GET').length === 1 && !state.requests.some(row => row.method === 'POST'), 'Expired inventory authority routes to pairing without mutation');
  });
}
