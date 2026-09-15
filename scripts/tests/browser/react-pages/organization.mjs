import {projectID, archivedID, missingID, malicious, organization, csrf} from './fixture.mjs';

export async function organizationChecks(t) {
  const {page, wait, until, evaluate, click, type, assert, report, state, delay} = t;
  const posts = () => state.requests.filter(request => request.method === 'POST');
  const session = id => `#organization-session-${projectID}-${id}`;
  const submit = async (selector, fields) => {
    const count = posts().length; await click(selector);
    await until(() => posts().length === count + 1, 'Native form POST recorded');
    await until(() => !!posts().at(-1).fields, 'Native form body read');
    assert(JSON.stringify(posts().at(-1).fields) === JSON.stringify(fields), `Exact form fields: ${JSON.stringify(posts().at(-1).fields)}`);
    await delay(50); // Native 204 deliberately preserves the current document.
  };
  await report('Organization React filtering retains stable inputs, drafts and session rows', async () => {
    await page('organization');
    assert(await evaluate('document.querySelectorAll("[data-organization-session]").length===3'), 'Real bounded catalog props render all sessions');
    await type(`input#organization-name-${projectID}`, 'Unsaved workspace label');
    await evaluate(`window.savedName=document.querySelector('#organization-name-${projectID}');window.savedFilter=document.querySelector('[data-organization-filter]');window.savedRow=document.querySelector('${session('session-active')}');void 0`);
    await type('[data-organization-filter]', 'Alpha');
    await wait('document.querySelectorAll("[data-organization-session]:not([hidden])").length===1');
    assert(await evaluate('document.activeElement===savedFilter && savedFilter===document.querySelector("[data-organization-filter]")'), 'React state update preserves filter identity/focus');
    assert(await evaluate(`savedRow===document.querySelector('${session('session-active')}') && savedName.value==='Unsaved workspace label'`), 'Filter does not recreate rows or reset unrelated unsaved label');
    await type('[data-organization-filter]', '');
    await wait('document.querySelectorAll("[data-organization-session]:not([hidden])").length===3');
    await click('[data-organization-archived]');
    await wait('document.querySelectorAll("[data-organization-session]:not([hidden])").length===2');
    assert(await evaluate(`document.querySelector('${session('session-archived')}').hidden`), 'Archived checkbox filters via React state');
    assert(await evaluate('document.querySelector("[data-organization-status]").textContent.includes("2")'), 'Loaded-page result status updates');
    await click('[data-organization-archived]');
    assert(await evaluate('savedName.value==="Unsaved workspace label" && savedName===document.querySelector(".organization-rename input[name=name]")'), 'Uncontrolled rename draft survives both filter state changes');
    assert(posts().length === 0 && !state.requests.some(request => request.path === '/activity'), 'Filtering stays local, no POST or Activity GET');
  });
  await report('Organization labels are text; canonical pagination, unavailable restore and confirmations', async () => {
    await page('organization');
    assert(await evaluate(`document.querySelector('#organization-active-${projectID} strong').textContent===${JSON.stringify(malicious)}`), 'Malicious workspace label preserved as literal text');
    assert(await evaluate(`document.querySelector('${session('session-archived')} strong').textContent===${JSON.stringify(malicious)}`), 'Malicious saved title preserved as literal text');
    assert(await evaluate('!document.querySelector("[data-react-page=organization] img") && !window.fixtureXSS'), 'React never interprets malicious fixture markup');
    assert(await evaluate(`document.querySelector('a[href="/?offset=50&project=${projectID}&view=organization"]')!==null && document.querySelector('a[href="/?archived_offset=25&view=organization"]')!==null`), 'Canonical server-pagination URLs retained');
    assert(await evaluate(`document.querySelector('#organization-archived-${missingID} button').disabled`), 'Unavailable original folder cannot be restored');
    await click(`#organization-active-${projectID} summary`);
    assert(posts().length === 0, 'Opening the workspace archive confirmation sends no mutation');
    assert(await evaluate(`document.querySelector('#organization-active-${projectID} details').open && !document.querySelector('${session('session-active')} details')`), 'Workspace archive retains confirmation; reversible conversation archive has no extra disclosure');
    assert(await evaluate(`document.querySelector('${session('session-active')} form[action$="/archive"] button').textContent==='Archive conversation'`), 'Conversation archive has a direct, clearly named action');
  });
  await report('Organization native forms use exact public endpoint/body contracts; fixture records only', async () => {
    await page('organization');
    await type(`#organization-name-${projectID}`, 'Renamed & <literal>');
    await submit(`#organization-active-${projectID} form[action$="/rename"] button`, {csrf, name: 'Renamed & <literal>'});
    await submit(`#organization-active-${projectID} form[action$="/pin"] button`, {csrf});
    await click(`#organization-active-${projectID} summary`);
    await submit(`#organization-active-${projectID} form[action$="/archive"] button`, {csrf, confirm: 'archive'});
    await submit(`#organization-archived-${archivedID} form[action$="/restore"] button`, {csrf, confirm: 'restore'});
    await submit(`${session('session-active')} form[action$="/pin"] button`, {csrf, session_id: 'session-active', offset: '25'});
    await submit(`${session('session-archived')} form[action$="/unpin"] button`, {csrf, session_id: 'session-archived', offset: '25'});
    await submit(`${session('session-active')} form[action$="/archive"] button`, {csrf, session_id: 'session-active', offset: '25', confirm: 'archive'});
    await submit(`${session('session-archived')} form[action$="/restore"] button`, {csrf, session_id: 'session-archived', offset: '25', confirm: 'restore'});
    assert(posts().length === 8 && state.errors.length === 0, 'Eight native POSTs; strict fixture rejects missing/extra/duplicate fields');
    assert(state.requests.every(request => !/runtime|prompt|cancel|fork|delete/.test(request.path)), 'No execution or destructive endpoints');
    await page('organization', {setup: state => { state.organization.organization.projects[0].pinned = true; }});
    await submit(`#organization-active-${projectID} form[action$="/unpin"] button`, {csrf});
  });
  const malformed = [
    ['invalid JSON', '{'],
    ['invalid CSRF', {...organization, csrf: 'not-a-token'}],
    ['project path traversal ID', (() => { const value = structuredClone(organization); value.organization.projects[0].id = '../outside'; return value; })()],
    ['nonlocal pagination', (() => { const value = structuredClone(organization); value.organization.nextURL = 'https://invalid.example/'; return value; })()],
    ['offset outside bound', (() => { const value = structuredClone(organization); value.organization.offset = 10001; return value; })()],
    ['duplicate session identity', (() => { const value = structuredClone(organization); value.organization.sessions.push(value.organization.sessions[0]); return value; })()],
    ['oversized bootstrap', 'x'.repeat(1024 * 1024 + 1)],
  ];
  await report('Malformed Organization bootstrap fails closed before forms mount', async () => {
    for (const [label, props] of malformed) {
      await page('organization', {query: '&malformed=1', setup: state => { state.badProps = props; }});
      await wait('document.querySelector("[data-react-page=organization] [role=alert]")?.textContent.includes("safely")');
      assert(await evaluate('document.querySelectorAll("[data-react-page=organization] form").length===0 && !window.fixtureXSS'), `${label}: safe error without mutation-capable form`);
      assert(posts().length === 0, `${label}: no POST`);
    }
  });
}
