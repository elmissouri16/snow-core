// Production HTTP assets and native browser input only; snapshot GETs observe.
import {writeFile, readFile} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {controlsChecks} from './controls.mjs';

export async function exercise({client, sessionId, ready, directory, width, theme, artifacts, secrets = []}) {
  const results = [], failures = [], requests = [], responses = [], errors = [], receipts = [];
  let fatal = null, screenshots = 0, loseSteerResponse = false;
  const send = (method, params = {}) => client.send(method, params, sessionId);
  const finishedResponses = new Set();
  const responseBody = async requestId => {
    const end = Date.now() + 5000;
    while (!finishedResponses.has(requestId) && Date.now() < end) await delay(20);
    if (!finishedResponses.has(requestId)) throw Error('HTTP receipt body did not finish loading');
    return send('Network.getResponseBody', {requestId});
  };
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const evaluate = async expression => {
    const result = await send('Runtime.evaluate', {expression, awaitPromise: true, returnByValue: true});
    if (result.exceptionDetails) throw Error('DOM observation failed');
    return result.result?.value;
  };
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); if (process.env.SNOW_RUNTIME_CONTROLS_TRACE === '1') console.log((ok ? 'PASS ' : 'FAIL ') + label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const wait = async (expression, label, timeout = 15000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { if (await evaluate(expression)) return; await delay(35); }
    throw Error('Timed out: ' + label);
  };
  const key = async (key, code, windowsVirtualKeyCode, modifiers = 0) => {
    const params = {key, code, windowsVirtualKeyCode, modifiers, ...(key === 'a' && modifiers ? {commands: ['selectAll']} : {})};
    await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', ...params});
    await send('Input.dispatchKeyEvent', {type: 'keyUp', ...params});
  };
  const click = async selector => {
    const point = await evaluate(`(() => {const el=${q(selector)};if(!el||el.disabled)return null;el.scrollIntoView({block:'center',inline:'nearest'});for(const r of el.getClientRects()){const x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;if(r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y)))return {x,y};}return null;})()`);
    if (!point) {
      const geometry = await evaluate(`(() => {const el=${q(selector)}, r=el?.getBoundingClientRect();return {target:r?.toJSON(),disabled:el?.disabled,header:document.querySelector('.live-header')?.getBoundingClientRect().toJSON(),controls:document.querySelector('.live-controls')?.getBoundingClientRect().toJSON(),hit:r?document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.outerHTML.slice(0,500):null,parents:r?Array.from((function*(n){while(n){yield n.id||n.tagName+':'+n.getAttribute('class');n=n.parentElement}})(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2))):[]};})()`);
      throw Error('Native target missing, disabled or obscured: ' + selector + ' ' + JSON.stringify(geometry));
    }
    await send('Input.dispatchMouseEvent', {type: 'mouseMoved', ...point});
    await send('Input.dispatchMouseEvent', {type: 'mousePressed', ...point, button: 'left', clickCount: 1});
    await send('Input.dispatchMouseEvent', {type: 'mouseReleased', ...point, button: 'left', clickCount: 1});
    await delay(25);
  };
  const replace = async (selector, text) => { await click(selector); await key('a', 'KeyA', 65, process.platform === 'darwin' ? 4 : 2); await send('Input.insertText', {text}); };
  const select = async (selector, value) => {
    const index = await evaluate(`[...${q(selector)}.options].findIndex(option=>option.value===${JSON.stringify(value)})`);
    if (index < 0) throw Error('Native select option unavailable: ' + selector);
    for (let step=0; step<24 && !await evaluate(`document.activeElement===${q(selector)}`); step++) await key('Tab','Tab',9);
    if (!await evaluate(`document.activeElement===${q(selector)}`)) throw Error('Native select keyboard focus unavailable');
    const label=await evaluate(`${q(selector)}.options[${index}].textContent`);
    for (const character of label) await send('Input.dispatchKeyEvent',{type:'char',text:character,unmodifiedText:character});
    await key('Tab','Tab',9);
    await wait(`${q(selector)}.value===${JSON.stringify(value)}`, 'native select change '+selector+' to '+value+' (observed '+await evaluate(`${q(selector)}.value`)+')');
  };
  const snapshot = () => evaluate(`fetch(${JSON.stringify(`/projects/${ready.project}/runtime`)},{cache:'no-store'}).then(r=>{if(!r.ok)throw Error('Snapshot unavailable');return r.json()})`);
  const waitSnapshot = async (predicate, label, timeout = 15000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { const s = await snapshot(); if (predicate(s)) return s; await delay(35); }
    throw Error('Timed out: ' + label);
  };
  const count = async () => Number(await readFile(join(directory, 'provider-count'), 'utf8').catch(() => '0'));
  const waitCount = async n => { const end = Date.now() + 15000; while (Date.now() < end) { if (await count() === n) return; await delay(30); } throw Error('Provider count did not reach expected entry'); };
  const waitFile = async name => { const end=Date.now()+5000; while(Date.now()<end) { if (await readFile(join(directory,name)).then(()=>true,()=>false)) return; await delay(20); } throw Error('Private fixture gate not reached'); };
  const file = (name, text = 'release') => writeFile(join(directory, name), text, {mode: 0o600});
  const release = async (n, text = 'complete') => { await file(`response-${n}`, text); await file(`release-${n}`); };
  const posts = action => requests.filter(request => request.method === 'POST' && request.path.endsWith('/' + action));
  const privacy = text => !/PRIVATE-RUNTIME|PRIVATE-VERSION|Bearer\s|sk-[A-Za-z0-9]{12}/.test(text) && secrets.every(secret => !secret || !text.includes(secret));
  const capture = async name => {
    if (!artifacts || screenshots >= 4) return;
    const visible = await evaluate('document.body.innerText');
    if (!privacy(visible)) throw Error('Privacy scan rejected screenshot');
    const image = await send('Page.captureScreenshot', {format: 'png', captureBeyondViewport: false});
    await writeFile(join(artifacts, `${width}-${theme}-${name}.png`), Buffer.from(image.data, 'base64'), {mode: 0o600}); screenshots++;
  };
  const idle = () => wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live' && !${q('#live-send')}?.disabled`, 'idle current worker', 25000);
  const mode = async value => { await click('[data-mode-menu]'); await click(`[data-menu-key="${value}"]`); await waitSnapshot(s => s.mode === value && s.status === 'idle', 'authoritative mode'); await idle(); };
  const armSteerLoss = async () => {
    loseSteerResponse = true;
    await send('Fetch.enable',{patterns:[{urlPattern:ready.origin+'/projects/'+ready.project+'/runtime/steer',requestStage:'Response'}]});
  };
  const dispose = client.onEvent(event => {
    if (event.sessionId !== sessionId) return;
    if (event.method === 'Fetch.requestPaused') {
      const requestId=event.params.requestId;
      if(loseSteerResponse) { loseSteerResponse=false; send('Fetch.failRequest',{requestId,errorReason:'Failed'}).catch(()=>errors.push('transport fault injection failed')); }
      else send('Fetch.continueRequest',{requestId}).catch(()=>errors.push('transport continuation failed'));
    }
    if (event.method === 'Network.requestWillBeSent') {
      const request = event.params.request;
      if (request.url.startsWith(ready.origin + '/')) {
        if (requests.length >= 2000) { if (!errors.includes('request bound')) errors.push('request bound'); return; }
        const fields = new URLSearchParams(request.postData || '');
        requests.push({method: request.method, path: new URL(request.url).pathname, keys: [...fields.keys()].filter(key => key !== 'csrf'), scope: fields.get('scope'), field: fields.get('field')});
      } else if (/^https?:/.test(request.url)) errors.push('unexpected external HTTP');
    }
    if (event.method === 'Network.loadingFinished' && finishedResponses.size < 2000) finishedResponses.add(event.params.requestId);
    if (event.method === 'Network.responseReceived' && event.params.response.url.startsWith(ready.origin + '/')) {
      const response = event.params.response, path = new URL(response.url).pathname;
      if (responses.length < 2000) responses.push({path, status: response.status});
      if (/\/(steer|compaction-start|history-session-fork|reasoning-set)$/.test(path) && response.status === 200) receipts.push({action: path.split('/').at(-1), requestId: event.params.requestId});
    }
  });
  try {
    await send('Page.addScriptToEvaluateOnNewDocument', {source: `localStorage.setItem('snow-manager-theme',${JSON.stringify(theme)});window.runtimeControlsErrors=[];addEventListener('error',()=>runtimeControlsErrors.push('script error'));addEventListener('unhandledrejection',()=>runtimeControlsErrors.push('unhandled rejection'));`});
    await send('Page.navigate', {url: `${ready.origin}/?view=projects&project=${ready.project}&session=${ready.session}`});
    await wait('document.readyState==="complete" && window.SnowReactReady===true', 'production page and React facade handshake loaded');
    await wait(`!!${q('[data-runtime-open]')}`, 'native resume form');
    check(await count() === 0, 'Passive saved history browsing does not invoke provider');
    await click('[data-runtime-open] input[name="confirm"]');
    await click('[data-runtime-open] button[type=submit]');
    await idle();
    const initial = await snapshot();
    check(initial.session_id === ready.session && initial.provider === 'fake' && initial.model === 'fake-1' && initial.permission_mode === 'ask', 'Native Resume uses private fake defaults and saved Ask policy');
    check(initial.reasoning_enabled && initial.history_control_enabled && initial.compaction_enabled && initial.steer != null, 'Actual RPC capabilities enable all four production panels');
    const assetPaths = await evaluate(`[...document.querySelectorAll('script[src],link[rel="stylesheet"][href]')].map(el=>new URL(el.src||el.href).pathname)`);
    check(assetPaths.length > 0 && assetPaths.every(path => responses.some(response => response.path === path && response.status === 200)), 'Every referenced production asset loads with HTTP 200');
    await assert(`document.querySelectorAll('script[type="module"][src="/static/generated/app.js"]').length===1`, 'Actual production head loads exactly one React module');
    check(responses.some(r=>r.path==='/static/generated/app.js' && r.status===200), 'React bundle is served successfully by the real manager');
    check(['reasoning','compaction','goals','versions','history-controls'].every(name=>!assetPaths.includes('/static/'+name+'.js') && !requests.some(r=>r.path==='/static/'+name+'.js')), 'Actual head and network retire all five replaced classic scripts');
    await assert(`['reasoning','goals','versions'].every(name=>document.querySelector('[data-react-live-panel="'+name+'"] dialog')) && !!document.querySelector('[data-react-live-panel="compaction"] [data-compaction-status]') && !!document.querySelector('#live-composer-normal [data-compaction-open]') && !document.querySelector('#compaction-dialog')`, 'Activated live root contains three React dialogs and direct composer compaction with inline status');
    await assert("['SnowReasoning','SnowHistoryControls','SnowCompaction','SnowSteer'].every(name=>typeof window[name]?.init==='function')", 'All actual panel controllers are initialized');
    await controlsChecks({send,responseBody,q,evaluate,check,assert,wait,waitSnapshot,key,click,replace,select,snapshot,count,waitCount,waitFile,file,release,posts,capture,idle,mode,directory,width,theme,ready,receipts,armSteerLoss});
    check(privacy(await evaluate('document.body.innerText')), 'Public DOM excludes private continuity, hidden reasoning and credential markers');
    await assert('runtimeControlsErrors.length===0', 'Production assets report no JS error or unhandled rejection');
    check(errors.length === 0 && !posts('queue-enqueue').length, 'All observed HTTP stays local; steering never tunnels Queue next');
    return {results, failures};
  } catch (error) {
    fatal = error.message; error.results = results; error.failures = failures;
    error.observation = await evaluate(`({status:${q('#live-status')}?.textContent,connected:${q('#live-connection')}?.textContent,dialog:document.querySelector('dialog[open]')?.id,activationError:${q('[data-action-error]')}?.textContent,activationChecked:${q('[data-runtime-open] input[name=confirm]')}?.checked,activationSubmitDisabled:${q('[data-runtime-open] button[type=submit]')}?.disabled,scriptErrors:runtimeControlsErrors?.length})`).catch(() => null);
    const failureSnapshot = await snapshot().catch(()=>null); error.observation.providerCount=await count(); error.observation.permissionMode=failureSnapshot?.permission_mode; error.observation.compaction=failureSnapshot?.compaction; error.observation.compactNotice=await evaluate(`${q('[data-compaction-notice]')}?.textContent`); error.observation.compactPOSTs=posts('compaction-start').length; error.observation.activities=failureSnapshot?.activities?.map(item=>({tool:item.tool,status:item.status}));
    await capture('failure').catch(() => {}); throw error;
  } finally {
    dispose();
    const report = JSON.stringify({width,theme,results,failures,fatal,requests,responses,errors}, null, 2);
    if (artifacts && privacy(report)) await writeFile(join(artifacts, `${width}-${theme}-report.json`), report, {mode: 0o600});
  }
}
