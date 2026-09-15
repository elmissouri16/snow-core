// Real production HTTP assets, native input, private gated fake provider only.
// Snapshot GETs are observations; every application mutation uses native UI.
import {writeFile, readFile} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {goalChecks} from './goals.mjs';
import {processChecks} from './processes.mjs';

export async function exercise({client, sessionId, ready, directory, width, theme, artifacts}) {
  const results = [], failures = [], requests = [], responses = [], errors = [], receipts = [], observations = [];
  let fatal = null;
  const send = (method, params = {}) => client.send(method, params, sessionId);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const evaluate = async expression => {
    const value = await send('Runtime.evaluate', {expression, awaitPromise: true, returnByValue: true});
    if (value.exceptionDetails) throw Error('DOM observation failed: ' + value.exceptionDetails.text);
    return value.result?.value;
  };
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const wait = async (expression, label, timeout = 15000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { if (await evaluate(expression)) return; await delay(40); }
    throw Error('Timed out: ' + label);
  };
  const key = async (key, code, windowsVirtualKeyCode, modifiers = 0) => {
    const params = {key, code, windowsVirtualKeyCode, modifiers, ...(key === 'a' && modifiers ? {commands: ['selectAll']} : {})};
    await send('Input.dispatchKeyEvent', {type: 'keyDown', ...params});
    await send('Input.dispatchKeyEvent', {type: 'keyUp', ...params});
  };
  const click = async selector => {
    const point = await evaluate(`(() => {const el=${q(selector)};if(!el)return null;el.scrollIntoView({block:'center',inline:'nearest'});for(const r of el.getClientRects()){const x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;if(r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y)))return {x,y};}return null;})()`);
    if (!point) throw Error('Native pointer target missing or obscured: ' + selector);
    await send('Input.dispatchMouseEvent', {type: 'mouseMoved', ...point});
    await send('Input.dispatchMouseEvent', {type: 'mousePressed', ...point, button: 'left', clickCount: 1});
    await send('Input.dispatchMouseEvent', {type: 'mouseReleased', ...point, button: 'left', clickCount: 1});
    await delay(30);
  };
  const replace = async (selector, text) => { await click(selector); await key('a', 'KeyA', 65, process.platform === 'darwin' ? 4 : 2); await send('Input.insertText', {text}); };
  const snapshot = () => evaluate(`fetch(${JSON.stringify(`/projects/${ready.project}/runtime`)},{cache:'no-store'}).then(r=>{if(!r.ok)throw Error('Snapshot unavailable');return r.json()})`);
  const count = async () => Number(await readFile(join(directory, 'provider-count'), 'utf8').catch(() => '0'));
  const waitCount = async n => { const end = Date.now() + 15000; while (Date.now() < end) { if (await count() === n) return; await delay(35); } throw Error('Provider call count did not reach ' + n); };
  const release = async (n, text) => { await writeFile(join(directory, `response-${n}`), text, {mode: 0o600}); await writeFile(join(directory, `release-${n}`), 'release', {mode: 0o600}); };
  const posts = suffix => requests.filter(r => r.method === 'POST' && r.path.endsWith('/' + suffix));
  const capture = async name => {
    if (!artifacts) return;
    try { const image = await send('Page.captureScreenshot', {format: 'png', captureBeyondViewport: false}); await writeFile(join(artifacts, `${width}-${theme}-${name}.png`), Buffer.from(image.data, 'base64'), {mode: 0o600}); }
    catch { check(false, 'Chrome screenshot support for ' + name); }
  };
  const idle = () => wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live'`, 'idle live worker', 25000);
  const reload = async () => { await send('Page.reload', {ignoreCache: true}); await wait('document.readyState==="complete" && window.SnowReactReady===true', 'reload assets and React facade handshake'); await idle(); };
  const dispose = client.onEvent(event => {
    if (event.sessionId !== sessionId) return;
    if (event.method === 'Network.requestWillBeSent') {
      const r = event.params.request;
      if (r.url.startsWith(ready.origin + '/')) {
        const p = new URLSearchParams(r.postData || '');
        // Only synthetic text/public identity; never cookie, CSRF or tokens.
        if (requests.length < 2000) requests.push({method: r.method, path: new URL(r.url).pathname, keys: [...p.keys()].filter(k => k !== 'csrf'), objective: p.get('objective'), goal: p.get('expected_goal_id'), process: p.get('process_id'), text: p.get('text'), budget: p.get('token_budget')});
        else if (!errors.includes('request observation overflow')) errors.push('request observation overflow');
      } else if (/^https?:/.test(r.url)) errors.push('Unexpected external HTTP request');
    }
    if (event.method === 'Network.responseReceived') {
      const r = event.params.response, path = new URL(r.url).pathname;
      if (responses.length < 2000) responses.push({path, status: r.status});
      if (/\/goal-(start|resume)$/.test(path) && r.status === 200) receipts.push({requestId: event.params.requestId, action: path.split('/').at(-1)});
    }
    if (event.method === 'Page.javascriptDialogOpening') send('Page.handleJavaScriptDialog', {accept: true}).catch(() => {});
  });
  const h = {send, q, evaluate, check, assert, wait, key, click, replace, snapshot, count, waitCount, release, posts, capture, idle, reload, directory, ready, receipts, observations, width, theme};
  try {
    await send('Page.addScriptToEvaluateOnNewDocument', {source: `localStorage.setItem('snow-manager-theme',${JSON.stringify(theme)});window.managerExecutionErrors=[];addEventListener('error',e=>{if(managerExecutionErrors.length<30)managerExecutionErrors.push(e.message)});addEventListener('unhandledrejection',()=>{if(managerExecutionErrors.length<30)managerExecutionErrors.push('unhandled rejection')});`});
    await send('Page.navigate', {url: `${ready.origin}/?view=projects&project=${ready.project}`});
    await wait(`!!${q('[data-runtime-open]')}`, 'real activation page');
    await wait('document.readyState==="complete" && window.SnowReactReady===true', 'real assets and React facade handshake loaded');
    await assert("typeof SnowGoals?.init==='function' && typeof SnowProcesses?.init==='function'", 'Production HTTP-loaded controllers exist');
    check(await count() === 0, 'Browsing does not activate provider');
    // Fixture bootstrap only: the real activation form has no model override,
    // while this fixture rejects absent explicit fake/fake-1 worker arguments.
    // Use the production activation HTTP contract, never fabricate a snapshot,
    // alter app assets, or intercept a request. All tested actions below are UI.
    const opened = await evaluate(`(async()=>{const form=${q('[data-runtime-open]')};const body=new URLSearchParams(new FormData(form));body.set('confirm','activate');body.set('provider','fake');body.set('model','fake-1');const r=await fetch(form.action,{method:'POST',headers:{Accept:'application/json'},body});return r.status})()`);
    check(opened===200, 'Fixture bootstrap uses real production HTTP activation with explicit fake model');
    await reload();
    const initial = await snapshot();
    check(initial.provider === 'fake' && initial.model === 'fake-1' && initial.permission_mode === 'ask', 'Actual RPC worker uses fake provider and source-owned Ask policy');
    check(await count() === 0, 'Activation does not call provider');
    const assetPaths = await evaluate(`[...document.querySelectorAll('script[src],link[rel="stylesheet"][href]')].map(el=>new URL(el.src||el.href).pathname)`);
    for (const path of assetPaths) check(responses.some(r => r.path === path && r.status === 200), path + ': referenced production asset HTTP 200');
    await assert(`document.querySelectorAll('script[type="module"][src="/static/generated/app.js"]').length===1`, 'Actual production head loads exactly one React module');
    check(responses.some(r=>r.path==='/static/generated/app.js' && r.status===200), 'React bundle is served successfully by the real manager');
    check(['reasoning','compaction','goals','versions','history-controls'].every(name=>!assetPaths.includes('/static/'+name+'.js') && !requests.some(r=>r.path==='/static/'+name+'.js')), 'Actual head and network retire all five replaced classic scripts');
    await assert(`['reasoning','goals','versions'].every(name=>document.querySelector('[data-react-live-panel="'+name+'"] dialog')) && document.querySelector('#live-composer [data-compaction-open]') && document.querySelector('[data-react-live-panel="compaction"] [data-compaction-status]') && !document.querySelector('#compaction-dialog')`, 'Activated live root contains three React panel dialogs and direct composer compaction');
    await goalChecks(h);
    await processChecks(h);
    await assert('managerExecutionErrors.length===0', 'Production assets have no JavaScript error or unhandled rejection');
    check(errors.length === 0, 'Bounded network observation stays local');
    check(!requests.some(r => /\/queue-enqueue$/.test(r.path)), 'No queue execution tunneled into goal workflow');
    return {results, failures};
  } catch (error) {
    fatal = error.message;
    error.results = results; error.failures = failures;
    error.observation = await evaluate(`({status:${q('#live-status')}?.textContent,notice:${q('[data-goal-notice]')}?.textContent,goalState:${q('[data-goal-state]')}?.textContent,error:${q('#live-error')}?.textContent,processError:${q('[data-process-error]')}?.textContent,js:window.managerExecutionErrors})`).catch(() => null);
    await capture('failure'); throw error;
  } finally {
    dispose();
    if (artifacts) await writeFile(join(artifacts, `${width}-${theme}-report.json`), JSON.stringify({width,theme,results,failures,fatal,observations,requests,responses,errors}, null, 2), {mode: 0o600});
  }
}
