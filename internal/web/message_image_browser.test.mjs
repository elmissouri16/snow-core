// Local-only native DOM regression. Node 22+ and Chrome; no packages/providers.
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {readFile, mkdtemp, rm} from "node:fs/promises";
import {createServer} from "node:http";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";
import test from "node:test";
import {chromeBinary, debuggingURL, connect} from "../../scripts/tests/browser/live-stream/cdp.mjs";

const js = await readFile(new URL("./static/generated/app.js", import.meta.url), "utf8");
const css = await readFile(new URL("./static/messages.css", import.meta.url), "utf8");
const coldProject = '00000000-0000-4000-8000-000000000001';
const escapeHTML = value => value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
function coldPage() {
  const props = {csrf: 'fixture-csrf', error: '', networkProfile: 'local', project: {id: coldProject, name: 'Saved workspace', path: '/fixture', available: true, trusted: false, skillsEnabled: false}, sessionID: 'saved', sessionTitle: 'Saved conversation', runtimeEnabled: true, hasHistory: true, nextURL: '', recoveryMessage: '', recoveryURL: ''};
  const text = '**Saved Markdown** <script>not executable</script>';
  const rows = ['cold-loaded', 'cold-held', 'cold-queued'].map(id => `<article class="catalog-message user-message" data-message-id="${id}" data-message-role="user"><div class="message-images"><div class="message-image" data-image-index="0" data-image-mime="image/png"><img class="message-image-preview" data-image-url="/projects/${coldProject}/sessions/saved/images/${id}/0" hidden></div></div><span class="message-source" hidden>${id}</span></article>`).join('');
  return `<div id="workspace"><div id="cold-root" data-react-page="workspace-cold" data-react-props="${escapeHTML(JSON.stringify(props))}"><section class="catalog-history" data-project="${coldProject}" data-session="saved"><div data-react-messages><article class="catalog-message" data-message-id="saved-text" data-message-role="assistant"><div class="message-body"><strong>Saved Markdown</strong> &lt;script&gt;not executable&lt;/script&gt;</div><span class="message-source" hidden>${escapeHTML(text)}</span><div class="message-tools"><details class="history-tool" data-history-tool-id="saved-tool" data-tool-name="read" data-status="completed" data-output-available="true"><pre class="activity-output">saved tool output</pre></details></div></article>${rows}</div></section></div></div>`;
}
const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=", "base64");

test("sent image templates retain bounded independent raster strips and saved scope", async () => {
  const messages = await readFile(new URL("./templates/messages.html", import.meta.url), "utf8");
  const projects = await readFile(new URL("./templates/projects.html", import.meta.url), "utf8");
  assert.match(messages, /define "message-images"/);
  assert.match(messages, /if lt \$position 8/);
  assert.match(messages, /template "message-images" .Images/);
  assert.match(projects, /template "message-images" .Images/);
  assert.match(projects, /class="catalog-history" data-project="{{.Project.ID}}" data-session="{{.SessionID}}"/);
  for (const mime of ["png", "jpeg", "gif", "webp"]) assert.ok(messages.includes(`eq .MIMEType "image/${mime}"`));
  assert.match(messages, /and \(eq .Role "user"\) \(not .Images\)/);
  assert.match(messages, /alt="Attached image/);
  const imageTemplate = messages.slice(messages.indexOf('{{define "message-images"}}'));
  assert.match(imageTemplate, /data-image-url="{{.URL}}"/);
  assert.doesNotMatch(imageTemplate, /\ssrc=/, "saved markup cannot start an image before global admission");
  assert.ok(messages.includes("{{else if and $raster (not .URL)}}Image pending{{else}}Image unavailable{{end}}"), "empty MIME is unavailable, never pending in server markup");
});

test("native React thumbnails and parent-owned ColdWorkspace saved-history lifetime", {timeout: 45000}, async t => {
  let binary;
  try { binary = chromeBinary(); } catch (error) { t.skip(error.message); return; }
  const requests = [], held = new Map(), canceled = [], mutations = [];
  const server = createServer((req, res) => {
    if (requests.length + mutations.length >= 2048) { res.writeHead(429); res.end(); return; }
    if (req.method !== 'GET') { mutations.push(req.method); res.writeHead(405); res.end(); return; }
    if (req.url === "/test-image-status") {
      res.setHeader("Content-Type", "application/json"); res.end(JSON.stringify({requests, canceled, active: [...held.keys()]}));
    } else if (req.url === "/test-image-release") {
      for (const [url, pending] of held) if (url.includes("/serial/")) { pending.setHeader("Content-Type", "image/png"); pending.end(png); held.delete(url); }
      res.end("released");
    } else if (req.url === "/") {
      res.setHeader("Content-Type", "text/html; charset=utf-8");
      res.setHeader("Set-Cookie", "image_fixture=paired; HttpOnly; SameSite=Strict; Path=/");
      res.end(`<!doctype html><style>:root{--text:#111;--muted:#555;--raised:#eee;--border:#ccc}*{box-sizing:border-box}${css}</style><div id="live-session" data-project="p" data-instance="i" data-session="s" data-runtime="true" data-message-edit-enabled="true"><div id="transcript" data-react-messages></div><div id="activities"></div></div><script type="module" src="/static/generated/app.js"></script>`);
    } else if (req.url === '/cold') {
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      res.end(`<!doctype html><style>${css}</style>${coldPage()}<script type="module" src="/static/generated/app.js"></script>`);
    } else if (req.url === '/?fixture=cancel') {
      if (req.headers['x-snow-navigation'] !== 'workspace') { res.writeHead(400); res.end(); return; }
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      res.end('<p>Invalid fragment</p>');
    } else if (req.url === '/?fixture=next') {
      if (req.headers['x-snow-navigation'] !== 'workspace') { res.writeHead(400); res.end(); return; }
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      res.end('<div id="workspace"><p id="departed">Departed saved workspace</p></div>');
    } else if (req.url === "/static/generated/app.js") {
      res.setHeader("Content-Type", "text/javascript; charset=utf-8"); res.end(js);
    } else if (req.url.startsWith("/projects/")) {
      requests.push(req.url);
      if (req.headers.cookie !== "image_fixture=paired" || req.headers.accept !== "image/png") { res.writeHead(403); res.end(); return; }
      if (["/serial/0?", "/timeout/0?", "/cancel/0?", "/dispose-active/0?", "/images/cold-held/0"].some(part => req.url.includes(part))) {
        held.set(req.url, res);
        res.on("close", () => { if (!res.writableEnded) canceled.push(req.url); held.delete(req.url); });
        return;
      }
      if (req.url.includes("/fail/") || req.url.includes("/saved-fail/")) { res.writeHead(404); res.end(); }
      else { res.setHeader("Content-Type", "image/png"); res.end(png); }
    } else { res.writeHead(404); res.end(); }
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const profile = await mkdtemp(join(tmpdir(), "snow-message-images-"));
  let chrome, client;
  try {
    chrome = spawn(binary, ["--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank"], {stdio: ["ignore", "ignore", "pipe"]});
    client = await connect(await debuggingURL(chrome));
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
    const errors = [];
    client.onEvent(event => {
      if (event.sessionId !== sessionId) return;
      if (event.method === "Runtime.exceptionThrown") errors.push(event.params.exceptionDetails.text);
      if (event.method === "Runtime.consoleAPICalled" && event.params.type === "error") errors.push(event.params.args.map(arg => arg.value || arg.description || '').join(' '));
    });
    await client.send("Page.enable", {}, sessionId);
    await client.send("Runtime.enable", {}, sessionId);
    await client.send("Emulation.setDeviceMetricsOverride", {width: 1280, height: 900, deviceScaleFactor: 1, mobile: false}, sessionId);
    await client.send("Page.navigate", {url: origin}, sessionId);
    for (let i = 0; i < 100; i++) {
      const ready = await client.send("Runtime.evaluate", {expression: "window.SnowReactReady === true && !!window.SnowMessages", returnByValue: true}, sessionId);
      if (ready.result.value) break;
      await delay(20);
    }
    const result = await client.send("Runtime.evaluate", {expression: `(${exercise.toString()})()`, awaitPromise: true, returnByValue: true}, sessionId);
    assert.equal(result.exceptionDetails, undefined, JSON.stringify(result.exceptionDetails));
    assert.ok(result.result.value >= 60, `Expected broad native assertions, got ${result.result.value}`);
    await client.send("Emulation.setDeviceMetricsOverride", {width: 320, height: 900, deviceScaleFactor: 1, mobile: false}, sessionId);
    const mobile = await client.send("Runtime.evaluate", {expression: `({width: document.querySelector('.message-image').getBoundingClientRect().width, height: document.querySelector('.message-image').getBoundingClientRect().height, overflow: document.documentElement.scrollWidth > innerWidth})`, returnByValue: true}, sessionId);
    assert.deepEqual(mobile.result.value, {width: 72, height: 72, overflow: false});
    assert.equal(requests.filter(url => url.includes("/fail/")).length, 1, "failed image never automatically retried");
    assert.equal(requests.filter(url => url.includes("/saved-fail/")).length, 1, "saved failed image never automatically retried");
    assert.equal(requests.filter(url => url.includes("/eight/")).length, 8, "all eight thumbnails actually fetched");
    assert.equal(requests.filter(url => url.includes("/serial/")).length, 8, "all deferred thumbnails eventually fetched");
    assert.equal(requests.filter(url => url.includes("/timeout/0?")).length, 1, "timed-out source never automatically retried");
    assert.ok(canceled.some(url => url.includes("/timeout/0?")), "timeout aborts browser read");
    assert.ok(canceled.some(url => url.includes("/cancel/0?")), "row eviction aborts browser read");
    assert.ok(canceled.some(url => url.includes("/dispose-active/0?")), "disposal aborts browser read");
    assert.ok(requests.every(url => /^\/projects\/p\/(runtime\/images|sessions\/s\/images)\//.test(url)), "no rejected route fetched");
    let coldAssertions = 0;
    for (const width of [320, 1280]) {
      await client.send('Emulation.setDeviceMetricsOverride', {width, height: 900, deviceScaleFactor: 1, mobile: false}, sessionId);
      await client.send('Page.navigate', {url: origin + '/cold'}, sessionId);
      for (let n = 0; n < 150; n++) {
        const ready = await client.send('Runtime.evaluate', {expression: 'window.SnowReactReady === true && !!window.SnowNavigation && document.querySelector("#cold-root")?.dataset.reactMounted === "true"', returnByValue: true}, sessionId);
        if (ready.result.value) break;
        await delay(20);
      }
      const cold = await client.send('Runtime.evaluate', {expression: `(${exerciseCold.toString()})()`, awaitPromise: true, returnByValue: true}, sessionId);
      assert.equal(cold.exceptionDetails, undefined, JSON.stringify(cold.exceptionDetails));
      assert.ok(cold.result.value >= 20, 'parent-owned lifecycle assertions executed');
      coldAssertions += cold.result.value;
    }
    assert.deepEqual(mutations, [], 'saved-history lifecycle never submits or activates');
    assert.deepEqual(errors, [], "production module has no uncaught errors or React diagnostics");
    t.diagnostic(`${result.result.value} native React thumbnail assertions; ${coldAssertions} parent-owned ColdWorkspace assertions at 320/1280; sizing and server-side request/cancellation checks also passed`);
  } finally {
    client?.close();
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) {
      const exited = new Promise(resolve => chrome.once("exit", resolve)); chrome.kill("SIGKILL");
      await Promise.race([exited, delay(1500)]);
    }
    for (const pending of held.values()) pending.destroy();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
    await rm(profile, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
});

async function exercise() {
  let assertions = 0;
  const check = (value, label) => { assertions++; if (!value) throw new Error(label); };
  const root = document.querySelector("#live-session"), transcript = document.querySelector("#transcript"), activities = document.querySelector("#activities");
  const api = window.SnowMessages;
  api.renderSnapshot(activities, {messages: [{id: "marker", role: "tool_activity"}, {id: "answer", role: "assistant", text: "done", html: "<p>done</p>"}], activities: [{id: "read-1", message_id: "marker", tool: "read", status: "completed", output: "ok"}]}, transcript);
  check(transcript.querySelector('[data-message-id="marker"] [data-activity-id="read-1"]')?.dataset.status === "completed", "one snapshot places chronological activity in its message marker");
  check(activities.hidden, "snapshot with fully associated activity hides the fallback region");
  window.SnowVisibility.renderActivities(activities, {activities: []}, transcript);
  api.render(transcript, []);
  const url = (id, index = 0, query = "instance_id=i&session_id=s") => `/projects/p/runtime/images/${id}/${index}?${query}`;
  const image = (id, index = 0) => ({index, mime_type: "image/png", url: url(id, index)});
  const message = (id, images, text = "", extra = {}) => ({id, role: "user", text, images, can_edit: true, ...extra});
  const row = () => transcript.firstElementChild;
  const preview = () => row().querySelector(".message-image-preview");
  const tile = () => row().querySelector(".message-image");
  const wait = async predicate => {
    for (let n = 0; n < 550; n++) { if (await predicate()) return; await new Promise(resolve => setTimeout(resolve, 20)); }
    throw new Error("Image load timed out");
  };
  api.render(transcript, [message("pending", [{index: 0, mime_type: "image/png", url: ""}])]);
  check(transcript.children.length === 1, "image-only pending row retained");
  check(row().querySelector(".message-body").hidden, "image-only body hides empty bubble");
  check(!preview().hasAttribute("src"), "pending metadata does not fetch a source");
  check(tile().textContent === "Image pending", "pending receipt placeholder");
  check(!row().querySelector("[data-message-edit], [data-message-reuse]"), "image edit/reuse suppressed");
  check(row()._snowEditable === false && row()._snowReusable === false, "text-only actions disabled internally");
  const pendingRow = row(), pendingImage = preview();
  api.render(transcript, [message("pending", [image("pending")], "caption")]);
  await wait(() => tile().dataset.imageState === "loaded");
  check(row() === pendingRow && preview() !== pendingImage && !pendingImage.isConnected, "ack retains message row but retires the prior image-identity node");
  check(!preview().hidden && preview().alt === "Attached image 1", "generic numbered alt and visible preview");
  check(row().querySelector(".message-images").parentElement === row(), "strip separate from body");
  check(!row().querySelector(".message-body").contains(preview()), "body replacement cannot replace image");
  check(!row().querySelector(".message-body").hidden, "caption visible");
  check(getComputedStyle(preview()).objectFit === "cover", "thumbnail cover sizing");
  check(tile().getBoundingClientRect().width === 96 && tile().getBoundingClientRect().height === 96, "desktop 96px square");
  let sourceWrites = 0;
  const observer = new MutationObserver(records => { sourceWrites += records.filter(record => record.attributeName === "src").length; });
  const loadedImage = preview();
  observer.observe(loadedImage, {attributes: true});
  for (let n = 0; n < 20; n++) api.render(transcript, [message("pending", [image("pending")], `caption ${n}`)]);
  api.enhance(root);
  await Promise.resolve(); observer.disconnect();
  check(sourceWrites === 0, "SSE/text/enhancement never rewrite identical image src");
  check(preview() === loadedImage, "SSE retains image DOM for unchanged identity");
  check(row()._snowCopyText === "caption 19", "copy uses only current text");
  const rejected = [
    "https://example.invalid/image.png", "data:image/png;base64,eA==", "blob:http://127.0.0.1/example",
    "javascript:alert(1)", "//example.invalid/image.png", "/projects/q/runtime/images/bad/0?instance_id=i&session_id=s",
    url("other"), url("bad", 1), url("bad", 0, "instance_id=old&session_id=s"), url("bad", 0, "instance_id=i&session_id=old"),
    url("bad", 0, "instance_id=i&session_id=s&session_id=s"), url("bad", 0, "instance_id=i&session_id=s&evil=yes"),
    url("bad") + "#fragment", " " + url("bad"), "/projects/p/runtime/../runtime/images/bad/0?instance_id=i&session_id=s",
    `/projects/p/sessions/s/images/bad/0`, location.origin.replace("http:", "https:") + url("bad")
  ];
  for (const raw of rejected) {
    api.render(transcript, [message("bad", [{...image("bad"), url: raw}])]);
    check(!preview().hasAttribute("src"), `reject source ${raw}`);
    check(tile().textContent === "Image unavailable", "rejected URL gives clear fallback");
    api.enhance(root);
    check(tile().textContent === "Image unavailable" && tile().dataset.imageState === "unavailable", "enhancement preserves rejected source identity, never pending");
  }
  for (const mime of ["image/svg+xml", "text/html", "image/avif", "IMAGE/PNG"]) {
    api.render(transcript, [message("bad", [{...image("bad"), mime_type: mime}])]);
    check(!preview().hasAttribute("src"), `raster MIME rejects ${mime}`);
    check(!row().querySelector("[data-message-edit], [data-message-reuse]"), "invalid image metadata still suppresses text actions");
  }
  for (const mime of ["", undefined]) {
    const unsupported = message("unsupported", [{index: 0, mime_type: mime, url: ""}]);
    api.render(transcript, [unsupported]);
    check(transcript.children.length === 1 && row().querySelectorAll(".message-image").length === 1, "unsupported image-only history retains its row and metadata slot");
    check(tile().dataset.imageState === "unavailable" && tile().textContent === "Image unavailable", "empty or omitted historical MIME is unavailable, not pending");
    check(!preview().hasAttribute("src"), "unsupported historical metadata never fetches image bytes");
    check(row()._snowHasImages && !row()._snowEditable && !row()._snowReusable, "unsupported image metadata retains text-control suppression");
    check(!row().querySelector("[data-message-edit], [data-message-reuse]"), "unsupported image history offers no text-only actions");
    api.enhance(root);
    check(tile().textContent === "Image unavailable", "enhancement never relabels unsupported image as pending");
  }
  api.render(transcript, [message("eight", Array.from({length: 12}, (_, index) => image("eight", index)))]);
  check(row().querySelectorAll(".message-image").length === 8, "bounded to eight tiles");
  await wait(() => [...row().querySelectorAll(".message-image")].every(node => node.dataset.imageState === "loaded"));
  check(row().querySelectorAll('.message-image-preview:not([hidden])').length === 8, "all eight images eventually display");
  check(row().querySelectorAll(".message-image-preview")[7].alt === "Attached image 8", "eighth alt is numbered");
  const requestStatus = async () => (await fetch("/test-image-status")).json();
  const serial = message("serial", Array.from({length: 8}, (_, index) => image("serial", index)));
  api.render(transcript, [serial, message("serial-peer", [image("serial-peer")])]);
  await wait(async () => (await requestStatus()).requests.some(source => source.includes("/serial/0?")));
  await new Promise(resolve => setTimeout(resolve, 80));
  check(transcript.querySelectorAll('.message-image-preview[src]').length === 0 && transcript.querySelectorAll('[data-image-state="loading"]').length === 1, "one globally active fetch, no Blob source before its bounded body arrives");
  check(preview().loading === "eager", "admitted image cannot stall behind offscreen lazy loading");
  check((await requestStatus()).requests.filter(source => source.includes("/serial/") || source.includes("/serial-peer/")).length === 1, "deferred first request prevents all sibling and peer requests");
  check(transcript.querySelectorAll('[data-image-state="queued"]').length === 8, "remaining metadata is queued without fetching");
  const serialFirst = preview();
  api.render(transcript, [serial, message("serial-peer", [image("serial-peer")], "updated while queued")]);
  check(preview() === serialFirst, "queued update retains active image node");
  await fetch("/test-image-release");
  await wait(() => [...transcript.querySelectorAll('.message-image')].every(node => node.dataset.imageState === "loaded"));
  check(transcript.querySelectorAll('.message-image-preview:not([hidden])').length === 9, "serial queue eventually loads all eight images and next gallery");

  api.render(transcript, [message("cancel", [image("cancel")]), message("cancel-next", [image("cancel-next")])]);
  await wait(async () => (await requestStatus()).requests.some(source => source.includes("/cancel/0?")));
  const canceledPreview = preview();
  api.render(transcript, [message("cancel-next", [image("cancel-next")])]);
  await wait(() => tile().dataset.imageState === "loaded");
  check(!canceledPreview.hasAttribute("src"), "row eviction clears active source");
  check(preview().src.startsWith("blob:") && preview().dataset.imageUrl === url("cancel-next"), "row eviction advances exact queued survivor through a Blob preview");

  api.render(transcript, [message("dispose-active", [image("dispose-active")]), message("never-started", [image("never-started")])]);
  await wait(async () => (await requestStatus()).requests.some(source => source.includes("/dispose-active/0?")));
  const retiredImage = preview();
  api.dispose();
  check(!retiredImage.isConnected && !transcript.children.length, "dispose unmounts React and retires the active read");
  await wait(async () => (await requestStatus()).canceled.some(source => source.includes("/dispose-active/0?")));
  api.render(transcript, [message("after-dispose", [image("after-dispose")])]);
  await wait(() => tile().dataset.imageState === "loaded");
  check(!(await requestStatus()).requests.some(source => source.includes("/never-started/")), "disposed queued work never starts");

  const timeoutMessage = message("timeout", [image("timeout"), image("timeout", 1)]);
  api.render(transcript, [timeoutMessage]);
  await wait(async () => (await requestStatus()).requests.some(source => source.includes("/timeout/0?")));
  check(!row().querySelectorAll("img")[1].hasAttribute("src"), "second timeout-gallery image initially waits");
  await wait(() => row().querySelectorAll('.message-image')[1].dataset.imageState === "loaded");
  check(tile().dataset.imageState === "unavailable" && !preview().hasAttribute("src"), "8-second timeout releases stalled source and displays fallback");
  api.render(transcript, [timeoutMessage]); api.enhance(root);
  check(tile().dataset.imageState === "unavailable" && !preview().hasAttribute("src"), "timeout remains sticky through render and enhancement");
  api.render(transcript, [message("fail", [image("fail")]), message("after-fail", [image("after-fail")])]);
  await wait(() => tile().dataset.imageState === "unavailable" && transcript.lastElementChild.querySelector(".message-image").dataset.imageState === "loaded");
  check(transcript.lastElementChild.querySelector("img").src.startsWith("blob:") && transcript.lastElementChild.querySelector("img").dataset.imageUrl === url("after-fail"), "HTTP failure advances the shared queue to the exact next image");
  const failedImage = preview();
  for (let n = 0; n < 10; n++) api.render(transcript, [message("fail", [image("fail")], "changed")]);
  api.enhance(root); failedImage.dispatchEvent(new Event("load"));
  check(preview() === failedImage && preview().hidden, "failed preview remains failed through updates and stale load");
  check(tile().textContent === "Image unavailable", "error is explicit and sticky");
  api.render(transcript, [message("turn", [{...image("turn"), url: url("turn") + "&turn_id=turn-one"}])]);
  await wait(() => tile().dataset.imageState === "loaded");
  check(preview().src.startsWith("blob:") && (await requestStatus()).requests.includes(url("turn") + "&turn_id=turn-one"), "optional turn identity accepted by the exact credentialed read");
  const turnImage = preview(), turnTile = tile();
  root.dataset.instance = "next";
  turnImage.dispatchEvent(new Event("error"));
  check(turnTile.dataset.imageState === "loaded", "stale scope callback ignored");
  api.render(transcript, [message("turn", [image("turn")])]);
  check(!preview().hasAttribute("src"), "old instance URL revoked");
  root.dataset.instance = "i";
  api.render(transcript, [message("dispose", [image("dispose")])]);
  await wait(() => tile().dataset.imageState === "loaded");
  const disposedImage = preview(), disposedTile = tile(), disposedURL = disposedImage.src;
  api.dispose(); disposedImage.dispatchEvent(new Event("error"));
  check(disposedTile.dataset.imageState === "loaded" && !disposedTile.isConnected, "disposed callback ignored on the retired React node");
  check(await fetch(disposedURL).then(() => false, () => true), "unmount revokes the retained Blob URL");
  api.enhance(root);
  await wait(() => tile().dataset.imageState === "loaded");
  check(preview() !== disposedImage && preview().src !== disposedURL, "same-host remount restores public projection with fresh image ownership");
  disposedImage.dispatchEvent(new Event("error"));
  check(tile().dataset.imageState === "loaded", "departed image error cannot alter the remounted thumbnail");
  api.render(transcript, []); disposedImage.dispatchEvent(new Event("load"));
  check(!transcript.children.length && !disposedImage.isConnected, "removed image stale callback cannot recreate a message");
  api.render(transcript, [message("text", [], "only text")]);
  check(!row().querySelector(".message-images") && row()._snowEditable, "ordinary text edit control preserved");
  check(!!row().querySelector("[data-message-edit]"), "text edit button remains");
  root.dataset.messageEditEnabled = "false";
  api.render(transcript, [message("text", [], "only text")]);
  check(!!row().querySelector("[data-message-reuse]"), "legacy text reuse preserved");
  api.render(transcript, [message("text", [image("text")], "text plus image")]);
  check(!row().querySelector("[data-message-reuse]"), "legacy image reuse suppressed");
  const saved = document.createElement("div");
  saved.className = "catalog-history"; saved.dataset.project = "p"; saved.dataset.session = "s";
  saved.dataset.reactMessages = '';
  saved.innerHTML = ['saved', 'saved-fail', 'saved-unsupported'].map(id => `<article class="catalog-message user-message" data-message-id="${id}" data-message-role="user" data-message-has-images="true"><div class="message-images"><div class="message-image" data-image-index="0" data-image-mime="${id === 'saved-unsupported' ? '' : 'image/png'}"><img class="message-image-preview" alt="Attached image 1" data-image-url="${id === 'saved-unsupported' ? '' : `/projects/p/sessions/s/images/${id}/0`}" hidden><span class="message-image-fallback" hidden>Image unavailable</span></div></div><pre class="message-body"></pre><span class="message-source" hidden></span></article>`).join('');
  document.body.append(saved);
  check([...saved.querySelectorAll('img')].every(img => !img.hasAttribute("src")), "saved markup defers all image sources until admission");
  const savedImg = saved.querySelector('img');
  api.enhance(saved);
  await wait(() => [...saved.querySelectorAll(".message-image")].every(node => ["loaded", "unavailable"].includes(node.dataset.imageState)));
  check(savedImg !== saved.querySelector('img') && !savedImg.isConnected, "saved public SSR is imported into a single React-owned image tree");
  check(saved.querySelector('.message-image').dataset.imageState === "loaded", "saved image displayed");
  check(saved.querySelectorAll('.message-image')[1].dataset.imageState === "unavailable", "cached saved failure enhanced");
  check(!saved.querySelector('[data-message-edit], [data-message-reuse]'), "saved image text-only actions suppressed");
  check(saved.querySelector('.message-body').hidden, "saved image-only row preserved without empty bubble");
  const savedReactImage = saved.querySelector('img');
  api.enhance(saved);
  check(saved.querySelector('img') === savedReactImage, "repeated enhancement retains the current React image owner");
  const unsupportedSaved = saved.lastElementChild;
  const unsupportedTile = unsupportedSaved.querySelector(".message-image");
  check(unsupportedTile.dataset.imageState === "unavailable", "saved empty-MIME metadata is unavailable");
  check(unsupportedTile.querySelector(".message-image-fallback").textContent === "Image unavailable" && !unsupportedTile.querySelector(".message-image-fallback").hidden, "saved unsupported fallback is visible and not pending");
  check(unsupportedSaved._snowHasImages && !unsupportedSaved._snowEditable && !unsupportedSaved._snowReusable, "saved unsupported image retains metadata ownership");
  check(!unsupportedSaved.querySelector("[data-message-edit], [data-message-reuse]"), "saved unsupported image suppresses text-only actions");
  check(unsupportedSaved.querySelector(".message-body").hidden && !unsupportedTile.querySelector("img").hasAttribute("src"), "saved unsupported image-only row remains without fetching bytes");
  return assertions;
}

async function exerciseCold() {
  let assertions = 0;
  const check = (value, label) => { assertions++; if (!value) throw Error(label); };
  const wait = async predicate => {
    for (let n = 0; n < 150; n++) { if (await predicate()) return; await new Promise(resolve => setTimeout(resolve, 20)); }
    throw Error('Cold history lifecycle did not settle');
  };
  const status = () => fetch('/test-image-status').then(response => response.json());
  const parent = document.querySelector('#cold-root');
  const history = () => parent.querySelector('[data-react-saved-history]');
  const image = () => parent.querySelector('[data-message-id="cold-loaded"] img');
  await wait(async () => image()?.closest('.message-image').dataset.imageState === 'loaded' && (await status()).active.some(url => url.includes('/cold-held/')));
  const before = await status();
  const savedRow = parent.querySelector('[data-message-id="saved-text"]');
  const originalImage = image(), originalURL = image().src;
  check(parent.dataset.reactMounted === 'true' && history()?.parentElement !== null, 'actual ColdWorkspace parent owns saved history');
  check(!parent.querySelector('[data-react-messages]'), 'parent composition has no competing standalone message root');
  check(savedRow.querySelector('strong')?.textContent === 'Saved Markdown' && !savedRow.querySelector('script'), 'initial parent presentation preserves escaped Markdown');
  check(savedRow._snowCopyText === '**Saved Markdown** <script>not executable</script>', 'copy metadata preserves exact public source');
  const tool = savedRow.querySelector('details'); tool.querySelector('summary').click();
  check(tool.open && tool.textContent.includes('saved tool output'), 'saved tool disclosure remains usable in the parent tree');
  window.SnowWorkspace.updateDraft({workspaceText: 'unsent local draft', workspaceEnabled: true});
  check(parent.querySelector('[data-message-id="saved-text"]') === savedRow && tool.open && image() === originalImage, 'parent presentation updates retain history, disclosure and image nodes');
  check(parent.querySelector('#workspace-prompt').value === 'unsent local draft' && !parent.querySelector('#workspace-prompt').hasAttribute('name'), 'cold draft is separate from activation submission');
  const beforeHTML = history().innerHTML;
  window.SnowMessages.render(history(), [{id: 'forged', role: 'user', text: 'not owned'}]);
  window.SnowMessages.updateActions(history(), {edit: {hidden: false, disabled: false}});
  window.SnowMessages.enhance(parent);
  check(history().innerHTML === beforeHTML, 'standalone facade cannot render or admit actions inside parent-owned history');
  window.SnowMessages.dispose();
  await new Promise(resolve => setTimeout(resolve, 60));
  check(image() === originalImage && savedRow.isConnected && tool.open, 'global standalone disposal does not unmount parent history');
  check((await status()).canceled.length === before.canceled.length && (await status()).active.some(url => url.includes('/cold-held/')), 'standalone disposal does not cancel the parent-held image read');
  check(await fetch(originalURL).then(response => response.ok), 'standalone disposal leaves the parent Blob URL alive');
  check(!parent.querySelector('[data-message-edit]:not([hidden]), [data-message-reuse]:not([hidden]), [data-message-regenerate]:not([hidden])'), 'saved history cannot acquire live mutation controls');
  await window.SnowNavigation.visit('/?fixture=cancel', {history: 'none'}).then(() => { throw Error('invalid fragment accepted'); }, () => {});
  check(parent.isConnected && image() === originalImage && tool.open, 'rejected native navigation preserves the exact parent owner');
  check((await status()).canceled.length === before.canceled.length, 'rejected navigation does not retire the image read');
  // Exercise the production page lifecycle listeners, not a fabricated React root.
  window.dispatchEvent(new PageTransitionEvent('pagehide', {persisted: true}));
  await wait(async () => (await status()).canceled.length === before.canceled.length + 1);
  check(!parent.children.length && !savedRow.isConnected, 'pagehide unmounts connected parent descendants');
  check(await fetch(originalURL).then(() => false, () => true), 'parent pagehide revokes its Blob URL');
  const readsBeforeRestore = (await status()).requests.length;
  for (let n = 0; n < 3; n++) window.dispatchEvent(new PageTransitionEvent('pageshow', {persisted: true}));
  await wait(async () => image()?.closest('.message-image').dataset.imageState === 'loaded' && (await status()).active.some(url => url.includes('/cold-held/')));
  check(parent.querySelector('[data-message-id="saved-text"]')?._snowCopyText === savedRow._snowCopyText, 'same-host lifecycle restore retains the opaque public history projection');
  check(image() !== originalImage && image().src !== originalURL, 'restored parent obtains fresh image ownership');
  check((await status()).requests.length === readsBeforeRestore + 2, 'repeated lifecycle notifications mount exactly one image owner');
  check(parent.querySelectorAll('[data-react-saved-history]').length === 1 && !parent.querySelector('[data-react-messages]'), 'restoration never creates nested message roots');
  const restoredURL = image().src;
  let cleanedBeforeDetach = false;
  document.addEventListener('snow:navigation-before-swap', event => {
    if (event.detail.target?.id === 'workspace') cleanedBeforeDetach = parent.isConnected && !parent.children.length;
  }, {once: true});
  await window.SnowNavigation.visit('/?fixture=next', {history: 'none'});
  await wait(async () => (await status()).canceled.length === before.canceled.length + 2);
  check(cleanedBeforeDetach && !parent.isConnected && !!document.querySelector('#departed'), 'native cleanup unmounts parent before detaching its ancestor');
  check(await fetch(restoredURL).then(() => false, () => true), 'committed ancestor replacement revokes the restored Blob');
  check(!(await status()).requests.some(url => url.includes('/cold-queued/')), 'retired queued parent image is never fetched');
  const afterDeparture = (await status()).requests.length;
  originalImage.dispatchEvent(new Event('load')); originalImage.dispatchEvent(new Event('error'));
  check(!parent.children.length, 'stale detached image callbacks cannot recreate parent history');
  const props = JSON.parse(parent.dataset.reactProps); props.sessionID = 'different-session';
  parent.dataset.reactProps = JSON.stringify(props);
  document.querySelector('#workspace').append(parent);
  window.dispatchEvent(new PageTransitionEvent('pageshow', {persisted: true}));
  check(parent.dataset.reactMounted === 'true' && !history() && !parent.querySelector('[data-message-id]'), 'changed session identity rejects the old same-host history projection');
  await new Promise(resolve => setTimeout(resolve, 60));
  check((await status()).requests.length === afterDeparture, 'scope mismatch never rereads old image routes');
  return assertions;
}
