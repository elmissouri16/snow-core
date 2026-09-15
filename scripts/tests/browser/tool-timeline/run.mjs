// Isolated chronological-tool regression checks against exported production
// templates/assets. Synthetic HTTP snapshots only; no provider or manager process.
// Node 22+ / installed Chrome; the main harness owns the full layout matrix.
import {spawn} from "node:child_process";
import {createHash} from "node:crypto";
import {mkdir, mkdtemp, readdir, readFile, rm, writeFile} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {dirname, extname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-tool-timeline-"));
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
  const fixtures = ["chat", "saved-markdown"].map(name => manifest.find(item => item.name === name));
  if (fixtures.some(item => !item)) throw new Error("Missing production tool-timeline fixture");
  const files = new Map(await Promise.all(fixtures.map(async item => [`/${item.name}.html`, await readFile(join(directory, `${item.name}.html`))])));
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
  const digest = createHash("sha256");
  for (const [path, body] of [...files].sort(([a], [b]) => a.localeCompare(b))) digest.update(path).update("\0").update(body);
  console.log(JSON.stringify({exportedProductionSHA256: digest.digest("hex"), fixtures: fixtures.map(item => item.name)}));
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
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Tool timeline checks exceeded 120s")), 120000); });
  await Promise.race([deadline, (async () => {
    client = await connect(await debuggingURL(chrome));
    console.log(JSON.stringify({browser: await client.send("Browser.getVersion")}));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
    await client.send("Page.enable", {}, sessionId);
    let failures = 0, assertions = 0;
    const height = 740;
    for (const fixture of fixtures) for (const width of [320, 1280]) for (const theme of ["dark", "light"]) {
      await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__harnessConfig = ${JSON.stringify({...fixture, theme})};\n${mock}`}, sessionId);
      await client.send("Page.navigate", {url: `http://127.0.0.1:${server.address().port}/${fixture.name}.html`}, sessionId);
      let ready = false;
      for (let i = 0; i < 100; i++) {
        await delay(30);
        const state = await client.send("Runtime.evaluate", {expression: 'document.readyState === "complete" && window.SnowMessages && (window.harnessFixture.snapshot ? document.querySelector("#live-connection")?.textContent === "Live" : !!document.querySelector(".history-tool .activity-leading"))', returnByValue: true}, sessionId);
        if (state.result?.value) { ready = true; break; }
      }
      if (!ready) throw new Error("Production fixture failed to settle");
      const evaluated = await client.send("Runtime.evaluate", {expression: tests, awaitPromise: true, returnByValue: true}, sessionId);
      if (evaluated.exceptionDetails) throw new Error(JSON.stringify(evaluated.exceptionDetails));
      const evaluate = async expression => {
        const result = await client.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}, sessionId);
        if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
        return result.result.value;
      };
      const key = async (key, code, virtualKey) => {
        await client.send("Input.dispatchKeyEvent", {type: "keyDown", key, code, windowsVirtualKeyCode: virtualKey, ...(key === "Enter" ? {text: "\r", unmodifiedText: "\r"} : key === " " ? {text: " ", unmodifiedText: " "} : {})}, sessionId);
        await client.send("Input.dispatchKeyEvent", {type: "keyUp", key, code, windowsVirtualKeyCode: virtualKey}, sessionId);
        await delay(50);
      };
      const point = await evaluate("window.toolTimelineChecks.point()");
      await client.send("Input.dispatchMouseEvent", {type: "mouseMoved", ...point}, sessionId);
      await evaluate('toolTimelineChecks.check(getComputedStyle(toolTimelineChecks.summary.querySelector(".activity-chevron")).opacity === "1", "Pointer hover reveals the leading disclosure chevron")');
      await client.send("Input.dispatchMouseEvent", {type: "mousePressed", button: "left", clickCount: 1, ...point}, sessionId);
      await client.send("Input.dispatchMouseEvent", {type: "mouseReleased", button: "left", clickCount: 1, ...point}, sessionId);
      await delay(50);
      await evaluate('toolTimelineChecks.check(toolTimelineChecks.target.open, "Real pointer click opens the literal output disclosure")');
      const object = await client.send("Runtime.evaluate", {expression: "toolTimelineChecks.summary"}, sessionId);
      const ax = await client.send("Accessibility.getPartialAXTree", {objectId: object.result.objectId}, sessionId);
      const wireName = fixture.snapshot ? "glob" : "read";
      const accessible = ax.nodes.some(node => !node.ignored && node.name?.value.includes(wireName) && node.name.value.includes("Completed") && node.properties?.some(property => property.name === "expanded" && property.value.value === true));
      await evaluate(`toolTimelineChecks.check(${accessible}, "Chrome accessibility tree retains wire tool name, success outcome and native expanded state")`);
      await client.send("Runtime.releaseObject", {objectId: object.result.objectId}, sessionId);
      await evaluate("toolTimelineChecks.summary.focus()");
      await evaluate("toolTimelineChecks.retain()");
      await key("Enter", "Enter", 13);
      await evaluate('toolTimelineChecks.check(!toolTimelineChecks.target.open && document.activeElement === toolTimelineChecks.summary, "Real Enter collapses the same retained disclosure")');
      await client.send("Input.dispatchMouseEvent", {type: "mouseMoved", x: 1, y: 1}, sessionId);
      await key("Tab", "Tab", 9);
      await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9, modifiers: 8}, sessionId);
      await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9, modifiers: 8}, sessionId);
      await evaluate('toolTimelineChecks.check(document.activeElement === toolTimelineChecks.summary && getComputedStyle(toolTimelineChecks.summary.querySelector(".activity-chevron")).opacity === "1", "Keyboard focus reveals collapsed disclosure chevron without hover")');
      await key(" ", "Space", 32);
      await evaluate('toolTimelineChecks.check(toolTimelineChecks.target.open, "Real Space reopens the same disclosure")');
      await evaluate("toolTimelineChecks.retain()");
      if (process.env.SNOW_TOOL_TIMELINE_EVIDENCE === "1") {
        const evidence = join(root, "dist/tool-timeline-evidence");
        await mkdir(evidence, {recursive: true});
        for (const open of [false, true]) {
          await evaluate(`toolTimelineChecks.target.open = ${open}; toolTimelineChecks.target.scrollIntoView({block: "center"})`);
          await delay(100);
          const image = await client.send("Page.captureScreenshot", {format: "png"}, sessionId);
          await writeFile(join(evidence, `${fixture.name}-${width}-${theme}-${open ? "open" : "closed"}.png`), Buffer.from(image.data, "base64"));
        }
      }
      const result = await evaluate("toolTimelineChecks.finish()");
      assertions += result.results.length; failures += result.failures.length;
      console.log(JSON.stringify({fixture: fixture.name, width, height, theme, ...result}));
      await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
    }
    console.log(`${assertions} assertions passed; ${failures} failed across 8 focused reports.`);
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
