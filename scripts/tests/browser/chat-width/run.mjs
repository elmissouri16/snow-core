// Production templates/assets + mocked public snapshots. No manager or provider.
// Node 22+, Go fixture exporter, installed Chrome (SNOW_CHROME_BIN override).
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {mkdtemp, mkdir, readdir, readFile, writeFile, rm} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {dirname, extname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";
import {runScenarios} from "./scenarios.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-chat-width-"));
const evidence = process.env.SNOW_CHAT_WIDTH_EVIDENCE === "1" ? join(root, "dist/chat-width-evidence") : null;
let chrome, client, server, timer, sessionId, scriptID, assertions = 0;
const reports = [], failures = [], requests = [], captures = [];
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
  assert.ok(fixture?.snapshot?.project_id, "Exporter supplies a public production stream fixture");
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
    const path = new URL(request.url, "http://localhost").pathname;
    if (requests.length >= 2048) { response.writeHead(429); response.end(); return; }
    requests.push({path, method: request.method});
    const body = files.get(path);
    if (request.method !== "GET" || !body) { response.writeHead(404); response.end(); return; }
    const type = {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream";
    response.writeHead(200, {"Content-Type": type, "Cache-Control": "no-store"}); response.end(body);
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const mock = await readFile(join(here, "../harness-layout/fixture.js"), "utf8");
  const helpers = await readFile(join(here, "page.js"), "utf8");
  const url = `http://127.0.0.1:${server.address().port}/stream.html`;
  if (evidence) await mkdir(evidence, {recursive: true, mode: 0o700});
  chrome = spawn(binary, ["--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    "--remote-debugging-port=0", `--user-data-dir=${join(temporary, "profile")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Chat width checks exceeded 120s")), 120000); });
  await Promise.race([deadline, (async () => {
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    ({sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true}));
    const send = (method, params = {}) => client.send(method, params, sessionId);
    await send("Page.enable");
    async function evaluate(expression) {
      const answer = await send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true});
      if (answer.exceptionDetails) throw new Error(JSON.stringify(answer.exceptionDetails));
      return answer.result.value;
    }
    async function wait(expression, label) {
      for (let i = 0; i < 160; i++) { if (await evaluate(expression)) return; await delay(25); }
      throw new Error(`Timed out: ${label}`);
    }
    async function check(expression, label) {
      assert.equal(await evaluate(expression), true, `${label}; state=${JSON.stringify(await evaluate("window.widthState?.()"))}`);
      assertions++;
    }
    async function viewport(width, height = 900) {
      await send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false});
      await delay(100);
    }
    async function navigate({width = 1512, height = 900, stored = null, blocked = false, preserve = false, theme = "dark"} = {}) {
      await viewport(width, height);
      await send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]});
      if (scriptID) await send("Page.removeScriptToEvaluateOnNewDocument", {identifier: scriptID});
      const storage = preserve ? "" : stored === null ? 'localStorage.removeItem("snow-manager-chat-width");' : `localStorage.setItem("snow-manager-chat-width", ${JSON.stringify(String(stored))});`;
      // Install the public fixture first: its unrelated theme write must not be
      // mistaken for production width code failing under blocked storage.
      const blocking = blocked ? 'Object.defineProperty(window, "localStorage", {configurable: true, get() { throw new DOMException("Synthetic blocked storage", "SecurityError"); }});' : "";
      ({identifier: scriptID} = await send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__harnessConfig = ${JSON.stringify({...fixture, theme})};\n${mock}\n${storage}\n${blocking}`}));
      await send("Page.navigate", {url});
      await wait('document.readyState === "complete" && document.querySelector("#live-connection")?.textContent === "Live"', "production public stream connected");
      await evaluate(helpers);
      await evaluate("document.fonts.ready");
      await delay(150);
      await check('!!window.SnowWidth && ["init", "dispose", "reset"].every(name => typeof SnowWidth[name] === "function")', "Production width lifecycle is mounted");
    }
    async function screenshot(name) {
      if (!evidence) return;
      const {data} = await send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false});
      const image = Buffer.from(data, "base64");
      if (image.length > 8 * 1024 * 1024) throw new Error("Screenshot exceeds 8MiB bound");
      await writeFile(join(evidence, name + ".png"), image, {mode: 0o600}); captures.push(name + ".png");
    }
    async function scenario(name, fn) {
      const start = assertions;
      try { await fn(); await screenshot(name); reports.push({name, assertions: assertions - start}); }
      catch (error) { failures.push({name, error: error.message}); await screenshot(name + "-failure"); }
      console.log(JSON.stringify(failures.at(-1)?.name === name ? failures.at(-1) : reports.at(-1)));
    }
    await runScenarios({send, evaluate, check, wait, viewport, navigate, scenario, delay});
    assert.ok(requests.every(request => request.method === "GET"), "Fixture server receives no mutation requests");
    const result = {assertions, reports, failures, screenshots: evidence ? {directory: evidence, files: captures} : null,
      limitations: ["Public snapshots and inspector responses are mocked; templates, assets, DOM layout, pointer capture and input dispatch are production browser behavior."]};
    if (evidence) await writeFile(join(evidence, "report.json"), JSON.stringify(result, null, 2) + "\n", {mode: 0o600});
    console.log(`${assertions} assertions passed; ${failures.length} scenarios failed across ${reports.length + failures.length} production-browser reports.`);
    if (failures.length) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer); client?.close();
  if (chrome && chrome.exitCode === null) {
    const exited = new Promise(resolve => chrome.once("exit", resolve)); chrome.kill("SIGKILL"); await exited;
  }
  if (server) await new Promise(resolve => server.close(resolve));
  await rm(temporary, {recursive: true, force: true});
}
