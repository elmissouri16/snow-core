import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";
const source = readFileSync(new URL("../../internal/web/static/reasoning.js", import.meta.url), "utf8");
class Element {
  constructor(tag = "div") { this.tag = tag; this.children = []; this.nodes = new Map(); this.listeners = []; this.textContent = ""; this.disabled = false; this.hidden = false; this.checked = false; this.open = false; this.selected = ""; }
  querySelector(s) { return this.nodes.get(s) || null; }
  replaceChildren(...children) { this.children = children; this.selected = ""; }
  append(child) { this.children.push(child); }
  get options() { return this.children; }
  set value(value) { this.selected = value; }
  get value() { return this.selected || (this.tag === "select" ? this.children[0]?.value || "" : ""); }
  addEventListener(type, listener, options) { this.listeners.push({type, listener, signal: options?.signal}); }
  focus() { this.focused = true; }
}
const identity = {project_id: "project", instance_id: "instance", session_id: "session"};
const result = () => ({...identity, branch_id:"branch",tip_id:"",revision: 3, provider: "provider", model: "model-without-name-heuristics", mode: "plan", permission_mode: "deny", thinking: "medium", reasoning_summary: "auto", text_verbosity: "low", thinking_levels: ["off", "medium", "high"], reasoning_summaries: [], text_verbosities: ["low", "high"], current_session_available: true, defaults_available: false});
function fixture() {
  const root = new Element(), dialog = new Element("dialog"); root.nodes.set("#reasoning-dialog", dialog); root.nodes.set("[data-reasoning-open]", new Element("button"));
  for (const name of ["field", "value", "refresh", "review", "confirmation", "confirm", "consent", "close", "notice", "identity", "authority", "mode", "thinking", "summary", "verbosity", "confirm-target"]) dialog.nodes.set(`[data-reasoning-${name}]`, new Element(["field", "value"].includes(name) ? "select" : "div"));
  const context = vm.createContext({window: {}, document: {createElement: tag => new Element(tag)}, AbortController});
  vm.runInContext(source.replace("  window.SnowReasoning =", "  globalThis.reasoningHooks = {get: () => view, inspect, prepare, commit, cancel, close};\n  window.SnowReasoning ="), context);
  const calls = [], reservations = []; let readResolve, writeResolve;
  const api = {root, identity, openDialog: d => {d.open = true;}, closeDialog: d => {d.open = false;}, reserve: v => reservations.push(v), inspect: signal => {calls.push({read: true, signal}); return new Promise(r => {readResolve = r;});}, set: fields => {calls.push({fields}); return new Promise(r => {writeResolve = r;});}};
  context.window.SnowReasoning.init(api); const at = name => dialog.querySelector(`[data-reasoning-${name}]`);
  const ui = {safe: true, readable: true}; context.window.SnowReasoning.render(result(), ui); dialog.open = true;
  return {root, dialog, context, hooks: context.reasoningHooks, api: context.window.SnowReasoning, at, calls, reservations, ui, read: value => readResolve(value), write: value => writeResolve(value)};
}
async function inspect(f, data = result()) { const pending = f.hooks.inspect(); f.read(data); await pending; }
test("authoritative capabilities populate choices; unknown summaries have no browser guesses", async () => {
  const f = fixture(); await inspect(f);
  assert.deepEqual(f.at("field").options.map(o => o.value), ["thinking", "text_verbosity"]);
  assert.deepEqual(f.at("value").options.map(o => o.value), ["off", "medium", "high"]);
  assert.equal(f.at("value").value, "medium"); assert.match(f.at("authority").textContent, /permissions: deny/);
  assert.equal(f.calls.length, 1); f.api.dispose();
});
test("explicit consent sends one allowlisted session preference and inspected facts", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare();
  assert.match(f.at("confirm-target").textContent, /project.*session.*plan.*provider.*permissions deny/);
  assert.match(f.at("confirm-target").textContent, /Host and project defaults are not written/);
  await f.hooks.commit(); assert.equal(f.calls.length, 1);
  f.at("consent").checked = true; const pending = f.hooks.commit();
  assert.equal(f.calls.length, 2); const payload = f.calls[1].fields;
  assert.equal(payload.field, "thinking"); assert.equal(payload.value, "high"); assert.equal(payload.scope, "session"); assert.equal(payload.confirm, "session"); assert.equal(payload.thinking, "medium"); assert.equal(payload.expected_revision, "3"); assert.equal(payload.branch_id, "branch"); assert.equal(payload.tip_id, "");
  for (const key of ["plugins", "mcp", "skills_enabled", "subagents_enabled", "debug_enabled", "command"]) assert.equal(key in payload, false);
  f.write({...result(), revision: 4, thinking: "high"}); await pending;
  assert.match(f.at("notice").textContent, /worker confirmed/); assert.equal(f.at("review").disabled, true); f.api.dispose();
});
test("revision or authority changes retire confirmation rather than rebase consent", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare(); f.at("consent").checked = true;
  f.api.render({...result(), revision: 4, mode: "default"}, f.ui); await f.hooks.commit();
  assert.equal(f.calls.length, 1); assert.equal(f.hooks.get().confirmation, null); assert.equal(f.at("consent").checked, false); assert.equal(f.at("review").disabled, true); f.api.dispose();
});
test("late retired inspection cannot populate the next session", async () => {
  const f = fixture(); const pending = f.hooks.inspect(); f.api.dispose(); f.read(result()); await pending;
  assert.equal(f.hooks.get(), null); assert.equal(f.at("authority").textContent, ""); assert.equal(f.calls.length, 1); assert.equal(f.calls[0].signal.aborted, true);
});
test("unverified update latches uncertainty, no retry and no activation", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare(); f.at("consent").checked = true; const pending = f.hooks.commit();
  f.write({...result(), instance_id: "replacement", revision: 4, thinking: "high"}); await pending;
  assert.equal(f.hooks.get().uncertain, true); assert.match(f.at("notice").textContent, /Do not retry/);
  await f.hooks.inspect(); await f.hooks.commit(); assert.equal(f.calls.length, 2); assert.equal(f.at("refresh").disabled, true); f.api.dispose();
});
test("composition and external safety gate prevent confirmation", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.get().composing = true; f.hooks.prepare(); assert.equal(f.hooks.get().confirmation, undefined);
  f.hooks.get().composing = false; f.api.render(result(), {safe: false, readable: true}); f.hooks.prepare(); assert.equal(f.hooks.get().confirmation, undefined); assert.equal(f.calls.length, 1); f.api.dispose();
});
test("browser rejects malformed capabilities, unverified scope and persisted fallback", () => {
  const f = fixture(); assert.equal(f.api.valid(result(), identity), true);
  for (const extra of [{revision: 0}, {revision: Number.MAX_SAFE_INTEGER + 1}, {permission_mode: "ALLOW"}, {current_session_available: false}, {defaults_available: true}, {branch_id: ""}, {tip_id: null}, {thinking_levels: ["high", "high"]}, {reasoning_summaries: ["private\ntext"]}, {project_id: "other"}]) assert.equal(f.api.valid({...result(), ...extra}, identity), false);
  f.api.dispose();
});
test("disposal removes old model, permissions, preferences and confirmation labels", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare(); f.api.dispose();
  for (const name of ["identity", "authority", "mode", "thinking", "summary", "verbosity", "confirm-target"]) assert.equal(f.at(name).textContent, "", name);
  for (const name of ["field", "value"]) { assert.equal(f.at(name).options.length, 0); assert.equal(f.at(name).disabled, true); }
  assert.equal(f.at("confirmation").hidden, true); assert.equal(f.at("consent").checked, false);
});
test("retiring a panel does not retry or publish its already-admitted mutation", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare(); f.at("consent").checked = true; const pending = f.hooks.commit();
  f.api.dispose(); f.write({...result(), revision: 4, thinking: "high"}); await pending;
  assert.equal(f.hooks.get(), null); assert.equal(f.at("authority").textContent, ""); assert.equal(f.calls.length, 2);
});
test("identity replacement retires presentation even without a higher revision", async () => {
  const f = fixture(); await inspect(f); f.at("value").value = "high"; f.hooks.prepare();
  f.api.render({...result(), revision: 1, session_id: "replacement"}, f.ui);
  assert.equal(f.hooks.get(), null); assert.equal(f.at("authority").textContent, ""); assert.equal(f.at("confirmation").hidden, true);
});
