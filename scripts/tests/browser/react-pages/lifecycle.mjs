import {writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {summary} from './fixture.mjs';

export async function lifecycleChecks(t) {
  const {page, wait, until, evaluate, click, assert, report, state, delay, send, evidence} = t;
  const reads = () => state.requests.filter(request => request.path === '/activity').length;
  const fresh = () => wait('document.querySelector("[data-manager-activity]")?.dataset.freshness === "fresh" && !document.querySelector("[data-manager-activity-refresh]").disabled');
  const nav = view => `a.sidebar-utility[href="/?view=${view}"]`;
  await report('Workspace view options visibly track their checkbox state', async () => {
    await page();
    await click('[data-sidebar-view-toggle]');
    await wait('document.querySelectorAll("[role=menuitemcheckbox]").length===2');
    assert(await evaluate(`[...document.querySelectorAll('[role=menuitemcheckbox]')].every(row => {
      const marker = row.querySelector('.snow-menu-check > span');
      return marker && (getComputedStyle(marker).visibility !== 'hidden') === (row.getAttribute('aria-checked') === 'true');
    })`), 'Both workspace view options reserve a checkmark that matches aria-checked');
    await click('[role=menuitemcheckbox]:last-child');
    await wait('document.querySelector("[role=menuitemcheckbox]:last-child")?.getAttribute("aria-checked")==="true"');
    assert(await evaluate('getComputedStyle(document.querySelector("[role=menuitemcheckbox]:last-child .snow-menu-check > span")).visibility!=="hidden"'), 'Toggling pinned-only exposes its visible checkmark');
    await evaluate('document.dispatchEvent(new KeyboardEvent("keydown",{key:"Escape",bubbles:true}))');
    await wait('!document.querySelector(".snow-menu")');
  });
  await report('Native ancestor swap Activity → Organization → Activity and back remount once', async () => {
    await page(); await fresh();
    await evaluate('window.previousRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.cleanupEvents=0;document.addEventListener("snow:navigation-before-swap",()=>window.cleanupEvents++)');
    state.next = {holdBody: true, body: summary('STALE navigation body')};
    await click('[data-manager-activity-refresh]'); await until(() => state.held.length === 1, 'Held body before navigation');
    await evaluate('window.navigationSpacer=document.createElement("div");navigationSpacer.style.height="2400px";document.body.append(navigationSpacer);scrollTo(0,900)');
    assert(await evaluate('scrollY>500'), 'Outgoing page has a meaningful stored scroll position');
    await evaluate('SnowNavigation.visit("/?view=organization",{history:"push"})');
    await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    assert(await evaluate('scrollY===0'), 'Explicit push navigation resets document scroll');
    await until(() => state.aborts >= 1, 'Navigation aborts Activity body'); state.release();
    assert(await evaluate('!previousRoot.isConnected && !previousRoot.dataset.reactMounted && previousRoot.childElementCount===0 && cleanupEvents>0'), 'Native cleanup unmounts React before detached root retirement');
    const count = reads(); await delay(2150);
    assert(reads() === count && state.active === 0, 'Organization owns no orphan Activity polling');
    await evaluate('window.organizationRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");void 0');
    await click(nav('activity')); await fresh();
    assert(reads() === count + 1, 'Activity remount sends one immediate GET');
    assert(await evaluate('!organizationRoot.dataset.reactMounted && organizationRoot.childElementCount===0'), 'Organization React root also cleaned before swap');
    const pageRequests = state.requests.filter(request => request.path === '/' && request.headers['x-snow-navigation'] === 'workspace');
    assert(pageRequests.length === 2 && pageRequests.every(request => request.headers.accept === 'text/html'), 'Navigation uses the native bounded workspace protocol');
    await evaluate('history.back()');
    await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"', 'Browser Back restores Organization');
    await evaluate('history.forward()'); await fresh();
    assert(await evaluate('document.querySelectorAll("[data-react-page=activity][data-react-mounted=true], [data-react-page=organization][data-react-mounted=true]").length===1'), 'History restoration leaves exactly one committed React root');
    let traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('history.go(-2)');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Stored-scroll history read');
    await wait('scrollY>500', 'Stored outgoing scroll restored after traversal');
    assert(await evaluate('scrollY>500'), 'History traversal restores the stored entry scroll position');
    traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('history.go(2)');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Return from stored-scroll entry');
    await fresh();
    await evaluate('scrollTo(0,700)');
    assert(await evaluate('scrollY>500'), 'Newest pushed entry has an updated scroll position before traversal');
    const rapidBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('window.rapidHistoryMarker={};window.addEventListener("popstate",()=>history.forward(),{once:true});history.back()');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length >= rapidBefore + 2, 'Rapid Back and Forward both dispatch native reads');
    await fresh();
    assert(await evaluate('window.rapidHistoryMarker && location.search==="?view=activity" && scrollY>500 && document.querySelector("[data-react-page=activity]")?.dataset.reactMounted==="true"'), 'Rapid Back→Forward supersession keeps the document, newest React owner and newest-entry scroll');

    await evaluate('navigationSpacer.id="navigation-scroll-target"');
    traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('SnowNavigation.visit("/?view=activity#navigation-scroll-target",{history:"push"})');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Hash-entry push read');
    await wait('location.hash==="#navigation-scroll-target" && scrollY>500', 'Hash destination scroll');
    traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('history.back()');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Leave hash history entry');
    await wait('!location.hash', 'Return from hash history entry');
    traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('history.forward()');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Restore hash history entry');
    await wait('location.hash==="#navigation-scroll-target" && scrollY>500', 'Forward restores hash entry scroll');
    assert(await evaluate('location.hash==="#navigation-scroll-target" && scrollY>500'), 'Hash Back→Forward retains the destination position');
    traversalBefore = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await evaluate('history.back()');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length > traversalBefore, 'Leave hash entry after restoration');
    await fresh();
    await evaluate('navigationSpacer.remove()');
    assert(state.maxActive === 1, 'History and native navigation lifecycle do not duplicate poll ownership');
  });
  await report('private-IP crypto fallback initializes native navigation without randomUUID', async () => {
    const {identifier} = await send('Page.addScriptToEvaluateOnNewDocument', {source: 'Object.defineProperty(Crypto.prototype,"randomUUID",{value:undefined,configurable:true})'});
    try {
      await page(); await fresh();
      await click(nav('organization'));
      await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
      assert(await evaluate('typeof crypto.randomUUID==="undefined" && !!SnowNavigation && location.search==="?view=organization"'), 'getRandomValues fallback mounts React and completes native navigation');
    } finally {
      await send('Page.removeScriptToEvaluateOnNewDocument', {identifier});
    }
  });
  await report('Rejected native navigation preserves mounted root and listeners, resumes one poll owner', async () => {
    await page(); await fresh();
    state.invalidNextNavigation = true;
    await evaluate('window.keptRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.rejectedRequestDone=false;document.addEventListener("snow:navigation-end",()=>window.rejectedRequestDone=true,{once:true})');
    const before = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    await click(nav('organization'));
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length === before + 1, 'Rejected navigation fetched one native response');
    await wait('window.rejectedRequestDone', 'Rejected request lifecycle completed'); await fresh();
    assert(await evaluate('keptRoot===document.querySelector("[data-react-page=activity], [data-react-page=organization]") && keptRoot.dataset.reactMounted==="true" && keptRoot.childElementCount>0'), 'Rejected swap never unmounts surviving React root');
    state.current = summary('Listeners survive rejected swap');
    const count = reads(); await click('[data-manager-activity-refresh]');
    await until(() => reads() === count + 1, 'Refresh listener survives rejected swap'); await wait('document.querySelector(".manager-activity-card h3")?.textContent==="Listeners survive rejected swap"');
    assert(await evaluate('document.querySelector(".manager-activity-card h3").textContent==="Listeners survive rejected swap"'), 'Surviving root applies explicit read normally');
    await click(nav('organization')); await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    assert(await evaluate('!keptRoot.dataset.reactMounted && keptRoot.childElementCount===0'), 'Subsequent valid swap cleans same root');
    assert(state.maxActive === 1, 'Rejected navigation never adds a second polling owner');
  });
  await report('failed stale-DOM supersession repairs the popstate URL/DOM mismatch', async () => {
    await page(); await fresh();
    await click(nav('organization')); await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    await click(nav('activity')); await fresh();
    await evaluate('window.staleTraversalDocumentMarker={}');
    const before = state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length;
    state.holdNextNavigation = true;
    await evaluate('history.back()');
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length === before + 1 && state.held.length === 1, 'Held popstate navigation');
    state.invalidNextNavigation = true;
    await click(nav('organization'));
    await until(() => state.requests.filter(request => request.headers['x-snow-navigation'] === 'workspace').length === before + 2, 'Failing stale-DOM navigation supersedes popstate');
    await wait('location.search==="?view=organization" && document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true" && !window.staleTraversalDocumentMarker', 'Ordinary reload reconciles URL and DOM');
    state.release();
    assert(await evaluate('location.search==="?view=organization" && !window.staleTraversalDocumentMarker'), 'Failed supersession cannot leave a destination URL over stale DOM');
  });
  await report('pagehide unmount and repeated pageshow remount are idempotent', async () => {
    await page(); await fresh(); state.next = {holdHeaders: true};
    await click('[data-manager-activity-refresh]'); await until(() => state.held.length === 1, 'Pending read before pagehide');
    await evaluate('window.pageRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");window.dispatchEvent(new PageTransitionEvent("pagehide",{persisted:true}))');
    await until(() => state.aborts >= 1, 'pagehide aborts active read');
    assert(await evaluate('!pageRoot.dataset.reactMounted && pageRoot.childElementCount===0'), 'pagehide unmounts React in connected root');
    state.release(); const count = reads(); await delay(2150);
    assert(reads() === count, 'Unmounted page has no timer');
    await evaluate('window.dispatchEvent(new PageTransitionEvent("pageshow",{persisted:true}));window.dispatchEvent(new PageTransitionEvent("pageshow",{persisted:true}))');
    await fresh();
    assert(reads() === count + 1 && state.maxActive === 1, 'Repeated restore/show/swap events create only one root/request');
    assert(await evaluate('pageRoot===document.querySelector("[data-react-page=activity], [data-react-page=organization]") && pageRoot.dataset.reactMounted==="true"'), 'Same retained page element remounts on pageshow');
  });
  await report('committed native replacement unmounts owned ancestor and stops poll lifecycle', async () => {
    await page(); await fresh();
    await evaluate('window.removedRoot=document.querySelector("[data-react-page=activity], [data-react-page=organization]");void 0');
    await click(nav('organization')); await wait('document.querySelector("[data-react-page=organization]")?.dataset.reactMounted==="true"');
    const count = reads(); await delay(2150);
    assert(await evaluate('removedRoot.childElementCount===0 && !removedRoot.dataset.reactMounted && !removedRoot.isConnected'), 'Ancestor cleanup removes React effects and descendants before detachment');
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
