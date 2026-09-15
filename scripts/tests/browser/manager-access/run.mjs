// Real production HTTP/assets and durable manager restarts; no asset server,
// response interception, cookie injection, provider or worker activation.
import {spawn} from "node:child_process";
import {mkdtemp, chmod, rm, readdir} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, debuggingURL, connect} from "../live-stream/cdp.mjs";
import {exercise} from "./tests.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
const exited = child => new Promise(resolve => { child.once("exit", resolve); child.once("error", resolve); });
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
  const directory = await mkdtemp(join(tmpdir(), "snow-manager-access-")); await chmod(directory, 0o700);
  let manager, managerExited, chrome, chromeExited, client, timer;
  async function stopManager() {
    if (!manager) return;
    manager.stdin.end("stop\n");
    await Promise.race([managerExited, delay(9000)]);
    if (manager.exitCode === null && manager.signalCode === null) { manager.kill("SIGKILL"); await managerExited; throw Error("Fixture manager shutdown exceeded deadline"); }
    if (manager.exitCode !== 0) throw Error("Fixture manager exited unsuccessfully");
    manager = null;
  }
  async function startManager() {
    manager = spawn(binary, ["-test.run=^TestWebAccessFixture$", "-test.count=1", "-test.timeout=110s"], {
      cwd: repository, env: {PATH: process.env.PATH || "/usr/bin:/bin", TMPDIR: tmpdir(), HOME: join(directory, "home"), SNOW_WEB_ACCESS_FIXTURE_DIR: directory}, stdio: ["pipe", "pipe", "pipe"]
    });
    managerExited = exited(manager);
    manager.stdin.on("error", () => {});
    const ready = await new Promise((resolve, reject) => {
      let buffer = "", limit = 0;
      manager.stdout.on("data", chunk => {
        limit += chunk.length;
        if (limit > 65536) { reject(Error("Fixture startup output limit")); return; }
        buffer += chunk;
        let index;
        while ((index = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, index); buffer = buffer.slice(index + 1);
          // Never print the private credential-bearing startup frame.
          if (line.startsWith("SNOW_ACCESS_READY ")) {
            try { resolve(JSON.parse(line.slice("SNOW_ACCESS_READY ".length))); }
            catch { reject(Error("Invalid private fixture startup frame")); }
          }
        }
      });
      manager.stderr.resume();
      manager.once("error", () => reject(Error("Fixture manager launch failed")));
      manager.once("exit", code => reject(Error(`Fixture exited before ready (${code})`)));
    });
    if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(ready.origin) || new URL(ready.origin).port === "7331" || typeof ready.code !== "string") throw Error("Invalid isolated manager origin");
    return ready;
  }
  try {
    const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(Error("Production browser access deadline exceeded")), 130000); });
    return await Promise.race([deadline, (async () => {
      const ready = await startManager();
      chrome = spawn(chromeBinary(), ["--headless=new", "--no-sandbox", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--disable-sync", "--no-first-run", "--no-default-browser-check", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", `--user-data-dir=${join(directory, "chrome")}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
      chromeExited = exited(chrome);
      client = await connect(await debuggingURL(chrome));
      const restart = async () => { await stopManager(); return startManager(); };
      const result = await exercise({client, ready, restart, width, theme});
      const files = await readdir(directory);
      result.results.push("Manager-only native access never creates a project/session/provider/worker fixture artifact");
      if (files.some(name => name === "sessions" || name.startsWith("call-") || name === "worker-exit")) result.failures.push(result.results.at(-1));
      console.log(JSON.stringify({width, height: 740, theme, ...result}));
      return result;
    })()]);
  } finally {
    clearTimeout(timer);
    if (client) { try { await Promise.race([client.send("Browser.close"), delay(1000)]); } catch {} client.close(); }
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (chromeExited) await Promise.race([chromeExited, delay(2000)]);
    try { await stopManager(); }
    finally { await rm(directory, {recursive: true, force: true, maxRetries: 3, retryDelay: 100}); }
  }
}
const build = await mkdtemp(join(tmpdir(), "snow-manager-access-build-"));
let passed = 0, failures = 0, reports = 0;
try {
  const binary = join(build, "snow-fixture.test"); await compile(binary);
  const widths = process.env.SNOW_MANAGER_ACCESS_WIDTH ? [Number(process.env.SNOW_MANAGER_ACCESS_WIDTH)] : [320, 1280];
  const themes = process.env.SNOW_MANAGER_ACCESS_THEME ? [process.env.SNOW_MANAGER_ACCESS_THEME] : ["dark", "light"];
  for (const width of widths) for (const theme of themes) {
    reports++;
    try { const result = await run(width, theme, binary); passed += result.results.length - result.failures.length; failures += result.failures.length; }
    catch (error) { passed += (error.results?.length || 0) - (error.failures?.length || 0); failures += 1 + (error.failures?.length || 0); console.error(JSON.stringify({width, theme, passedAssertions: (error.results?.length || 0) - (error.failures?.length || 0), failures: error.failures || [], fatal: error.message, observation: error.observation})); }
  }
} catch (error) { failures++; console.error(`manager-access: ${error.message}`); }
finally { await rm(build, {recursive: true, force: true}); }
console.log(`${passed} assertions passed; ${failures} failures across ${reports} real-manager reports.`);
if (failures) process.exitCode = 1;
