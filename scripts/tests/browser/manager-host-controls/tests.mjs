import {randomUUID} from 'node:crypto';
import {readFile, writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {page, publicRequest} from './page.mjs';
import {operations} from './operations.mjs';

export async function exercise({client, ready, width, theme, artifacts}) {
  const results = [], failures = [], requests = [], responses = [], sessions = new Set();
  const secrets = [ready.code, ready.httpCode, ...Array.from({length: 4}, () => 'FICTIONAL_HOST_KEY_' + randomUUID())];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const posts = suffix => requests.filter(r => r.method === 'POST' && r.path.endsWith(suffix));
  let unsafeNetwork = false, pageErrors = 0;
  const dispose = client.onEvent(event => {
    if (!sessions.has(event.sessionId)) return;
    if (event.method === 'Network.requestWillBeSent') {
      const request = event.params.request;
      if (!/^https?:/.test(request.url)) return;
      const safe = publicRequest(request, secrets);
      if (!safe.local || safe.secretInURL) unsafeNetwork = true;
      if (requests.length < 2000) requests.push(safe);
    }
    if (event.method === 'Network.responseReceived') {
      const u = new URL(event.params.response.url);
      if (/^https?:$/.test(u.protocol) && responses.length < 2000) responses.push({path: publicRequest({method: '', url: u.href}, secrets).path, status: event.params.response.status});
    }
    if (event.method === 'Runtime.exceptionThrown') pageErrors++;
  });
  const newPage = async (context, origin) => { const p = await page(client, context, origin, width, theme); sessions.add(p.sessionId); await p.send('Runtime.enable'); return p; };
  const privacy = p => p.evaluate(`(() => {const keys=${JSON.stringify(secrets)};const values=[location.href,document.body.innerText,document.documentElement.outerHTML,...Object.values(localStorage),...Object.values(sessionStorage)];return keys.every(key=>values.every(value=>!value.includes(key)));})()`);
  const capture = async (label, p) => {
    if (!artifacts) return;
    if (!await privacy(p) || !await p.evaluate(`![...document.querySelectorAll('input[type=password]')].some(e=>e.value)`)) throw Error('Refusing screenshot with sensitive field or page state');
    const data = await p.send('Page.captureScreenshot', {format: 'png'});
    await writeFile(join(artifacts, `${width}-${theme}-${label}.png`), Buffer.from(data.data, 'base64'), {mode: 0o600});
  };
  try {
    const {browserContextId: insecureContext} = await client.send('Target.createBrowserContext');
    const h = await newPage(insecureContext, ready.httpOrigin);
    await h.login(ready.httpCode); await h.settings();
    check(await h.evaluate(`${h.q('[data-host-api-key]')}.dataset.enabled==='false' && ${h.q('[data-api-key-form]')}.hidden && ${h.q('[data-api-key-secret]')}.disabled && ${h.q('[data-api-key-save]')}.disabled && ${h.q('[data-api-key-inspect]')}.disabled`), 'Direct HTTP visibly disables API-key inspection, input and submission');
    check(!requests.some(r => r.path.endsWith('/api-key')), 'Opening HTTP Settings makes no API-key request');
    await capture('http-disabled', h);
    await client.send('Target.disposeBrowserContext', {browserContextId: insecureContext});

    const {browserContextId: context} = await client.send('Target.createBrowserContext');
    const p = await newPage(context, ready.origin); await p.login(ready.code);
    check(await p.evaluate("location.protocol==='https:' && document.querySelector('#workspace')!==null"), 'Native form pairing succeeds over real direct TLS without injected cookies');
    await p.navigate(`/?view=projects&project=${ready.project}`);
    await p.wait(`!!${p.q('[data-runtime-open]')}`, 'explicit activation review');
    await p.click('[data-runtime-open] input[name="confirm"]'); await p.click('[data-runtime-open] button[type="submit"]');
    await p.wait(`${p.q('#live-status')}?.textContent==='Ready' && ${p.q('#live-connection')}?.textContent==='Live'`, 'existing fictional worker', 25000).catch(async error => { error.observation = {runtimeExit: await readFile(join(ready.directory, 'host-runtime-exit'), 'utf8').catch(()=>'absent'), page: await p.evaluate(`({status:${p.q('#live-status')}?.textContent,connection:${p.q('#live-connection')}?.textContent,activationError:${p.q('[data-action-error]')}?.textContent,checked:${p.q('[data-runtime-open] input[name=confirm]')}?.checked,disabled:${p.q('[data-runtime-open] button[type=submit]')}?.disabled})`)}; throw error; });
    const initial = (await p.get(`/projects/${ready.project}/runtime`)).data;
    check(initial?.provider === 'fake' && initial?.model === 'fake-1', 'One explicitly activated fake worker supplies the unchanged-current-worker comparison');
    const beforeHostReads = requests.filter(r => r.path === '/settings/host').length;
    await p.settings();
    check(await p.evaluate(`${p.q('[data-host-form]')}.hidden`) && requests.filter(r => r.path === '/settings/host').length === beforeHostReads, 'Opening General neither loads host defaults nor dispatches a CONTROL read');
    check(await p.evaluate(`${p.q('#settings-dialog')}.contains(document.activeElement) && document.documentElement.scrollWidth<=innerWidth+1 && ${p.q('#settings-dialog')}.scrollWidth<=${p.q('#settings-dialog')}.clientWidth+1`), 'Settings focus stays in the modal and host controls fit its viewport');
    const row = '[data-host-fields] .host-default-row:nth-child(2)';
    const operation = `${row} label:nth-of-type(1) select`, value = `${row} label:nth-of-type(2) select`;
    const load = async () => { await p.click('[data-host-load]'); await p.wait(`${p.q('[data-host-status]')}.textContent.startsWith('Loaded.')`, 'explicit host defaults load').catch(async error => { const wire = await readFile(join(ready.directory,'rpc-wire'),'utf8').catch(()=>''); error.observation={status:await p.evaluate(`${p.q('[data-host-status]')}.textContent`), responses:responses.filter(r=>r.path==='/settings/host'), frames:wire.trim().split('\n').filter(Boolean).map(line=>{try{const f=JSON.parse(line);return {type:f.type,command:f.command,success:f.success,error_code:f.error_code};}catch{return {invalid:true};}})};throw error;}); };
    const save = async (op, setting) => {
      await p.select(operation, op); if (op === 'set') await p.select(value, setting);
      await p.click('[data-host-save]'); await p.wait(`${p.q('[data-host-status]')}.textContent.startsWith('Saved for future workers')`, 'explicit host defaults save');
    };
    await load(); await save('set', 'high');
    check(await p.evaluate(`${p.q(row+' .fine')}.textContent.includes('Explicit: high')`), 'Global thinking default changes only after explicit load/set/save');
    await save('reset');
    check(await p.evaluate(`${p.q(row+' .fine')}.textContent.includes('Explicit: Inherited')`), 'Global reset removes the explicit override rather than inventing a value');
    await p.select('[data-host-scope]', 'project');
    check(await p.evaluate(`${p.q('[data-host-form]')}.hidden`), 'Changing scope retires the loaded global revision');
    await p.select('[data-host-project]', ready.project); await load(); await save('set', 'medium'); await save('reset');
    check(await p.evaluate(`${p.q(row+' .fine')}.textContent.includes('Explicit: Inherited')`), 'Project defaults require their own load/set/reset and preserve inheritance');
    await p.click('[data-host-providers-load]'); await p.wait(`${p.q('[data-host-providers-status]')}.textContent.startsWith('Checked locally.')`, 'local provider status');
    check(await p.evaluate(`${p.q('[data-host-providers-list]')}.textContent.includes('not network verified')`), 'Provider presence is explicitly local and never presented as network verification');
    const after = (await p.get(`/projects/${ready.project}/runtime`)).data;
    check(after?.instance_id === initial?.instance_id && after?.model === initial?.model && after?.thinking === initial?.thinking, 'Host global/project writes leave the existing worker identity, model and thinking unchanged');
    await capture('defaults', p);

    const provider = 'opencode-go';
    const keyPath = `/settings/providers/${provider}/api-key`;
    const inspect = async tab => {
      await tab.replace('[data-api-key-provider]', provider); await tab.click('[data-api-key-inspect]');
      await tab.wait(`${tab.q('[data-api-key-status]')}.textContent.startsWith('Inspection complete.')`, 'explicit exact-provider key inspection');
    };
    const submit = async (tab, key, replacement) => {
      await tab.replace('[data-api-key-secret]', key);
      if (replacement) await tab.click('[data-api-key-replace]');
      await tab.click('[data-api-key-confirm]'); await tab.click('[data-api-key-save]');
    };
    await inspect(p);
    check(await p.evaluate(`${p.q('[data-api-key-secret]')}.type==='password' && ${p.q('[data-api-key-target]')}.textContent.startsWith(${JSON.stringify(provider)})`), 'Fresh exact-provider inspection enables a masked write-only password field');
    const beforeConsent = posts(keyPath).length;
    await p.replace('[data-api-key-secret]', secrets[2]); await p.click('[data-api-key-save]');
    await p.wait(`${p.q('[data-api-key-secret]')}.value===''`, 'missing-consent secret clearing');
    check(posts(keyPath).length === beforeConsent, 'Missing explicit host consent clears the key without dispatching a write');
    await submit(p, secrets[2], false); await p.wait(`${p.q('[data-api-key-status]')}.textContent.startsWith('Key saved locally')`, 'write-only key success');
    check(await p.evaluate(`${p.q('[data-api-key-secret]')}.value==='' && ${p.q('[data-api-key-form]')}.hidden && ${p.q('[data-api-key-save]')}.disabled`) && await privacy(p), 'Successful save clears and retires the key, with no DOM/URL/browser-draft echo');
    await inspect(p);
    check(await p.evaluate(`!${p.q('[data-api-key-replace-label]')}.hidden && !${p.q('[data-api-key-replace]')}.checked`), 'Existing credential requires separate explicit replacement consent');
    // A second native tab consumes the browser's inspection. The original tab
    // then submits its stale inspection through the real UI and receives 409.
    const second = await newPage(context, ready.origin); await second.navigate('/'); await second.wait(`!!${second.q('#workspace')}`, 'second tab shares native pairing'); await second.settings();
    await inspect(second); await submit(second, secrets[3], true); await second.wait(`${second.q('[data-api-key-status]')}.textContent.startsWith('Key saved locally')`, 'second-tab explicit replacement');
    await submit(p, secrets[4], true);
    await p.wait(`${p.q('[data-api-key-status]')}.textContent.startsWith('Save outcome is unknown or was rejected')`, 'consumed inspection rejected');
    check(responses.some(r => r.path === keyPath && r.status === 409), 'Reused inspection is refused by the production HTTPS handler');
    check(await p.evaluate(`${p.q('[data-api-key-secret]')}.value==='' && ${p.q('[data-api-key-save]')}.disabled`) && await privacy(p), 'Failed write clears the secret, disables retry and retains no draft or echoed key');
    const writes = posts(keyPath).length;
    await p.closeSettings(); await p.settings(); await delay(120);
    check(posts(keyPath).length === writes && await p.evaluate(`${p.q('[data-api-key-secret]')}.value===''`), 'Closing/reopening Settings never retries an uncertain key submission');
    await capture('api-key-cleared', p); await p.closeSettings();
    await operations({p, ready, check, posts, capture});
    check(!unsafeNetwork && !pageErrors, 'Observed requests remain numeric-loopback and native flows produce no page exceptions');
    check(!await readFile(join(ready.directory, 'provider-count'), 'utf8').catch(() => ''), 'Host controls and project operations never call even the fictional model provider');
    const wire = await readFile(join(ready.directory, 'rpc-wire'), 'utf8').catch(() => '');
    check(secrets.every(secret => !wire.includes(secret)) && !wire.includes('FICTIONAL_GIT_OUTPUT_MUST_NEVER_BE_PUBLIC'), 'Recorded CONTROL output contains neither API-key canaries nor Git output');
    check(await privacy(p), 'Final document, URL and persistent browser drafts contain no private fixture canary');
    return {results, failures};
  } catch (error) {
    error.results = results; error.failures = failures; throw error;
  } finally {
    dispose();
    if (artifacts) await writeFile(join(artifacts, `${width}-${theme}-report.json`), JSON.stringify({width, theme, results, failures, requests, responses, pageErrors}, null, 2), {mode: 0o600});
  }
}
