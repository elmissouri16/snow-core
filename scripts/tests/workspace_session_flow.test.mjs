import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const app = readFileSync(new URL("../../internal/web/static/app.js", import.meta.url), "utf8");
const conversation = readFileSync(new URL("../../internal/web/static/conversation.js", import.meta.url), "utf8");
const owner = (project = "alpha", session = "one", instance = "worker-one") => ({project, session, instance, key: `${project}:${session}`, connected: true, draftRevision: 0});
const pending = () => ({text: "review before sending", project: "alpha", targetSession: "one", pending: true});
function harness() {
  const nodes = new Map(), events = new Map(), actions = [], errors = [], reads = [];
  class Element {
    constructor() { this.isConnected = true; }
    matches() { return false; }
    getAttribute() { return null; }
  }
  const document = {
    documentElement: {dataset: {}}, body: {addEventListener(name, fn) { events.set(name, fn); }},
    querySelector: selector => nodes.get(selector), querySelectorAll: () => [],
    addEventListener(name, fn) { events.set(name, fn); }, createElement: () => ({setAttribute() {}})
  };
  // Project only the view fields read by this transport fixture. Draft ownership
  // and admission still execute from the production controller below.
  const window = {SnowWorkspace: {
    updateDraft(value) {
      const prompt = nodes.get("#workspace-prompt");
      if (prompt && "workspaceEnabled" in value) prompt.disabled = !value.workspaceEnabled;
      if (prompt && "workspaceText" in value) prompt.value = value.workspaceText;
    },
    updateOpening() {}, updateFlowError: message => errors.push(message)
  }, SnowShell: {navigation(open) { events.get("snow:shell-command")?.({detail: {type: "navigation", open}}); }}, htmx: {ajax: async (...args) => { reads.push(args); }},
    SnowLiveView: {updateDraft(text) { const prompt = nodes.get("#live-prompt"); if (!prompt) return false; prompt.value = text; return true; }},
    SnowConversation: {select: async detail => { actions.push(detail); return true; }}};
  let saved = 0;
  const context = vm.createContext({document, window, Element, location: new URL("http://snow.test/"), localStorage: {getItem() {}}, matchMedia: () => ({}),
    TextEncoder, URL, encodeURIComponent, setNav() {}, liveError: message => errors.push(message), saveDraft() { saved++; },
    projectLocation: project => `/?view=projects&project=${encodeURIComponent(project)}`});
  const prefix = app.slice(0, app.indexOf("  function setTheme("));
  vm.runInContext(prefix + `
    window.testFlow = {syncWorkspaceDraft, takeHomeDraft, selectWorkspaceSession, consumeWorkspaceIntent,
      setLive: value => { live = value; }, setDraft: value => { homeDraft = value; }, draft: () => homeDraft,
      intent: () => workspaceIntent, setIntent: value => { workspaceIntent = value; },
      edit: key => editDrafts.set(key, {}), reuse: key => reuseDrafts.set(key, {})};
  })();`, context);
  return {...window.testFlow, window, nodes, events, actions, errors, reads, Element, saved: () => saved};
}

for (const mismatch of ["missing", "project", "session", "instance"]) test(`startup transfer rejects ${mismatch} acknowledgement`, () => {
  const h = harness(), draft = pending();
  draft.ack = {project: "alpha", session: "one", instance: "worker-one"};
  if (mismatch === "missing") delete draft.ack;
  else draft.ack[mismatch] = "different";
  h.setDraft(draft); h.setLive(owner()); h.nodes.set("#live-prompt", {value: ""});
  assert.equal(h.takeHomeDraft(), false);
  assert.equal(h.draft().text, draft.text); assert.equal(h.saved(), 0);
});

test("matching receipt transfers once into empty composer without sending", () => {
  const h = harness(), draft = pending(), prompt = {value: ""};
  draft.ack = {project: "alpha", session: "one", instance: "worker-one"};
  h.setDraft(draft); h.setLive(owner()); h.nodes.set("#live-prompt", prompt);
  assert.equal(h.takeHomeDraft(), true); assert.equal(prompt.value, draft.text);
  assert.equal(h.takeHomeDraft(), false); assert.equal(h.saved(), 1);
  assert.equal(h.actions.length, 0); assert.equal(h.reads.length, 0);
});

for (const blocker of ["existing draft", "unknown", "edit", "reuse", "other workspace"]) test(`explicit draft use preserves ${blocker}`, () => {
  const h = harness(), current = owner(), prompt = {value: ""};
  if (blocker === "existing draft") prompt.value = "session-owned text";
  if (blocker === "unknown") current.unknown = true;
  if (blocker === "edit") h.edit(current.key);
  if (blocker === "reuse") h.reuse(current.key);
  if (blocker === "other workspace") current.project = "beta";
  h.setDraft(pending()); h.setLive(current); h.nodes.set("#live-prompt", prompt);
  assert.equal(h.takeHomeDraft(true), false); assert.equal(h.saved(), 0);
  assert.equal(h.draft().text, pending().text);
});

test("explicit Use draft can authorize a different session in the same workspace", () => {
  const h = harness(); h.setDraft(pending()); h.setLive(owner("alpha", "two")); h.nodes.set("#live-prompt", {value: ""});
  assert.equal(h.takeHomeDraft(), false); assert.equal(h.takeHomeDraft(true), true);
});

test("cold session cannot overwrite another cold session's retained draft", () => {
  const h = harness(), draft = pending(), prompt = {value: "", dataset: {draftProject: "alpha", draftSession: "two", draftName: "Alpha"}};
  h.setDraft(draft); h.nodes.set("#workspace-prompt", prompt);
  assert.equal(h.syncWorkspaceDraft(), false); assert.equal(prompt.disabled, true); assert.equal(h.draft(), draft);
  prompt.dataset.draftSession = "one";
  assert.equal(h.syncWorkspaceDraft(), true); assert.equal(prompt.disabled, false); assert.equal(prompt.value, draft.text);
});

test("same-workspace explicit selection delegates once to conversation admission", async () => {
  const h = harness(); h.setLive(owner());
  await h.selectWorkspaceSession({project: "alpha", session: "two", instance: "worker-one"});
  await h.consumeWorkspaceIntent();
  assert.equal(h.actions.length, 1); assert.equal(h.actions[0].session, "two"); assert.equal(h.reads.length, 0);
});

test("cross-workspace intent waits for connected mounted owner and is consumed once", async () => {
  const h = harness(), next = {...owner("beta", "current", "beta-worker"), connected: false};
  h.setLive(owner());
  h.window.htmx.ajax = async () => h.setLive(next);
  await h.selectWorkspaceSession({project: "beta", session: "saved", instance: "beta-worker"});
  assert.equal(h.actions.length, 0); assert.ok(h.intent());
  next.connected = true; await h.consumeWorkspaceIntent(); await h.consumeWorkspaceIntent();
  assert.equal(h.actions.length, 1); assert.equal(h.actions[0].project, "beta"); assert.equal(h.intent(), null);
});

test("replacement owner rejects remembered mutation intent instead of replaying it", async () => {
  const h = harness(); h.setLive(owner("beta", "current", "replacement"));
  await h.selectWorkspaceSession({project: "beta", session: "saved", instance: "old-owner"});
  assert.equal(h.actions.length, 0); assert.equal(h.errors.length, 1); assert.equal(h.intent(), null);
});

test("superseding workspace navigation discards pending intent", async () => {
  const h = harness(); h.setIntent({project: "beta", session: "saved", trigger: {}});
  h.events.get("htmx:beforeRequest")({detail: {target: {id: "workspace"}, elt: {}}});
  h.setLive(owner("beta")); await h.consumeWorkspaceIntent();
  assert.equal(h.actions.length, 0); assert.equal(h.intent(), null);
});

test("React shell navigation dispatches one read and preempts an older session intent", async () => {
  const h = harness(), source = new h.Element();
  h.nodes.set("#workspace", {});
  h.setIntent({project: "beta", session: "saved", trigger: {}});
  let preemptions = 0;
  h.window.SnowShell.preemptInventory = () => {
    assert.equal(h.reads.length, 0, "inventory retires before foreground dispatch");
    preemptions++;
  };
  h.events.get("snow:shell-navigate")({detail: {href: "/?view=projects&project=beta&new=1", source}});
  await Promise.resolve();
  assert.equal(h.intent(), null);
  assert.equal(preemptions, 1);
  assert.equal(h.reads.length, 1);
  const [method, url, options] = h.reads[0];
  assert.equal(method, "GET"); assert.equal(url, "/?view=projects&project=beta&new=1");
  assert.equal(options.source, source); assert.equal(options.target, "#workspace");
  assert.equal(options.swap, "outerHTML"); assert.equal(options.push, "true");
  h.setLive(owner("beta")); await h.consumeWorkspaceIntent();
  assert.equal(h.actions.length, 0, "preempted session intent cannot replay after mount");
});

test("cold New is a read-only explicit empty-state navigation", async () => {
  const h = harness(); await h.selectWorkspaceSession({project: "alpha"}, true);
  assert.equal(h.reads.length, 1); assert.equal(h.reads[0][0], "GET"); assert.match(h.reads[0][1], /&new=1$/);
  assert.equal(h.actions.length, 0); assert.equal(h.intent(), null);
});

function selectionHarness(status = "idle") {
  const state = {controls: {safe: true}, snapshot: {project_id: "alpha", instance_id: "worker-one", session_id: "one", status}, hooks: {}};
  let reads = 0, switches = 0, requested;
  state.hooks.sessions = async target => { reads++; requested = target; return {project_id: "alpha", instance_id: "worker-one", available: true, sessions: [{session_id: "older-page-session"}]}; };
  const context = vm.createContext({state, activeTurn: value => ["running", "permission", "input"].includes(value),
    canSwitch: s => s.controls.safe && !s.loading, valid: (s, instance) => s === state && s.snapshot.instance_id === instance,
    render() {}, switchTo: async () => { switches++; return false; }});
  vm.runInContext(conversation.slice(conversation.indexOf("  async function select("), conversation.indexOf("  window.SnowConversation")) + "globalThis.selectSession = select;", context);
  return {state, select: context.selectSession, reads: () => reads, switches: () => switches, requested: () => requested};
}

test("session-only selection requests exact target and propagates action rejection", async () => {
  const h = selectionHarness();
  assert.equal(await h.select({project: "alpha", session: "older-page-session", instance: "worker-one"}), false);
  assert.equal(h.requested(), "older-page-session"); assert.equal(h.reads(), 1); assert.equal(h.switches(), 1);
});

for (const status of ["idle", "running"]) test(`${status} sidebar target delegates to server membership validation without a redundant inventory read`, async () => {
  const h = selectionHarness(status), trigger = {dataset: {project: "alpha", instance: "worker-one"}, closest: () => ({dataset: {shellSession: "saved"}})};
  await h.select({project: "alpha", session: "saved", instance: "worker-one", trigger});
  assert.equal(h.reads(), 0); assert.equal(h.switches(), 1);
  trigger.dataset.instance = "replaced";
  await h.select({project: "alpha", session: "saved", instance: "worker-one", trigger});
  assert.equal(h.switches(), 1);
});
