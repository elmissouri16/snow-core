import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const steerSource = readFileSync(new URL("./static/steer.js", import.meta.url), "utf8");
const appSource = readFileSync(new URL("./static/app.js", import.meta.url), "utf8");
// Reuse the existing small DOM harness without changing its source ownership.
const harnessSource = readFileSync(new URL("./runtime_steer_frontend.test.mjs", import.meta.url), "utf8");
const domHarness = harnessSource.slice(harnessSource.indexOf("const dataKey"), harnessSource.indexOf('\ntest('));
function integratedFixture(status = 409) {
  const f = vm.runInNewContext(domHarness + "\nfixture()", {source: steerSource, vm, TextEncoder, AbortController});
  const lifetime = new AbortController(), identity = {project_id: "project", instance_id: "instance", session_id: "session"};
  const live = {project: "project", instance: "instance", session: "session", key: "scope", connected: true, status: "running", turnCancel: true, revision: 1, panels: lifetime,
    snapshot: {project_id: "project", instance_id: "instance", session_id: "session", revision: 1, status: "running", cancel_token: "captured-root-stop", steer: {live_steer_token: "opaque-root", revision: 1, can_steer: true, items: []}}};
  const calls = [];
  const context = vm.createContext({live, window: {SnowSteer: f.snow}, editDrafts: new Map(), reuseDrafts: new Map(), uncertain: new Map(),
    panelCurrent: (current, bound, scope) => current === live && current.instance === bound.instance_id && current.session === bound.session_id && current.panels === scope && !scope.signal.aborted,
    csrf: () => "fictional-csrf", liveError() {}, connectionState() {},
    request: async (path, fields) => { calls.push({path, fields}); const error = new Error("fixture rejected or lost response"); error.status = status; throw error; },
    readPanelSnapshot: async () => live.snapshot,
    updateControls() { vm.runInContext("renderLivePanels()", context); }
  });
  // Execute production control/admission/render logic, not copies of its guards.
  const functions = appSource.slice(appSource.indexOf("  const activeStatus"), appSource.indexOf("  function attentionState"))
    + appSource.slice(appSource.indexOf("  function renderLivePanels"), appSource.indexOf("  function setupLivePanels"))
    + appSource.slice(appSource.indexOf("  async function panelMutation"), appSource.indexOf("  async function refreshPanelInventory"));
  vm.runInContext(functions, context);
  f.api.changed = () => { live.panelMutation = f.snow.blocking() ? "steer" : null; context.updateControls(); };
  f.api.request = (action, fields) => context.panelMutation(live, identity, lifetime, "steer", action, fields);
  context.updateControls();
  return {...f, live, calls, stop: () => context.canStop(), mutationSafe: () => context.mutationSafe(), renderLive: () => context.updateControls()};
}

for (const status of [409, 0]) {
  test(`integrated steering uncertainty keeps only captured native Stop authority (${status})`, async () => {
    const f = integratedFixture(status); f.snow.open(); f.text("literal draft");
    assert.equal(await f.hooks.submit(), false);
    assert.equal(f.live.unknown, true); assert.equal(f.hooks.get().store.unknown, true);
    assert.equal(f.stop(), true, "uncertain steering must not remove the same authoritative root Stop");
    f.live.snapshot.cancel_token = "different-root-stop";
    assert.equal(f.stop(), false, "uncertain old steering must not grant another root Stop");
    assert.equal(f.calls.length, 1); assert.equal(f.at("text").value, "literal draft");
  });
}

test("idle read-only steering dismissal releases reservation while retaining uncertainty and draft", async () => {
  const f = integratedFixture(); f.snow.open(); f.text("literal draft"); await f.hooks.submit();
  f.dialog.close();
  f.live.status = "idle"; f.live.snapshot.status = "idle"; f.live.snapshot.cancel_token = ""; f.live.snapshot.revision = 2;
  f.live.snapshot.steer = {live_steer_token: "", revision: 2, can_steer: false, items: [{request_id: "request-1", text: "literal draft", status: "uncertain"}]};
  f.live.connected = false; f.renderLive();
  assert.equal(f.snow.open(), false, "disconnected state cannot grant read-only idle review");
  f.live.connected = true; f.renderLive();
  assert.equal(f.snow.open(), true, "idle uncertainty must have a read-only draft review path");
  assert.equal(f.at("review").disabled, false); assert.equal(f.at("submit").disabled, true);
  f.hooks.onClick({target: f.at("review"), preventDefault() {}});
  assert.equal(f.snow.blocking(), false); assert.equal(f.live.panelMutation, null);
  assert.equal(f.live.unknown, true, "local draft dismissal cannot assert global request certainty");
  assert.equal(f.at("text").value, "literal draft"); assert.equal(f.hooks.get().projection.items[0].status, "uncertain");
  assert.equal(f.snow.canSteer(), false); assert.equal(f.calls.length, 1);
  assert.equal(f.mutationSafe(), false, "global request uncertainty must still gate mutations");
  // The separate explicit global review can now unlock controls: the steering
  // dialog/store no longer holds an unreachable reservation after idle.
  f.live.unknown = false; f.renderLive();
  assert.equal(f.mutationSafe(), true); assert.equal(f.at("text").value, "literal draft");
  assert.equal(f.calls.length, 1);
});
