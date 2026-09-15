// Real app -> provider stream -> agent -> RPC subprocess -> RuntimeManager ->
// production HTTP/SSE -> unmodified production browser assets. No npm packages.
import {spawn} from "node:child_process";
import {mkdtemp, chmod, mkdir, rm, readFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, debuggingURL, connect} from "./cdp.mjs";
import {exercise} from "./switch-flicker-tests.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
const artifacts = join(repository, ".snow/browser/live-stream");
const timeoutMS = 85000;

async function compile(binary) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "-c", "-o", binary, "./cmd/snow"], {cwd: repository, stdio: ["ignore", "pipe", "pipe"], timeout: 120000});
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", code => code === 0 ? resolve() : reject(new Error(`Fixture compile failed: ${output}`)));
  });
}

async function run() {
  const directory = await mkdtemp(join(tmpdir(), "snow-live-stream-"));
  await chmod(directory, 0o700);
  let manager, chrome, client, timer, managerExited, chromeExited;
  const exited = child => new Promise(resolve => { child.once("exit", resolve); child.once("error", resolve); });
  try {
    const binary = join(directory, "snow-fixture.test");
    await compile(binary);
    await mkdir(artifacts, {recursive: true});
    const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Live stream browser deadline exceeded")), timeoutMS); });
    await Promise.race([deadline, (async () => {
      // Do not forward provider/auth/config environments to the fixture. HOME,
      // project, stores, browser profile, and all provider scripts are isolated.
      manager = spawn(binary, ["-test.run=^TestWebLiveStreamFixture$", "-test.count=1", "-test.timeout=160s"], {
        cwd: repository, env: {PATH: process.env.PATH || "/usr/bin:/bin", TMPDIR: tmpdir(), SNOW_WEB_LIVE_FIXTURE_DIR: directory}, stdio: ["pipe", "pipe", "pipe"]
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
            if (line.startsWith("SNOW_LIVE_READY ")) {
              try { resolve(JSON.parse(line.slice("SNOW_LIVE_READY ".length))); }
              catch { reject(new Error("Invalid private fixture startup frame")); }
            }
          }
        });
        manager.stderr.resume();
        manager.once("error", () => reject(new Error("Fixture launch failed")));
        manager.once("exit", code => reject(new Error(`Fixture exited before ready (${code})`)));
      });
      chrome = spawn(chromeBinary(), ["--headless=new", "--no-sandbox", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--disable-sync", "--no-first-run", "--no-default-browser-check", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", `--user-data-dir=${join(directory, "chrome")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
      chromeExited = exited(chrome);
      client = await connect(await debuggingURL(chrome));
      const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
      const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
      await client.send("Network.enable", {}, sessionId);
      await client.send("Page.enable", {}, sessionId);
      await client.send("Emulation.setDeviceMetricsOverride", {width: 1280, height: 900, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Network.setCookie", {name: "snow_manager_local_session", value: ready.cookie, url: ready.origin, httpOnly: true, sameSite: "Strict"}, sessionId);
      delete ready.cookie;
      await exercise({client, sessionId, ready, directory, artifacts, pauseManager: () => manager.kill("SIGSTOP"), resumeManager: () => manager.kill("SIGCONT")});
    })()]);
  } catch (error) {
    for (const name of ["worker-exit"]) console.error(name + ": " + await readFile(join(directory, name), "utf8").catch(() => "absent"));
    throw error;
  } finally {
    clearTimeout(timer);
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (chromeExited) await Promise.race([chromeExited, delay(2000)]);
    // Closing stdin cancels web.Run, which closes RuntimeManager and its worker.
    if (manager && manager.exitCode === null && manager.signalCode === null) manager.kill("SIGCONT");
    manager?.stdin.end("stop\n");
    if (managerExited) await Promise.race([managerExited, delay(9000)]);
    if (manager && manager.exitCode === null && manager.signalCode === null) { manager.kill("SIGKILL"); await managerExited; }
    await rm(directory, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}

try { await run(); }
catch (error) { console.error(`live-stream: ${error.message}`); process.exitCode = 1; }
