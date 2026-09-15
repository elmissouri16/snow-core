// Node 22+ and native Chrome/CDP; no installed npm dependencies.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {readFile, mkdtemp, rm} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {chromeBinary, connect, debuggingURL} from '../live-stream/cdp.mjs';
import {cases, fixtureHTML} from './fixture.mjs';
import {runFocusTests} from './tests.mjs';

const root = new URL('../../../../', import.meta.url);
const template = await readFile(new URL('internal/web/templates/pages.html', root), 'utf8');
const head = template.match(/{{define "head"}}([\s\S]*?){{end}}/)?.[1];
assert.ok(head, 'Production head template found');
const css = [...head.matchAll(/href="(\/static\/[^" ]+\.css)"/g)].map(match => match[1]);
assert.ok(css.length > 10, 'Load complete production stylesheet cascade');
const files = new Map(await Promise.all(css.map(async path => [path, await readFile(new URL('internal/web' + path, root))])));
const html = fixtureHTML(css), violations = [], exceptions = [], failures = [];
const temporary = await mkdtemp(join(tmpdir(), 'snow-input-focus-'));
let chrome, client, sessionId, assertions = 0, scenarios = 0;
const server = createServer((request, response) => {
  const path = new URL(request.url, 'http://fixture').pathname;
  if (request.method === 'GET' && path === '/fixture.html') {
    response.writeHead(200, {'Content-Type': 'text/html', 'Cache-Control': 'no-store'}); response.end(html); return;
  }
  if (request.method === 'GET' && files.has(path)) {
    response.writeHead(200, {'Content-Type': 'text/css', 'Cache-Control': 'no-store'}); response.end(files.get(path)); return;
  }
  if (request.method === 'GET' && path === '/favicon.ico') { response.writeHead(204); response.end(); return; }
  violations.push(request.method + ' ' + request.url); response.writeHead(404); response.end('Unmocked transport forbidden');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const deadline = setTimeout(() => { console.error('Input focus suite exceeded 120s'); chrome?.kill('SIGKILL'); client?.close(); }, 120000);
try {
  chrome = spawn(chromeBinary(), ['--headless', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--disable-extensions', '--remote-debugging-port=0', `--user-data-dir=${temporary}`, 'about:blank'], {stdio: ['ignore', 'ignore', 'pipe']});
  client = await connect(await debuggingURL(chrome));
  const {targetId} = await client.send('Target.createTarget', {url: 'about:blank'});
  ({sessionId} = await client.send('Target.attachToTarget', {targetId, flatten: true}));
  const send = (method, params = {}) => client.send(method, params, sessionId);
  await send('Runtime.enable'); await send('Page.enable');
  client.onEvent(event => { if (event.sessionId === sessionId && event.method === 'Runtime.exceptionThrown') exceptions.push(event.params.exceptionDetails); });
  async function evaluate(expression) {
    const result = await send('Runtime.evaluate', {expression, awaitPromise: true, returnByValue: true});
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
    return result.result.value;
  }
  function check(actual, expected, message) {
    assertions++;
    try { assert.deepEqual(actual, expected, message); } catch (error) { failures.push({message, actual, expected}); }
  }
  await send('Page.navigate', {url: `http://127.0.0.1:${server.address().port}/fixture.html`});
  for (let attempt = 0; attempt < 100; attempt++) {
    if (await evaluate(`document.readyState === 'complete' && !!document.querySelector('#fixture')`)) break;
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  check(await evaluate('Array.from(document.styleSheets, sheet => new URL(sheet.href).pathname)'), css, 'Stylesheets loaded in exact production head order');
  await runFocusTests({cases, evaluate, send, check, scenario: () => scenarios++});
  check(exceptions, [], 'No browser runtime exceptions'); check(violations, [], 'No unexpected HTTP transport');
  console.log(`${assertions} native input-focus assertions across ${scenarios} scenarios; ${failures.length} failures (${css.length} production stylesheets)`);
  if (failures.length) { console.error(JSON.stringify(failures.slice(0, 30), null, 2)); process.exitCode = 1; }
} finally {
  clearTimeout(deadline); client?.close();
  if (chrome && chrome.exitCode === null) { const stopped = new Promise(resolve => chrome.once('exit', resolve)); chrome.kill('SIGTERM'); await stopped; }
  server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  await rm(temporary, {recursive: true, force: true, maxRetries: 5, retryDelay: 100});
}
