// Node 22+ and installed Chrome. Exports actual Go templates/assets, then serves
// only an isolated public HTTP fixture. No user config, manager, or provider.
import {spawn} from "node:child_process";
import {mkdtemp, readdir, readFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, connect, debuggingURL} from "../live-stream/cdp.mjs";
import {transport} from "./fixture.mjs";
import {checks} from "./tests.mjs";
import {settingsChecks} from "./settings.mjs";
import {refreshChecks} from "./refresh.mjs";
import {telemetryChecks} from "./telemetry.mjs";
import {sidebarChecks} from "./sidebar.mjs";

const here = dirname(fileURLToPath(import.meta.url)), root = resolve(here, "../../../..");
const temporary = await mkdtemp(join(tmpdir(), "snow-permission-policy-"));
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
  const fixture = manifest.find(item => item.name === "workflow-edit");
  if (!fixture) throw new Error("Missing production streaming workflow fixture");
  const html = await readFile(join(directory, "workflow-edit.html"));
  if (!html.includes("data-permission-policy-menu")) throw new Error("Go workflow fixture must set PermissionPolicyEnabled; tests never synthesize production controls");
  const sidebarHTML = await readFile(join(directory, "many-home.html"));
  const files = new Map([["/workflow.html", html], ["/sidebar.html", sidebarHTML], ["/", sidebarHTML]]);
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
  const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Permission policy checks exceeded 180s")), 180000); });
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
      await send("Input.dispatchKeyEvent", {type: "keyDown", key, code, windowsVirtualKeyCode, modifiers, ...(key === "Enter" ? {text: "\r", unmodifiedText: "\r"} : key === " " ? {text: " ", unmodifiedText: " "} : {})});
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
    let total = 0, failures = 0, assertionFailures = 0;
    const quick = process.env.SNOW_BROWSER_QUICK === "1";
    for (const width of quick ? [1280] : [320, 1280]) for (const height of quick ? [740] : [740, 240]) for (const theme of quick ? ["dark"] : ["dark", "light"]) {
      await send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false});
      await send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]});
      const {identifier} = await send("Page.addScriptToEvaluateOnNewDocument", {source: `
        localStorage.setItem("snow-manager-theme", ${JSON.stringify(theme)});
        window.policyErrors=[]; window.policyInvalidEvents=0;
        window.policyEvents=[]; for (const type of ['click','focusin','close']) document.addEventListener(type,e=>{policyEvents.push([type,e.target.id,e.target.dataset?.menuKey,e.target.dataset?.permissionPolicyMenu,performance.now()]);if(policyEvents.length>30)policyEvents.shift();},true);
        addEventListener("error", e => policyErrors.push(e.message));
        addEventListener("unhandledrejection", e => policyErrors.push(String(e.reason)));
        document.addEventListener("invalid", () => policyInvalidEvents++, true);
        const nativeTimeout=window.setTimeout;
        window.setTimeout=(fn,ms,...args)=>nativeTimeout(fn,ms===2000?40:ms,...args);
      `});
      const navigate = async ({streaming = false, messages = []} = {}) => {
        mock.state.reset(); mock.state.streaming = streaming;
        mock.state.snapshot.messages = messages;
        await send("Page.navigate", {url: `http://127.0.0.1:${server.address().port}/workflow.html`});
        await wait('document.readyState === "complete" && document.querySelector("#live-connection")?.textContent === "Live" && !document.querySelector("[data-permission-policy-menu]")?.disabled', "production fixture connected");
      };
      try {
        const helpers = {state: mock.state, evaluate, wait, key, click, navigate, insert: text => send("Input.insertText", {text}), width, height, theme};
        const result = await checks(helpers);
        const settings = await settingsChecks(helpers);
        result.results.push(...settings.results); result.failures.push(...settings.failures);
        const refresh = await refreshChecks(helpers);
        result.results.push(...refresh.results); result.failures.push(...refresh.failures);
        const telemetry = await telemetryChecks(helpers);
        result.results.push(...telemetry.results); result.failures.push(...telemetry.failures);
        if (width >= 768 && height === 740) {
          const sidebar = await sidebarChecks(helpers);
          result.results.push(...sidebar.results); result.failures.push(...sidebar.failures);
        }
        total += result.results.length; failures += result.failures.length; assertionFailures += result.failures.length;
        console.log(JSON.stringify({width, height, theme, ...result}));
      } catch (error) { failures++; mock.state.held?.(mock.state.snapshot.permission_mode); console.log(JSON.stringify({width, height, theme, fatal: error.message})); }
      await send("Page.removeScriptToEvaluateOnNewDocument", {identifier});
    }
    if (external.length) { failures++; console.error("Unexpected external requests", external); }
    console.log(`${total - assertionFailures} assertions passed; ${failures} failures across ${quick ? 1 : 8} permission-policy reports.`);
    if (failures) process.exitCode = 1;
  })()]);
} finally {
  clearTimeout(timer); client?.close();
  if (chrome && chrome.exitCode === null) { const exited = new Promise(resolve => chrome.once("exit", resolve)); chrome.kill("SIGKILL"); await exited; }
  if (server) { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
  await rm(temporary, {recursive: true, force: true});
}
