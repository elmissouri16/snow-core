import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source = name => readFileSync(new URL(`../../internal/web/static/${name}.js`, import.meta.url), "utf8");
const app = source("app");
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => {resolve = a; reject = b;}); return {promise, resolve, reject}; };
const scope = {project_id: "p", instance_id: "i", session_id: "s"};
const preferences = { ...scope, revision: 5, branch_id: "b", tip_id: "t", provider: "provider", model: "model", mode: "plan", permission_mode: "ask", thinking: "medium", reasoning_summary: "auto", text_verbosity: "medium", current_session_available: true, defaults_available: false, thinking_levels: ["medium", "high"], reasoning_summaries: ["auto"], text_verbosities: ["medium"]};
const historyFields = {session_id: "s", expected_revision: "5", current_branch_id: "b", current_tip_id: "t", branch_id: "selected", tip_id: "selected-tip", name: "New branch"};
const compactFields = {session_id: "s", branch_id: "b", expected_tip_id: "t", expected_revision: "5"};
function harness() {
  const hooks = {}, calls = [], applied = [], actions = [];
  const snapshot = {...scope, revision: 5, status: "idle", reasoning_enabled: true, history_control_enabled: true, compaction_enabled: true, goal: {session_id: "s", branch_id: "b", tip_id: "t"}, queue: {items: []}, steer: {can_steer: true, live_steer_token: "token", revision: 3, items: []}};
  const current = {project: "p", instance: "i", session: "s", key: "p:s", revision: 5, status: "idle", connected: true, controller: new AbortController(), snapshot};
  const region = {dataset: {project: "p", instance: "i", session: "s"}};
  const context = vm.createContext({window: {}, current, live: current, AbortController, AbortSignal, TextEncoder, TextDecoder, console,
    $: () => region, csrf: () => "csrf", runtimeURL: action => `/projects/p/runtime${action ? "/" + action : ""}`, openDialog() {}, closeDialog() {}, updateControls() {}, validEditText: value => !!value?.trim(), editDrafts: new Map(), reuseDrafts: new Map(), uncertain: new Map(), liveError() {},
    request: async (url, body, signal, timeout, bound) => {calls.push({url, body, signal, timeout, bound}); return body ? context.respond(url, body) : current.snapshot;},
    runtimeAction: async (action, fields) => { actions.push({action, fields}); return {...current.snapshot, instance_id: "replacement"}; },
    applySnapshot: result => {if (result.revision >= current.revision) {applied.push(result); current.revision = result.revision; current.snapshot = result; current.status = result.status;}},
    connectionState: () => {current.invalidInstance = true; context.disposeLivePanels();}
  });
  vm.runInContext(source("reasoning"), context);
  vm.runInContext(source("compaction"), context);
  for (const name of ["Reasoning", "HistoryControls", "Compaction", "Steer"]) context.window[`Snow${name}`] = {...context.window[`Snow${name}`], init(api) {hooks[name] = api;}, dispose() {}, render() {}, blocking: () => false};
  context.window.SnowQueue = {blocking: () => false};
  vm.runInContext(app.slice(app.indexOf("  const activeStatus ="), app.indexOf("  function attentionState()")), context);
  vm.runInContext(app.slice(app.indexOf("  function panelIdentity("), app.indexOf("  function setupVersions(")), context);
  vm.runInContext(app.slice(app.indexOf("  function panelCurrent("), app.indexOf("  function validGoalRunACK(")), context);
  context.setupLivePanels();
  return {context, current, hooks, calls, applied, actions};
}
const mutationFields = () => {
  const fields = {...preferences, expected_revision: "5", scope: "session", confirm: "session", field: "thinking", value: "high"};
  for (const key of ["project_id", "instance_id", "revision", "current_session_available", "defaults_available", "thinking_levels", "reasoning_summaries", "text_verbosities"]) delete fields[key];
  return fields;
};
const compactResult = (revision = 6) => ({...scope, revision, status: "running", compaction_ack: {compaction_id: "compact", turn_id: "compact", turn_origin: "compact", root_epoch: 2, turn_sequence: 3, session_id: "s", branch_id: "b"}});

test("four controllers capture immutable identity and exclusively reserve shared ownership", async () => {
  const {current, hooks, calls} = harness();
  for (const api of Object.values(hooks)) assert.ok(Object.isFrozen(api.identity));
  assert.ok(hooks.Reasoning.reserve(true));
  assert.equal(hooks.Compaction.reserve(true), false);
  assert.equal(await hooks.Compaction.commit(compactFields), false);
  assert.equal(calls.length, 0);
  current.instance = "other";
  assert.equal(hooks.Reasoning.reserve(false), false);
  assert.equal(await hooks.Reasoning.set(mutationFields()), false);
  assert.equal(calls.length, 0);
});

test("history authority is exact captured revision and branch fork delegates to restore-style replacement", async () => {
  const {current, hooks, calls, actions} = harness();
  hooks.HistoryControls.reserve(true);
  for (const revision of [undefined, "4", "6", "05", 5, "0"]) assert.equal(await hooks.HistoryControls.mutate("history-branch-fork", {...historyFields, expected_revision: revision}), false);
  assert.equal(await hooks.HistoryControls.mutate("rpc-command", historyFields), false);
  assert.equal(calls.length, 0);
  assert.equal((await hooks.HistoryControls.mutate("history-branch-fork", historyFields)).instance_id, "replacement");
  assert.equal(actions.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(actions[0].fields)), historyFields);
  current.revision++;
  assert.equal(await hooks.HistoryControls.mutate("history-branch-fork", historyFields), false);
  assert.equal(actions.length, 1);
  const replacement = app.slice(app.indexOf("  async function runtimeAction("), app.indexOf("  async function cancelTurn("));
  assert.match(replacement, /forking = action === "history-branch-fork", restoring = action === "version-restore-commit" \|\| forking/);
  assert.match(replacement, /result.instance_id === current.instance/);
  assert.match(replacement, /if \(!replacing\) window\.SnowLiveView\?\.updateDraft/);
  assert.match(replacement, /setupQueue\(\); setupVersions\(\); setupGoals\(\); setupLivePanels\(\); setupComposerContext\(\); applySnapshot\(result\)/);
});

test("reasoning inspection accepts cancellation and reconciles higher DTO revision using a real read", async () => {
  const {context, current, hooks, calls, applied} = harness();
  context.respond = () => {current.snapshot = {...current.snapshot, revision: 6}; return {...preferences, revision: 6};};
  const read = new AbortController();
  const result = await hooks.Reasoning.inspect(read.signal);
  assert.equal(result.revision, 6);
  assert.equal(calls[0].url, "/projects/p/runtime/reasoning-inspect");
  assert.equal(calls[0].body.session_id, "s");
  assert.equal(calls[1].body, null);
  assert.equal(applied[0].status, "idle", "DTO is not a fabricated runtime snapshot");
  read.abort();
  assert.ok(calls[0].signal.aborted);
  assert.ok(calls.every(call => call.bound > 0));
});

test("session preference mutation returns DTO, never inherits read/modal cancellation, never invents revision", async () => {
  const {context, current, hooks, calls, applied} = harness();
  hooks.Reasoning.reserve(true);
  const wait = deferred(); context.respond = () => wait.promise;
  const promise = hooks.Reasoning.set(mutationFields());
  assert.equal(calls[0].signal, undefined);
  assert.equal(calls[0].body.expected_revision, "5");
  assert.equal(calls[0].body.scope, "session");
  assert.equal(calls[0].body.confirm, "session");
  current.snapshot = {...current.snapshot, revision: 6, thinking: "high"};
  wait.resolve({...preferences, revision: 6, thinking: "high"});
  const result = await promise;
  assert.equal(result.thinking, "high");
  assert.equal(result.status, undefined);
  assert.ok(applied.every(snapshot => snapshot.status === "idle"));
  assert.equal(calls.filter(call => call.body).length, 1);
});

test("metadata-only history results do not rotate instance or paint chat", async () => {
  const {context, current, hooks, calls, actions, applied} = harness();
  hooks.HistoryControls.reserve(true);
  context.respond = () => {current.snapshot = {...current.snapshot, revision: 6}; return {...scope, revision: 6, branch_id: "selected", tip_id: "selected-tip", name: "New branch", child_session_id: "detached"};};
  const result = await hooks.HistoryControls.mutate("history-session-fork", historyFields);
  assert.equal(result.child_session_id, "detached");
  assert.equal(current.session, "s"); assert.equal(current.instance, "i");
  assert.equal(actions.length, 0);
  assert.ok(applied.every(snapshot => snapshot.status === "idle"));
  assert.equal(calls.filter(call => call.body).length, 1);
  const inventory = app.slice(app.indexOf("  async function refreshPanelInventory("), app.indexOf("  function validGoalRunACK("));
  assert.match(inventory, /runtime\/choices/);
  assert.doesNotMatch(inventory, /runtimeAction|applySnapshot|reloadWorkspace|\.click\(|location\.(assign|replace)/);
});

test("compaction fast completion before clone-only ACK never downgrades SSE; Plan idle is allowed", async () => {
  const {context, current, hooks, calls, applied} = harness();
  hooks.Compaction.reserve(true);
  current.snapshot.mode = "plan";
  const wait = deferred(); context.respond = () => wait.promise;
  const result = hooks.Compaction.commit(compactFields);
  assert.equal(calls[0].url, "/projects/p/runtime/compaction-start");
  assert.equal(calls[0].signal, undefined);
  current.revision = 9; current.snapshot = {...current.snapshot, revision: 9, status: "idle", compaction: {state: "completed"}};
  wait.resolve(compactResult(6));
  assert.equal((await result).compaction_ack.compaction_id, "compact");
  assert.ok(applied.every(snapshot => snapshot.revision === 9));
  assert.equal(current.snapshot.compaction.state, "completed");
  assert.equal(calls.filter(call => call.body).length, 1);
});

test("whole compaction Stop remains available in pending/running idle gaps despite panel reservation and busy admission", () => {
  const {context, current} = harness();
  current.turnCancel = true;
  for (const state of ["pending", "running"]) {
    current.snapshot.compaction = {state}; current.snapshot.cancel_token = "stop";
    current.panelMutation = "compaction"; current.action = true; current.actionName = "compaction-start"; current.metadata = true;
    assert.equal(context.canStop(), true);
    assert.equal(context.mutationSafe(), false);
    current.snapshot.compaction.progress_done = true;
    assert.equal(context.canStop(), true, "native progress is not terminal completion");
  }
});

test("steer requires ordinary running native token/revision and accepts correlated late receipt without fake chat", async () => {
  const {context, current, hooks, calls, applied} = harness();
  current.panelMutation = "steer";
  const fields = {session_id: "s", live_steer_token: "token", steer_revision: "3", request_id: "req", text: "literal direction"};
  assert.equal(await hooks.Steer.request("steer", fields), false);
  current.status = "running"; current.snapshot.status = "running";
  for (const change of [{live_steer_token: "old"}, {steer_revision: "2"}]) assert.equal(await hooks.Steer.request("steer", {...fields, ...change}), false);
  current.snapshot.compaction = {state: "running"};
  assert.equal(await hooks.Steer.request("steer", fields), false); delete current.snapshot.compaction;
  current.snapshot.permission = {id: "attention"};
  assert.equal(await hooks.Steer.request("steer", fields), false); delete current.snapshot.permission;
  assert.equal(calls.length, 0);
  const wait = deferred(); context.respond = () => wait.promise;
  const promise = hooks.Steer.request("steer", fields);
  current.revision = 8; current.status = "idle";
  current.snapshot = {...current.snapshot, revision: 8, status: "idle", steer: {can_steer: false, live_steer_token: "", revision: 5, items: [{request_id: "req", item_id: "native-item", text: fields.text, status: "delivered"}]}};
  wait.resolve({...scope, revision: 6, status: "running", steer_ack: {live_steer_token: "token", request_id: "req", item_id: "native-item", status: "accepted"}});
  assert.equal((await promise).steer_ack.status, "accepted");
  assert.ok(applied.every(snapshot => snapshot.revision === 8));
  assert.equal(current.snapshot.steer.items[0].status, "delivered");
  assert.equal(calls.filter(call => call.body).length, 1);
});

test("unknown HTTP outcome permits only reconciliation reads, retains guard and never retries", async () => {
  const {context, current, hooks, calls} = harness();
  hooks.Compaction.reserve(true);
  context.respond = () => {throw new Error("HTTP timeout after admission");};
  assert.equal(await hooks.Compaction.commit(compactFields), false);
  assert.equal(current.unknown, true);
  assert.equal(context.uncertain.has("p:i"), true);
  assert.equal(await hooks.Compaction.commit(compactFields), false);
  assert.equal(calls.filter(call => call.body).length, 1);
  assert.equal(calls.filter(call => !call.body).length, 1);
});

test("retired panels fence late mutation receipts without aborting or replaying admitted mutation", async () => {
  const {context, hooks, calls, applied} = harness();
  hooks.Compaction.reserve(true);
  const wait = deferred(); context.respond = () => wait.promise;
  const promise = hooks.Compaction.commit(compactFields);
  context.disposeLivePanels();
  assert.equal(calls[0].signal, undefined);
  wait.resolve(compactResult());
  assert.equal(await promise, false);
  assert.equal(applied.length, 0);
  assert.equal(calls.length, 1);
});

test("panel lifecycle is wired at replacement, failure/closed/auth, HTMX swap and page exit", () => {
  for (const [start, end] of [["  function connectionState(", "  function startUpdates("], ["  function setupLive()", "  function setupQueue("], ["  function applySnapshot(", "  function updateControls()"]]) assert.match(app.slice(app.indexOf(start), app.indexOf(end)), /disposeLivePanels\(\)/);
  assert.match(app, /htmx:beforeCleanupElement[\s\S]*?event\.detail\.elt\?\.id === "workspace"[\s\S]*?disposeLivePanels\(\)/);
  assert.match(app, /pagehide[\s\S]*?disposeLivePanels\(\)/);
});

test("panel HTTP reader enforces streamed byte bound and fatal UTF-8, with an independent deadline", async () => {
  const context = vm.createContext({AbortController, TextDecoder, Uint8Array, URLSearchParams, setTimeout, clearTimeout});
  vm.runInContext(app.slice(app.indexOf("  async function request("), app.indexOf("  async function browseFolders(")), context);
  let calls = 0, sentSignal;
  context.fetch = async (_url, options) => {
    calls++; sentSignal = options.signal;
    return {ok: true, body: new ReadableStream({start(controller) {controller.enqueue(new TextEncoder().encode('{"ok":true}')); controller.close();}})};
  };
  assert.equal((await context.request("/panel", {csrf: "csrf"}, undefined, 500, 16)).ok, true);
  await assert.rejects(context.request("/panel", {}, undefined, 500, 5), /too large/);
  assert.equal(sentSignal.aborted, true);
  context.fetch = async () => ({ok: true, body: new ReadableStream({start(controller) {controller.enqueue(new Uint8Array([0xff])); controller.close();}})});
  await assert.rejects(context.request("/panel", {}, undefined, 500, 5));
  assert.equal(calls, 2, "reader failure never triggers retry");
});

test("missing capability and mismatched receipts fail closed without replacing authority", async () => {
  const {context, current, hooks, calls} = harness();
  hooks.Compaction.reserve(true);
  current.snapshot.compaction_enabled = false;
  assert.equal(await hooks.Compaction.commit(compactFields), false);
  assert.equal(calls.length, 0);
  current.snapshot.compaction_enabled = true;
  context.respond = () => ({...compactResult(), instance_id: "other"});
  assert.equal(await hooks.Compaction.commit(compactFields), false);
  assert.equal(current.instance, "i"); assert.equal(current.unknown, true);
  assert.equal(calls.filter(call => call.body).length, 1);
});

test('uncertain steering Stop is captured before dispatch and never follows replacement scope', async () => {
  for (const replacement of [null, 'project_id', 'instance_id', 'session_id', 'cancel_token']) {
    const {context,current,hooks,calls} = harness();
    current.status = current.snapshot.status = 'running'; current.turnCancel = true;
    current.snapshot.cancel_token = 'original-root';
    current.panelMutation = 'steer';
    const pending = deferred(); context.respond = () => pending.promise;
    const request = hooks.Steer.request('steer',{session_id:'s',live_steer_token:'token',steer_revision:'3',request_id:'fictional-request',text:'preserved text'});
    if (replacement) current.snapshot = {...current.snapshot,[replacement]:'replacement'};
    pending.reject(new Error('lost response'));
    assert.equal(await request,false);
    assert.equal(current.unknown,true);
    assert.equal(current.unknownSteerStop.cancel_token,'original-root');
    assert.equal(context.canStop(), replacement === null);
    assert.equal(calls.filter(call=>call.body).length,1);
  }
});
