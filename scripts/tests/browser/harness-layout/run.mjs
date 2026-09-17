// Network-free production-template layout regression. Node 22+ and an existing
// Chrome are required. No npm dependencies, credentials, workers, or installs.
import {spawn} from "node:child_process";
import {accessSync, constants} from "node:fs";
import {mkdir, mkdtemp, readFile, readdir, rm, writeFile} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {dirname, extname, isAbsolute, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";

const here = dirname(fileURLToPath(import.meta.url));
const repository = resolve(here, "../../../..");
// The complete surface matrix now includes 1,806 reports and optional PNGs.
// Retain a finite whole-run bound without giving the full suite a smoke budget.
const timeoutMS = 1800000;
function options() {
  const result = {screenshots: false};
  const args = process.argv.slice(2);
  while (args.length) {
    const flag = args.shift();
    if (flag === "--screenshots") result.screenshots = true;
    else if (flag === "--runtime-only") result.runtimeOnly = true;
    else if (flag === "--trust-only") result.trustOnly = true;
    else if (flag === "--saved-only") result.savedOnly = true;
    else if (flag === "--smoke") result.smoke = true;
    else if (flag === "--width") { const value = Number(args.shift()); if (![320, 360, 390, 768, 1024, 1280, 1512].includes(value)) throw new Error("Unsupported matrix width"); result.width = value; }
    else if (flag === "--theme") { const value = args.shift(); if (!["dark", "light"].includes(value)) throw new Error("Unsupported theme"); result.theme = value; }
    else if (["--output-dir", "--fixtures-dir"].includes(flag)) {
      const value = args.shift();
      if (!value || !isAbsolute(value)) throw new Error(`${flag} requires an absolute directory`);
      result[flag === "--output-dir" ? "output" : "fixtures"] = value;
    } else throw new Error(`Unknown argument: ${flag}`);
  }
  if (result.screenshots && !result.output) throw new Error("--screenshots requires --output-dir (artifacts are opt-in)");
  return result;
}
function chromeBinary() {
  const candidates = process.env.SNOW_CHROME_BIN ? [process.env.SNOW_CHROME_BIN] : [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/opt/google/chrome/chrome"
  ];
  for (const candidate of candidates) {
    try { accessSync(candidate, constants.X_OK); return candidate; } catch (_) { /* Fixed candidates only. */ }
  }
  throw new Error("Chrome not found; set SNOW_CHROME_BIN to its executable path. Nothing is installed automatically.");
}
async function exportFixtures(destination) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "./internal/web", "-run", "^TestExportHarnessVisualFixtures$", "-count=1"], {
      cwd: repository, env: {...process.env, SNOW_WEB_FIXTURE_DIR: destination}, stdio: ["ignore", "pipe", "pipe"], timeout: timeoutMS
    });
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", (code, signal) => code === 0 ? resolve() : reject(new Error(`Go fixture export failed (${code ?? signal}):\n${output}`)));
  });
}
async function serveFixtures(directory, fixtures) {
  // Load only explicit fixture documents and static regular files into memory.
  // Requests cannot browse the repository or invoke any real application route.
  const files = new Map();
  let total = 0;
  const load = async relative => {
    const body = await readFile(join(directory, relative));
    total += body.length;
    if (total > 16 * 1024 * 1024) throw new Error("Fixture assets exceed 16MiB bound");
    files.set("/" + relative, body);
  };
  for (const fixture of fixtures) await load(fixture.name + ".html");
  const walk = async relative => {
    for (const entry of await readdir(join(directory, relative), {withFileTypes: true})) {
      const name = relative + "/" + entry.name;
      if (entry.isDirectory()) await walk(name);
      else if (entry.isFile()) await load(name);
    }
  };
  await walk("static");
  const unexpected = [];
  const server = createServer((request, response) => {
    const pathname = new URL(request.url, "http://127.0.0.1").pathname;
    if (pathname === "/favicon.ico") { response.writeHead(204).end(); return; }
    const body = files.get(pathname);
    if (request.method !== "GET" || !body) {
      unexpected.push(`${request.method} ${pathname}`);
      response.writeHead(404).end("Fixture route unavailable"); return;
    }
    const types = {".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".svg": "image/svg+xml"};
    response.writeHead(200, {"Content-Type": types[extname(pathname)] || "application/octet-stream", "Cache-Control": "no-store"});
    response.end(body);
  });
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
  return {server, unexpected, origin: `http://127.0.0.1:${server.address().port}`};
}
function debuggingURL(chrome) {
  return new Promise((resolve, reject) => {
    let stderr = "";
    chrome.stderr.on("data", chunk => {
      stderr = (stderr + chunk).slice(-65536);
      const match = stderr.match(/DevTools listening on (ws:\/\/[^\s]+)/);
      if (match) resolve(match[1]);
    });
    chrome.once("error", reject);
    chrome.once("exit", (code, signal) => reject(new Error(`Chrome exited (${code ?? signal}): ${stderr}`)));
  });
}
async function connect(url) {
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = () => reject(new Error("Cannot connect to isolated Chrome debugging socket"));
  });
  let sequence = 0;
  const pending = new Map();
  socket.onmessage = event => {
    const message = JSON.parse(event.data), handler = pending.get(message.id);
    if (!handler) return;
    pending.delete(message.id); clearTimeout(handler.timer);
    if (message.error) handler.reject(new Error(JSON.stringify(message.error)));
    else handler.resolve(message.result);
  };
  socket.onclose = () => {
    for (const handler of pending.values()) { clearTimeout(handler.timer); handler.reject(new Error("Chrome debugging socket closed")); }
    pending.clear();
  };
  return {
    close() { socket.close(); },
    send(method, params = {}, sessionId) {
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        const timer = setTimeout(() => { pending.delete(id); reject(new Error(`CDP timeout: ${method}`)); }, 10000);
        pending.set(id, {resolve, reject, timer});
        socket.send(JSON.stringify({id, method, params, sessionId}));
      });
    }
  };
}
async function run() {
  const args = options();
  if (typeof WebSocket !== "function") throw new Error("Node 22+ with built-in WebSocket is required");
  const binary = chromeBinary();
  // The failure report must not mask an earlier export/manifest error with
  // ENOENT when this is the first run in a new evidence directory. Create it
  // before allocating temporary resources so a permission failure leaks none.
  if (args.output) await mkdir(args.output, {recursive: true, mode: 0o700});
  const temporary = await mkdtemp(join(tmpdir(), "snow-harness-layout-"));
  const reports = [];
  let chrome, client, timer, exited, server;
  try {
    const directory = args.fixtures || join(temporary, "fixtures");
    if (!args.fixtures) await exportFixtures(directory);
    let fixtures = JSON.parse(await readFile(join(directory, "fixtures.json"), "utf8")).filter(fixture => !["workflow", "workflow-cancel", "workflow-edit", "workflow-regenerate", "workflow-queue"].includes(fixture.name));
    const expected = ["home", "empty-home", "chat", "plan", "attention", "inspector", "markdown", "model-empty", "model-unavailable", "model-disconnected", "runtime-panels", "runtime-unsupported", "inactive", "inactive-trusted", "saved-markdown", "saved-user", "many-home", "registration", "registration-error", "pairing", "login", "login-error", "questions", "permission-unknown", "permission-truncated", "stream", "usage-known"];
    if (!Array.isArray(fixtures) || JSON.stringify(fixtures.map(value => value.name)) !== JSON.stringify(expected)) throw new Error("Unexpected fixture manifest; rerun the Go exporter");
    if (args.runtimeOnly) fixtures = fixtures.filter(fixture => fixture.name.startsWith("runtime-"));
    if (args.trustOnly) fixtures = fixtures.filter(fixture => fixture.name.startsWith("inactive"));
    if (args.savedOnly) fixtures = fixtures.filter(fixture => ["saved-markdown", "saved-user"].includes(fixture.name));
    const runtimeTests = await readFile(join(here, "runtime-panels.js"), "utf8");
    const mock = await readFile(join(here, "fixture.js"), "utf8");
    const tests = await readFile(join(here, "tests.js"), "utf8");
    const controls = await readFile(join(here, "controls.js"), "utf8");
    const served = await serveFixtures(directory, fixtures); server = served.server;
    if (args.output) await mkdir(args.output, {recursive: true, mode: 0o700});
    chrome = spawn(binary, [
      "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
      "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
      "--remote-debugging-port=0", `--user-data-dir=${join(temporary, "profile")}`, "about:blank"
    ], {stdio: ["ignore", "ignore", "pipe"]});
    exited = new Promise(resolve => { chrome.once("exit", resolve); chrome.once("error", resolve); });
    const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`Browser regression exceeded ${timeoutMS / 1000}s`)), timeoutMS); });
    const exercise = async () => {
      client = await connect(await debuggingURL(chrome));
      const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
      const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
      await client.send("Page.enable", {}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: "dark"}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      for (const width of args.width ? [args.width] : args.smoke ? [390] : [320, 360, 390, 768, 1024, 1280, 1512]) {
      for (const theme of args.theme ? [args.theme] : args.smoke ? ["dark"] : ["dark", "light"]) {
        await client.send("Emulation.setDeviceMetricsOverride", {width, height: 740, deviceScaleFactor: 1, mobile: false}, sessionId);
        for (const original of fixtures) {
        for (const pageHeight of (original.name.startsWith("runtime-") || original.name === "inactive-trusted" || expected.indexOf(original.name) >= expected.indexOf("saved-user")) ? [740, 360, 240] : [740]) {
          const fixture = {...original, theme};
          await client.send("Emulation.setDeviceMetricsOverride", {width, height: pageHeight, deviceScaleFactor: 1, mobile: false}, sessionId);
          const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {
            source: `window.__harnessConfig = ${JSON.stringify(fixture)};\n${mock}`
          }, sessionId);
          try {
            const navigation = await client.send("Page.navigate", {url: `${served.origin}/${fixture.name}.html`}, sessionId);
            if (navigation.errorText) throw new Error(navigation.errorText);
            let ready = false;
            for (let i = 0; i < 100; i++) {
              await delay(30);
              const state = await client.send("Runtime.evaluate", {expression: 'document.readyState === "complete" && !!window.harnessFixture', returnByValue: true}, sessionId);
              if (state.result?.value) { ready = true; break; }
            }
            if (!ready) throw new Error("Production fixture failed to load");
            if (args.screenshots && ["questions", "permission-unknown", "permission-truncated", "stream", "inactive", "many-home", "registration", "pairing", "login"].includes(fixture.name)) {
              const screenshot = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
              await writeFile(join(args.output, `${fixture.name}-initial-${width}x${pageHeight}-${theme}.png`), Buffer.from(screenshot.data, "base64"), {mode: 0o600});
            }
            const nativeResults = [], nativeFailures = [], nativeMeasurements = {};
            const nativeCheck = async (expression, label) => {
              await delay(40);
              const value = await client.send("Runtime.evaluate", {expression, returnByValue: true}, sessionId);
              (value.result?.value ? nativeResults : nativeFailures).push(label);
            };
            if (fixture.name === "questions") {
              await client.send("Runtime.evaluate", {expression: 'document.querySelector("[data-attention-collapse]").focus()'}, sessionId);
              for (let i = 0; i < 3; i++) {
                await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9}, sessionId);
                await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9}, sessionId);
              }
              await nativeCheck('document.querySelector("#live-attention").contains(document.activeElement) && !document.querySelector("#live-composer").contains(document.activeElement)', "Native Tab traverses actual question controls, never the hidden normal composer");
              if (pageHeight === 740) {
                await client.send("Runtime.evaluate", {expression: 'Object.defineProperty(visualViewport, "height", {configurable:true,value:420});visualViewport.dispatchEvent(new Event("resize"));'}, sessionId);
                await nativeCheck('visualViewport.height < innerHeight && document.querySelector(".attention-footer").getBoundingClientRect().bottom <= visualViewport.offsetTop + visualViewport.height + 1', "Simulated scale-1 reduced visualViewport retains question footer within visible height (not a physical keyboard test)");
                const measured = await client.send("Runtime.evaluate", {expression: '({height:innerHeight, visualHeight:visualViewport.height, offsetTop:visualViewport.offsetTop, footer:document.querySelector(".attention-footer").getBoundingClientRect().toJSON(), seat:document.querySelector("#live-composer-seat").getBoundingClientRect().toJSON()})', returnByValue: true}, sessionId);
                nativeMeasurements.reducedViewport = measured.result?.value;
                if (args.screenshots) {
                  const screenshot = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
                  await writeFile(join(args.output, `${fixture.name}-reduced-visual-viewport-${width}x${pageHeight}-${theme}.png`), Buffer.from(screenshot.data, "base64"), {mode: 0o600});
                }
                await client.send("Runtime.evaluate", {expression: 'delete visualViewport.height;visualViewport.dispatchEvent(new Event("resize"));'}, sessionId);
                await delay(60);
              }
            }
            const runtimeScreenshots = args.screenshots && fixture.name === "runtime-panels";
            if (runtimeScreenshots) await client.send("Runtime.evaluate", {expression: "window.__harnessRuntimeScreenshots = true"}, sessionId);
            const evaluation = client.send("Runtime.evaluate", {expression: fixture.name.startsWith("runtime-") ? runtimeTests : tests, returnByValue: true, awaitPromise: true}, sessionId);
            if (runtimeScreenshots) {
              // The actual open-state test pauses before dismissal so evidence
              // depicts each modal, not the closed conversation after the test.
              let finished = false;
              evaluation.then(() => { finished = true; }, () => { finished = true; });
              while (!finished) {
                await delay(20);
                const pending = await client.send("Runtime.evaluate", {expression: "window.__harnessRuntimeCapture", returnByValue: true}, sessionId);
                const name = pending.result?.value;
                if (!name) continue;
                if (!/^(versions|goals|processes|reasoning|compaction|steer)-(open|scrolled)$/.test(name)) throw new Error("Unexpected runtime screenshot state");
                const image = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
                await writeFile(join(args.output, `runtime-${name}-${width}x${pageHeight}-${theme}.png`), Buffer.from(image.data, "base64"), {mode: 0o600});
                await client.send("Runtime.evaluate", {expression: "window.__harnessRuntimeCapture = null"}, sessionId);
              }
            }
            const result = await evaluation;
            if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
            const report = result.result?.value;
            if (!report || !Array.isArray(report.failures) || !Array.isArray(report.results) || report.results.length !== report.passed) throw new Error("Malformed layout report");
            Object.assign(report.measurements, nativeMeasurements);
            report.results.push(...nativeResults); report.passed += nativeResults.length; report.failures.push(...nativeFailures);
            reports.push(report);
            console.log(`harness-layout ${fixture.name} ${width}×${pageHeight} ${theme}: ${report.passed} passed, ${report.failures.length} failed`);
            for (const failure of report.failures) console.error(`  ${failure}`);
            if (args.screenshots) {
              const screenshot = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
              await writeFile(join(args.output, `${fixture.name}-${width}x${pageHeight}-${theme}.png`), Buffer.from(screenshot.data, "base64"), {mode: 0o600});
            }
            const states = fixture.name === "chat" ? ["model-unloaded", "model-root", "model-groups", "mode", "session", "sessions", "rename", "telemetry", "inspector-files", "inspector-changes", "inspector-project", "workspace-actions", "composer-growth"] : ["home", "many-home"].includes(fixture.name) ? ["settings-general", "settings-workspaces", "settings-access"] : fixture.name.startsWith("registration") ? ["folder-populated", "folder-empty", "folder-limited", "folder-error"] : fixture.name.startsWith("model-") ? [fixture.name] : [];
            for (const height of states.length && pageHeight === 740 ? [740, 360, 240] : []) {
              await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
              await client.send("Page.navigate", {url: `${served.origin}/${fixture.name}.html`}, sessionId);
              for (let i = 0; i < 100; i++) {
                await delay(20);
                const ready = await client.send("Runtime.evaluate", {expression: 'document.readyState === "complete" && !!window.harnessFixture && (!harnessFixture.snapshot || document.querySelector("#live-connection")?.textContent === "Live")', returnByValue: true}, sessionId);
                if (ready.result?.value) break;
              }
              for (const state of states) {
                const result = await client.send("Runtime.evaluate", {expression: `window.__harnessControl=${JSON.stringify(state)};${controls}`, returnByValue: true, awaitPromise: true}, sessionId);
                if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
                const report = result.result?.value;
                if (!report || !Array.isArray(report.failures)) throw new Error("Malformed control report");
                if (state === "settings-general") {
                  const nativeCheck = async (expression, label) => {
                    await delay(30);
                    const value = await client.send("Runtime.evaluate", {expression, returnByValue: true}, sessionId);
                    if (value.result?.value) { report.results.push(label); report.passed++; } else report.failures.push(label);
                  };
                  for (let i = 0; i < 8; i++) {
                    await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9}, sessionId);
                    await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9}, sessionId);
                  }
                  const tabbed = await client.send("Runtime.evaluate", {expression: 'document.querySelector("#settings-dialog").contains(document.activeElement) && document.activeElement.getBoundingClientRect().bottom <= innerHeight && document.activeElement.getBoundingClientRect().top >= 0', returnByValue: true}, sessionId);
                  const tabLabel = "Native Tab remains in Settings and scrolls focused controls into the short viewport";
                  if (tabbed.result?.value) { report.results.push(tabLabel); report.passed++; } else {
                    report.failures.push(tabLabel);
                    const focus = await client.send("Runtime.evaluate", {expression: '({tag:document.activeElement?.tagName,id:document.activeElement?.id,rect:document.activeElement?.getBoundingClientRect().toJSON(),dialog:document.querySelector("#settings-dialog").getBoundingClientRect().toJSON()})', returnByValue: true}, sessionId);
                    console.error('Settings focus geometry: ' + JSON.stringify(focus.result?.value));
                  }
                  await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
                  await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
                  await nativeCheck('!document.querySelector("#settings-dialog").open && document.activeElement === document.querySelector("[data-settings-open]")', "Native Escape closes Settings and restores trigger focus");
                  await client.send("Runtime.evaluate", {expression: 'document.querySelector("[data-settings-open]").click()'}, sessionId);
                  await client.send("Input.dispatchMouseEvent", {type: "mousePressed", x: 1, y: 1, button: "left", clickCount: 1}, sessionId);
                  await client.send("Input.dispatchMouseEvent", {type: "mouseReleased", x: 1, y: 1, button: "left", clickCount: 1}, sessionId);
                  await nativeCheck('!document.querySelector("#settings-dialog").open && document.activeElement === document.querySelector("[data-settings-open]")', "Backdrop pointer closes Settings and restores trigger focus");
                  await client.send("Runtime.evaluate", {expression: 'document.querySelector("[data-settings-open]").click()'}, sessionId);
                }
                reports.push(report);
                console.log(`harness-layout ${report.name} ${width}×${height} ${theme}: ${report.passed} passed, ${report.failures.length} failed`);
                for (const failure of report.failures) console.error(`  ${failure}`);
                if (args.screenshots) {
                  const screenshot = await client.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false}, sessionId);
                  await writeFile(join(args.output, `${report.name}-${width}x${height}-${theme}.png`), Buffer.from(screenshot.data, "base64"), {mode: 0o600});
                }
              }
            }
            await client.send("Emulation.setDeviceMetricsOverride", {width, height: 740, deviceScaleFactor: 1, mobile: false}, sessionId);
          } finally {
            await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
            await client.send("Page.navigate", {url: "about:blank"}, sessionId);
          }
        }
      }
      }
      }
      if (served.unexpected.length) throw new Error(`Unexpected HTTP requests: ${served.unexpected.join(", ")}`);
      const failed = reports.filter(report => report.failures.length);
      if (failed.length) throw new Error(`${failed.length} of ${reports.length} viewport/state combinations failed`);
    };
    await Promise.race([exercise(), deadline]);
  } finally {
    clearTimeout(timer);
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (exited) await Promise.race([exited, delay(2000)]);
    if (server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
    if (args.output) await writeFile(join(args.output, "layout-report.json"), JSON.stringify(reports, null, 2) + "\n", {mode: 0o600});
    await rm(temporary, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}
try { await run(); } catch (error) { console.error(`harness-layout: ${error.message}`); process.exitCode = 1; }
