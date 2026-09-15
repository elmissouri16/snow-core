// Node/CDP native-browser runner. All session data and transports are fictional.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {readFile, mkdtemp, rm} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {chromeBinary, connect, debuggingURL} from '../live-stream/cdp.mjs';
import {fixtureHTML} from './fixture.mjs';
import {deletionTests} from './tests.mjs';

const root = new URL('../../../../', import.meta.url);
const template = await readFile(new URL('internal/web/templates/pages.html', root), 'utf8');
const css = [...new Set([...template.matchAll(/href="(\/static\/[^" ]+\.css)"/g)].map(match => match[1]))];
const files = new Map(await Promise.all(css.map(async path => [path, await readFile(new URL('internal/web' + path, root))])));
const menus = await readFile(new URL('internal/web/static/menus.js', root), 'utf8');
files.set('/static/generated/app.js', await readFile(new URL('internal/web/static/generated/app.js', root)));
const app = await readFile(new URL('internal/web/static/app.js', root), 'utf8');
const cut = app.indexOf('  function syncThemeChoices(');
assert.ok(cut > app.indexOf('document.addEventListener("snow:shell-navigate"'), 'Production prefix includes the real navigation delegate');
const appPrefix = app.slice(0, cut) + '\n})();';
const htmx = await readFile(new URL('internal/web/static/vendor/htmx-2.0.10.min.js', root), 'utf8');
const html = fixtureHTML(css, menus, htmx, appPrefix);
const temporary = await mkdtemp(join(tmpdir(), 'snow-session-delete-'));
let chrome, client, sessionId, assertions = 0, scenarios = 0;
const failures = [], exceptions = [], serverViolations = [], nativeRequests = [], nativeWaiters = [];
const server = createServer((request, response) => {
  const url = new URL(request.url, 'http://fixture');
  if (request.method === 'GET' && url.pathname === '/fixture.html') {
    response.writeHead(200, {'Content-Type': 'text/html', 'Cache-Control': 'no-store'}); response.end(html); return;
  }
  if (request.method === 'GET' && files.has(url.pathname)) {
    response.writeHead(200, {'Content-Type': url.pathname.endsWith('.js') ? 'application/javascript' : 'text/css', 'Cache-Control': 'no-store'}); response.end(files.get(url.pathname)); return;
  }
  if (request.method === 'GET' && url.pathname === '/favicon.ico') { response.writeHead(204); response.end(); return; }
  if (request.method === 'GET' && /^\/\?view=projects&project=00000000-0000-4000-8000-00000000000[12]&new=1$/.test(request.url) && request.headers['hx-request'] === 'true') {
    // Strict fixture navigation is deliberately held until an assertion releases it.
    const pending = {url: request.url, response, closed: false};
    response.on('close', () => { pending.closed = true; });
    nativeRequests.push(pending); nativeWaiters.splice(0).forEach(callback => callback()); return;
  }
  serverViolations.push(request.method + ' ' + request.url);
  response.writeHead(404); response.end('Unmocked transport forbidden');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const deadline = setTimeout(() => { console.error('Session deletion suite exceeded 120s'); chrome?.kill('SIGKILL'); client?.close(); }, 120000);
try {
  chrome = spawn(chromeBinary(), ['--headless', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--disable-extensions', '--remote-debugging-port=0', `--user-data-dir=${temporary}`, 'about:blank'], {stdio: ['ignore', 'ignore', 'pipe']});
  client = await connect(await debuggingURL(chrome));
  const {targetId} = await client.send('Target.createTarget', {url: 'about:blank'});
  ({sessionId} = await client.send('Target.attachToTarget', {targetId, flatten: true}));
  await client.send('Runtime.enable', {}, sessionId); await client.send('Page.enable', {}, sessionId);
  client.onEvent(event => { if (event.sessionId === sessionId && event.method === 'Runtime.exceptionThrown') exceptions.push(event.params.exceptionDetails); });
  async function evaluate(code) {
    const result = await client.send('Runtime.evaluate', {expression: `(async () => { ${code} })()`, awaitPromise: true, returnByValue: true}, sessionId);
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
    return result.result.value;
  }
  async function check(expression, label) {
    assert.equal(await evaluate(`return (${expression});`), true, label); assertions++; console.log('  PASS', label);
  }
  const viewport = (width, height) => client.send('Emulation.setDeviceMetricsOverride', {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
  async function key(key) {
    const code = {' ': 'Space'}[key] || key;
    const windowsVirtualKeyCode = {Enter: 13, Escape: 27, Tab: 9, ' ': 32}[key];
    await client.send('Input.dispatchKeyEvent', {type: 'keyDown', key, code, windowsVirtualKeyCode, ...(key === 'Enter' ? {text: '\r'} : key === ' ' ? {text: ' '} : {})}, sessionId);
    await client.send('Input.dispatchKeyEvent', {type: 'keyUp', key, code, windowsVirtualKeyCode}, sessionId);
  }
  async function run(name, options, test) {
    scenarios++;
    const exceptionStart = exceptions.length, serverStart = serverViolations.length;
    try {
      await viewport(1280, 740);
      const loaded = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => { unsubscribe(); reject(new Error('Page load deadline')); }, 5000);
        const unsubscribe = client.onEvent(event => {
          if (event.sessionId === sessionId && event.method === 'Page.loadEventFired') { clearTimeout(timeout); unsubscribe(); resolve(); }
        });
      });
      await client.send('Page.navigate', {url: origin + '/fixture.html?' + new URLSearchParams(options)}, sessionId);
      await loaded;
      await evaluate('await f.until(() => window.fixtureReady === true, "React Shell initialization"); await f.idle();');
      console.log('SCENARIO', name);
      await test();
      await check('f.violations.length === 0', 'No unexpected runtime transport, navigation or page error');
      assert.deepEqual(exceptions.slice(exceptionStart), [], 'No native JavaScript exception');
      assert.deepEqual(serverViolations.slice(serverStart), [], 'No unmocked HTTP request');
    } catch (error) {
      failures.push({name, error: error.message}); console.error('FAIL', name, error.message);
      try { console.error(await evaluate('return {requests: f.requests, navigation: f.navigation, errors: f.violations, dialog: {open: f.$("#session-delete-dialog")?.open, error: f.$("[data-delete-error]")?.textContent}, active: document.activeElement?.tagName, button: f.button()?.getBoundingClientRect().toJSON(), row: f.row()?.getBoundingClientRect().toJSON(), hit: (() => { const r = f.button()?.getBoundingClientRect(); return r ? document.elementFromPoint(r.x+r.width/2, r.y+r.height/2)?.outerHTML.slice(0, 160) : null; })()};')); } catch { /* Browser may have reached global deadline. */ }
    } finally {
      for (const pending of nativeRequests) if (!pending.closed && !pending.response.writableEnded) pending.response.destroy();
    }
  }
  await deletionTests({run, evaluate, check, key, viewport});

  function awaitNavigation(index) {
    if (nativeRequests[index]) return Promise.resolve(nativeRequests[index]);
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => { nativeWaiters.splice(nativeWaiters.indexOf(ready), 1); reject(new Error('Native HTMX navigation deadline')); }, 3000);
      const ready = () => { if (nativeRequests[index]) { clearTimeout(timeout); resolve(nativeRequests[index]); } else nativeWaiters.push(ready); };
      nativeWaiters.push(ready);
    });
  }
  function releaseNavigation(pending, project) {
    project = {a: '00000000-0000-4000-8000-000000000001', b: '00000000-0000-4000-8000-000000000002'}[project] || project;
    pending.response.writeHead(200, {'Content-Type': 'text/html', 'Cache-Control': 'no-store'});
    pending.response.end(`<div id="workspace" class="app-layout" data-project="${project}" data-session="" data-fixture-empty="true"><aside id="project-navigation" class="sidebar"><nav class="project-tree"></nav></aside><main class="workspace"><h1>Fictional new conversation in ${project}</h1><textarea>Unsent new conversation</textarea></main></div>`);
  }
  await run('native HTMX cold success pushes exact new=1 URL', {viewed: 'saved-one', htmx: 'native'}, async () => {
    const index = nativeRequests.length;
    await evaluate('await f.open(); await f.confirm(); f.succeed();');
    const pending = await awaitNavigation(index);
    assert.equal(pending.url, '/?view=projects&project=00000000-0000-4000-8000-000000000001&new=1'); assertions++;
    releaseNavigation(pending, 'a');
    await evaluate('await f.until(() => !!document.querySelector("[data-fixture-empty]"), "native new state swap");');
    await check('location.search === "?view=projects&project=00000000-0000-4000-8000-000000000001&new=1" && f.$("#workspace").dataset.session === ""', 'Actual HTMX pushes session-free empty-state URL');
    await check('f.posts().length === 1 && f.events.length === 1 && f.navigation.length === 1', 'Actual HTMX navigation adds no activation, send or duplicate delete');
  });
  await run('native superseding navigation wins over delayed deletion navigation', {viewed: 'saved-one', htmx: 'native'}, async () => {
    const index = nativeRequests.length;
    await evaluate('await f.open(); await f.confirm(); f.succeed();');
    const deletionNavigation = await awaitNavigation(index);
    await evaluate('void htmx.ajax("GET", "/?view=projects&project=00000000-0000-4000-8000-000000000002&new=1", {source: f.newLink("b"), target: "#workspace", swap: "outerHTML"}).catch(() => {});');
    const superseding = await awaitNavigation(index + 1);
    releaseNavigation(superseding, 'b');
    await evaluate('await f.until(() => f.$("#workspace").dataset.project === f.project("b"), "superseding new state");');
    // The canceled older response is released after the newer swap: it cannot win.
    releaseNavigation(deletionNavigation, 'a');
    await check('location.search === "?view=projects&project=00000000-0000-4000-8000-000000000002&new=1" && f.$("#workspace").dataset.project === f.project("b")', 'Shared navigation hx-sync protects the newer workspace and history');
    assert.ok(deletionNavigation.closed, 'Superseded native request was canceled before its stale response'); assertions++;
    await check('f.posts().length === 1 && f.events.length === 1', 'Superseding navigation never replays deletion');
  });
  console.log(`${assertions} native deletion assertions across ${scenarios} scenarios; ${failures.length} failures`);
  if (failures.length) { console.error(JSON.stringify(failures, null, 2)); process.exitCode = 1; }
} finally {
  clearTimeout(deadline); client?.close();
  if (chrome && chrome.exitCode === null) { const stopped = new Promise(resolve => chrome.once('exit', resolve)); chrome.kill('SIGTERM'); await stopped; }
  for (const pending of nativeRequests) if (!pending.response.writableEnded) pending.response.destroy();
  server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  await rm(temporary, {recursive: true, force: true, maxRetries: 5, retryDelay: 100});
}
