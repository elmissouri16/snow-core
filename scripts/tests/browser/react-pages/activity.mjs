import {projectID, malicious, summary} from './fixture.mjs';

export async function activityChecks(t) {
  const {page, wait, until, evaluate, click, assert, report, state, delay} = t;
  const fresh = () => wait('document.querySelector("[data-manager-activity]")?.dataset.freshness === "fresh" && !document.querySelector("[data-manager-activity-refresh]").disabled');
  const reads = () => state.requests.filter(request => request.path === '/activity').length;
  const refresh = async () => { const count = reads(); await click('[data-manager-activity-refresh]'); await until(() => reads() === count + 1, 'one explicit Activity GET'); };
  await report('Activity real React commit, DTO, read-only controls and single poll owner', async () => {
    await page(); await fresh();
    assert(reads() === 1, 'Exactly one initial request');
    assert(await evaluate('typeof window.snowManagerActivity === "undefined" && typeof window.snowOrganization === "undefined"'), 'No legacy page globals');
    assert(await evaluate('!Array.from(document.scripts).some(s => /\\/(manager-activity|organization)\\.js$/.test(s.src))'), 'No legacy page scripts loaded');
    assert(await evaluate(`document.querySelector('[data-project="${projectID}"] .manager-activity-state').textContent.includes('approval needed')`), 'Strict public DTO rendered');
    assert(await evaluate('document.querySelectorAll(".manager-activity-count").length === 8'), 'All public counts rendered');
    assert(await evaluate(`document.querySelector('[data-project="${projectID}"] a').getAttribute('href') === '/?view=projects&project=${projectID}&session=session-active'`), 'Exact navigation-only conversation URL');
    assert(await evaluate('document.querySelectorAll("[data-manager-activity] form").length === 0'), 'Activity has no mutation forms');
    state.next = {holdHeaders: true};
    await refresh();
    await evaluate('for(let i=0;i<8;i++)document.querySelector("[data-manager-activity-refresh]").click()');
    await delay(2250);
    assert(reads() === 2 && state.maxActive === 1, 'Repeated refresh + poll interval cannot overlap in-flight GET');
    assert(await evaluate('document.querySelector("[data-manager-activity-refresh]").disabled'), 'Explicit refresh disabled while busy');
    state.release(); await fresh();
    await until(() => reads() === 3, 'One subsequent real 2-second poll'); await fresh();
    assert(state.maxActive === 1 && state.requests.every(request => request.method === 'GET'), 'Poll remains serial and strictly read-only');
  });
  await report('Activity keyed project DOM/focus survives fresh updates; labels remain text', async () => {
    await page(); await fresh();
    await evaluate(`window.savedCard=document.querySelector('[data-project="${projectID}"]');window.savedLink=savedCard.querySelector('a');savedLink.focus()`);
    state.current = summary(malicious);
    // Let the real timer update while link focus remains undisturbed by clicking Refresh.
    await until(() => reads() >= 2, 'Automatic updated snapshot');
    await wait(`document.querySelector('[data-project="${projectID}"] h3').textContent === ${JSON.stringify(malicious)}`);
    assert(await evaluate('savedCard===document.querySelector(".manager-activity-card[data-project]") && savedLink===savedCard.querySelector("a") && document.activeElement===savedLink'), 'React key preserves card/link identity and active focus');
    assert(await evaluate('!document.querySelector("[data-manager-activity] img") && !window.fixtureXSS'), 'Malicious label never parsed as markup');
  });
  await report('Activity strict failed-response validation retains last good state without stale apply', async () => {
    await page(); await fresh();
    const bad = [
      ['missing state boolean', (() => { const value = summary('Rejected missing field'); delete value.projects[0].host_running; return {body: value}; })()],
      ['duplicate project', (() => { const value = summary('Rejected duplicate'); value.projects.push(value.projects[0]); return {body: value}; })()],
      ['nonlocal URL', (() => { const value = summary('Rejected URL'); value.projects[0].session_url = 'https://invalid.example/'; return {body: value}; })()],
      ['invalid count', (() => { const value = summary('Rejected count'); value.counts.registered = 101; return {body: value}; })()],
      ['too many projects', {body: {...summary('Rejected oversized list'), projects: Array.from({length: 101}, () => summary().projects[0])}}],
      ['malformed JSON', {raw: '{"projects":'}],
      ['wrong content type', {body: summary('Rejected media type'), headers: {'Content-Type': 'text/html'}}],
      ['advertised body bound', {body: summary('Rejected content length'), headers: {'Content-Length': '262145'}}],
      ['streamed body bound', {raw: JSON.stringify({...summary('Rejected streamed body'), ignored: 'x'.repeat(262145)})}],
      ['HTTP failure', {status: 503, body: summary('Rejected HTTP failure')}],
    ];
    for (const [label, behavior] of bad) {
      state.next = behavior; await refresh();
      await wait('document.querySelector("[data-manager-activity]")?.dataset.freshness === "error" && !document.querySelector("[data-manager-activity-refresh]").disabled', label);
      assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent === "Alpha running"'), `${label}: previous valid data preserved, rejected state not applied`);
      assert(await evaluate('document.querySelector("[data-manager-activity-fresh]").textContent.includes("stale") && !document.querySelector("[data-manager-activity-refresh]").disabled'), `${label}: explicit stale/error state, refresh recovers`);
    }
    state.current = summary('Recovered summary'); await refresh(); await fresh();
    assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent === "Recovered summary"'), 'Valid explicit GET recovers after failures');
    assert(state.maxActive === 1, 'Validation failures release the sole polling slot');
  });
  for (const hold of ['holdHeaders', 'holdBody']) await report(`Activity ${hold}: hidden owner aborts HTTP/body, late response fenced, visible resume single read`, async () => {
    await page(); await fresh();
    state.next = {[hold]: true, body: summary('STALE hidden response')};
    await refresh(); if (hold === 'holdBody') await until(() => state.held.length === 1, 'Held streamed response body');
    await evaluate('document.querySelector("#workspace").hidden=true');
    await until(() => state.aborts >= 1, 'Native fetch aborted on hidden ancestor');
    await wait('document.querySelector("[data-manager-activity]").dataset.freshness === "paused"');
    state.release(); const count = reads(); await delay(2150);
    assert(reads() === count && state.active === 0, 'Hidden root owns no HTTP or scheduled polling');
    assert(await evaluate('!document.querySelector("[data-manager-activity]").textContent.includes("STALE hidden response")'), 'Late hidden response never applied');
    state.current = summary('Visible resume');
    await evaluate('document.querySelector("#workspace").hidden=false'); await fresh();
    assert(reads() === count + 1 && state.maxActive === 1, 'One immediate resume GET, no duplicate owner');
    assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent === "Visible resume"'), 'Resume discards stale visibility epoch');
  });
  for (const status of [401, 403]) await report(`Activity HTTP ${status} retires ownership and data until remount`, async () => {
    await page(); await fresh(); state.next = {status}; await refresh();
    await wait('!document.querySelector("[data-manager-activity-login]").hidden');
    assert(await evaluate('document.querySelectorAll(".manager-activity-card").length===0 && document.querySelectorAll(".manager-activity-count").length===0'), 'Access-ended response clears stale summary and counts');
    const count = reads();
    await evaluate('document.dispatchEvent(new Event("visibilitychange"));document.querySelector("[data-manager-activity-refresh]").click()');
    await delay(2200);
    assert(reads() === count, 'No poll, visibility or manual retry after access ends');
    assert(await evaluate('document.querySelector("[data-manager-activity-refresh]").disabled && document.querySelector("[data-manager-activity-fresh]").textContent.includes("host work was not stopped")'), 'Access message does not imply host stop');
  });
  await report('Activity unavailable registry and malformed bootstrap send no reads', async () => {
    await page('activity', {query: '&malformed=1', setup: state => { state.badProps = {registryEnabled: false, error: ''}; }});
    await wait('document.querySelector("[data-manager-activity]")?.dataset.freshness === "error"');
    await delay(2150);
    assert(reads() === 0 && await evaluate('document.querySelector("[data-manager-activity-refresh]").disabled'), 'Unavailable registry never polls or offers refresh');
    await page('activity', {query: '&malformed=1', setup: state => { state.badProps = {registryEnabled: 'true', error: ''}; }});
    await wait('document.querySelector("[data-react-page=activity] [role=alert]")?.textContent.includes("safely")');
    assert(reads() === 0 && await evaluate('!document.querySelector("[data-manager-activity-refresh]")'), 'Invalid Activity bootstrap fails safely before read/control setup');
  });
  await report('Activity request deadline aborts stalled body and explicit refresh recovers', async () => {
    await page(); await fresh(); state.next = {holdBody: true}; await refresh();
    await until(() => state.aborts >= 1, '5-second request deadline cancels body', 6500);
    await wait('document.querySelector("[data-manager-activity]").dataset.freshness === "error"');
    state.release(); state.current = summary('After timeout'); await refresh(); await fresh();
    assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent === "After timeout"'), 'Timeout releases busy slot for read-only refresh');
    assert(state.maxActive === 1, 'Timed-out stream never overlaps replacement request');
  });
}
