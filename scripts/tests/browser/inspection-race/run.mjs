// Standalone, network-free regression. Requires Node with built-in WebSocket.
// Run from any directory; SNOW_CHROME_BIN may select an explicit Chrome binary.
import {spawn} from "node:child_process";
import {accessSync, constants} from "node:fs";
import {mkdtemp, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";

const fixtureURL = new URL("./index.html", import.meta.url).href;
const expectedAssertions = 60;
const timeoutMS = 30000;

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
  const pending = new Map();
  socket.onmessage = event => {
    const message = JSON.parse(event.data);
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
  const profile = await mkdtemp(join(tmpdir(), "snow-inspection-race-"));
  let chrome, client, timer, exited;
  try {
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
      for (const theme of ["dark", "light"]) for (const [width, height] of [[1440, 900], [320, 240], [768, 360]]) {
        const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
        const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
        await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
        await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `document.addEventListener("DOMContentLoaded", () => document.documentElement.dataset.theme = ${JSON.stringify(theme)})`}, sessionId);
        const navigation = await client.send("Page.navigate", {url: fixtureURL}, sessionId);
        if (navigation.errorText) throw new Error(`Fixture navigation failed: ${navigation.errorText}`);
        let completed = false;
        for (let attempt = 0; attempt < 80; attempt++) {
          await delay(100);
          const result = await client.send("Runtime.evaluate", {
            expression: 'document.querySelector("#test-result")?.textContent || ""',
            returnByValue: true
          }, sessionId);
          const value = result.result?.value;
          if (!value) continue;
          const report = JSON.parse(value);
          if (!Array.isArray(report.failures) || !Array.isArray(report.results)) throw new Error("Malformed browser regression report.");
          if (report.failures.length) throw new Error(`Browser assertions failed (${width}x${height} ${theme}):\n${report.failures.join("\n")}`);
          if (report.passed !== expectedAssertions || report.results.length !== expectedAssertions) {
            throw new Error(`Expected ${expectedAssertions} assertions; received ${report.passed}.`);
          }
          console.log(`inspection-race: ${report.passed} passed (${width}x${height} ${theme})`);
          completed = true; break;
        }
        await client.send("Target.closeTarget", {targetId});
        if (!completed) throw new Error("No browser regression result was produced.");
      }
    };
    await Promise.race([exercise(), deadline]);
  } finally {
    clearTimeout(timer);
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (exited) await Promise.race([exited, delay(2000)]);
    await rm(profile, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}

try {
  await run();
} catch (error) {
  console.error(`inspection-race: ${error.message}`);
  process.exitCode = 1;
}
