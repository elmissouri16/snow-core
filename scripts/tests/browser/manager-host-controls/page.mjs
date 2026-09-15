// Native CDP input; evaluate is limited to observation, scrolling and read-only GETs.
import {setTimeout as delay} from 'node:timers/promises';
export async function page(client, context, origin, width, theme) {
  const {targetId} = await client.send('Target.createTarget', {url: 'about:blank', browserContextId: context});
  const {sessionId} = await client.send('Target.attachToTarget', {targetId, flatten: true});
  const send = (method, params = {}) => client.send(method, params, sessionId);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const evaluate = async expression => {
    const result = await send('Runtime.evaluate', {expression, awaitPromise: true, returnByValue: true});
    if (result.exceptionDetails) throw Error('Native browser observation failed');
    return result.result?.value;
  };
  const wait = async (expression, label, timeout = 15000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { if (await evaluate(expression)) return; await delay(40); }
    throw Error('Timed out: ' + label);
  };
  const key = async (key, code, windowsVirtualKeyCode, modifiers = 0) => {
    // Let Chromium translate the CDP key; supplying macOS native keycodes as
    // well causes a second native Escape cancellation after a dialog reopens.
    const params = {key, code, windowsVirtualKeyCode, modifiers, ...(key === 'a' && modifiers ? {commands: ['selectAll']} : {})};
    await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', ...params});
    await send('Input.dispatchKeyEvent', {type: 'keyUp', ...params});
  };
  const click = async selector => {
    await send('Page.bringToFront');
    const point = await evaluate(`(() => {const el=${q(selector)};if(!el||el.disabled)return null;el.scrollIntoView({block:'center',inline:'nearest'});for(const r of el.getClientRects()){const x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;if(r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y)))return {x,y};}return null;})()`);
    if (!point) throw Error('Native target missing, disabled or obscured: ' + selector);
    await send('Input.dispatchMouseEvent', {type: 'mouseMoved', ...point});
    await send('Input.dispatchMouseEvent', {type: 'mousePressed', ...point, button: 'left', clickCount: 1});
    await send('Input.dispatchMouseEvent', {type: 'mouseReleased', ...point, button: 'left', clickCount: 1});
    await delay(25);
  };
  const replace = async (selector, text) => { await click(selector); await key('a', 'KeyA', 65, process.platform === 'darwin' ? 4 : 2); await send('Input.insertText', {text}); };
  const select = async (selector, value) => {
    const index = await evaluate(`[...${q(selector)}.options].findIndex(o=>o.value===${JSON.stringify(value)})`);
    if (index < 0) throw Error('Native selection unavailable');
    const label = await evaluate(`${q(selector)}.options[${index}].textContent`);
    await click(selector);
    // Native select type-ahead, not a DOM value assignment or synthetic change.
    for (const character of label) {
      await send('Input.dispatchKeyEvent', {type:'char', text:character});
    }
    await key('Tab', 'Tab', 9);
    await wait(`${q(selector)}.value===${JSON.stringify(value)}`, 'native selection').catch(async error => {error.observation={selector,expected:value,current:await evaluate(`${q(selector)}?.value`),focused:await evaluate(`document.activeElement===${q(selector)}`)};throw error;});
  };
  const navigate = async path => { await send('Page.navigate', {url: origin + path}); await wait('document.readyState==="complete"', 'native navigation'); };
  const login = async code => {
    await navigate('/login'); await wait(`!!${q('#code')}`, 'native pairing form');
    await replace('#code', code); await click('form[action="/login"] button[type="submit"]');
    await wait(`!!${q('#workspace')} && location.pathname==='/'`, 'native pairing');
  };
  const settings = async () => {
    if (width < 768 && !await evaluate(`!!${q('#project-navigation.nav-open')}`)) await click('[data-nav-toggle]');
    await click('[data-settings-open]'); await wait(`${q('#settings-dialog')}?.open`, 'Settings opens');
    await click(`[data-theme-choice="${theme}"]`);
  };
  const closeSettings = async () => {
    await key('Escape', 'Escape', 27); await wait(`!${q('#settings-dialog')}?.open`, 'Escape closes Settings');
    if (await evaluate(`!!${q('#project-navigation.nav-open')}`)) await click('#project-navigation [data-nav-close]');
    await wait(`!${q('#workspace-content')}?.inert`, 'workspace input released');
  };
  const get = path => evaluate(`fetch(${JSON.stringify(path)},{cache:'no-store'}).then(async r=>({status:r.status,data:r.ok?await r.json():null}))`);
  await send('Network.enable'); await send('Page.enable'); await send('Network.setCacheDisabled', {cacheDisabled: true});
  await send('Emulation.setDeviceMetricsOverride', {width, height: 740, deviceScaleFactor: 1, mobile: false});
  await send('Emulation.setEmulatedMedia', {features: [{name: 'prefers-color-scheme', value: theme}, {name: 'prefers-reduced-motion', value: 'reduce'}]});
  return {sessionId, send, q, evaluate, wait, key, click, replace, select, navigate, login, settings, closeSettings, get};
}

export function publicRequest(request, secrets) {
  const u = new URL(request.url);
  // Deliberately never retain headers, credentials, values or complete URLs.
  const redact = value => secrets.reduce((text, secret) => text.replaceAll(secret, '[redacted]').replaceAll(encodeURIComponent(secret), '[redacted]'), value);
  return {method: request.method, path: redact(u.pathname), local: u.hostname === '127.0.0.1',
    secretInURL: secrets.some(s => request.url.includes(s) || request.url.includes(encodeURIComponent(s))),
    fields: request.postData ? [...new URLSearchParams(request.postData).keys()].slice(0, 64).map(redact).sort() : []};
}
