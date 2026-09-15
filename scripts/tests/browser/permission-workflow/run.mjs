// Real permission broker and builtin writes; all projects/providers are isolated.
import {spawn} from "node:child_process";
import {mkdtemp, chmod, mkdir, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {setTimeout as delay} from "node:timers/promises";
import {chromeBinary, debuggingURL, connect} from "../live-stream/cdp.mjs";
import {exercise} from "./tests.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
const args = process.argv.slice(2);
if (args.length && (args.length !== 2 || args[0] !== "--output-dir" || !args[1].trim())) throw Error("Usage: run.mjs [--output-dir PATH]");
const artifacts = args.length ? resolve(args[1]) : join(repository, ".snow/browser/permission-workflow");
async function compile(binary) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "-c", "-o", binary, "./cmd/snow"], {cwd: repository, stdio: ["ignore", "pipe", "pipe"], timeout: 120000});
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", code => code === 0 ? resolve() : reject(new Error(`Fixture compile failed: ${output}`)));
  });
}
const startedAt = new Date().toISOString();
const report = value => writeFile(join(artifacts, "report.json"), JSON.stringify({startedAt, ...value}, null, 2) + "\n");
async function run() {
  // A compile/startup failure must not leave a previous run's successful report.
  await mkdir(artifacts, {recursive: true});
  await report({status: "running"});
  const directory = await mkdtemp(join(tmpdir(), "snow-permission-workflow-"));
  await chmod(directory, 0o700);
  let manager, chrome, client, timer, managerExited, chromeExited;
  let completed = false, result;
  const exited = child => new Promise(resolve => { child.once("exit", resolve); child.once("error", resolve); });
  try {
    const binary = join(directory, "snow-fixture.test");
    await compile(binary);
    await mkdir(artifacts, {recursive: true});
    const deadline = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("Permission browser deadline exceeded")), 180000); });
    await Promise.race([deadline, (async () => {
      manager = spawn(binary, ["-test.run=^TestWebPermissionFixture$", "-test.count=1", "-test.timeout=230s"], {
        cwd: repository,
        env: {PATH: process.env.PATH || "/usr/bin:/bin", TMPDIR: tmpdir(), SNOW_WEB_PERMISSION_FIXTURE_DIR: directory},
        stdio: ["pipe", "pipe", "pipe"]
      });
      managerExited = exited(manager);
      const ready = await new Promise((resolve, reject) => {
        let buffer = "";
        manager.stdout.on("data", chunk => {
          buffer += chunk;
          if (buffer.length > 65536) { reject(new Error("Fixture startup output limit")); buffer = ""; return; }
          let end;
          while ((end = buffer.indexOf("\n")) >= 0) {
            const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
            // Cookie-bearing IPC must never be echoed, including error paths.
            if (line.startsWith("SNOW_PERMISSION_READY ")) {
              try { resolve(JSON.parse(line.slice("SNOW_PERMISSION_READY ".length))); }
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
      await client.send("Network.setCookie", {name: "snow_manager_local_session", value: ready.cookie, url: ready.origin, httpOnly: true, sameSite: "Strict"}, sessionId);
      delete ready.cookie;
      result = await exercise({client, sessionId, ready, directory, artifacts});
      completed = true;
    })()]);
  } finally {
    clearTimeout(timer);
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (chromeExited) await Promise.race([chromeExited, delay(2000)]);
    manager?.stdin.end("stop\n");
    if (managerExited) await Promise.race([managerExited, delay(9000)]);
    if (manager && manager.exitCode === null && manager.signalCode === null) { manager.kill("SIGKILL"); await managerExited; }
    await rm(directory, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
    if (completed && manager?.exitCode !== 0) throw Error("Permission fixture did not shut down successfully");
  }
  await report({status: "passed", ...result, failures: 0, cleanup: "completed"});
  console.log(`permission-workflow: ${result.assertions} assertions passed; cleanup complete; screenshots ${artifacts}/`);
}
try { await run(); }
catch (error) {
  await report({status: "failed", failures: 1}).catch(() => {});
  console.error(`permission-workflow: ${error.message}`); process.exitCode = 1;
}
