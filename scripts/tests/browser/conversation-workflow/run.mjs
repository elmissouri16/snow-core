// Standalone, network-free regression. Requires Node with built-in WebSocket.
// Run from any directory; SNOW_CHROME_BIN may select an explicit Chrome binary.
import {spawn} from "node:child_process";
import {accessSync, constants} from "node:fs";
import {mkdtemp, rm, readFile, writeFile, readdir} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname, extname} from "node:path";
import {fileURLToPath} from "node:url";
import {createServer} from "node:http";
import {setTimeout as delay} from "node:timers/promises";

const here = dirname(fileURLToPath(import.meta.url));
const repository = resolve(here, "../../../..");
async function exportFixture(directory) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "./internal/web", "-run", "^TestExportHarnessVisualFixtures$", "-count=1"], {cwd: repository, env: {...process.env, SNOW_WEB_FIXTURE_DIR: directory}, stdio: ["ignore", "pipe", "pipe"], timeout: 120000});
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", code => code === 0 ? resolve() : reject(new Error(`Go fixture export failed: ${output}`)));
  });
  let html = await readFile(join(directory, "workflow.html"), "utf8");
  html = html.replace(/<script src="\/static\/vendor\/htmx[^>]*><\/script>/, "");
  // The fixture only replaces public transport and HTMX; all page markup and
  // production script order are exported on this run, never copied by hand.
  html = html.replace("</body>", `<pre id="test-result" hidden></pre><script src="/fixture.js"></script><script src="/history-tools.js"></script><script type="module" src="/tests.js"></script></body>`);
  await writeFile(join(directory, "workflow.html"), html, {mode: 0o600});
  const files = new Map();
  let bytes = 0;
  async function load(relative) {
    for (const entry of await readdir(join(directory, relative), {withFileTypes: true})) {
      const name = relative ? relative + "/" + entry.name : entry.name;
      if (entry.isDirectory()) await load(name);
      else if (entry.isFile()) {
        const body = await readFile(join(directory, name)); bytes += body.length;
        if (bytes > 16 * 1024 * 1024) throw new Error("Fixture assets exceed 16MiB bound");
        files.set("/" + name, body);
      }
    }
  }
  files.set("/workflow.html", Buffer.from(html));
  await load("static");
  for (const name of ["fixture.js", "history-tools.js", "tests.js"]) files.set("/" + name, await readFile(join(here, name)));
  const unexpected = [];
  const server = createServer((request, response) => {
    const path = new URL(request.url, "http://fixture").pathname;
    if (path === "/favicon.ico") { response.writeHead(204).end(); return; }
    const body = files.get(path);
    if (request.method !== "GET" || !body) {
      unexpected.push(request.method + " " + path); response.writeHead(404).end(); return;
    }
    const type = {".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream";
    response.writeHead(200, {"Content-Type": type, "Cache-Control": "no-store"}).end(body);
  });
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
  return {server, unexpected, url: `http://127.0.0.1:${server.address().port}/workflow.html`};
}
const expectedAssertions = 124;
const timeoutMS = 240000;

function chromeBinary() {
  const candidates = process.env.SNOW_CHROME_BIN ? [process.env.SNOW_CHROME_BIN] : [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
    "/opt/google/chrome/chrome"
  ];
  for (const candidate of candidates) {
    try {
      accessSync(candidate, constants.X_OK);
      return candidate;
    } catch (_) { /* Try the next fixed candidate, never a shell command. */ }
  }
  throw new Error("Chrome executable not found. Set SNOW_CHROME_BIN to its executable path.");
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
    chrome.once("exit", (code, signal) => {
      reject(new Error(`Chrome exited before debugging was ready (${code ?? signal}).`));
    });
  });
}

async function connect(url) {
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = () => reject(new Error("Cannot connect to isolated Chrome debugging socket."));
  });
  let sequence = 0;
  const pending = new Map(), exceptions = [];
  socket.onmessage = event => {
    const message = JSON.parse(event.data);
    if (message.method === "Runtime.exceptionThrown" && exceptions.length < 30) exceptions.push(message.params.exceptionDetails);
    const handlers = pending.get(message.id);
    if (!handlers) return;
    pending.delete(message.id);
    const [resolve, reject] = handlers;
    if (message.error) reject(new Error(JSON.stringify(message.error)));
    else resolve(message.result);
  };
  socket.onclose = () => {
    for (const [, reject] of pending.values()) reject(new Error("Chrome debugging socket closed."));
    pending.clear();
  };
  return {
    exceptions,
    close() { socket.close(); },
    send(method, params = {}, sessionId) {
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        pending.set(id, [resolve, reject]);
        socket.send(JSON.stringify({id, method, params, sessionId}));
      });
    }
  };
}

async function run() {
  if (typeof WebSocket !== "function") throw new Error("Use Node 22 or newer with built-in WebSocket support.");
  const binary = chromeBinary();
  const profile = await mkdtemp(join(tmpdir(), "snow-conversation-workflow-"));
  let chrome, client, timer, exited, served;
  try {
    served = await exportFixture(join(profile, "fixtures"));
    const fixtureURL = served.url;
    chrome = spawn(binary, [
      "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
      "--disable-background-networking", "--disable-component-update",
      "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank"
    ], {stdio: ["ignore", "ignore", "pipe"]});
    exited = new Promise(resolve => {
      chrome.once("exit", resolve);
      chrome.once("error", resolve);
    });
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(`Browser regression exceeded ${timeoutMS / 1000} seconds.`)), timeoutMS);
    });
    const exercise = async () => {
      client = await connect(await debuggingURL(chrome));
      const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
      const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
      await client.send("Page.enable", {}, sessionId);
      await client.send("Runtime.enable", {}, sessionId);
      for (const width of process.env.SNOW_WORKFLOW_WIDTH ? [Number(process.env.SNOW_WORKFLOW_WIDTH)] : [320, 360, 390, 768, 1024, 1280, 1512]) {
      for (const theme of process.env.SNOW_WORKFLOW_THEME ? [process.env.SNOW_WORKFLOW_THEME] : ["dark", "light"]) {
      const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__workflowTheme=${JSON.stringify(theme)};localStorage.setItem("snow-manager-theme",${JSON.stringify(theme)});`}, sessionId);
      await client.send("Emulation.setDeviceMetricsOverride", {width, height: width === 360 ? 740 : 900, deviceScaleFactor: 1, mobile: false}, sessionId);
      const navigation = await client.send("Page.navigate", {url: fixtureURL}, sessionId);
      if (navigation.errorText) throw new Error(`Fixture navigation failed: ${navigation.errorText}`);
      for (let attempt = 0; attempt < 180; attempt++) {
        await delay(100);
        const key = await client.send("Runtime.evaluate", {expression: "window.fixture?.nativeEscape === true", returnByValue: true}, sessionId);
        if (key.result?.value) {
          await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
          await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
          await client.send("Runtime.evaluate", {expression: "fixture.nativeEscape = false"}, sessionId);
        }
        const result = await client.send("Runtime.evaluate", {
          expression: 'document.querySelector("#test-result")?.textContent || ""',
          returnByValue: true
        }, sessionId);
        const value = result.result?.value;
        if (!value) continue;
        const report = JSON.parse(value);
        if (!Array.isArray(report.failures) || !Array.isArray(report.results)) throw new Error("Malformed browser regression report.");
        if (report.failures.length) throw new Error(`Browser assertions failed:\n${report.failures.join("\n")}\n${JSON.stringify(client.exceptions)}`);
        if (report.passed !== expectedAssertions || report.results.length !== expectedAssertions) {
          throw new Error(`Expected ${expectedAssertions} assertions; received ${report.passed}.`);
        }
        console.log(`conversation-workflow ${width}px ${theme}: ${report.passed} passed`);
        break;
      }
      const final = await client.send("Runtime.evaluate", {expression: 'document.querySelector("#test-result")?.textContent || ""', returnByValue: true}, sessionId);
      if (!final.result?.value) throw new Error("No browser regression result was produced.");
      await client.send("Page.navigate", {url: "about:blank"}, sessionId);
      await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
      }
      }
    };
    await Promise.race([exercise(), deadline]);
    if (served.unexpected.length) throw new Error("Unexpected fixture requests: " + served.unexpected.join(", "));
  } finally {
    clearTimeout(timer);
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (exited) await Promise.race([exited, delay(2000)]);
    if (served) { served.server.closeAllConnections(); await new Promise(resolve => served.server.close(resolve)); }
    await rm(profile, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}

try {
  await run();
} catch (error) {
  console.error(`conversation-workflow: ${error.message}`);
  process.exitCode = 1;
}
