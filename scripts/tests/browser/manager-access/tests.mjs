// Native Chrome controls against unmodified production HTML/JS/CSS. Evaluation
// only observes DOM or performs a read-only inventory GET; no DOM/fetch mocks,
// injected session cookies, synthetic clicks or JavaScript form submissions.
import {setTimeout as delay} from "node:timers/promises";

export async function exercise({client, ready, restart, width, theme}) {
  const results = [], failures = [], requests = [], responses = [], pages = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  let origin = ready.origin, code = ready.code;
  const inventoryRoot = '#workspace [data-browser-inventory]';
  const selector = child => `${inventoryRoot} ${child}`;
  const dispose = client.onEvent(message => {
    if (!pages.some(page => page.sessionId === message.sessionId)) return;
    if (message.method === "Network.requestWillBeSent") {
      const request = message.params.request, url = new URL(request.url);
      if (url.hostname !== "127.0.0.1") return;
      // Keep bounded public route/field-name evidence only, never values,
      // cookies, request headers, CSRF tokens or the private pairing code.
      if (requests.length < 2000) requests.push({method: request.method, path: url.pathname, keys: request.postData ? [...new URLSearchParams(request.postData).keys()].sort() : []});
    }
    if (message.method === "Network.responseReceived") {
      const response = message.params.response, url = new URL(response.url);
      if (url.hostname === "127.0.0.1" && responses.length < 2000) responses.push({path: url.pathname, status: response.status});
    }
  });
  async function page() {
    const {browserContextId} = await client.send("Target.createBrowserContext");
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank", browserContextId});
    const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
    const send = (method, params = {}) => client.send(method, params, sessionId);
    const q = value => `document.querySelector(${JSON.stringify(value)})`;
    const evaluate = async expression => {
      const value = await send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true});
      if (value.exceptionDetails) throw Error("Native browser observation failed");
      return value.result?.value;
    };
    const wait = async (expression, label, timeout = 15000) => {
      const end = Date.now() + timeout;
      while (Date.now() < end) { if (await evaluate(expression)) return; await delay(35); }
      throw Error(`Timed out: ${label}`);
    };
    const click = async value => {
      await send("Page.bringToFront");
      const point = await evaluate(`(() => {const el=${q(value)};if(!el||el.disabled)return null;el.scrollIntoView({block:'center',inline:'nearest'});for(const r of el.getClientRects()){const x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;if(r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y)))return {x,y};}return null;})()`);
      if (!point) {
        const error = Error("Native pointer target missing or obscured: " + value.replace(/browser_[a-f0-9]{32}/g, "browser_[public-id]"));
        error.observation = await evaluate(`(() => {const el=${q(value)},rect=el?.getBoundingClientRect();return {path:location.pathname,workspaceView:document.querySelector('#workspace')?.dataset.view,targetPresent:!!el,targetDisabled:!!el?.disabled,workspaceInert:!!document.querySelector('#workspace-content')?.inert,navigationOpen:!!document.querySelector('#project-navigation.nav-open'),settingsOpen:!!document.querySelector('#settings-dialog')?.open,rect:rect?{x:rect.x,y:rect.y,width:rect.width,height:rect.height}:null};})()`);
        throw error;
      }
      await send("Input.dispatchMouseEvent", {type: "mouseMoved", ...point});
      await send("Input.dispatchMouseEvent", {type: "mousePressed", ...point, button: "left", clickCount: 1});
      await send("Input.dispatchMouseEvent", {type: "mouseReleased", ...point, button: "left", clickCount: 1});
      await delay(25);
    };
    const navigate = async path => { await send("Page.navigate", {url: origin + path}); };
    const login = async () => {
      await navigate("/login"); await wait(`!!${q('#code')}`, "real pairing form");
      await click("#code"); await send("Input.insertText", {text: code});
      await click('form[action="/login"] button[type="submit"]');
      await wait(`!!${q('#workspace')} && location.pathname==='/'`, "native pairing succeeds");
    };
    const setTheme = async () => {
      if (width < 768) { await click("[data-nav-toggle]"); await delay(100); }
      await click("[data-settings-open]"); await wait(`!!${q('#settings-dialog')}?.open`, "native Settings opens");
      await click(`[data-theme-choice="${theme}"]`);
      await click("#settings-dialog [data-settings-close]");
      await wait(`!${q('#settings-dialog')}?.open`, "native Settings closes");
      // Settings returns focus to its opener in the mobile project drawer;
      // closing that dialog does not close the drawer or release its inert
      // workspace. Return through the real drawer Close control, not a DOM
      // state override or an obscured/synthetic click into the workspace.
      if (await evaluate(`!!${q('#project-navigation.nav-open')}`)) {
        await click("#project-navigation [data-nav-close]");
        await wait(`!${q('#project-navigation.nav-open')} && !${q('#workspace-content')}?.inert`, "native project drawer closes after Settings");
      }
      check(await evaluate(`!${q('#settings-dialog')}?.open && !${q('#workspace-content')}?.inert`), "Native Settings return releases the workspace before inventory interaction");
      check(await evaluate(`document.documentElement.dataset.theme===${JSON.stringify(theme)}`), `Native settings select ${theme} theme`);
    };
    const access = async () => {
      await navigate("/?view=access");
      await wait(`!!${q(inventoryRoot)} && ${q(selector('[data-browser-status]'))}?.textContent.includes('browser slots used')`, "production browser inventory loads");
    };
    const inventory = () => evaluate("fetch('/access/browsers',{cache:'no-store'}).then(async response=>({status:response.status,data:response.ok?await response.json():null}))");
    const refresh = async count => {
      await click(selector("[data-browser-refresh]"));
      await wait(`${q(selector('[data-browser-status]'))}?.textContent===${JSON.stringify(`${count} of 8 browser slots used.`)}`, "native inventory refresh");
    };
    const select = async id => {
      await click(selector(`li[data-browser-id="${id}"] button`));
      await wait(`${q(selector('[data-browser-confirm]'))}?.hidden===false`, "native targeted confirmation opens");
    };
    const confirm = () => click(selector("[data-browser-revoke]"));
    const loggedOut = () => wait(`location.pathname==='/login' && !!${q('#code')}`, "revoked browser reaches real pairing page");
    const bounds = async label => check(await evaluate("document.documentElement.scrollWidth<=innerWidth+1"), `${label}: no horizontal overflow at ${width}px`);
    const p = {sessionId, send, q, evaluate, wait, click, navigate, login, setTheme, access, inventory, refresh, select, confirm, loggedOut, bounds};
    pages.push(p);
    await send("Network.enable"); await send("Page.enable");
    await send("Network.setCacheDisabled", {cacheDisabled: true});
    await send("Emulation.setDeviceMetricsOverride", {width, height: 740, deviceScaleFactor: 1, mobile: false});
    await send("Emulation.setEmulatedMedia", {features: [{name: "prefers-reduced-motion", value: "reduce"}]});
    await send("Page.addScriptToEvaluateOnNewDocument", {source: "globalThis.accessFixtureErrorCount=0;addEventListener('error',()=>accessFixtureErrorCount++);addEventListener('unhandledrejection',()=>accessFixtureErrorCount++);"});
    return p;
  }
  const postCount = () => requests.filter(request => request.method === "POST").length;
  try {
    const a = await page(), b = await page();
    await a.login(); await a.setTheme(); await a.access();
    await b.login(); await b.setTheme(); await b.access();
    await a.refresh(2);
    const first = await a.inventory(), second = await b.inventory();
    check(first.status === 200 && second.status === 200 && first.data.browsers.length === 2 && second.data.browsers.length === 2, "Two native-paired independent browser contexts see the same bounded inventory");
    const aID = first.data.browsers.find(browser => browser.current)?.id, bID = second.data.browsers.find(browser => browser.current)?.id;
    check(aID !== bID && /^browser_[a-f0-9]{32}$/.test(aID) && /^browser_[a-f0-9]{32}$/.test(bID), "Native browser identities are distinct opaque public IDs, not authentication tokens");
    check(first.data.browsers.filter(browser => browser.current).length === 1 && second.data.browsers.filter(browser => browser.current).length === 1, "Each independent browser sees exactly its own current-browser indicator");
    check(first.data.browsers.every(browser => browser.label.length <= 80 && Date.parse(browser.created) <= Date.parse(browser.last_used) && Date.parse(browser.expires) > Date.parse(browser.created)), "Production inventory provides bounded labels and created/last-seen/expiry metadata");
    check(!/csrf|cookie|hash|pair_code|pairing_code|signing_key/.test(JSON.stringify(first.data)), "Public production inventory omits private credential fields");
    check(first.data.limit === 8, "Production inventory preserves the eight-browser limit");
    for (const name of ["generated/app.js", "browser-access.css"]) check(responses.some(response => response.path === `/static/${name}` && response.status === 200), `Actual production asset allowlist serves ${name}`);
    check(await a.evaluate(`${a.q(inventoryRoot)}.textContent.includes('every 5 seconds') && ${a.q(inventoryRoot)}.textContent.includes('does not stop a running agent')`), "Actual UI states periodic reauthorization, not immediate stream kill or worker stop");
    check(await a.evaluate(`${a.q(inventoryRoot)}.textContent.includes('approximate after a restart')`), "Actual UI explains approximate last-seen persistence");
    await a.bounds("Paired browser inventory");
    const beforeCancel = postCount();
    await a.select(bID); await a.bounds("Other-browser confirmation");
    check(await a.evaluate(`!${a.q(selector('[data-browser-confirm-description]'))}.textContent.includes('current browser')`), "Other-browser confirmation identifies only the selected remote browser");
    await a.click(selector("[data-browser-cancel]"));
    check(postCount() === beforeCancel, "Native Cancel sends no mutation");
    check(await a.evaluate(`${a.q(selector('[data-browser-confirm]'))}.hidden`), "Native Cancel closes individual confirmation");
    await a.select(bID); await a.confirm();
    await a.wait(`${a.q(selector('[data-browser-status]'))}?.textContent.startsWith('Browser revoked.')`, "native other-browser revoke commits");
    check(postCount() === beforeCancel + 1, "Confirm other-browser revoke sends exactly one mutation");
    check(requests.filter(request => request.method === "POST").at(-1)?.path === `/access/browsers/${bID}/revoke`, "Native other-browser revoke targets the exact confirmed public ID");
    check((await a.inventory()).data?.browsers.length === 1, "Other-browser revoke preserves the actor and removes exactly one inventory record");
    await b.click(selector("[data-browser-refresh]")); await b.loggedOut();
    check((await b.inventory()).status === 401, "Revoked browser cannot read inventory and returns to native pairing");
    check(await a.evaluate("accessFixtureErrorCount===0"), "Native inventory and successful revocation produce no page errors");

    // Stop the real Go test process; retain only its private durable root and
    // existing native Chrome contexts. This is not an in-memory shell restore.
    const restart1 = await restart();
    check(restart1.code === code, "Actual manager process restart preserves the reusable pairing credential");
    origin = restart1.origin; code = restart1.code;
    await a.access(); await a.setTheme();
    check((await a.inventory()).data?.browsers[0]?.id === aID, "Unrevoked browser cookie and public identity survive actual manager process restart");
    await b.navigate("/?view=access"); await b.loggedOut();
    check((await b.inventory()).status === 401, "Individually revoked browser remains revoked after actual manager process restart");
    await b.login(); await b.setTheme(); await b.access();
    const reparied = await b.inventory(), newBID = reparied.data?.browsers.find(browser => browser.current)?.id;
    check(newBID && newBID !== bID && newBID !== aID, "Explicit native re-pair obtains a fresh public ID instead of resurrecting revoked identity");
    await a.refresh(2);
    await b.select(newBID); await b.bounds("Current-browser confirmation");
    check(await b.evaluate(`${b.q(selector('[data-browser-confirm-description]'))}.textContent.includes('current browser') && ${b.q(selector('[data-browser-confirm-description]'))}.textContent.includes('signs you out')`), "Native current-browser confirmation explicitly warns of logout");
    await b.confirm(); await b.loggedOut();
    check((await b.inventory()).status === 401, "Native self-revocation removes current browser authority");
    // A still has the prior real server-rendered inventory. Its native stale
    // confirmation must fail exactly, never fall back to another target.
    await a.select(newBID); const beforeStale = postCount(); await a.confirm();
    await a.wait(`${a.q(selector('[data-browser-status]'))}?.textContent.includes('no longer paired')`, "native stale-target response is explicit");
    check(postCount() === beforeStale + 1, "Stale native confirmation is one failed request, never automatically replayed");
    check((await a.inventory()).data?.browsers[0]?.id === aID, "Stale target does not revoke the remaining unrelated actor");
    await a.refresh(1); await a.select(aID); await a.confirm(); await a.loggedOut();
    check((await a.inventory()).status === 401, "Last remaining browser can explicitly revoke itself and is signed out");
    const restart2 = await restart(); origin = restart2.origin; code = restart2.code;
    await a.navigate("/?view=access"); await a.loggedOut(); await b.navigate("/?view=access"); await b.loggedOut();
    check((await a.inventory()).status === 401 && (await b.inventory()).status === 401, "Both self-revocations survive a second actual process restart");
    await a.login(); await a.setTheme(); await a.access();
    const final = await a.inventory();
    check(final.data?.browsers.length === 1 && ![aID, bID, newBID].includes(final.data.browsers[0].id), "Explicit native pairing after all individual revocations creates only one fresh browser");
    await a.bounds("Fresh inventory after durable revocations");
    check(await a.evaluate("accessFixtureErrorCount===0"), "Final real production page has no JavaScript errors or unhandled rejections");
    const posts = requests.filter(request => request.method === "POST");
    check(posts.filter(request => request.path === "/login").length === 4, "Exactly four explicit native pairing submissions across fresh manager processes");
    check(posts.filter(request => request.path.startsWith("/access/browsers/")).length === 4, "Exactly three explicit successful revokes and one stale attempt; no mutation replay");
    check(posts.every(request => request.path === "/login" || /^\/access\/browsers\/browser_[a-f0-9]{32}\/revoke$/.test(request.path)), "Native access flow never invokes activation, logout fallback, pairing rotation or revoke-all");
    check(posts.filter(request => request.path.startsWith("/access/browsers/")).every(request => request.keys.join(",") === "confirm,csrf"), "Individual revoke submits only confirmation and CSRF, never credentials as target IDs");
    check(!requests.some(request => request.path.includes("/runtime")), "Opening/using browser access never starts or contacts a project runtime");
    return {results, failures};
  } catch (error) { error.results = results; error.failures = failures; throw error; }
  finally { dispose(); }
}
