// Node 22+ and installed Chrome. Exports actual Go templates/assets, then serves
// only an isolated public HTTP fixture. No user config, manager, or provider.
import {spawn} from "node:child_process";
import {mkdtemp, readdir, readFile, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";
import {transport} from "./fixture.mjs";
import {checks} from "./tests.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-stop-reuse-"));
const evidenceDirectory = process.env.SNOW_STOP_REUSE_EVIDENCE === "1" ? await mkdtemp(join(tmpdir(), "snow-stop-reuse-evidence-")) : null;
let chrome, client, server, timer;
try {
  const binary = chromeBinary(), directory = join(temporary, "fixtures");
  await new Promise((resolve, reject) => {
    const exporter = spawn("go", ["test", "./internal/web", "-run", "^TestExportHarnessVisualFixtures$", "-count=1"], {
      cwd: root, env: {...process.env, SNOW_WEB_FIXTURE_DIR: directory}, stdio: ["ignore", "pipe", "pipe"], timeout: 120000
    });
    let output = "";
    for (const stream of [exporter.stdout, exporter.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    exporter.once("error", reject);
    exporter.once("exit", code => code === 0 ? resolve() : reject(new Error(`Fixture export failed: ${output}`)));
  });
  const manifest = JSON.parse(await readFile(join(directory, "fixtures.json"), "utf8"));
  const fixture = manifest.find(item => item.name === "workflow-cancel");
  if (!fixture) throw new Error("Missing production workflow fixture");
  const html = await readFile(join(directory, "workflow-cancel.html"));
  if (!html.includes('data-turn-cancel="true"')) throw new Error("Go workflow fixture must set TurnCancelEnabled; tests never synthesize production controls");
  if (!manifest.some(item => item.name === "saved-user")) throw new Error("Missing production saved-user fixture");
  const savedUser = await readFile(join(directory, "saved-user.html"));
  const files = new Map([["/workflow-cancel.html", html], ["/saved-user.html", savedUser]]);
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
  const mock = transport(files, fixture); server = mock.server;
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  chrome = spawn(binary, ["--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    "--remote-debugging-port=0", `--user-data-dir=${join(temporary, "profile")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Stop/reuse checks exceeded 180s")), 180000); });
  await Promise.race([deadline, (async () => {
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
    const send = (method, params = {}) => client.send(method, params, sessionId);
    await send("Page.enable");
    await send("Network.enable");
    const external = [];
    client.onEvent(event => {
      if (event.sessionId !== sessionId || event.method !== "Network.requestWillBeSent") return;
      const url = event.params.request.url;
      if (!url.startsWith(`http://127.0.0.1:${server.address().port}/`) && !url.startsWith("data:") && url !== "about:blank") external.push(url);
    });
    const evaluate = async expression => {
      const result = await send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true});
      if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
      return result.result.value;
    };
    const wait = async (expression, label = expression) => {
      for (let i = 0; i < 160; i++) { if (await evaluate(expression)) return; await delay(20); }
      throw new Error(`Timed out: ${label}; DOM=${await evaluate('document.querySelector("#live-connection")?.textContent')}`);
    };
    const key = async (key, code = key, windowsVirtualKeyCode = 0, modifiers = 0) => {
      await send("Input.dispatchKeyEvent", {type: "keyDown", key, code, windowsVirtualKeyCode, modifiers, ...(key === "a" && modifiers ? {commands: ["selectAll"]} : {}), ...(key === "Enter" ? {text: "\r", unmodifiedText: "\r"} : key === " " ? {text: " ", unmodifiedText: " "} : {})});
      await send("Input.dispatchKeyEvent", {type: "keyUp", key, code, windowsVirtualKeyCode, modifiers});
      await delay(20);
    };
    const click = async selector => {
      const point = await evaluate(`(() => {const n = document.querySelector(${JSON.stringify(selector)}); if (!n) throw Error("Missing click target"); n.scrollIntoView({block:"nearest",inline:"nearest"}); const r=n.getBoundingClientRect(); const x=r.x+r.width/2,y=r.y+r.height/2; return {x,y,hit:n.contains(document.elementFromPoint(x,y)),width:r.width,height:r.height};})()`);
      if (!point.hit || point.width <= 0 || point.height <= 0) throw new Error(`Native click target obscured: ${selector} ${JSON.stringify(point)}`);
      await send("Input.dispatchMouseEvent", {type: "mousePressed", x: point.x, y: point.y, button: "left", clickCount: 1});
      await send("Input.dispatchMouseEvent", {type: "mouseReleased", x: point.x, y: point.y, button: "left", clickCount: 1});
      await delay(20);
    };
    let total = 0, failures = 0, assertionFailures = 0, reports = 0;
    for (const width of (process.env.SNOW_STOP_REUSE_WIDTH ? [Number(process.env.SNOW_STOP_REUSE_WIDTH)] : [320, 1280])) for (const height of [740]) for (const theme of (process.env.SNOW_STOP_REUSE_THEME ? [process.env.SNOW_STOP_REUSE_THEME] : ["dark", "light"])) {
      reports++;
      await send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false});
      await send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]});
      const {identifier} = await send("Page.addScriptToEvaluateOnNewDocument", {source: `
        localStorage.setItem("snow-manager-theme", ${JSON.stringify(theme)});
        window.stopReuseErrors=[]; window.stopReuseInvalidEvents=0;
        addEventListener("error", e => stopReuseErrors.push(e.message));
        addEventListener("unhandledrejection", e => stopReuseErrors.push(String(e.reason)));
        document.addEventListener("invalid", () => stopReuseInvalidEvents++, true);
        const nativeTimeout=window.setTimeout;
        window.setTimeout=(fn,ms,...args)=>nativeTimeout(fn,(ms===2000||ms===5000)?40:ms,...args);
      `});
      const navigate = async (page = "workflow-cancel") => {
        mock.state.reset();
        await send("Page.navigate", {url: `http://127.0.0.1:${server.address().port}/${page}.html`});
        if (page === "saved-user") {
          await wait('document.readyState === "complete" && typeof window.SnowMessages?.enhance === "function" && document.querySelector("[data-message-id=fixture-saved-user-message]") !== null', "production saved-user fixture rendered");
          return;
        }
        await wait('document.readyState === "complete" && document.querySelector("#live-connection")?.textContent === "Live" && !document.querySelector("#live-send")?.disabled', "production fixture connected");
      };
      const captures = [];
      const capture = async name => {
        if (!evidenceDirectory) return;
        const {data} = await send("Page.captureScreenshot", {format: "png", captureBeyondViewport: false});
        const image = Buffer.from(data, "base64");
        if (image.length > 8 * 1024 * 1024) throw new Error("Screenshot exceeds 8MiB bound");
        const path = join(evidenceDirectory, `${width}-${height}-${theme}-${name}.png`);
        await writeFile(path, image); captures.push(path);
      };
      try {
        const result = await checks({state: mock.state, evaluate, wait, key, click, navigate, send, capture, insert: text => send("Input.insertText", {text}), width, height, theme});
        total += result.results.length; failures += result.failures.length; assertionFailures += result.failures.length;
        console.log(JSON.stringify({width, height, theme, ...result, ...(captures.length ? {evidence: captures} : {})}));
      } catch (error) { failures++; mock.state.release(); console.log(JSON.stringify({width, height, theme, fatal: error.message})); }
      await send("Page.removeScriptToEvaluateOnNewDocument", {identifier});
    }
    if (external.length) { failures++; console.error("Unexpected external requests", external); }
    console.log(`${total - assertionFailures} assertions passed; ${failures} failures across ${reports} stop/reuse reports.`);
    if (failures) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer);
  // Ask Chrome to terminate its own subprocesses before killing the parent.
  // A killed parent can otherwise leave inherited stderr pipes open in Node.
  if (client) { try { await Promise.race([client.send("Browser.close"), delay(1000)]); } catch { /* already disconnected */ } client.close(); }
  if (chrome && chrome.exitCode === null && chrome.signalCode === null) {
    const exited = new Promise(resolve => chrome.once("exit", resolve));
    await Promise.race([exited, delay(1000)]);
    if (chrome.exitCode === null && chrome.signalCode === null) { chrome.kill("SIGKILL"); await exited; }
  }
  chrome?.stderr.destroy();
  if (server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
  await rm(temporary, {recursive: true, force: true});
}
