// Deterministic unit tests of the actual production client; no npm or network.
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";
import {TextDecoder, TextEncoder} from "node:util";

const sourceURL = new URL("../../../../internal/web/static/stream.js", import.meta.url);
const source = readFileSync(sourceURL, "utf8");
const encoder = new TextEncoder();
const frameLimit = 16 * 1024 * 1024;
const tests = [];
const test = (name, body) => tests.push({name, body});
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
};
// No polling or wall-clock sleeps. Each operation has a bounded microtask drain.
async function settle() { for (let i = 0; i < 16; i++) await Promise.resolve(); }

class Target {
  listeners = new Map();
  addEventListener(type, callback, options = {}) {
    const group = this.listeners.get(type) || new Map();
    group.set(callback, options); this.listeners.set(type, group);
  }
  removeEventListener(type, callback) {
    this.listeners.get(type)?.delete(callback);
  }
  dispatchEvent(event) {
    for (const [callback, options] of [...(this.listeners.get(event.type) || [])]) {
      if (!this.listeners.get(event.type)?.has(callback)) continue;
      if (options.once) this.removeEventListener(event.type, callback);
      callback.call(this, event);
    }
    return true;
  }
  count() { return [...this.listeners.values()].reduce((sum, group) => sum + group.size, 0); }
}
class Controller {
  signal = Object.assign(new Target(), {aborted: false});
  abort() {
    if (this.signal.aborted) return;
    this.signal.aborted = true;
    this.signal.dispatchEvent({type: "abort"});
  }
}
class Clock {
  now = 0;
  sequence = 0;
  timers = new Map();
  set = (callback, delay) => {
    assert.ok(Number.isFinite(delay) && delay >= 0, "finite nonnegative timer");
    assert.ok(this.timers.size < 20, "bounded pending timers");
    const id = ++this.sequence;
    this.timers.set(id, {callback, due: this.now + delay});
    return id;
  };
  clear = id => this.timers.delete(id);
  delays() { return [...this.timers.values()].map(timer => timer.due - this.now).sort((a, b) => a - b); }
  async advance(ms) {
    const end = this.now + ms;
    for (let count = 0; ; count++) {
      assert.ok(count < 100, "bounded fake timer execution");
      const next = [...this.timers].filter(([, timer]) => timer.due <= end)
        .sort((a, b) => a[1].due - b[1].due || a[0] - b[0])[0];
      if (!next) break;
      this.now = next[1].due; this.timers.delete(next[0]);
      next[1].callback(); await settle();
    }
    this.now = end; await settle();
  }
}
class Reader {
  pending = [];
  reads = 0;
  cancellations = 0;
  releases = 0;
  read() {
    assert.ok(this.pending.length < 1, "only one outstanding read per reader");
    this.reads++;
    const operation = deferred(); this.pending.push(operation);
    return operation.promise;
  }
  // Deliberately do NOT settle reads on abort/cancel. A retired read may still
  // resolve with bytes, reproducing the post-await ownership race reliably.
  cancel() { this.cancellations++; return Promise.resolve(); }
  releaseLock() { this.releases++; }
  async deliver(value, done = false) {
    assert.equal(this.pending.length, 1, "delivery requires an outstanding read");
    this.pending.shift().resolve({done, value: typeof value === "string" ? encoder.encode(value) : value});
    await settle();
  }
  async fail() {
    assert.equal(this.pending.length, 1);
    this.pending.shift().reject(new Error("controlled transport failure"));
    await settle();
  }
}
const snapshotFrame = (revision, text = "hello") => `event: snapshot\ndata: ${JSON.stringify({revision, text})}\n\n`;
const terminalFrame = kind => `event: ${kind}\ndata: {}\n\n`;

function harness({onSnapshot, onState, hidden = false, aborted = false} = {}) {
  const clock = new Clock();
  const document = Object.assign(new Target(), {hidden});
  const owner = new Controller();
  if (aborted) owner.abort();
  const requests = [], snapshots = [], states = [];
  let fallbacks = 0, client;
  const context = vm.createContext({
    window: {}, document, AbortController: Controller, TextDecoder,
    setTimeout: clock.set, clearTimeout: clock.clear,
    fetch(url, options) {
      assert.ok(requests.length < 40, "bounded request count");
      assert.equal(url, "/runtime/events?instance_id=unit-one");
      assert.equal(options.method, "GET", "reconnect must never POST or replay work");
      assert.equal(options.credentials, "same-origin");
      assert.equal(options.cache, "no-store");
      assert.equal(options.headers.Accept, "text/event-stream");
      const operation = deferred();
      requests.push({url, options, ...operation});
      return operation.promise;
    }
  });
  vm.runInContext(source, context, {filename: sourceURL.pathname, timeout: 1000});
  const h = {
    clock, document, owner, requests, snapshots, states,
    get fallbacks() { return fallbacks; },
    get client() { return client; },
    async respond(status = 200, {request = requests.at(-1), type = "text/event-stream; charset=utf-8", body = true} = {}) {
      assert.ok(request, "response requires a fetch");
      const reader = new Reader();
      request.reader = reader;
      request.resolve({status, ok: status >= 200 && status < 300,
        headers: {get: name => name === "content-type" ? type : null},
        body: body ? {getReader: () => reader} : null});
      await settle(); return reader;
    },
    async visible(visible) {
      document.hidden = !visible;
      document.dispatchEvent({type: "visibilitychange"});
      await settle();
    },
    assertDisposed() {
      assert.equal(clock.timers.size, 0, "disposed client has no timers");
      assert.equal(document.count(), 0, "visibility listener removed");
      assert.equal(owner.signal.count(), 0, "owner abort listener removed");
      assert.ok(requests.every(request => request.options.signal.aborted), "all connections aborted");
    },
    async cleanup() {
      client.close();
      // Resolve intentionally delayed transports so finally blocks run as well.
      for (const request of requests) {
        if (request.reader?.pending.length) await request.reader.deliver(undefined, true);
        else request.resolve({status: 404});
      }
      await settle(); h.assertDisposed();
    }
  };
  client = context.window.SnowStream.open({
    url: "/runtime/events?instance_id=unit-one", signal: owner.signal,
    snapshot(value) { snapshots.push(JSON.parse(JSON.stringify(value))); onSnapshot?.(h, value); },
    state(value) { states.push(value); onState?.(h, value); },
    fallback() { fallbacks++; }
  });
  return h;
}
async function using(options, body) {
  const h = harness(options);
  try { await body(h); } finally { await h.cleanup(); }
}

// Race matrix: both actionable event types, retired by close or visibility.
for (const kind of ["snapshot", "closed"]) {
  const bytes = kind === "snapshot" ? snapshotFrame(99, "retired") : terminalFrame(kind);
  test(`retired ${kind} after close dispatches nothing`, () => using({}, async h => {
    const reader = await h.respond();
    h.client.close(); h.assertDisposed();
    await reader.deliver(bytes);
    assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, []);
    assert.equal(reader.cancellations, 1); assert.equal(reader.releases, 1);
    h.assertDisposed();
  }));
  test(`retired ${kind} while hidden dispatches nothing`, () => using({}, async h => {
    const reader = await h.respond();
    await h.visible(false);
    assert.deepEqual(h.states, ["paused"]);
    await reader.deliver(bytes);
    assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, ["paused"]);
    assert.equal(h.clock.timers.size, 0);
    await h.visible(true);
    const replacement = await h.respond();
    await replacement.deliver(snapshotFrame(1, "replacement"));
    assert.equal(h.snapshots[0].text, "replacement");
  }));
  test(`retired ${kind} cannot dispatch or kill visible replacement`, () => using({}, async h => {
    const reader = await h.respond();
    await h.visible(false); await h.visible(true);
    const replacement = await h.respond();
    await replacement.deliver(snapshotFrame(1, "replacement"));
    await h.clock.advance(100);
    const timers = h.clock.delays();
    await reader.deliver(bytes);
    assert.deepEqual(h.states, ["paused"]);
    assert.deepEqual(h.snapshots.map(value => value.revision), [1]);
    assert.equal(h.requests[1].options.signal.aborted, false, "stale closed must not abort current reader");
    assert.deepEqual(h.clock.delays(), timers, "stale finally must not clear current watchdog");
    await replacement.deliver(snapshotFrame(2));
    assert.deepEqual(h.snapshots.map(value => value.revision), [1, 2]);
    assert.equal(h.requests.length, 2);
  }));
}
for (const action of ["close", "abort", "hide", "replace"]) {
  test(`snapshot callback ${action} stops the remaining frame batch`, () => using({
    onSnapshot(h) {
      if (action === "close") h.client.close();
      if (action === "abort") h.owner.abort();
      if (action === "hide" || action === "replace") {
        h.document.hidden = true; h.document.dispatchEvent({type: "visibilitychange"});
        if (action === "replace") {
          h.document.hidden = false; h.document.dispatchEvent({type: "visibilitychange"});
        }
      }
    }
  }, async h => {
    const reader = await h.respond();
    await reader.deliver(snapshotFrame(1) + snapshotFrame(2) + terminalFrame("closed"));
    assert.deepEqual(h.snapshots.map(value => value.revision), [1]);
    assert.deepEqual(h.states, action === "hide" || action === "replace" ? ["paused"] : []);
    if (action === "replace") {
      const replacement = await h.respond();
      assert.equal(replacement.pending.length, 1);
      assert.equal(h.requests[1].options.signal.aborted, false);
    }
  }));
}
test("UTF-8, JSON, and LF frame boundaries survive one-byte chunks", () => using({}, async h => {
  const reader = await h.respond();
  const text = "雪 ❄️ café 😀";
  const bytes = encoder.encode(`: heartbeat\n\n${snapshotFrame(1, text)}${snapshotFrame(2, text)}`);
  for (const byte of bytes) await reader.deliver(Uint8Array.of(byte));
  assert.deepEqual(h.snapshots, [{revision: 1, text}, {revision: 2, text}]);
  assert.deepEqual(h.states, []);
}));
test("heartbeat refreshes watchdog without a snapshot or mutation", () => using({}, async h => {
  const reader = await h.respond();
  await h.clock.advance(24000);
  await reader.deliver(": heartbeat\n\n");
  assert.deepEqual(h.clock.delays(), [25000]);
  await h.clock.advance(24000);
  assert.equal(h.requests[0].options.signal.aborted, false);
  assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, []);
  assert.equal(h.requests.length, 1);
}));
test("multiline JSON data and unrelated events", () => using({}, async h => {
  const reader = await h.respond();
  await reader.deliver('event: unrelated\ndata: not-json\n\nevent: snapshot\ndata: {"revision": 3,\ndata: "text": "joined"}\n\n');
  assert.deepEqual(h.snapshots, [{revision: 3, text: "joined"}]);
}));
for (const [name, data] of [
  ["invalid JSON", "{no}"], ["null snapshot", "null"],
  ["missing revision", '{}'], ["fractional revision", '{"revision":1.5}'],
  ["unsafe revision", '{"revision":9007199254740992}']
]) {
  test(`${name} reconnects without dispatching later frames`, () => using({}, async h => {
    const reader = await h.respond();
    await reader.deliver(`event: snapshot\ndata: ${data}\n\n${snapshotFrame(7)}`);
    assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, ["reconnecting"]);
    assert.deepEqual(h.clock.delays(), [500]);
    assert.equal(reader.cancellations, 1); assert.equal(reader.releases, 1);
    assert.equal(h.fallbacks, 0);
  }));
}
test("invalid UTF-8 is rejected rather than replacement-decoded", () => using({}, async h => {
  const reader = await h.respond();
  await reader.deliver(Uint8Array.of(0xff));
  assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, ["reconnecting"]);
}));
for (const complete of [false, true]) {
  test(`oversize ${complete ? "complete" : "partial"} frame is bounded and retried`, () => using({}, async h => {
    const reader = await h.respond();
    if (complete) {
      await reader.deliver(snapshotFrame(1, "x".repeat(frameLimit)));
    } else {
      await reader.deliver("x".repeat(frameLimit));
      assert.deepEqual(h.states, [], "exact limit remains a bounded partial frame");
      await reader.deliver("x");
    }
    assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, ["reconnecting"]);
    assert.deepEqual(h.clock.delays(), [500]);
    assert.equal(reader.cancellations, 1); assert.equal(reader.releases, 1);
  }));
}
test("EOF backs off to a cap, valid snapshot resets backoff, all requests stay GET", () => using({}, async h => {
  for (const delay of [500, 1000, 2000, 4000, 8000, 10000, 10000]) {
    const reader = await h.respond();
    const count = h.requests.length;
    await reader.deliver(undefined, true);
    assert.deepEqual(h.clock.delays(), [delay]);
    await h.clock.advance(delay - 1); assert.equal(h.requests.length, count);
    await h.clock.advance(1); assert.equal(h.requests.length, count + 1);
  }
  const reader = await h.respond();
  await reader.deliver(snapshotFrame(8));
  await reader.deliver(undefined, true);
  assert.deepEqual(h.clock.delays(), [500]);
  assert.equal(h.fallbacks, 0);
}));
for (const [status, state] of [[401, "auth_required"], [403, "auth_required"], [404, "closed"], [409, "replaced"]]) {
  test(`HTTP ${status} is terminal ${state}`, () => using({}, async h => {
    await h.respond(status);
    assert.deepEqual(h.states, [state]); assert.equal(h.fallbacks, 0);
    h.assertDisposed();
    await h.clock.advance(100000); assert.equal(h.requests.length, 1);
  }));
}
for (const kind of ["auth_required", "closed", "replaced"]) {
  test(`SSE ${kind} terminates before subsequent snapshot`, () => using({}, async h => {
    const reader = await h.respond();
    await reader.deliver(terminalFrame(kind) + snapshotFrame(2));
    assert.deepEqual(h.states, [kind]); assert.deepEqual(h.snapshots, []);
    h.assertDisposed();
  }));
}
test("HTTP 501 alone invokes the legacy fallback once and disposes", () => using({}, async h => {
  await h.respond(501);
  assert.equal(h.fallbacks, 1); assert.deepEqual(h.states, []); h.assertDisposed();
  await h.visible(false); await h.visible(true); await h.clock.advance(100000);
  assert.equal(h.requests.length, 1); assert.equal(h.fallbacks, 1);
}));
for (const status of [429, 500, 502, 503]) {
  test(`HTTP ${status} retries without legacy fallback`, () => using({}, async h => {
    await h.respond(status);
    assert.deepEqual(h.states, ["reconnecting"]); assert.equal(h.fallbacks, 0);
    assert.deepEqual(h.clock.delays(), [500]);
    await h.clock.advance(500); assert.equal(h.requests.length, 2);
    const reader = await h.respond(); await reader.deliver(snapshotFrame(1));
    assert.equal(h.snapshots.length, 1);
  }));
}
for (const options of [{type: "application/json"}, {body: false}]) {
  test(`malformed successful transport ${JSON.stringify(options)} never falls back`, () => using({}, async h => {
    await h.respond(200, options);
    assert.deepEqual(h.states, ["reconnecting"]); assert.equal(h.fallbacks, 0);
    assert.deepEqual(h.clock.delays(), [500]);
  }));
}
test("fetch rejection schedules bounded retry, not fallback", () => using({}, async h => {
  h.requests[0].reject(new Error("offline")); await settle();
  assert.deepEqual(h.states, ["reconnecting"]); assert.equal(h.fallbacks, 0);
  assert.deepEqual(h.clock.delays(), [500]);
}));
test("watchdog aborts stalled reader then retries after read rejection", () => using({}, async h => {
  const reader = await h.respond();
  await h.clock.advance(24999); assert.equal(h.requests[0].options.signal.aborted, false);
  await h.clock.advance(1); assert.equal(h.requests[0].options.signal.aborted, true);
  await reader.fail();
  assert.deepEqual(h.states, ["reconnecting"]); assert.deepEqual(h.clock.delays(), [500]);
}));
for (const stage of ["fetch", "read", "retry"]) {
  test(`owner abort during ${stage} clears every timer/listener`, () => using({}, async h => {
    let reader;
    if (stage !== "fetch") reader = await h.respond();
    if (stage === "retry") await reader.deliver(undefined, true);
    h.owner.abort(); h.assertDisposed();
    const states = [...h.states];
    if (stage === "fetch") await h.respond(501);
    if (stage === "read") await reader.deliver(snapshotFrame(99));
    await h.visible(false); await h.visible(true); await h.clock.advance(100000);
    assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, states);
    assert.equal(h.fallbacks, 0); assert.equal(h.requests.length, 1); h.assertDisposed();
  }));
}
for (const action of ["close", "abort", "hide"]) {
  test(`reconnecting callback ${action} leaves no scheduled retry`, () => using({
    onState(h, state) {
      if (state !== "reconnecting") return;
      if (action === "close") h.client.close();
      if (action === "abort") h.owner.abort();
      if (action === "hide") {
        h.document.hidden = true;
        h.document.dispatchEvent({type: "visibilitychange"});
      }
    }
  }, async h => {
    const reader = await h.respond();
    await reader.deliver(undefined, true);
    assert.equal(h.clock.timers.size, 0, "callback retired connection: do not create retry afterward");
    if (action !== "hide") h.assertDisposed();
    await h.clock.advance(100000); assert.equal(h.requests.length, 1);
  }));
}
test("aborted watchdog read cannot deliver terminal or snapshot callbacks", () => using({}, async h => {
  const reader = await h.respond();
  await h.clock.advance(25000);
  assert.equal(h.requests[0].options.signal.aborted, true);
  await reader.deliver(snapshotFrame(99) + terminalFrame("closed"));
  assert.deepEqual(h.snapshots, []); assert.deepEqual(h.states, []);
  assert.equal(reader.cancellations, 1); assert.equal(reader.releases, 1);
}));
test("retired read rejection cannot retry or clear the replacement watchdog", () => using({}, async h => {
  const reader = await h.respond();
  await h.visible(false); await h.visible(true);
  const replacement = await h.respond();
  await reader.fail();
  assert.deepEqual(h.states, ["paused"]);
  assert.deepEqual(h.clock.delays(), [25000]);
  assert.equal(h.requests[1].options.signal.aborted, false);
  await replacement.deliver(snapshotFrame(1)); assert.equal(h.snapshots.length, 1);
}));
test("already-aborted owner never fetches or installs retained listeners", () => using({aborted: true}, async h => {
  assert.equal(h.requests.length, 0); h.assertDisposed();
}));
test("initially hidden waits for visibility and cancels pending retry when hidden", () => using({hidden: true}, async h => {
  assert.equal(h.requests.length, 0); assert.equal(h.clock.timers.size, 0);
  await h.visible(true); const reader = await h.respond();
  await reader.deliver(undefined, true);
  await h.visible(false); assert.equal(h.clock.timers.size, 0);
  await h.clock.advance(100000); assert.equal(h.requests.length, 1);
  await h.visible(true); assert.equal(h.requests.length, 2);
}));
for (const status of [200, 404, 501]) {
  test(`retired fetch HTTP ${status} cannot affect its replacement`, () => using({}, async h => {
    const retired = h.requests[0];
    await h.visible(false); await h.visible(true);
    const current = await h.respond();
    await h.respond(status, {request: retired});
    assert.deepEqual(h.states, ["paused"]); assert.equal(h.fallbacks, 0);
    assert.equal(h.requests[1].options.signal.aborted, false);
    await current.deliver(snapshotFrame(1)); assert.equal(h.snapshots.length, 1);
    assert.deepEqual(h.clock.delays(), [25000]);
  }));
}

// Wall time is only a failure deadline, never used for test sequencing.
const deadline = setTimeout(() => {
  console.error("FAIL: stream-client unit harness exceeded its 20s deadline");
  process.exit(1);
}, 20000);
let passed = 0;
try {
  for (const {name, body} of tests) {
    try { await body(); passed++; console.log(`PASS ${name}`); }
    catch (error) { console.error(`FAIL ${name}\n${error.stack || error}`); }
  }
} finally { clearTimeout(deadline); }
console.log(`stream-client: ${passed}/${tests.length} tests passed (production stream.js, Node VM unit only)`);
if (passed !== tests.length) process.exitCode = 1;
