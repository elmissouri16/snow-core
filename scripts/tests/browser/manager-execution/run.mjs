// Real app -> provider stream -> agent -> RPC subprocess -> RuntimeManager ->
// production HTTP/SSE -> unmodified production browser assets. No npm packages.
import {spawn} from "node:child_process";
import {mkdtemp, chmod, mkdir, rm, readFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, debuggingURL, connect} from "../live-stream/cdp.mjs";
import {exercise} from "./tests.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
const artifacts = process.env.SNOW_MANAGER_EXECUTION_ARTIFACTS ? resolve(process.env.SNOW_MANAGER_EXECUTION_ARTIFACTS) : null;
const timeoutMS = 110000;

async function compile(binary) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "-c", "-o", binary, "./cmd/snow"], {cwd: repository, stdio: ["ignore", "pipe", "pipe"], timeout: 120000});
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", code => code === 0 ? resolve() : reject(new Error(`Fixture compile failed: ${output}`)));
  });
}

async function run(width, theme, binary) {
  const directory = await mkdtemp(join(tmpdir(), "snow-manager-execution-"));
  await chmod(directory, 0o700);
  let manager, chrome, client, timer, managerExited, chromeExited;
  const exited = child => new Promise(resolve => { child.once("exit", resolve); child.once("error", resolve); });
  try {

    const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Production manager browser deadline exceeded")), timeoutMS); });
    return await Promise.race([deadline, (async () => {
      // Do not forward provider/auth/config environments to the fixture. HOME,
      // project, stores, browser profile, and all provider scripts are isolated.
      manager = spawn(binary, ["-test.run=^TestWebManagerExecutionBrowserFixture$", "-test.count=1", "-test.timeout=160s"], {
        cwd: repository, env: {PATH: process.env.PATH || "/usr/bin:/bin", TMPDIR: tmpdir(), SNOW_WEB_MANAGER_EXECUTION_FIXTURE_DIR: directory}, stdio: ["pipe", "pipe", "pipe"]
      });
      managerExited = exited(manager);
      const ready = await new Promise((resolve, reject) => {
        let buffer = "";
        manager.stdout.on("data", chunk => {
          buffer += chunk;
          if (buffer.length > 65536) { reject(new Error("Fixture startup output limit")); buffer = ""; return; }
          let index;
          while ((index = buffer.indexOf("\n")) >= 0) {
            const line = buffer.slice(0, index); buffer = buffer.slice(index + 1);
            // Never echo the cookie-bearing private IPC frame, even on failure.
            if (line.startsWith("SNOW_MANAGER_EXECUTION_READY ")) {
              try { resolve(JSON.parse(line.slice("SNOW_MANAGER_EXECUTION_READY ".length))); }
              catch { reject(new Error("Invalid private fixture startup frame")); }
            }
          }
        });
        manager.stderr.resume();
        manager.once("error", () => reject(new Error("Fixture launch failed")));
        manager.once("exit", code => reject(new Error(`Fixture exited before ready (${code})`)));
      });
      const origin = new URL(ready.origin);
      if (origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port || origin.port === '7331' || ready.provider !== 'fake' || ready.model !== 'fake-1') throw Error('Unexpected fixture authority');
      chrome = spawn(chromeBinary(), ["--headless=new", "--no-sandbox", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--disable-sync", "--no-first-run", "--no-default-browser-check", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", `--user-data-dir=${join(directory, "chrome")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
      chromeExited = exited(chrome);
      client = await connect(await debuggingURL(chrome));
      const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
      const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
      await client.send("Network.enable", {}, sessionId);
      await client.send("Page.enable", {}, sessionId);
      await client.send("Emulation.setDeviceMetricsOverride", {width, height: 740, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      await client.send("Network.setCacheDisabled", {cacheDisabled: true}, sessionId);
      await client.send("Network.setCookie", {name: "snow_manager_local_session", value: ready.cookie, url: ready.origin, httpOnly: true, sameSite: "Strict"}, sessionId);
      delete ready.cookie;
      const result = await exercise({client, sessionId, ready, directory, width, theme, artifacts});
      console.log(JSON.stringify({width, height: 740, theme, ...result}));
      return result;
    })()]);
  } catch (error) {
    for (const name of ["worker-exit"]) console.error(name + ": " + await readFile(join(directory, name), "utf8").catch(() => "absent"));
    throw error;
  } finally {
    clearTimeout(timer);
    if (client) { try { await Promise.race([client.send("Browser.close"), delay(1000)]); } catch {} client.close(); }
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (chromeExited) await Promise.race([chromeExited, delay(2000)]);
    // Closing stdin cancels web.Run, which closes RuntimeManager and its worker.
    manager?.stdin.end();
    if (managerExited) await Promise.race([managerExited, delay(9000)]);
    if (manager && manager.exitCode === null && manager.signalCode === null) { manager.kill("SIGKILL"); await managerExited; }
    await rm(directory, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}

const build = await mkdtemp(join(tmpdir(), "snow-manager-execution-build-"));
let passed = 0, failures = 0, reports = 0;
try {
  if (artifacts) await mkdir(artifacts, {recursive: true, mode: 0o700});
  const binary = join(build, "snow-fixture.test"); await compile(binary);
  const widths = process.env.SNOW_MANAGER_EXECUTION_WIDTH ? [Number(process.env.SNOW_MANAGER_EXECUTION_WIDTH)] : [320, 1280];
  const themes = process.env.SNOW_MANAGER_EXECUTION_THEME ? [process.env.SNOW_MANAGER_EXECUTION_THEME] : ["dark", "light"];
  if (widths.some(width => ![320, 1280].includes(width)) || themes.some(theme => !['dark', 'light'].includes(theme))) throw Error('Only the 320/1280 dark/light matrix is supported');
  for (const width of widths) for (const theme of themes) {
    reports++;
    try { const result = await run(width, theme, binary); passed += result.results.length - result.failures.length; failures += result.failures.length; }
    catch (error) { passed += (error.results?.length || 0) - (error.failures?.length || 0); failures += 1 + (error.failures?.length || 0); console.error(JSON.stringify({width, theme, passedAssertions: (error.results?.length || 0) - (error.failures?.length || 0), failures: error.failures || [], fatal: error.message, observation: error.observation})); }
  }
} catch (error) { failures++; console.error(`manager-execution: ${error.message}`); }
finally { await rm(build, {recursive: true, force: true}); }
console.log(`${passed} assertions passed; ${failures} failures across ${reports} real-manager reports.`);
if (failures) process.exitCode = 1;
