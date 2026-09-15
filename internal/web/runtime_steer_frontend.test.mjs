import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("./static/steer.js", import.meta.url), "utf8");
const dataKey = selector => selector.match(/^\[data-([^\]]+)\]$/)?.[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase());
class Element {
  constructor(tag = "div") { this.tag = tag; this.dataset = {}; this.nodes = new Map(); this.listeners = []; this.children = []; this.textContent = ""; this.value = ""; this.disabled = false; this.hidden = false; this.open = false; this.isConnected = true; }
  querySelector(selector) { if (this.nodes.has(selector)) return this.nodes.get(selector); return this.children.find(child => child.matches(selector)) || this.children.map(child => child.querySelector(selector)).find(Boolean) || null; }
  matches(selector) { const key = dataKey(selector); return !!key && Object.hasOwn(this.dataset, key); }
  closest(selector) { return selector.split(",").some(s => this.matches(s)) ? this : this.parent?.closest(selector) || null; }
  contains(node) { return node === this || [...this.nodes.values(), ...this.children].some(child => child.contains(node)); }
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  remove() { this.parent.children = this.parent.children.filter(child => child !== this); }
  addEventListener(type, listener, options = {}) { this.listeners.push({type, listener, signal: options.signal}); }
  emit(type, event = {}) { for (const l of this.listeners) if (l.type === type && !l.signal?.aborted) l.listener(event); }
  showModal() { this.open = true; }
  close() { this.open = false; this.emit("close"); }
  focus() {}
}
function fixture() {
  const root = new Element(), dialog = new Element("dialog"), history = new Element(), document = {createElement: tag => new Element(tag), activeElement: new Element()};
  root.dataset = {project: "project", instance: "instance", session: "session"};
  root.nodes.set("#live-steer-dialog", dialog); root.nodes.set("#live-steer-history", history);
  const at = name => { const selector = `[data-steer-${name}]`; return root.querySelector(selector) || dialog.querySelector(selector) || history.querySelector(selector); };
  for (const name of ["text", "submit", "error", "review", "close", "form"]) { const el = new Element(); el.dataset[dataKey(`[data-steer-${name}]`)] = ""; dialog.nodes.set(`[data-steer-${name}]`, el); }
  const trigger = new Element("button"); trigger.dataset.steerOpen = ""; root.nodes.set("[data-steer-open]", trigger);
  history.nodes.set("[data-steer-items]", new Element("ol"));
  const calls = []; let resolve, reject;
  const context = vm.createContext({window: {}, document, TextEncoder, AbortController, crypto: {randomUUID: () => "request-1"}});
  vm.runInContext(source.replace("  window.SnowSteer =", "  globalThis.steerTest = {get: () => view, submit, onClick};\n  window.SnowSteer ="), context);
  const api = {root, session: "session", validText: text => typeof text === "string" && text.trim() !== "" && !text.includes("\0") && new TextEncoder().encode(text).length <= 65536, changed() {}, request(action, fields) { calls.push({action, fields}); return new Promise((yes, no) => { resolve = yes; reject = no; }); }};
  const ui = {connected: true, busy: false, unknown: false, stopping: false, editing: false, status: "running"};
  const projection = {live_steer_token: "opaque-root", revision: 1, can_steer: true, items: []};
  const snow = context.window.SnowSteer, hooks = context.steerTest;
  snow.init(api); snow.render({revision: 1, steer: projection}, ui);
  function text(value) { at("text").value = value; at("text").emit("input"); }
  function render(revision, steer = projection, override = {}) { snow.render({revision, steer}, {...ui, ...override}); }
  function receipt(status = "accepted", token = "opaque-root") { return {project_id: "project", instance_id: "instance", session_id: "session", revision: 2, steer: {...projection, live_steer_token: token, revision: 2, items: [{request_id: "request-1", item_id: "native-1", text: "literal draft", status}]}, steer_ack: {live_steer_token: "opaque-root", request_id: "request-1", item_id: "native-1", status: "accepted"}}; }
  return {snow, hooks, api, root, dialog, history, at, calls, text, render, receipt, resolve: value => resolve(value), reject: () => reject(new Error("lost response"))};
}

test("only positive current projection enables steering; attention and conflicts fail closed", () => {
  const f = fixture(); assert.equal(f.snow.canSteer(), true);
  for (const ui of [{connected: false}, {busy: true}, {unknown: true}, {stopping: true}, {editing: true}, {status: "permission"}, {status: "input"}]) { f.render(2, undefined, ui); assert.equal(f.snow.canSteer(), false); }
  f.render(3, {live_steer_token: "opaque-root", revision: 0, can_steer: true, items: []}); assert.equal(f.snow.canSteer(), false);
  assert.equal(f.calls.length, 0);
});

test("native item event before ACK retains delivered and only accepted receipt clears unchanged draft", async () => {
  const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit();
  assert.equal(f.calls.length, 1); assert.equal(f.calls[0].action, "steer"); assert.equal(f.calls[0].fields.live_steer_token, "opaque-root"); assert.equal(f.calls[0].fields.steer_revision, "1"); assert.equal(f.calls[0].fields.text, "literal draft");
  f.render(3, f.receipt("delivered").steer); f.resolve(f.receipt()); assert.equal(await pending, true);
  assert.equal(f.hooks.get().projection.items[0].status, "delivered"); assert.equal(f.at("text").value, ""); assert.match(f.at("error").textContent, /Acceptance is not delivery/);
  f.render(4, f.receipt("delivered").steer); assert.equal(f.calls.length, 1);
});

test("unknown response keeps draft, cannot replay on reconnect, and offers explicit review", async () => {
  const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit(); f.reject(); assert.equal(await pending, false);
  assert.equal(f.at("text").value, "literal draft"); assert.equal(f.snow.canSteer(), false); f.dialog.close();
  f.render(2, undefined, {connected: false}); f.render(3);
  assert.equal(f.calls.length, 1); assert.equal(f.snow.open(), true); assert.equal(f.at("review").hidden, false);
  f.hooks.onClick({target: f.at("review"), preventDefault() {}});
  assert.equal(f.calls.length, 1); assert.equal(f.snow.canSteer(), true); assert.equal(f.at("text").value, "literal draft");
});

test("retired navigation response never clears old draft or mutates new scope", async () => {
  const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit(); const old = f.hooks.get();
  f.snow.dispose(); f.root.dataset.session = "new-session"; f.root.dataset.instance = "new-instance"; f.api.session = "new-session"; f.snow.init(f.api); f.render(1);
  f.text("new scope draft"); f.resolve(f.receipt()); assert.equal(await pending, false);
  assert.equal(old.store.text, "literal draft"); assert.equal(old.store.unknown, true); assert.equal(f.at("text").value, "new scope draft"); assert.equal(f.calls.length, 1);
});

test("same-session new root cannot be overwritten by an old correlated ACK or lose a newer draft", async () => {
  const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit();
  const next = {...f.receipt().steer, live_steer_token: "new-root", revision: 5, items: []}; f.render(5, next); f.text("new run draft"); f.resolve(f.receipt());
  assert.equal(await pending, true); assert.equal(f.at("text").value, "new run draft"); assert.equal(f.hooks.get().projection.live_steer_token, "new-root"); assert.equal(f.calls.length, 1);
});

test("IME, Enter/newlines and modal close preserve draft; oversized edits keep prior text", () => {
  const f = fixture(); f.snow.open(); f.text("first\nsecond"); f.at("text").emit("compositionstart");
  f.at("text").emit("keydown", {key: "Enter", ctrlKey: true, isComposing: true, preventDefault() { throw new Error("IME interrupted"); }});
  f.at("text").emit("compositionend"); f.at("text").emit("keydown", {key: "Enter", preventDefault() { throw new Error("newline intercepted"); }});
  f.text("x".repeat(65537)); assert.equal(f.at("text").value, "first\nsecond"); f.dialog.close(); f.snow.open(); assert.equal(f.at("text").value, "first\nsecond"); assert.equal(f.calls.length, 0);
});


test("native completion before HTTP ACK accepts the captured receipt without regranting retired authority", async () => {
  for (const status of ["delivered", "discarded"]) {
    const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit();
    const completed = f.receipt(status, ""); completed.steer.can_steer = false; completed.steer.revision = 3;
    f.render(3, completed.steer, {status: "idle"}); f.resolve(completed);
    assert.equal(await pending, true); assert.equal(f.at("text").value, "");
    assert.equal(f.hooks.get().projection.items[0].status, status);
    assert.equal(f.hooks.get().projection.live_steer_token, ""); assert.equal(f.snow.canSteer(), false);
    assert.equal(f.hooks.get().store.unknown, false); assert.equal(f.calls.length, 1);
  }
});

test("a correlated ACK from a different replacement instance is not accepted", async () => {
  const f = fixture(); f.snow.open(); f.text("literal draft"); const pending = f.hooks.submit();
  const result = f.receipt(); result.instance_id = "replacement-instance"; f.resolve(result);
  assert.equal(await pending, false); assert.equal(f.at("text").value, "literal draft");
  assert.equal(f.hooks.get().store.unknown, true); assert.equal(f.calls.length, 1);
});
