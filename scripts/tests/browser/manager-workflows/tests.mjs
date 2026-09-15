// Native Chrome against real production HTTP/RPC/agent; only provider events
// are gated by the existing opt-in Go fixture. No asset server or network stub.
import {writeFile, readdir, readFile} from "node:fs/promises";
import {join, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {spawn} from "node:child_process";
import {setTimeout as delay} from "node:timers/promises";
import {navigationChecks} from "./navigation.mjs";
import {versionsChecks} from "./versions.mjs";
import {trustChecks} from "./trust.mjs";

export async function exercise({client, sessionId, ready, directory, width, theme}) {
  const results = [], failures = [], requests = [], responses = [], snapshots = [], errors = [];
  const send = (method, params = {}) => client.send(method, params, sessionId);
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const evaluate = async expression => { const value = await send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}); if (value.exceptionDetails) throw Error(`DOM observation failed: ${value.exceptionDetails.text}`); return value.result?.value; };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const wait = async (expression, label, timeout = 15000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { if (await evaluate(expression)) return; await delay(35); }
    throw Error(`Timed out: ${label}; state=${await evaluate('JSON.stringify({status:document.querySelector("#live-status")?.textContent,connection:document.querySelector("#live-connection")?.textContent,error:document.querySelector("#live-error")?.textContent,queue:document.querySelector("[data-queue-error]")?.textContent,activation:document.querySelector("[data-action-error]")?.textContent,flow:document.querySelector("#workspace-flow-error")?.textContent,alerts:[...document.querySelectorAll("[role=alert]")].map(e=>e.textContent),cold:document.querySelector("[data-react-page=workspace-cold]")?.dataset.reactMounted,opening:!!document.querySelector("[data-react-page=workspace-opening]"),pending:document.querySelector("[data-runtime-open]")?.dataset.pending,url:location.pathname+location.search})')}`);
  };
  const key = async (key, code, windowsVirtualKeyCode, modifiers = 0) => { await send("Input.dispatchKeyEvent", {type: "keyDown", key, code, windowsVirtualKeyCode, modifiers, ...(key === "a" && modifiers ? {commands: ["selectAll"]} : {}), ...(key === "Enter" ? {text: "\r", unmodifiedText: "\r"} : key === " " ? {text: " ", unmodifiedText: " "} : {})}); await send("Input.dispatchKeyEvent", {type: "keyUp", key, code, windowsVirtualKeyCode, modifiers}); };
  const click = async selector => {
    const point = await evaluate(`(() => {const el=${q(selector)};if(!el)return null;el.scrollIntoView({block:'center',inline:'nearest'});let last=null;for(const r of el.getClientRects()){const x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;const hit=r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y));last={x,y,hit,rect:{left:r.left,top:r.top,width:r.width,height:r.height},hitTag:document.elementFromPoint(x,y)?.tagName,hitID:document.elementFromPoint(x,y)?.id,hitClass:document.elementFromPoint(x,y)?.className,disclosureOpen:el.closest('details')?.open,ready:document.readyState};if(hit)return last;}return last;})()`);
    if (!point?.hit) throw Error(`Native pointer target missing or obscured: ${selector} ${JSON.stringify(point)}`);
    await send("Input.dispatchMouseEvent", {type: "mouseMoved", x: point.x, y: point.y});
    await send("Input.dispatchMouseEvent", {type: "mousePressed", x: point.x, y: point.y, button: "left", clickCount: 1});
    await send("Input.dispatchMouseEvent", {type: "mouseReleased", x: point.x, y: point.y, button: "left", clickCount: 1}); await delay(20);
  };
  const focus = async selector => { for (let i = 0; i < 100; i++) { if (await evaluate(`document.activeElement===${q(selector)}`)) return; await key("Tab", "Tab", 9); } throw Error("Native Tab could not reach " + selector); };
  const replace = async text => { await click("#live-prompt"); await key("a", "KeyA", 65, process.platform === "darwin" ? 4 : 2); await send("Input.insertText", {text}); };
  const snapshot = () => evaluate(`fetch(${JSON.stringify(`/projects/${ready.project}/runtime`)}).then(r=>{if(!r.ok)throw Error('Snapshot unavailable');return r.json()})`);
  const gate = (call, phase) => writeFile(join(directory, `release-${call}-${phase}`), "release", {mode: 0o600});
  const callCount = async () => (await readdir(directory)).filter(name => /^call-\d+$/.test(name)).length;
  const posts = suffix => requests.filter(request => request.method === "POST" && request.path.endsWith("/" + suffix));
  const waitCall = async count => { for (let i = 0; i < 400 && await callCount() < count; i++) await delay(25); check(await callCount() === count, `Exactly ${count} real fake-provider calls observed`); };
  const history = () => new Promise((resolve, reject) => {
    const child = spawn("python3", [join(dirname(fileURLToPath(import.meta.url)), "history.py"), join(directory, "sessions")], {stdio: ["ignore", "pipe", "pipe"], timeout: 10000});
    let output = "", error = "";
    child.stdout.on("data", chunk => { output += chunk; if (output.length > 1 << 20) { child.kill(); reject(Error("Durable fixture observation exceeds 1 MiB")); } });
    child.stderr.on("data", chunk => { error = (error + chunk).slice(-1024); });
    child.once("error", () => reject(Error("Python read-only durable fixture observer unavailable")));
    child.once("close", code => { if (code !== 0) reject(Error("Read-only durable history observer failed: " + error)); else { try { resolve(JSON.parse(output)); } catch { reject(Error("Invalid durable fixture summary")); } } });
  });
  const navigation = navigationChecks({ready,directory,click,key,insert: text=>send("Input.insertText",{text}),evaluate,wait,check,assert,requests,callCount,snapshot,width});
  const streamBuffers = new Map();
  const streamData = (id, encoded) => {
    let buffer = (streamBuffers.get(id) || "") + Buffer.from(encoded || "", "base64").toString("utf8");
    let end;
    while ((end = buffer.indexOf("\n\n")) >= 0) {
      const frame = buffer.slice(0, end); buffer = buffer.slice(end + 2);
      if (frame.startsWith("event: snapshot\n")) {
        try { snapshots.push(JSON.parse(frame.split("\n").filter(line => line.startsWith("data:")).map(line => line.slice(5).trimStart()).join("\n"))); } catch { errors.push("Malformed production SSE snapshot"); }
        if (snapshots.length > 1000) snapshots.shift();
      }
    }
    if (buffer.length > 1 << 20) { errors.push("SSE observation exceeds 1 MiB"); buffer = ""; }
    streamBuffers.set(id, buffer);
  };
  const dispose = client.onEvent(event => {
    if (event.sessionId !== sessionId) return;
    if (event.method === "Network.requestWillBeSent") {
      const request = event.params.request;
      if (request.url.startsWith(ready.origin + "/")) {
        const fields = new URLSearchParams(request.postData || "");
        // Keep only public field names and synthetic fixture text. Never retain
        // pairing credentials, cookies, CSRF values or opaque queue authority.
        requests.push({method: request.method, path: new URL(request.url).pathname, keys: [...fields.keys()], text: fields.get("text")});
      } else if (/^https?:/.test(request.url)) errors.push("Unexpected external page request");
      if (requests.length > 2000) errors.push("Unexpected production request loop");
    }
    if (event.method === "Network.responseReceived") {
      const response = event.params.response, path = new URL(response.url).pathname;
      responses.push({path, status: response.status});
      if (path.endsWith("/events")) {
        const id = event.params.requestId; streamBuffers.set(id, "");
        send("Network.streamResourceContent", {requestId: id}).then(result => streamData(id, result.bufferedData)).catch(() => {});
      }
    }
    if (event.method === "Network.dataReceived" && streamBuffers.has(event.params.requestId) && event.params.data) streamData(event.params.requestId, event.params.data);
  });
  try {
    await send("Page.addScriptToEvaluateOnNewDocument", {source: `localStorage.setItem("snow-manager-theme",${JSON.stringify(theme)});window.managerWorkflowErrors=[];addEventListener('error',e=>managerWorkflowErrors.push(e.message));addEventListener('unhandledrejection',e=>managerWorkflowErrors.push(String(e.reason)));`});
    await send("Page.navigate", {url: `${ready.origin}/?view=projects&project=${ready.project}`});
    await wait(`!!${q('[data-runtime-open]')}`, "production activation page");
    await wait('document.readyState==="complete"', "production assets loaded");
    for (const file of ["generated/app.js", "queue.css"]) {
      check(responses.some(response => response.path.endsWith("/" + file) && response.status === 200), `${file}: actual production asset GET returns 200, not an exported-fixture bypass`);
    }
    await assert(`typeof window.SnowQueue?.init==='function' && typeof window.SnowQueue?.enqueue==='function'`, "Actual production HTTP-loaded SnowQueue controller initialized");
    check(await callCount() === 0, "Browsing real manager and loading assets never calls provider");
    await navigation.organizeInactive();
    await click('[data-runtime-open] input[name="confirm"]'); await click('[data-runtime-open] button[type="submit"]');
    await wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live'`, "native trusted activation starts real RPC worker", 25000);
    const initial = await snapshot();
    check(initial.provider === "fake" && initial.model === "fake-1", "Real manager selects only isolated scripted fake provider");
    check(await callCount() === 0, "Explicit activation alone does not run a provider turn");
    await assert(`${q('#live-session')}.dataset.queueNextEnabled==='true'`, "Production manager advertises real queue backend capability");
    const rootText = "Production manager root input.", queuedText = "Queued production follow-up — exact café 😀 text.", heldText = "Review only after Stop; never replay this input.";
    await replace(rootText); await click("#live-send");
    await wait(`${q('#live-transcript')}.textContent.includes('First chunk is visible.')`, "real gated root provider prefix"); await waitCall(1);
    await wait(`!${q('[data-queue-next]')}.hidden && !${q('[data-queue-next]')}.disabled`, "production queue is authoritatively open");
    check((await snapshot()).instance_id === initial.instance_id, "Running root retains activated worker instance");
    await replace(queuedText); await focus("[data-queue-next]"); await key("Enter", "Enter", 13);
    await wait(`${q('[data-queue-item-id]')}?.textContent.includes(${JSON.stringify(queuedText)}) && ${q('#live-prompt')}.value===''`, "actual enqueue acknowledgement renders separate pending item");
    await assert(`getComputedStyle(${q("[data-queue-items]")}).maxHeight!=='none' && getComputedStyle(${q("[data-queue-items]")}).overflowY==='auto'`, "Production-served queue stylesheet applies bounded scrollable pending panel");
    check(posts("queue-enqueue").length === 1 && posts("queue-enqueue")[0].text === queuedText, "Exactly one real queue-enqueue POST preserves submitted text");
    check(JSON.stringify(posts("queue-enqueue")[0].keys.toSorted()) === JSON.stringify(["csrf", "instance_id", "session_id", "queue_token", "queue_revision", "text"].toSorted()), "Real queue POST uses only exact instance/session/token/revision/text authority fields");
    check(posts("prompt").length === 1 && await callCount() === 1, "Queued input creates neither a second normal prompt nor premature provider request");
    await assert(`!${q('#live-transcript')}.textContent.includes(${JSON.stringify(queuedText)})`, "Pending queued input is not optimistically inserted in chat");
    let durable = (await history()).find(session => session.session_id === initial.session_id);
    check(!!durable && durable.entries.filter(entry => entry.role === "user" && entry.text === rootText).length === 1, "Root user exists exactly once in real durable SQLite history");
    check(durable && !durable.entries.some(entry => entry.role === "user" && entry.text === queuedText), "Pending-only queued input is absent from durable conversation history");
    await assert(`document.documentElement.scrollWidth<=innerWidth+1 && ${q('[data-queue-next]')}.getBoundingClientRect().right<=innerWidth+1`, `${width}×740 ${theme}: real production queue CSS prevents horizontal overflow`);
    await assert(`!${q('#live-composer-normal [data-runtime-abort]')}.disabled`, "Real Stop remains available with pending queue");
    await navigation.activity("1 queued follow-up.", initial.session_id);
    await gate(1, "second"); await gate(1, "done"); await waitCall(2);
    check(await readFile(join(directory, "call-2"), "utf8") === queuedText, "Same root agent loop delivers exact queued input to next real provider request");
    await wait(`${q('#live-transcript')}.textContent.includes(${JSON.stringify(queuedText)}) && !${q('[data-queue-item-id]')}`, "authoritative delivery retires pending item and adds real source user");
    const delivered = await snapshot();
    check(delivered.instance_id === initial.instance_id && delivered.session_id === initial.session_id && posts("prompt").length === 1, "Delivered follow-up stays within same RPC root/worker/session; no extra normal prompt");
    check(delivered.messages.filter(message => message.role === "user" && message.text === queuedText).length === 1, "Delivered queued user appears exactly once in authoritative runtime projection");
    durable = (await history()).find(session => session.session_id === initial.session_id);
    const durableUsers = durable.entries.filter(entry => entry.role === "user");
    check(JSON.stringify(durableUsers.map(entry => entry.text)) === JSON.stringify([rootText, queuedText]), "Real durable append-only history orders original and queued source users exactly once");
    check(durable.entries.every(entry => !entry.parent || durable.entries.some(parent => parent.id === entry.parent)), "Observed durable entries retain parent-linked ancestry");
    check(snapshots.some(snapshot => snapshot.queue?.items?.some(item => item.text === queuedText && item.state === "pending")), "Real production SSE observed queued pending state");
    await gate(2, "second"); await gate(2, "done");
    await wait(`${q('#live-status')}.textContent==='Ready'`, "queued provider turn completes through original root");
    await replace("Unsent draft during read-only reload.");
    const beforeReload = await callCount();
    await assert("managerWorkflowErrors.length===0", "Initial production queue workflow has no JavaScript errors or unhandled rejections");
    await send("Page.reload", {ignoreCache: true});
    await wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live'`, "real production reload rejoins existing worker"); await delay(150);
    check(await callCount() === beforeReload && posts("queue-enqueue").length === 1 && posts("prompt").length === 1, "Reload performs reads only, never replays consumed queued input");

    await replace("Second root for real Stop review."); await click("#live-send"); await waitCall(3);
    await wait(`!${q('[data-queue-next]')}.hidden && !${q('[data-queue-next]')}.disabled`, "second real root queue opens");
    await replace(heldText); await click("[data-queue-next]");
    await wait(`${q('[data-queue-item-id]')}?.textContent.includes(${JSON.stringify(heldText)})`, "second real pending input accepted");
    await click("#live-composer-normal [data-runtime-abort]");
    await wait(`${q('#live-status')}.textContent==='Ready' && ${q('[data-queue-item-id]')}?.dataset.queueState==='held'`, "real Stop retains held queue for review");
    const afterStop = await snapshot();
    check(afterStop.queue.items.some(item => item.text === heldText && item.state === "held"), "Actual backend—not fixture projection—marks undelivered input held after cancellation");
    check(!(await history()).find(session => session.session_id === initial.session_id).entries.some(entry => entry.role === "user" && entry.text === heldText), "Canceled undelivered queued text never enters durable user history");
    await send("Page.reload", {ignoreCache: true});
    await wait(`${q('[data-queue-item-id]')}?.dataset.queueState==='held' && ${q('#live-connection')}?.textContent==='Live'`, "reload retains held review without autoplay");
    await click("[data-queue-copy-draft]");
    await assert(`${q('#live-prompt')}.value===${JSON.stringify(heldText)}`, "Real held item Copy to draft remains an explicit local review action");
    await delay(150);
    check(await callCount() === 3 && posts("prompt").length === 2 && posts("queue-enqueue").length === 2 && posts("cancel").length === 1, "Stop, reload and review copy never replay queue, prompt or provider work");
    await navigation.activity("1 retained queue item to review.", initial.session_id);
    await navigation.organizeLive(initial.session_id);
    check(requests.filter(request => request.method === "POST").every(request => /\/(open|prompt|queue-enqueue|cancel)$/.test(request.path) || /^\/projects\/[^/]+\/organization\/(rename|pin|unpin|archive|restore)$/.test(request.path)), "No hidden branch/edit/regenerate/switch/restart fallback in real production workflow");
    await versionsChecks({ready,directory,click,key,replace,evaluate,wait,check,assert,requests,responses,callCount,waitCall,gate,snapshot,history,send,width,client,sessionId});
    await trustChecks({click,evaluate,wait,check,snapshot,callCount,requests,send});
    await assert("managerWorkflowErrors.length===0", "Final real production page has no JavaScript errors or unhandled rejections");
    check(errors.length === 0, `Bounded real HTTP/SSE observation has no errors (${errors.join('; ')})`);
    return {results, failures};
  } catch (error) {
    error.results = results; error.failures = failures;
    error.observation = {responses: responses.slice(-15), requests: requests.filter(request => request.method === "POST").map(({method,path,keys}) => ({method,path,keys})), page: await evaluate('({focused:{tag:document.activeElement?.tagName,id:document.activeElement?.id},draft:document.querySelector("#live-prompt")?.value,pending:document.querySelector("#live-queue-next")?.textContent,queueButton:{hidden:document.querySelector("[data-queue-next]")?.hidden,disabled:document.querySelector("[data-queue-next]")?.disabled}})').catch(() => null)};
    throw error;
  }
  finally { dispose(); }
}
