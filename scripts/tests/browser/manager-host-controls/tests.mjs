import {readFile, writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {page, publicRequest} from './page.mjs';
import {operations} from './operations.mjs';

export async function exercise({client, ready, width, theme, artifacts}) {
  const results = [], failures = [], requests = [], responses = [], sessions = new Set();
  const secrets = [ready.code];
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
    const {browserContextId: context} = await client.send('Target.createBrowserContext');
    const p = await newPage(context, ready.origin); await p.login(ready.code);
    check(await p.evaluate("location.protocol==='http:' && document.querySelector('#workspace')!==null"), 'Native form pairing succeeds over direct loopback HTTP without injected cookies');
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

    check(await p.evaluate("!document.querySelector('[data-host-api-key]')"), 'Removed browser API-key controls are absent from Settings');
    await p.closeSettings();
    await operations({p, ready, check, posts, capture});
    check(!unsafeNetwork && !pageErrors, 'Observed requests remain numeric-loopback and native flows produce no page exceptions');
    check(!await readFile(join(ready.directory, 'provider-count'), 'utf8').catch(() => ''), 'Host controls and project operations never call even the fictional model provider');
    const wire = await readFile(join(ready.directory, 'rpc-wire'), 'utf8').catch(() => '');
    check(secrets.every(secret => !wire.includes(secret)) && !wire.includes('FICTIONAL_GIT_OUTPUT_MUST_NEVER_BE_PUBLIC'), 'Recorded CONTROL output contains no pairing credential or Git output');
    check(await privacy(p), 'Final document, URL and persistent browser drafts contain no private fixture canary');
    return {results, failures};
  } catch (error) {
    error.results = results; error.failures = failures; throw error;
  } finally {
    dispose();
    if (artifacts) await writeFile(join(artifacts, `${width}-${theme}-report.json`), JSON.stringify({width, theme, results, failures, requests, responses, pageErrors}, null, 2), {mode: 0o600});
  }
}
