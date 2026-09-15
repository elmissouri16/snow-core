// Native Chromium + Node built-ins only. No fake React, DOM shim or browser test framework.
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdtemp, readdir, readFile, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {dirname, join, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';
import {chromeBinary, connect, debuggingURL} from '../live-stream/cdp.mjs';
import {transport} from './fixture.mjs';
import {activityChecks} from './activity.mjs';
import {organizationChecks} from './organization.mjs';
import {lifecycleChecks} from './lifecycle.mjs';
import {settingsTransport, settingsChecks} from './settings.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..');
const temporary = await mkdtemp(join(tmpdir(), 'snow-react-pages-'));
const evidence = process.env.SNOW_REACT_PAGES_EVIDENCE === '1' ? await mkdtemp(join(tmpdir(), 'snow-react-pages-evidence-')) : null;
let chrome, client, server, timer, failures = 0, assertions = 0, reports = 0;
try {
  const binary = chromeBinary(), directory = join(temporary, 'fixtures');
  await new Promise((resolve, reject) => {
    const exporter = spawn('go', ['test', './internal/web', '-run', '^TestExportHarnessVisualFixtures$', '-count=1'], {
      cwd: root, env: {...process.env, SNOW_WEB_FIXTURE_DIR: directory}, stdio: ['ignore', 'pipe', 'pipe'], timeout: 120000,
    });
    let output = '';
    for (const stream of [exporter.stdout, exporter.stderr]) stream.on('data', chunk => { output = (output + chunk).slice(-65536); });
    exporter.once('error', reject);
    exporter.once('exit', code => code === 0 ? resolve() : reject(Error(`Fixture export failed: ${output}`)));
  });
  const shell = await readFile(join(directory, 'home.html'), 'utf8'), files = new Map();
  const scripts = [...shell.matchAll(/<script\b[^>]*\bsrc="([^"]+)"[^>]*>/g)].map(match => match[1]);
  if (!shell.includes('<script type="module" src="/static/generated/app.js">')) throw Error('Build/integrate the real production React bundle first');
  if (scripts.some(path => /\/(manager-activity|organization|host-settings|host-api-key|browser-access)\.js$/.test(path))) throw Error('Retired React-page legacy scripts must not be loaded');
  let bytes = 0;
  async function load(relative) {
    for (const entry of await readdir(join(directory, relative), {withFileTypes: true})) {
      const name = relative + '/' + entry.name;
      if (entry.isDirectory()) await load(name);
      else if (entry.isFile()) {
        const body = await readFile(join(directory, name)); bytes += body.length;
        if (bytes > 24 * 1024 * 1024) throw Error('Fixture asset bound exceeded');
        files.set('/' + name, body);
      }
    }
  }
  await load('static');
  if (!files.has('/static/generated/app.js')) throw Error('Missing embedded production bundle');
  const bundle = files.get('/static/generated/app.js');
  console.log(`Production React module: ${bundle.length} bytes; sha256 ${createHash('sha256').update(bundle).digest('hex')}`);
  const mock = transport(files, shell); settingsTransport(mock.state); server = mock.server;
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  chrome = spawn(binary, ['--headless', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    '--disable-background-networking', '--disable-component-update', '--disable-sync', '--disable-extensions',
    '--remote-debugging-port=0', `--user-data-dir=${join(temporary, 'profile')}`, 'about:blank'], {stdio: ['ignore', 'ignore', 'pipe']});
  await Promise.race([new Promise((_, reject) => { timer = setTimeout(() => reject(Error('React page checks exceeded 300s')), 300000); }), (async () => {
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send('Target.createTarget', {url: 'about:blank'});
    const {sessionId} = await client.send('Target.attachToTarget', {targetId, flatten: true});
    const send = (method, params) => client.send(method, params, sessionId);
    const exceptions = [], external = [];
    client.onEvent(event => {
      if (event.sessionId !== sessionId) return;
      if (event.method === 'Runtime.exceptionThrown') exceptions.push(event.params.exceptionDetails.text + ': ' + (event.params.exceptionDetails.exception?.description || ''));
      if (event.method === 'Network.requestWillBeSent' && !event.params.request.url.startsWith(origin + '/') && !event.params.request.url.startsWith('about:')) external.push(event.params.request.url);
    });
    await send('Page.enable'); await send('Runtime.enable'); await send('Network.enable');
    await send('Emulation.setDeviceMetricsOverride', {width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false});
    const evaluate = async expression => {
      const result = await send('Runtime.evaluate', {expression, returnByValue: true, awaitPromise: true});
      if (result.exceptionDetails) throw Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
      return result.result.value;
    };
    const wait = async (predicate, label = predicate, timeout = 7000) => {
      const end = Date.now() + timeout;
      while (Date.now() < end) { if (await evaluate(predicate)) return; await delay(25); }
      throw Error(`Timed out: ${label}`);
    };
    const until = async (predicate, label, timeout = 7000) => {
      const end = Date.now() + timeout;
      while (Date.now() < end) { if (predicate()) return; await delay(25); }
      throw Error(`Timed out: ${label}`);
    };
    const assert = (value, label) => { assertions++; if (!value) throw Error(label); };
    const page = async (view = 'activity', options = {}) => {
      await send('Page.navigate', {url: 'about:blank'}); await delay(75);
      mock.state.reset(); if (options.setup) options.setup(mock.state);
      await send('Page.navigate', {url: origin + '/?view=' + view + (options.query || '')});
      const rootView = view === 'host-settings' || view === 'browser-access' ? 'shell' : view;
      await wait(`document.querySelector('[data-react-page="${rootView}"]')?.dataset.reactMounted === 'true'`, `React committed ${view}`);
      await wait('document.querySelector("[data-react-page=shell]")?.dataset.reactMounted === "true" && !!document.querySelector("[data-settings-open]")', 'Production React shell committed');
      await wait('!!window.htmx', 'Production HTMX initialized');
    };
    const click = async selector => {
      const rect = await evaluate(`(() => {const e=document.querySelector(${JSON.stringify(selector)}); if(!e) throw Error('Missing click target'); e.scrollIntoView({block:'center'}); const r=e.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2};})()`);
      await send('Input.dispatchMouseEvent', {type: 'mousePressed', button: 'left', clickCount: 1, ...rect});
      await send('Input.dispatchMouseEvent', {type: 'mouseReleased', button: 'left', clickCount: 1, ...rect});
    };
    const type = async (selector, text) => {
      await evaluate(`document.querySelector(${JSON.stringify(selector)}).focus();document.querySelector(${JSON.stringify(selector)}).select()`);
      await send('Input.insertText', {text});
    };
    const report = async (label, check) => {
      reports++; const before = assertions;
      try { await check(); assert(mock.state.errors.length === 0, `Fixture rejected: ${mock.state.errors.join('; ')}`); console.log(`PASS ${label} (${assertions - before} assertions)`); }
      catch (error) {
        failures++; console.error(`FAIL ${label}: ${error.stack}`);
        if (evidence) { const shot = await send('Page.captureScreenshot', {format: 'png'}); await writeFile(join(evidence, `failure-${reports}.png`), Buffer.from(shot.data, 'base64')); }
      }
    };
    const context = {send, evaluate, wait, until, assert, page, click, type, report, state: mock.state, delay, evidence, origin};
    await activityChecks(context);
    await organizationChecks(context);
    await lifecycleChecks(context);
    await settingsChecks(context);
    await report('Production assets and isolated transport', async () => {
      assert(mock.state.allErrors.length === 0, `Unexpected fixture requests across all scenarios: ${mock.state.allErrors.join('; ')}`);
      assert(exceptions.length === 0, `Uncaught browser errors: ${exceptions.join('\n')}`);
      assert(external.length === 0, `Unexpected external requests: ${external.join(', ')}`);
      assert(scripts.includes('/static/generated/app.js') && scripts.includes('/static/vendor/htmx-2.0.10.min.js'), 'Shipped React module and HTMX present');
      assert((shell.match(/<link rel="stylesheet"/g) || []).length === 26, 'Exact 26 production stylesheets retained in exported head order');
    });
    console.log(`${assertions} assertions executed; ${failures} failures across ${reports} React page scenarios.`);
    if (evidence) console.log(`Evidence: ${evidence}`);
    if (failures) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer);
  if (client) { try { await Promise.race([client.send('Browser.close'), delay(1000)]); } catch { /* already closed */ } client.close(); }
  if (chrome && chrome.exitCode === null && chrome.signalCode === null) {
    const exited = new Promise(resolve => chrome.once('exit', resolve)); await Promise.race([exited, delay(1000)]);
    if (chrome.exitCode === null && chrome.signalCode === null) { chrome.kill('SIGKILL'); await exited; }
  }
  chrome?.stderr.destroy();
  if (server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
  await rm(temporary, {recursive: true, force: true});
}
