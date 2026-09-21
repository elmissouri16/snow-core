// Standalone, network-free regression. Requires Node with built-in WebSocket.
// Run from any directory; SNOW_CHROME_BIN may select an explicit Chrome binary.
import {thumbnailFixture} from "./thumbnail-fixture.mjs";
import {spawn} from "node:child_process";
import {accessSync, constants} from "node:fs";
import {mkdtemp, mkdir, rm, readFile, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve, dirname} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";
import {setTimeout as delay} from "node:timers/promises";

const here = dirname(fileURLToPath(import.meta.url));
const repository = resolve(here, "../../../..");
async function exportFixture(directory) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["test", "./internal/web", "-run", "^TestExportHarnessVisualFixtures$", "-count=1"], {cwd: repository, env: {...process.env, SNOW_WEB_FIXTURE_DIR: directory}, stdio: ["ignore", "pipe", "pipe"], timeout: 120000});
    let output = "";
    for (const stream of [child.stdout, child.stderr]) stream.on("data", chunk => { output = (output + chunk).slice(-65536); });
    child.once("error", reject);
    child.once("close", code => code === 0 ? resolve() : reject(new Error(`Go fixture export failed: ${output}`)));
  });
  const urls = [];
  for (const name of ["workflow", "workflow-queue"]) {
  let html = await readFile(join(directory, name + ".html"), "utf8");
  html = html.replaceAll('"/static/', '"' + pathToFileURL(join(directory, "static")).href + '/');
  // The fixture only replaces public transport; all page markup and
  // production script order are exported on this run, never copied by hand.
  html = html.replace("</body>", `<pre id="test-result" hidden></pre><script src="${new URL("../conversation-workflow/fixture.js", import.meta.url).href}"></script><script src="${new URL("helpers.js", import.meta.url).href}"></script><script src="${new URL("attachments.js", import.meta.url).href}"></script><script src="${new URL("mentions.js", import.meta.url).href}"></script><script src="${new URL("races.js", import.meta.url).href}"></script><script src="${new URL("recovery.js", import.meta.url).href}"></script><script src="${new URL("limits.js", import.meta.url).href}"></script><script src="${new URL("visuals.js", import.meta.url).href}"></script><script src="${new URL("thumbnails.js", import.meta.url).href}"></script><script defer src="${new URL("tests.js", import.meta.url).href}"></script></body>`);
  await writeFile(join(directory, name + ".html"), html, {mode: 0o600});
  urls.push(pathToFileURL(join(directory, name + ".html")).href);
  }
  return urls;
}
const expectedAssertions = 109, expectedVisualAssertions = 29, expectedThumbnailAssertions = 32;
const screenshotDirectory = join(repository, "dist", "compact-composer");
// The original functional schedule plus fresh visual/evidence pages remain bounded.
const timeoutMS = 480000;

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
  const pending = new Map(), exceptions = [];
  socket.onmessage = event => {
    const message = JSON.parse(event.data);
    if (message.method === "Runtime.exceptionThrown" && exceptions.length < 30) exceptions.push(message.params.exceptionDetails);
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
    exceptions,
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
  const profile = await mkdtemp(join(tmpdir(), "snow-composer-context-"));
  let chrome, client, timer, exited, thumbnailHTTP;
  try {
    const fixtureURLs = await exportFixture(join(profile, "fixtures"));
    thumbnailHTTP = await thumbnailFixture(join(profile, "fixtures"), here);
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
      const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
      const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
      await client.send("Page.enable", {}, sessionId);
      await client.send("Runtime.enable", {}, sessionId);
      await mkdir(screenshotDirectory, {recursive: true});
      const failedReports = [];
      const schedule = [{url: thumbnailHTTP.url, thumbnails: true}, ...(process.env.SNOW_COMPOSER_THUMBNAILS_ONLY === "1" ? [] : [{url: thumbnailHTTP.urls[0], visual: true}, ...thumbnailHTTP.urls.map(url => ({url, visual: false}))])];
      for (const {url: fixtureURL, visual = false, thumbnails = false} of schedule) {
      for (const [width, height] of (process.env.SNOW_COMPOSER_NARROW_HEIGHT === "1" ? [[320, 900], [1280, 900], [320, 480], [320, 240]] : [[320, 900], [1280, 900]]).filter(([width, height]) => (!process.env.SNOW_COMPOSER_WIDTH || width === Number(process.env.SNOW_COMPOSER_WIDTH)) && (!process.env.SNOW_COMPOSER_HEIGHT || height === Number(process.env.SNOW_COMPOSER_HEIGHT)))) {
      for (const theme of (process.env.SNOW_COMPOSER_THEME ? [process.env.SNOW_COMPOSER_THEME] : ["dark", "light"])) {
      const imageRequestStart = thumbnailHTTP.requests.length;
      const {identifier} = await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `window.__composerVisualOnly=${visual};window.__composerThumbnailsOnly=${thumbnails};window.__workflowTheme=${JSON.stringify(theme)};localStorage.setItem("snow-manager-theme",${JSON.stringify(theme)});`}, sessionId);
      await client.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send("Emulation.setEmulatedMedia", {features: [{name: "prefers-color-scheme", value: theme}, {name: "prefers-reduced-motion", value: "reduce"}]}, sessionId);
      const navigation = await client.send("Page.navigate", {url: fixtureURL}, sessionId);
      if (navigation.errorText) throw new Error(`Fixture navigation failed: ${navigation.errorText}`);
      for (let attempt = 0; attempt < 180; attempt++) {
        await delay(100);
        if (visual || thumbnails) {
          const checkpoint = await client.send("Runtime.evaluate", {expression: "window.composerScreenshot || null", returnByValue: true}, sessionId);
          const state = checkpoint.result?.value;
          if (state) {
            if (!["empty", "files", "skills-long", "draft-image", "sent-image"].includes(state)) throw new Error("Unknown screenshot checkpoint: " + state);
            const image = await client.send("Page.captureScreenshot", {format: "png", fromSurface: true, captureBeyondViewport: false}, sessionId);
            await writeFile(join(screenshotDirectory, `${state}-${width}x${height}-${theme}.png`), Buffer.from(image.data, "base64"));
            await client.send("Runtime.evaluate", {expression: "window.composerScreenshot = null"}, sessionId);
          }
        }
        const input = await client.send("Runtime.evaluate", {expression: "window.fixture?.nativeInsert ?? null", returnByValue: true}, sessionId);
        if (typeof input.result?.value === "string") {
          await client.send("Input.insertText", {text: input.result.value}, sessionId);
          await client.send("Runtime.evaluate", {expression: "fixture.nativeInsert = null"}, sessionId);
        }
        const key = await client.send("Runtime.evaluate", {expression: "window.fixture?.nativeEscape === true", returnByValue: true}, sessionId);
        if (key.result?.value) {
          await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
          await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27}, sessionId);
          await client.send("Runtime.evaluate", {expression: "fixture.nativeEscape = false"}, sessionId);
        }
        const result = await client.send("Runtime.evaluate", {
          expression: 'document.querySelector("#test-result")?.textContent || ""',
          returnByValue: true
        }, sessionId);
        const value = result.result?.value;
        if (!value) continue;
        const report = JSON.parse(value);
        if (thumbnails) {
          report.imageRequests = thumbnailHTTP.requests.slice(imageRequestStart);
          if (!report.imageRequests.length || report.imageRequests.some(request => !request.authenticated)) failedReports.push(`Missing authenticated image request for ${width}x${height} ${theme}`);
        }
        if (!Array.isArray(report.failures) || !Array.isArray(report.results)) throw new Error("Malformed browser regression report.");
        if (visual || thumbnails) await writeFile(join(screenshotDirectory, `${thumbnails ? "thumbnail-measurements" : "measurements"}-${width}x${height}-${theme}.json`), JSON.stringify(report, null, 2) + "\n");
        if (report.failures.length) failedReports.push(`Browser ${thumbnails ? "thumbnails" : visual ? "visual" : "functional"} assertions failed (${width}x${height} ${theme}):\n${report.failures.join("\n")}\n${JSON.stringify(client.exceptions)}`);
        const expected = thumbnails ? expectedThumbnailAssertions : visual ? expectedVisualAssertions : expectedAssertions;
        if (!report.failures.length && (report.passed !== expected || report.results.length !== expected)) {
          throw new Error(`Expected ${expected} ${thumbnails ? "thumbnails" : visual ? "visual" : "functional"} assertions; received ${report.passed}.`);
        }
        console.log(`composer-context ${thumbnails ? "thumbnails" : visual ? "visual" : "functional"} ${fixtureURL.includes("workflow-queue") ? "edit/queue" : "legacy"} ${width}x${height} ${theme}: ${report.passed} passed, ${report.failures.length} failed`);
        break;
      }
      const final = await client.send("Runtime.evaluate", {expression: 'document.querySelector("#test-result")?.textContent || ""', returnByValue: true}, sessionId);
      if (!final.result?.value) throw new Error("No browser regression result was produced.");
      await client.send("Page.removeScriptToEvaluateOnNewDocument", {identifier}, sessionId);
      await client.send("Page.navigate", {url: "about:blank"}, sessionId);
      }
      }
      }
      if (!thumbnailHTTP.requests.length || thumbnailHTTP.requests.some(request => !request.authenticated)) failedReports.push("Sent thumbnails did not use the authenticated loopback HTTP image fixture.");
      if (failedReports.length) throw new Error(failedReports.join("\n\n"));
    };
    await Promise.race([exercise(), deadline]);
  } finally {
    clearTimeout(timer);
    client?.close();
    await thumbnailHTTP?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill("SIGKILL");
    if (exited) await Promise.race([exited, delay(2000)]);
    await rm(profile, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}

try {
  await run();
} catch (error) {
  console.error(`composer-context: ${error.message}`);
  process.exitCode = 1;
}
