import {writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {summary} from './fixture.mjs';

export async function lifecycleChecks(t) {
  const {page, wait, until, evaluate, click, assert, report, state, delay, send, evidence} = t;
  const reads = () => state.requests.filter(request => request.path === '/activity').length;
  const fresh = () => wait('document.querySelector("[data-manager-activity]")?.dataset.freshness === "fresh" && !document.querySelector("[data-manager-activity-refresh]").disabled');
  const nav = view => `a.sidebar-utility[href="/?view=${view}"]`;
  await report('Real HTMX ancestor swap Activity → Organization → Activity and back remount once', async () => {
    await page(); await fresh();
    await evaluate('window.previousRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.cleanupEvents=0;document.addEventListener("htmx:beforeCleanupElement",()=>window.cleanupEvents++)');
    state.next = {holdBody: true, body: summary('STALE navigation body')};
    await click('[data-manager-activity-refresh]'); await until(() => state.held.length === 1, 'Held body before navigation');
    await click(nav('organization'));
    await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    await until(() => state.aborts >= 1, 'Navigation aborts Activity body'); state.release();
    assert(await evaluate('!previousRoot.isConnected && !previousRoot.dataset.reactMounted && previousRoot.childElementCount===0 && cleanupEvents>0'), 'Actual HTMX cleanup unmounts React before detached root retirement');
    const count = reads(); await delay(2150);
    assert(reads() === count && state.active === 0, 'Organization owns no orphan Activity polling');
    await evaluate('window.organizationRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");void 0');
    await click(nav('activity')); await fresh();
    assert(reads() === count + 1, 'Activity remount sends one immediate GET');
    assert(await evaluate('!organizationRoot.dataset.reactMounted && organizationRoot.childElementCount===0'), 'Organization React root also cleaned before swap');
    const pageRequests = state.requests.filter(request => request.path === '/' && request.headers['hx-request']);
    assert(pageRequests.length === 2 && pageRequests.every(request => request.headers['hx-target'] === 'workspace'), 'Navigation uses real HTMX workspace ancestor requests');
    await evaluate('history.back()');
    await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"', 'Browser Back restores Organization');
    await evaluate('history.forward()'); await fresh();
    assert(await evaluate('document.querySelectorAll("[data-react-page=activity][data-react-mounted=true], [data-react-page=organization][data-react-mounted=true]").length===1'), 'History restoration leaves exactly one committed React root');
    assert(state.maxActive === 1, 'History and HTMX lifecycle do not duplicate poll ownership');
  });
  await report('Canceled real HTMX swap preserves mounted root and listeners, resumes one poll owner', async () => {
    await page(); await fresh();
    await evaluate('window.keptRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.canceledRequestDone=false;window.cancelSwap=e=>{e.detail.shouldSwap=false};document.addEventListener("htmx:beforeSwap",window.cancelSwap);document.addEventListener("htmx:afterRequest",()=>window.canceledRequestDone=true,{once:true})');
    const before = state.requests.filter(request => request.headers['hx-request']).length;
    await click(nav('organization'));
    await until(() => state.requests.filter(request => request.headers['hx-request']).length === before + 1, 'Canceled navigation still fetched actual HTMX response');
    await wait('window.canceledRequestDone', 'Canceled request lifecycle completed'); await fresh();
    assert(await evaluate('keptRoot===document.querySelector("[data-react-page=activity], [data-react-page=organization]") && keptRoot.dataset.reactMounted==="true" && keptRoot.childElementCount>0'), 'Canceled swap never unmounts surviving React root');
    await evaluate('document.removeEventListener("htmx:beforeSwap",window.cancelSwap)');
    state.current = summary('Listeners survive canceled swap');
    const count = reads(); await click('[data-manager-activity-refresh]');
    await until(() => reads() === count + 1, 'Refresh listener survives canceled swap'); await wait('document.querySelector(".manager-activity-card h3")?.textContent==="Listeners survive canceled swap"');
    assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent==="Listeners survive canceled swap"'), 'Surviving root applies explicit read normally');
    await click(nav('organization')); await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    assert(await evaluate('!keptRoot.dataset.reactMounted && keptRoot.childElementCount===0'), 'Subsequent noncanceled swap cleans same root');
    assert(state.maxActive === 1, 'Canceled navigation never adds a second polling owner');
  });
  await report('pagehide unmount and pageshow/historyRestore remount are idempotent', async () => {
    await page(); await fresh(); state.next = {holdHeaders: true};
    await click('[data-manager-activity-refresh]'); await until(() => state.held.length === 1, 'Pending read before pagehide');
    await evaluate('window.pageRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.dispatchEvent(new PageTransitionEvent("pagehide",{persisted:true}))');
    await until(() => state.aborts >= 1, 'pagehide aborts active read');
    assert(await evaluate('!pageRoot.dataset.reactMounted && pageRoot.childElementCount===0'), 'pagehide unmounts React in connected root');
    state.release(); const count = reads(); await delay(2150);
    assert(reads() === count, 'Unmounted page has no timer');
    await evaluate('window.dispatchEvent(new PageTransitionEvent("pageshow",{persisted:true}));window.dispatchEvent(new PageTransitionEvent("pageshow",{persisted:true}));document.dispatchEvent(new CustomEvent("htmx:historyRestore",{detail:{}}));document.dispatchEvent(new CustomEvent("htmx:afterSwap",{detail:{target:document.querySelector("#workspace")}}))');
    await fresh();
    assert(reads() === count + 1 && state.maxActive === 1, 'Repeated restore/show/swap events create only one root/request');
    assert(await evaluate('pageRoot===document.querySelector("[data-react-page=activity], [data-react-page=organization]") && pageRoot.dataset.reactMounted==="true"'), 'Same retained page element remounts on pageshow');
  });
  await report('beforeCleanupElement explicitly unmounts owned ancestor and stops poll lifecycle', async () => {
    await page(); await fresh();
    await evaluate('window.removedRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");document.dispatchEvent(new CustomEvent("htmx:beforeCleanupElement",{detail:{elt:document.querySelector("#workspace")}}));document.querySelector("#workspace").remove()');
    const count = reads(); await delay(2150);
    assert(await evaluate('removedRoot.childElementCount===0 && !removedRoot.dataset.reactMounted'), 'Ancestor cleanup removes React effects and descendants');
    assert(reads() === count && state.active === 0, 'Removed Activity remains inactive beyond poll interval');
  });
  await report('Representative desktop/narrow and dark/light production CSS geometry', async () => {
    const measurements = [];
    for (const view of ['activity', 'organization']) for (const width of [1440, 390]) for (const theme of ['dark', 'light']) {
      await send('Emulation.setDeviceMetricsOverride', {width, height: width === 390 ? 844 : 1000, deviceScaleFactor: 1, mobile: false});
      await page(view); if (view === 'activity') await fresh();
      await evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`);
      await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))');
      const metrics = await evaluate(`(() => {const root=document.querySelector('[data-react-page=activity], [data-react-page=organization]');const rect=root.getBoundingClientRect();const heading=root.querySelector('h1');const control=root.querySelector('button:not([disabled]),input:not([type=hidden])');const r=control.getBoundingClientRect();return {viewport:innerWidth,scrollWidth:document.documentElement.scrollWidth,rootWidth:rect.width,heading:heading.textContent,controlWidth:r.width,controlHeight:r.height,color:getComputedStyle(heading).color,background:getComputedStyle(document.body).backgroundColor}})()`);
      measurements.push({view, width, theme, ...metrics});
      assert(metrics.rootWidth > 200 && metrics.rootWidth <= width && metrics.scrollWidth <= width + 1, `${view}/${width}/${theme}: root fits viewport without document overflow`);
      assert(metrics.controlWidth >= 24 && metrics.controlHeight >= 24 && metrics.heading.length > 0, `${view}/${width}/${theme}: representative heading/control visibly laid out`);
      if (evidence) { const shot = await send('Page.captureScreenshot', {format: 'png', captureBeyondViewport: false}); await writeFile(join(evidence, `${view}-${width}-${theme}.png`), Buffer.from(shot.data, 'base64')); }
    }
    for (const view of ['activity', 'organization']) for (const width of [1440, 390]) {
      const [dark, light] = measurements.filter(row => row.view === view && row.width === width);
      assert(dark.color !== light.color || dark.background !== light.background, `${view}/${width}: actual production theme changes computed appearance`);
    }
    if (evidence) await writeFile(join(evidence, 'measurements.json'), JSON.stringify(measurements, null, 2));
  });
}
