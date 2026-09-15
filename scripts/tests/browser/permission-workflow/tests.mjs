import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";

export async function exercise({client, sessionId, ready, directory, artifacts}) {
  let passed = 0;
  const requests = [], settled = new Set();
  const assert = (condition, label) => { if (!condition) throw Error(label); passed++; };
  const read = path => readFile(path, "utf8").catch(error => { if (error.code === "ENOENT") return null; throw error; });
  const records = async name => (await read(join(directory, name)) || "").trim().split("\n").filter(Boolean).map(line => JSON.parse(line));
  const calls = async key => (await records(`${key}-calls.jsonl`)).length;
  const executions = async (key, token, phase = "executed") => (await records(`${key}-executions.jsonl`)).filter(row => row.token === token && row.phase === phase).length;
  const file = (key, token) => read(join(ready.paths[key], `${token}.txt`));
  const gate = name => writeFile(join(directory, name), name.endsWith("-kill") ? "kill" : "release", {mode: 0o600});
  const until = async (predicate, label, timeout = 15000) => {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) { if (await predicate()) return; await delay(40); }
    throw Error(`Timed out: ${label}`);
  };
  const dispose = client.onEvent(event => {
    if (event.method === "Network.requestWillBeSent" && event.params.request.url.startsWith(ready.origin)) {
      requests.push({id: event.params.requestId, session: event.sessionId, method: event.params.request.method, path: new URL(event.params.request.url).pathname});
    }
    if (["Network.loadingFailed", "Network.loadingFinished"].includes(event.method)) settled.add(`${event.sessionId}:${event.params.requestId}`);
  });
  const postCount = () => requests.filter(row => row.method === "POST" && row.path.endsWith("/prompt")).length;
  async function page(key, id) {
    await client.send("Page.enable", {}, id);
    await client.send("Network.enable", {}, id);
    await client.send("Emulation.setDeviceMetricsOverride", {width: 1280, height: 900, deviceScaleFactor: 1, mobile: false}, id);
    const evaluate = async expression => {
      const result = await client.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}, id);
      if (result.exceptionDetails) throw Error(`Browser evaluation failed (${key}): ${result.exceptionDetails.text}`);
      return result.result?.value;
    };
    const focus = () => client.send("Page.bringToFront", {}, id);
    const wait = async (expression, label, timeout) => {
      await focus();
      try { await until(() => evaluate(expression), `${key}: ${label}`, timeout); }
      catch (error) {
        const state = await evaluate('JSON.stringify({status:document.querySelector("#live-status")?.textContent,connection:document.querySelector("#live-connection")?.textContent,activation:document.querySelector("[data-action-error]")?.textContent,error:document.querySelector("#live-error")?.textContent,hidden:document.hidden})');
        throw Error(`${error.message}; state=${state}`);
      }
    };
    const click = selector => evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
    const snapshot = () => evaluate(`fetch('/projects/${ready.projects[key]}/runtime').then(r=>{if(!r.ok)throw Error('snapshot unavailable');return r.json()})`);
    const action = (name, fields) => evaluate(`fetch('/projects/${ready.projects[key]}/runtime/${name}', {method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({...${JSON.stringify(fields)},csrf:document.querySelector('input[name="csrf"]').value})}).then(r=>r.status)`);
    const navigate = async session => {
      await client.send("Page.navigate", {url: `${ready.origin}/?view=projects&project=${ready.projects[key]}${session ? `&session=${session}` : ""}`}, id);
      await wait('!!document.querySelector("[data-runtime-open], #live-session[data-runtime=true]")', "project page");
    };
    const activate = async () => {
      await wait('!!document.querySelector("[data-runtime-open]")', "explicit activation form");
      await click('[data-runtime-open] input[name="confirm"]');
      await evaluate('document.querySelector("[data-runtime-open]").requestSubmit()');
      await wait('document.querySelector("#live-status")?.textContent === "Ready" && document.querySelector("#live-connection")?.textContent === "Live"', "activated live session");
      return snapshot();
    };
    const prompt = async token => {
      await wait('document.querySelector("#live-status")?.textContent === "Ready" && !document.querySelector("#live-prompt")?.disabled', "composer ready");
      await evaluate(`document.querySelector('#live-prompt').value=${JSON.stringify(token)};document.querySelector('#live-prompt').dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('#live-composer').requestSubmit()`);
      await wait('!!document.querySelector("[data-permission=allow]") && !document.querySelector("[data-permission=allow]").disabled', "actual broker approval");
      return snapshot();
    };
    const decide = async decision => {
      await wait('document.querySelector("#live-connection")?.textContent === "Live"', "permission transport ready");
      await click(`[data-permission=${decision}]`);
    };
    const idle = () => wait('document.querySelector("#live-status")?.textContent === "Ready"', "terminal turn");
    const close = async session => {
      await wait('document.querySelector("#live-connection")?.textContent === "Live" && !document.querySelector("[data-runtime-close]")?.disabled', "close transport ready");
      await click("[data-runtime-close]");
      await click("[data-runtime-close-confirm]");
      await wait('!document.querySelector("#live-session[data-runtime=true]") && !!document.querySelector("[data-runtime-open]")', "closed worker and saved history");
      assert(await evaluate(`document.querySelector('[data-runtime-open] input[name="session_id"]')?.value === ${JSON.stringify(session)}`), `${key}: Close must preserve exact session target`);
    };
    const shot = async name => {
      const {data} = await client.send("Page.captureScreenshot", {format: "png"}, id);
      await writeFile(join(artifacts, `${name}.png`), Buffer.from(data, "base64"));
    };
    await navigate();
    return {id, key, evaluate, wait, click, snapshot, action, navigate, activate, prompt, decide, idle, close, shot};
  }
  let a, b;
  try {
    a = await page("a", sessionId);
    const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
    const attached = await client.send("Target.attachToTarget", {targetId, flatten: true});
    b = await page("b", attached.sessionId);
    const initialA = await a.activate(), initialB = await b.activate();
    assert(await calls("a") === 0 && await calls("b") === 0, "activation must not contact provider or execute tools");

    const approvalA = await a.prompt("allow"), approvalB = await b.prompt("allow");
    assert(approvalA.permission.tool === "write" && approvalB.permission.tool === "write", "requests must originate from actual builtin write admission");
    assert(approvalA.permission.id === approvalB.permission.id, "fixture must exercise worker-local request-ID collision");
    assert(await file("a", "allow") === null && await file("b", "allow") === null, "pending approvals must not mutate either project");
    assert(await executions("a", "allow", "started") === 0 && await executions("b", "allow", "started") === 0, "actual tool runner must remain unentered before approval");
    assert(await a.evaluate('document.querySelector("[data-permission=allow]").textContent.includes("Allow once")'), "browser must offer only Allow once");
    assert(approvalA.permission.paths.some(path => path.endsWith("allow.txt")), "public approval must identify intended destination");
    assert(!JSON.stringify(approvalA.permission).includes('"content":'), "approval must not expose raw tool argument object");
    await a.shot("01-real-pending-write");
    assert(await b.action("permission", {instance_id: initialA.instance_id, request_id: approvalA.permission.id, decision: "allow"}) === 409, "mixed project/instance authority must fail despite matching request IDs");
    assert((await b.snapshot()).permission.id === approvalB.permission.id, "wrong-project reply must leave B pending");
    await a.decide("allow"); await a.idle();
    assert(await executions("a", "allow") === 1 && await file("a", "allow") !== null, "Allow must execute actual write exactly once");
    const allowedBytes = await file("a", "allow");
    assert(allowedBytes === ready.contents.allow, "allowed destination must contain exact fixture bytes");
    assert(await file("b", "allow") === null && (await b.snapshot()).permission.id === approvalB.permission.id, "allowing A must not authorize B");
    const allowedState = await a.snapshot();
    const liveResult = allowedState.activities.find(row => row.tool === "write" && row.status === "completed");
    assert(!!liveResult?.output && allowedState.recovery.state === "completed", "actual tool completion must publish public output and terminal evidence");
    await b.decide("deny"); await b.idle();
    assert(await file("b", "allow") === null && await executions("b", "allow", "started") === 0, "Deny must never enter actual write runner");
    assert((await b.snapshot()).activities.some(row => row.tool === "write" && row.status === "failed"), "denial must retain an error tool result");

    const denied = await a.prompt("deny");
    assert(denied.permission.id !== approvalA.permission.id, "Allow once must not authorize a subsequent write");
    assert(await a.action("permission", {instance_id: initialA.instance_id, request_id: approvalA.permission.id, decision: "allow"}) === 409, "duplicate settled reply cannot authorize the next write");
    assert((await a.snapshot()).permission.id === denied.permission.id && await file("a", "deny") === null, "stale reply must leave current approval and filesystem unchanged");
    await a.decide("deny"); await a.idle();
    assert(await executions("a", "deny", "started") === 0 && await file("a", "deny") === null, "actual A denial must not mutate destination");

    const pendingA = await a.prompt("pending");
    await b.prompt("pending");
    const beforeReconnectCalls = await calls("a"), beforeReconnectPosts = postCount();
    await a.wait('document.querySelector("#live-connection")?.textContent === "Live"', "foreground live approval before disconnect");
    const streams = requests.filter(row => row.session === a.id && row.path.endsWith("/events"));
    const connections = streams.length, activeStream = streams.at(-1);
    assert(!!activeStream && !settled.has(`${a.id}:${activeStream.id}`), "foreground approval must have a real in-flight SSE request");
    await client.send("Network.emulateNetworkConditions", {offline: true, latency: 0, downloadThroughput: -1, uploadThroughput: -1}, a.id);
    // Stop the real in-flight browser request as well: localhost streams can
    // survive CDP offline emulation. Retry remains offline, without fetch mocks.
    await client.send("Page.stopLoading", {}, a.id);
    await a.wait('document.querySelector("#live-connection")?.textContent !== "Live" && !document.hidden', "actual pending-approval disconnect", 35000);
    await until(() => settled.has(`${a.id}:${activeStream.id}`), "original SSE request terminal network event");
    assert(settled.has(`${a.id}:${activeStream.id}`), "browser network events must confirm original SSE request termination");
    await b.decide("allow"); await b.idle();
    assert(await executions("b", "pending") === 1 && await file("b", "pending") === ready.contents.pending, "B must complete while A browser is disconnected with pending approval");
    assert(await file("a", "pending") === null && await executions("a", "pending", "started") === 0, "disconnect must neither approve nor cancel into execution");
    await client.send("Network.emulateNetworkConditions", {offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1}, a.id);
    await a.wait('document.querySelector("#live-connection")?.textContent === "Live" && !!document.querySelector("[data-permission=allow]")', "pending approval reconnect", 18000);
    assert(requests.filter(row => row.session === a.id && row.path.endsWith("/events")).length > connections, "reconnect must use a new real SSE subscription");
    assert((await a.snapshot()).permission.id === pendingA.permission.id, "reconnect must retain authoritative pending request");
    assert(await calls("a") === beforeReconnectCalls && postCount() === beforeReconnectPosts, "reconnect must not replay prompt or contact provider");
    await a.decide("allow"); await a.idle();
    assert(await executions("a", "pending") === 1 && await file("a", "pending") === ready.contents.pending, "explicit approval after reconnect must execute exactly once");

    const beforeCloseCalls = await calls("a"), beforeClosePosts = postCount();
    await a.close(initialA.session_id);
    const saved = await a.evaluate('Array.from(document.querySelectorAll("[data-history-tool-id]"), row=>({id:row.dataset.historyToolId,text:row.textContent,output:row.querySelector(".activity-output")?.textContent}))');
    assert(saved.length === 3 && saved.some(row => row.output === liveResult.output), "real saved history must retain allow, deny, and reconnect tool results");
    await a.shot("02-durable-allowed-denied-history");
    const resumed = await a.activate();
    assert(resumed.session_id === initialA.session_id && resumed.instance_id !== initialA.instance_id, "explicit resume must bind same durable session with fresh authority");
    assert(!resumed.permission && !resumed.input && (resumed.activities || []).length === 0, "resume must not restore approvals or old live activities");
    const resumedTools = resumed.messages.flatMap(row => row.tools || []);
    assert(saved.every(row => resumedTools.some(tool => tool.id === row.id)), "tool identities must survive actual catalog and RPC resume");
    assert(resumedTools.some(tool => tool.status === "completed" && tool.output === liveResult.output && tool.output_available), "actual explicit-public write output must survive resume");
    assert(resumedTools.some(tool => tool.status === "failed"), "denied outcome must survive durable resume");
    assert(await calls("a") === beforeCloseCalls && postCount() === beforeClosePosts && await file("a", "allow") === allowedBytes, "catalog/resume must not rerun provider or writes");
    const fresh = await a.prompt("fresh");
    assert(fresh.permission.id === approvalA.permission.id, "fresh worker must exercise old permission-ID reuse");
    assert(await a.action("permission", {instance_id: initialA.instance_id, request_id: approvalA.permission.id, decision: "allow"}) === 409, "old runtime instance must not authorize reused request ID");
    assert((await a.snapshot()).permission.id === fresh.permission.id && await file("a", "fresh") === null, "stale runtime reply must preserve fresh pending gate");
    await a.decide("deny"); await a.idle();

    // Real process death while still awaiting approval: B's independent broker stays live.
    const unfinished = await a.prompt("close-pending");
    const isolated = await b.prompt("fresh");
    await gate("a-kill");
    await a.wait('document.querySelector("#live-status")?.textContent === "Worker failed"', "worker death before approval");
    assert(await file("a", "close-pending") === null && await executions("a", "close-pending", "started") === 0, "worker death before approval must leave write unentered");
    assert((await b.snapshot()).permission.id === isolated.permission.id && (await b.snapshot()).instance_id === initialB.instance_id, "A worker failure must not retire B pending authority");
    assert(await a.action("permission", {instance_id: resumed.instance_id, request_id: unfinished.permission.id, decision: "allow"}) === 409, "failed worker cannot accept stale approval");
    await b.decide("allow"); await b.idle();
    assert(await executions("b", "fresh") === 1, "B must still execute after A worker failure");
    await a.close(initialA.session_id);
    await a.activate();

    for (const token of ["before-write", "after-write"]) {
      const before = await calls("a"), posts = postCount();
      const approval = await a.prompt(token);
      await a.decide("allow");
      const phase = token === "before-write" ? "started" : "executed";
      await until(async () => (await read(join(directory, `a-${token}-${phase}`))) !== null, `${token} actual execution boundary`);
      assert(await executions("a", token, "started") === 1, `${token}: runner must enter exactly once after approval`);
      assert(await file("a", token) === (token === "before-write" ? null : ready.contents[token]), `${token}: filesystem must prove correct failure boundary`);
      await gate("a-kill");
      await a.wait('document.querySelector("#live-status")?.textContent === "Worker failed"', `${token} worker death`);
      const failed = await a.snapshot();
      assert(failed.activities.some(row => row.tool === "write" && row.status === "unknown"), `${token}: worker loss must not falsely report canceled/completed tool`);
      assert(failed.recovery.state === "admitted", `${token}: loss before completion must retain conservative admission evidence`);
      assert(await a.action("permission", {instance_id: approval.instance_id, request_id: approval.permission.id, decision: "allow"}) === 409, `${token}: failed instance must reject repeat authorization`);
      await a.shot(`03-${token}-unknown`);
      await a.close(initialA.session_id);
      const after = await a.activate();
      assert(after.instance_id !== approval.instance_id && !after.permission && (after.activities || []).length === 0, `${token}: resume must create fresh authority without old pending work`);
      const last = after.messages.flatMap(row => row.tools || []).at(-1);
      assert(last?.status === "unresolved" && !last.output_available, `${token}: synthetic recovery must not manufacture a definitive tool result`);
      assert(await calls("a") === before + 1 && postCount() === posts + 1, `${token}: failure, review, and resume must not replay the admitted turn`);
      assert(await executions("a", token) === (token === "before-write" ? 0 : 1), `${token}: no repeated or posthumous execution`);
      assert(await file("a", token) === (token === "before-write" ? null : ready.contents[token]), `${token}: explicit resume must preserve actual filesystem evidence`);
    }
    const closing = await a.prompt("close-pending");
    const closingCalls = await calls("a"), closingPosts = postCount();
    await a.close(initialA.session_id);
    assert(await a.action("permission", {instance_id: closing.instance_id, request_id: closing.permission.id, decision: "allow"}) === 409, "explicitly closed runtime cannot accept pending approval");
    const reopened = await a.activate();
    assert(!reopened.permission && reopened.instance_id !== closing.instance_id, "explicit close while pending must not restore usable approval");
    assert(await calls("a") === closingCalls && postCount() === closingPosts && await executions("a", "close-pending", "started") === 0 && await file("a", "close-pending") === null, "closing and resuming pending approval must not execute or replay the write");
    assert((await b.snapshot()).instance_id === initialB.instance_id && (await b.snapshot()).status === "idle", "A close and recovery must leave B runtime intact");
    assert(await executions("a", "allow") === 1 && await executions("a", "pending") === 1 && await executions("a", "deny", "started") === 0 && await executions("a", "fresh", "started") === 0, "full workflow must retain exact allowed/denied execution counts");
    await a.close(initialA.session_id);
    await b.close(initialB.session_id);
    return {assertions: passed, providerSteps: {a: await calls("a"), b: await calls("b")}, promptPosts: postCount()};
  } catch (error) {
    await a?.shot("failure").catch(() => {});
    throw error;
  } finally {
    if (a) await client.send("Network.emulateNetworkConditions", {offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1}, a.id).catch(() => {});
    dispose();
  }
}
