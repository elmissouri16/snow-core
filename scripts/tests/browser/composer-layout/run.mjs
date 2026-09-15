// Focused production-template geometry checks. No provider or manager process.
// Node 22+ / installed Chrome; the main harness owns the full layout matrix.
import {spawn} from "node:child_process";
import {mkdtemp, readdir, readFile, rm} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {dirname, extname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-composer-layout-"));
let chrome, client, server, timer;
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
  if (!fixture) throw new Error("Missing production stream fixture");
  const files = new Map([["/stream.html", await readFile(join(directory, "stream.html"))]]);
  let bytes = 0;
  async function load(relative) {
    for (const entry of await readdir(join(directory, relative), {withFileTypes: true})) {
      const name = relative + "/" + entry.name;
      if (entry.isDirectory()) await load(name);
      else if (entry.isFile()) {
        const body = await readFile(join(directory, name));
        bytes += body.length;
        if (bytes > 16 * 1024 * 1024) throw new Error("Fixture assets exceed 16MiB bound");
        files.set("/" + name, body);
      }
    }
  }
  await load("static");
  server = createServer((request, response) => {
    const path = new URL(request.url, "http://localhost").pathname, body = files.get(path);
    if (!body) { response.writeHead(404); response.end(); return; }
    const type = {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream";
    response.writeHead(200, {"Content-Type": type}); response.end(body);
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const mock = await readFile(join(here, "../harness-layout/fixture.js"), "utf8");
  const tests = await readFile(join(here, "tests.js"), "utf8");
  chrome = spawn(binary, ["--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    "--remote-debugging-port=0", `--user-data-dir=${join(temporary, "profile")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Composer checks exceeded 120s")), 120000); });
  await Promise.race([deadline, (async () => {
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
    await client.send("Page.enable", {}, sessionId);
    let failures = 0, assertions = 0;
    for (const width of [390, 1280]) for (const height of [740, 360, 240]) for (const theme of ["dark", "light"]) {
      await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__harnessConfig = ${JSON.stringify({...fixture, theme})};\n${mock}`}, sessionId);
      await client.send("Page.navigate", {url: `http://127.0.0.1:${server.address().port}/stream.html`}, sessionId);
      let ready = false;
      for (let i = 0; i < 100; i++) {
        await delay(30);
        const state = await client.send("Runtime.evaluate", {expression: 'document.readyState === "complete" && document.querySelector("#live-connection")?.textContent === "Live"', returnByValue: true}, sessionId);
        if (state.result?.value) { ready = true; break; }
      }
      if (!ready) throw new Error("Production fixture failed to settle");
      const evaluated = await client.send("Runtime.evaluate", {expression: tests, awaitPromise: true, returnByValue: true}, sessionId);
      if (evaluated.exceptionDetails) throw new Error(JSON.stringify(evaluated.exceptionDetails));
      const result = evaluated.result.value;
      assertions += result.results.length; failures += result.failures.length;
      console.log(JSON.stringify({width, height, theme, ...result}));
      await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
    }
    console.log(`${assertions} assertions passed; ${failures} failed across 12 focused reports.`);
    if (failures) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer);
  client?.close();
  if (chrome && chrome.exitCode === null) {
    const exited = new Promise(resolve => chrome.once("exit", resolve));
    chrome.kill("SIGKILL"); await exited;
  }
  if (server) await new Promise(resolve => server.close(resolve));
  await rm(temporary, {recursive: true, force: true});
}
