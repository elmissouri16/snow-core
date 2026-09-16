// Production Go templates + assets, actual browser/native navigation, mocked public transport.
// No manager, worker, provider, npm install, or external application requests.
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {mkdtemp, mkdir, readdir, readFile, writeFile, rm} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {dirname, extname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-workspace-actions-"));
const evidence = process.env.SNOW_WORKSPACE_EVIDENCE === "0" ? null : join(root, "dist/workspace-action-evidence");
let chrome, client, server, timer, sessionId, assertions = 0;
const reports = [], failures = [], captures = [], serverRequests = [];
try {
  const binary = chromeBinary(), directory = join(temporary, "fixtures");
  await new Promise((resolve, reject) => {
    const exporter = spawn("go", ["test", "./internal/web", "-run", "^TestExportHarnessVisualFixtures$", "-count=1"], {
      cwd: root, env: {...process.env, SNOW_WEB_FIXTURE_DIR: directory},
      stdio: ["ignore", "pipe", "pipe"], timeout: 120000
    });
    let output = "";
    for (const stream of [exporter.stdout, exporter.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    exporter.once("error", reject);
    exporter.once("exit", code => code === 0 ? resolve() : reject(new Error(`Fixture export failed: ${output}`)));
  });
  const manifest = JSON.parse(await readFile(join(directory, "fixtures.json"), "utf8"));
  const fixture = manifest.find(item => item.name === "stream");
  assert.ok(fixture?.snapshot?.project_id, "Exporter provides the real stream project ID");
  assert.equal(fixture.snapshot.status, "running", "Fixture starts with an authoritative active turn");
  const project = fixture.snapshot.project_id;
  const productionHTML = await readFile(join(directory, "stream.html"), "utf8");
  assert.ok(productionHTML.includes('<script type="module" src="/static/generated/app.js">'), "Export loads production React module");
  assert.ok(!/<script[^>]+src="\/static\/(shell|sidebar-sessions|session-actions)\.js"/.test(productionHTML), "No retired shell scripts loaded");
  const files = new Map([["/stream.html", await readFile(join(directory, "stream.html"))]]);
  let bytes = 0;
  async function load(relative) {
    for (const entry of await readdir(join(directory, relative), {withFileTypes: true})) {
      const name = relative + "/" + entry.name;
      if (entry.isDirectory()) await load(name);
      else if (entry.isFile()) {
        const body = await readFile(join(directory, name)); bytes += body.length;
        if (bytes > 16 * 1024 * 1024) throw new Error("Fixture assets exceed 16MiB bound");
        files.set("/" + name, body);
      }
    }
  }
  await load("static");
  server = createServer((request, response) => {
    const url = new URL(request.url, "http://localhost");
    if (serverRequests.length >= 2048) { response.writeHead(429); response.end(); return; }
    serverRequests.push({path: url.pathname, search: url.search, method: request.method});
    // A canonical URL alias, not an invented manager: only this exported
    // project's exact HTML is served. Other project states are not fabricated.
    const path = url.pathname === "/" && url.searchParams.get("project") === project ? "/stream.html" : url.pathname;
    const body = files.get(path);
    if (request.method !== "GET" || !body) { response.writeHead(404); response.end(); return; }
    const type = {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream";
    response.writeHead(200, {"Content-Type": type, "Cache-Control": "no-store"}); response.end(body);
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const mock = await readFile(join(here, "../harness-layout/fixture.js"), "utf8");
  const origin = `http://127.0.0.1:${server.address().port}`;
  const url = origin + "/?" + new URLSearchParams({view: "projects", project});
  if (evidence) await mkdir(evidence, {recursive: true, mode: 0o700});
  chrome = spawn(binary, ["--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    "--remote-debugging-port=0", `--user-data-dir=${join(temporary, "profile")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Workspace browser checks exceeded 120s")), 120000); });
  await Promise.race([deadline, (async () => {
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    ({sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true}));
    await client.send("Page.enable", {}, sessionId);
    await client.send("Runtime.enable", {}, sessionId);
    const exceptions = [], diagnostics = [], external = [];
    await client.send("Network.enable", {}, sessionId);
    client.onEvent(event => {
      if (event.sessionId !== sessionId) return;
      if (event.method === "Runtime.exceptionThrown" && exceptions.length < 20) exceptions.push(event.params.exceptionDetails);
      if (event.method === "Runtime.consoleAPICalled" && ["error", "warning"].includes(event.params.type) && diagnostics.length < 20) diagnostics.push(event.params.args.map(value => value.value || value.description).join(" "));
      if (event.method === "Network.requestWillBeSent" && !event.params.request.url.startsWith(origin + "/") && external.length < 20) external.push(event.params.request.url);
    });
    async function evaluate(expression) {
      const answer = await client.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}, sessionId);
      if (answer.exceptionDetails) throw new Error(JSON.stringify(answer.exceptionDetails));
      return answer.result.value;
    }
    async function check(expression, label) {
      const value = await evaluate(expression);
      if (value !== true) {
        await screenshot("failure");
        throw new Error(`${label}; ${JSON.stringify(await evaluate("({requests: window.harnessFixture?.requests, navigation: window.navigationRequests, href: location.href, geometry: typeof currentMore === 'function' ? {more: currentMore()?.getBoundingClientRect().toJSON(), tree: $('.project-tree')?.getBoundingClientRect().toJSON(), nav: $('#project-navigation')?.getBoundingClientRect().toJSON(), hit: (()=>{const r=currentMore()?.getBoundingClientRect();return r ? document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.outerHTML.slice(0,240) : null;})()} : null})"))}`);
      }
      assertions++;
    }
    async function wait(expression, label) {
      for (let attempt = 0; attempt < 160; attempt++) {
        if (await evaluate(expression)) return;
        await delay(30);
      }
      throw new Error(`Timed out: ${label}; ${JSON.stringify(await evaluate("({active: document.activeElement?.id || document.activeElement?.tagName, errors: window.harnessFixture?.errors, navigation: window.navigationRequests, posts: window.harnessFixture?.requests.filter(request => request.method === \"POST\"), menu: !!document.querySelector(\".snow-menu\")})"))}`);
    }
    async function key(key) {
      const codes = {Enter: 13, Escape: 27, ArrowDown: 40, Home: 36, End: 35, Tab: 9};
      await client.send("Input.dispatchKeyEvent", {type: "keyDown", key, code: key, windowsVirtualKeyCode: codes[key], ...(key === "Enter" ? {text: "\r", unmodifiedText: "\r"} : {})}, sessionId);
      await client.send("Input.dispatchKeyEvent", {type: "keyUp", key, code: key, windowsVirtualKeyCode: codes[key]}, sessionId);
    }
    async function screenshot(name) {
      if (!evidence) return;
      const {data} = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
      const image = Buffer.from(data, "base64");
      if (image.length > 8 * 1024 * 1024) throw new Error("Screenshot exceeds 8MiB bound");
      await writeFile(join(evidence, name + ".png"), image, {mode: 0o600});
      captures.push(name + ".png");
    }
    const pageHelpers = `
      window.$ = selector => document.querySelector(selector);
      window.currentGroup = () => $('[data-sidebar-project="' + harnessFixture.snapshot.project_id + '"]');
      window.currentMore = () => currentGroup().querySelector('[data-shell-project-menu]');
      window.currentNew = () => currentGroup().querySelector('[data-shell-project-new]');
      window.postCount = () => harnessFixture.requests.filter(request => request.method === 'POST').length;
      window.fileCount = () => harnessFixture.requests.filter(request => request.path.includes('/inspect/')).length;
      window.visibleHit = node => {
        const r = node.getBoundingClientRect();
        const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
        return r.width > 0 && r.height > 0 && r.left >= 0 && r.top >= 0 && r.right <= innerWidth + 1 && r.bottom <= innerHeight + 1 && (hit === node || node.contains(hit));
      };
      window.navigationRequests = [];
      document.addEventListener('snow:navigation-start', event => navigationRequests.push(event.detail.requestConfig.path));
    `;
    for (const width of [1280, 320]) for (const height of [740, 240]) {
      const start = assertions, label = `${width}x${height}`;
      await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: "dark"}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__harnessConfig = ${JSON.stringify({...fixture, theme: "dark"})};\n${mock}`}, sessionId);
      async function navigate(destination) {
        const result = await client.send("Page.navigate", {url: destination}, sessionId);
        if (result.errorText) throw new Error(result.errorText);
        await wait('document.readyState === "complete" && document.querySelector("[data-react-page=shell]")?.dataset.reactMounted === "true" && document.querySelector("#live-connection")?.textContent === "Live"', "fresh production page connected");
        await evaluate(pageHelpers);
      }
      async function focusRow(kind) {
        if (width < 768) {
          await evaluate(`if (!$('#project-navigation').classList.contains('nav-open')) $('[data-nav-toggle]').focus()`);
          if (!await evaluate("$('#project-navigation').classList.contains('nav-open')")) await key("Enter");
          await wait("$('#project-navigation').classList.contains('nav-open') && !$('#project-navigation').inert", "React mobile navigation committed before focus");
        }
        // App's native drawer owner schedules its initial Close focus in rAF.
        // Let that finish before choosing a row; do not race its focus handoff.
        await evaluate("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
        await evaluate(`${kind === "new" ? "currentNew()" : "currentMore()"}.focus()`);
        await wait(`document.activeElement === ${kind === "new" ? "currentNew()" : "currentMore()"} && visibleHit(document.activeElement)`, "row focus reaches committed navigation");
      }
      async function closeInspector() {
        await evaluate(`$('#project-inspector [data-inspector-toggle]').focus()`);
        await check("visibleHit(document.activeElement)", label + " inspector Close is reachable after focus scroll");
        await key("Enter");
        await check("$('#project-inspector').hidden && document.activeElement === $('.workspace-heading [data-inspector-toggle]')", label + " inspector closes and restores heading focus");
      }
      async function scenario(name, run) {
        try { await run(); }
        catch (error) {
          failures.push({width, height, scenario: name, error: error.message});
          await screenshot(label + "-failure-" + name);
          console.error(JSON.stringify(failures.at(-1)));
        }
      }
      await scenario("row-menu", async () => {
      await navigate(url);
      await focusRow("more");
      await check("document.activeElement === currentMore() && visibleHit(currentMore()) && getComputedStyle(currentMore().parentElement).opacity === '1'", label + " keyboard focus reveals and reaches actual row ellipsis");
      await check("!currentGroup().querySelector('.project-link').contains(currentMore()) && !currentGroup().querySelector('.project-link').contains(currentNew())", label + " actions are outside navigation anchor");
      await key("Enter");
      await wait("!!$('.snow-menu')", "workspace portal opened by keyboard");
      await check("$('.snow-menu').parentElement === document.body && currentMore().getAttribute('aria-expanded') === 'true'", label + " real body portal and trigger state");
      await check("JSON.stringify([...$('.snow-menu').querySelectorAll('[role=menuitem]')].map(node => node.textContent)) === JSON.stringify(['Workspace settings…', 'Remove registration…'])", label + " only supported workspace menu entries");
      await check("(() => { const r = $('.snow-menu').getBoundingClientRect(); return r.left >= 11 && r.top >= 11 && r.right <= innerWidth - 11 && r.bottom <= innerHeight - 11 && $('.snow-menu').scrollWidth <= $('.snow-menu').clientWidth + 1; })()", label + " portal remains within viewport and unclipped");
      await screenshot(label + "-workspace-menu");
      await key("End");
      await check("document.activeElement.textContent === 'Remove registration…' && visibleHit(document.activeElement)", label + " End reaches removal menu row");
      await key("Home");
      await check("document.activeElement.textContent === 'Workspace settings…'", label + " Home reaches first row");
      await key("ArrowDown");
      await check("document.activeElement.textContent === 'Remove registration…'", label + " ArrowDown moves between rows");
      await key("Escape");
      await check("!$('.snow-menu') && document.activeElement === currentMore() && currentMore().getAttribute('aria-expanded') === 'false'", label + " Escape cleans portal and restores row focus");
      await check("(() => { const links = [...document.querySelectorAll('[data-shell-project-new]')].filter(node => node.closest('[data-sidebar-project]') !== currentGroup()); return links.length === 99 && links.every(node => { const url = new URL(node.href); return url.searchParams.get('project') === node.closest('[data-sidebar-project]').dataset.sidebarProject && !url.searchParams.has('session') && !url.searchParams.has('live') && url.searchParams.get('new') === '1'; }); })()", label + " all 99 other-project New URLs retain session-free new-draft fallback");
      await evaluate("[...document.querySelectorAll('[data-sidebar-project]')].find(node => node !== currentGroup()).querySelector('[data-shell-project-menu]').focus()");
      await key("Enter");
      await check("[...$('.snow-menu').querySelectorAll('a')].length === 2 && [...$('.snow-menu').querySelectorAll('a')].every(node => { const url = new URL(node.href); return url.searchParams.get('project') !== harnessFixture.snapshot.project_id && url.searchParams.get('inspect') === 'project' && !url.searchParams.has('session') && !url.searchParams.has('live'); })", label + " other-project portal actions use session-free inspector URLs");
      await key("Escape");
      await focusRow("more");
      await key("Enter"); await key("End"); await key("Enter");
      await check("!$('#project-inspector').hidden && $('#inspection-tab-project').getAttribute('aria-selected') === 'true' && $('.remove-project').open && !$('.remove-project input[name=confirm]').checked", label + " actual Remove entry opens unchecked confirmation on Project tab");
      await check("postCount() === 0 && fileCount() === 0 && navigationRequests.length === 0", label + " mounted-project inspection neither navigates nor reads files nor submits");
      await screenshot(label + "-row-removal-confirmation");
      await closeInspector();
      });
      await scenario("new-confirmation", async () => {
      await navigate(url);
      await focusRow("new"); await key("Enter");
      await wait("$('#workflow-switch-dialog').open", "current row New opens authoritative stop/switch dialog");
      await check("harnessFixture.snapshot.status === 'running' && !$('#workflow-switch-dialog [data-workflow-switch-confirm]').disabled", label + " active turn uses available existing stop confirmation");
      await check("postCount() === 0 && navigationRequests.length === 0", label + " opening New confirmation sends no mutation or navigation");
      await screenshot(label + "-stop-new-confirmation");
      await evaluate("$('#workflow-switch-dialog [data-workflow-cancel]').focus()");
      await check("visibleHit(document.activeElement)", label + " stop/new Cancel remains keyboard reachable");
      await key("Enter");
      await check("!$('#workflow-switch-dialog').open && postCount() === 0 && navigationRequests.length === 0", label + " Cancel makes no runtime or navigation request");
      });
      await scenario("disconnected-new", async () => {
      await navigate(url);
      await evaluate("harnessFixture.offline = true");
      await wait("$('#live-connection').dataset.connected === 'false' && $('[data-workflow-switch-confirm]').disabled", "disconnected controls fail closed");
      await focusRow("new"); await key("Enter");
      await check("!$('#workflow-switch-dialog').open && postCount() === 0 && navigationRequests.length === 0", label + " disconnected row New cannot mutate or navigate");
      await check("harnessFixture.errors.length === 0", label + " row actions have no browser/mock errors");
      });
      await scenario("inspector-url", async () => {
      // Fresh canonical URL exercises app startup, not a synthetic presentation event.
      await navigate(url + "&inspect=project#remove-project");
      await check("!$('#project-inspector').hidden && $('#inspection-tab-project').getAttribute('aria-selected') === 'true' && $('.remove-project').open", label + " canonical URL opens Project removal confirmation on actual load");
      await check("!$('.remove-project input[name=confirm]').checked && document.activeElement === $('.remove-project summary')", label + " URL opening focuses summary without accepting confirmation");
      await check("postCount() === 0 && fileCount() === 0", label + " URL opening makes no filesystem request or POST");
      await screenshot(label + "-url-removal-confirmation");
      await closeInspector();
      await check("harnessFixture.errors.length === 0", label + " URL presentation has no browser/mock errors");
      });
      await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
      reports.push({width, height, assertions: assertions - start, failures: failures.filter(item => item.width === width && item.height === height).length});
      console.log(JSON.stringify(reports.at(-1)));
    }
    assert.equal(exceptions.length, 0, "No uncaught browser exceptions");
    assert.deepEqual(diagnostics, [], "No browser/React console diagnostics");
    assert.deepEqual(external, [], "No external browser requests");
    assert.ok(serverRequests.every(request => request.method === "GET"), "Fixture HTTP server received no POST");
    const result = {assertions, reports, failures, screenshots: evidence ? {directory: evidence, files: captures} : null, limitations: ["Other-project navigation URLs verified without fabricating selected-project backend responses.", "Public transport is mocked; no manager, provider, or live worker is used."]};
    if (evidence) await writeFile(join(evidence, "report.json"), JSON.stringify(result, null, 2) + "\n", {mode: 0o600});
    console.log(`${assertions} assertions passed; ${failures.length} scenarios failed across ${reports.length} production-browser reports.`);
    if (failures.length) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer); client?.close();
  if (chrome && chrome.exitCode === null) {
    const exited = new Promise(resolve => chrome.once("exit", resolve));
    chrome.kill("SIGKILL"); await exited;
  }
  if (server) await new Promise(resolve => server.close(resolve));
  await rm(temporary, {recursive: true, force: true});
}
