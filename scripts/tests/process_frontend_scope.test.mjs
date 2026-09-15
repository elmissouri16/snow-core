import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../../internal/web/static/processes.js", import.meta.url), "utf8");
const pid = "proc_" + "a".repeat(32), otherPID = "proc_" + "b".repeat(32);
class Element {
  constructor(tag = "div") { this.tag = tag; this.dataset = {}; this.children = []; this.listeners = []; this.textContent = ""; this.hidden = false; this.disabled = false; this.isConnected = true; this.open = false; this.nodes = new Map(); }
  querySelector(selector) { return this.nodes.get(selector) || null; }
  closest(selector) { return selector === "dialog" ? this.dialog || null : null; }
  close() { this.open = false; this.emit("close"); }
  querySelectorAll(selector) { const found = new Set(); const walk = el => { if (selector === "button" && el.tag === "button") found.add(el); for (const child of el.children) walk(child); }; for (const el of this.nodes.values()) walk(el); return [...found]; }
  replaceChildren(...children) { for (const child of this.children) child.parentNode = null; this.children = []; this.append(...children); }
  append(...children) { for (const child of children) this.insertBefore(child, null); }
  get firstChild() { return this.children[0] || null; }
  get nextSibling() { return this.parentNode?.children[this.parentNode.children.indexOf(this) + 1] || null; }
  remove() { if (this.parentNode) { const at = this.parentNode.children.indexOf(this); this.parentNode.children.splice(at, 1); this.parentNode = null; } }
  insertBefore(child, at) { child.remove(); const index = at ? this.children.indexOf(at) : this.children.length; this.children.splice(index, 0, child); child.parentNode = this; }
  getClientRects() { return [{}]; }
  addEventListener(type, listener, options = {}) { this.listeners.push({type, listener, signal: options.signal}); }
  emit(type, event = {}) { for (const entry of [...this.listeners]) if (entry.type === type && !entry.signal?.aborted) entry.listener(event); }
}
function fixture() {
  const panel = new Element("details"), root = new Element(), document = new Element();
  root.dataset = {project: "project", instance: "instance-a", session: "session-a"};
  panel.dataset = {processProject: "project", processInstance: "instance-a", processSession: "session-a"};
  for (const name of ["list", "status", "error", "log-panel", "log-heading", "log-status", "output", "refresh", "log-more"]) panel.nodes.set(`[data-process-${name}]`, new Element(["refresh", "log-more"].includes(name) ? "button" : "div"));
  document.nodes.set("#managed-processes", panel); document.nodes.set("#live-session", root); document.visibilityState = "visible";
  document.createElement = tag => new Element(tag);
  const calls = []; let resolve;
  const context = vm.createContext({window: {confirm: () => true}, document, TextEncoder, AbortController, AbortSignal, URLSearchParams, clearTimeout() {}, setTimeout() { return 1; }, fetch: (url, options) => { calls.push({url, options}); return new Promise(done => { resolve = done; }); }});
  vm.runInContext(source.replace("  window.SnowProcesses =", "  globalThis.processTest = {get: () => view, logs, paint};\n  window.SnowProcesses ="), context);
  const at = name => panel.querySelector(`[data-process-${name}]`);
  function seed(prefix = "old") {
    for (const name of ["status", "error", "log-heading", "log-status", "output"]) at(name).textContent = `${prefix} ${name} cursor 88 EOF`;
    at("log-panel").hidden = false; at("error").hidden = false;
    at("refresh").disabled = false; at("log-more").disabled = false; at("list").append(new Element("button"));
  }
  function cleared(refreshEnabled = false) {
    for (const name of ["status", "error", "log-heading", "log-status", "output"]) assert.equal(at(name).textContent, "", name);
    assert.equal(at("log-panel").hidden, true); assert.equal(at("error").hidden, true);
    assert.equal(at("log-more").disabled, true); assert.equal(at("refresh").disabled, !refreshEnabled);
    assert.equal(at("list").children.length, 0);
  }
  const response = (session = "session-a", instance = "instance-a") => ({ok: true, text: async () => JSON.stringify({project_id: "project", instance_id: instance, result: {session_id: session, process_id: pid, status: "running", output: "old output", next_cursor: 99, omitted_bytes: 0, eof: true}})});
  return {panel, root, document, context, api: context.window.SnowProcesses, hooks: context.processTest, at, seed, cleared, calls, respond: () => resolve(response())};
}

test("same-scope process refresh preserves rows and updates only changed handles", () => {
  const f = fixture(); f.api.init(); const v = f.hooks.get();
  v.records.set(pid, {process_id: pid, name: "first", status: "running"});
  v.records.set(otherPID, {process_id: otherPID, name: "second", status: "running"});
  f.hooks.paint(v); const first = f.at("list").children[0], read = first.children[1], second = f.at("list").children[1];
  f.at("list").scrollTop = 170;
  for (let i = 0; i < 3; i++) f.hooks.paint(v);
  assert.equal(f.at("list").children[0], first); assert.equal(first.children[1], read); assert.equal(f.at("list").scrollTop, 170);
  v.records.get(pid).status = "exited"; f.hooks.paint(v);
  assert.equal(f.at("list").children[0], first); assert.equal(first.children[2].disabled, true); assert.match(first.children[0].children[0].textContent, /exited/);
  v.unknown = true; f.hooks.paint(v); assert.equal(second.children[2].disabled, true);
  v.records.delete(pid); f.hooks.paint(v); assert.equal(f.at("list").children.length, 1); assert.equal(f.at("list").children[0], second);
  f.api.dispose(); f.cleared();
});

test("init and disposal clear every process presentation field and old controls", () => {
  const f = fixture(); f.seed(); f.api.init(); f.cleared(true);
  const old = f.hooks.get(); old.cursor = 88; old.eof = true; old.selected = pid; old.records.set(pid, {name: "old"}); f.seed();
  f.panel.dialog = new Element("dialog"); f.panel.dialog.open = true; f.panel.open = true;
  f.api.dispose(); f.cleared(); assert.equal(old.controller.signal.aborted, true);
  assert.equal(f.panel.dialog.open, false); assert.equal(f.panel.open, false);
  assert.equal(old.cursor, undefined); assert.equal(old.eof, false); assert.equal(old.selected, ""); assert.equal(old.records.size, 0);
  f.seed(); f.api.dispose(); f.cleared(); assert.equal(f.calls.length, 0);
});

test("new session scope starts without the previous log name, cursor, EOF or output", () => {
  const f = fixture(); f.api.init(); const old = f.hooks.get(); f.seed();
  f.root.dataset.instance = f.panel.dataset.processInstance = "instance-b";
  f.root.dataset.session = f.panel.dataset.processSession = "session-b";
  f.api.init(); f.cleared(true); assert.equal(old.controller.signal.aborted, true);
  assert.equal(f.hooks.get().session, "session-b"); assert.equal(f.hooks.get().cursor, undefined); assert.equal(f.hooks.get().selected, "");
  f.api.dispose();
});

test("null, mismatched and disconnected scopes retire presentation without granting controls", () => {
  const f = fixture(); f.api.init(); f.seed(); f.document.nodes.delete("#live-session");
  f.document.emit("visibilitychange"); f.cleared(); assert.equal(f.hooks.get(), undefined);
  f.seed(); f.api.init(); f.cleared(); assert.equal(f.hooks.get(), undefined);
  f.document.nodes.set("#live-session", f.root); f.root.dataset.session = "other"; f.seed(); f.api.init(); f.cleared();
  f.root.dataset.session = "session-a"; f.api.init(); f.seed(); f.panel.isConnected = false;
  f.document.emit("visibilitychange"); f.cleared(); assert.equal(f.hooks.get(), undefined);
});

test("late old-session log results cannot overwrite or reopen the new scope", async () => {
  const f = fixture(); f.api.init(); const old = f.hooks.get(); old.records.set(pid, {name: "old", status: "running"}); old.selected = pid;
  f.panel.open = true; const pending = f.hooks.logs(old); assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].options.body.get("instance_id"), "instance-a"); assert.equal(f.calls[0].options.body.get("session_id"), "session-a");
  f.api.dispose(); f.root.dataset.instance = f.panel.dataset.processInstance = "instance-b"; f.root.dataset.session = f.panel.dataset.processSession = "session-b";
  f.panel.open = false; f.api.init(); f.cleared(true); f.seed("new");
  f.respond(); await pending;
  assert.equal(f.at("log-heading").textContent, "new log-heading cursor 88 EOF"); assert.equal(f.at("output").textContent, "new output cursor 88 EOF");
  assert.equal(f.hooks.get().selected, ""); assert.equal(f.hooks.get().cursor, undefined); assert.equal(f.calls.length, 1);
  f.api.dispose();
});

test("busy log selection cannot mix another process with the in-flight cursor", async () => {
  const f = fixture(); f.api.init(); const v = f.hooks.get();
  v.records.set(pid, {name: "first", status: "running"}); v.records.set(otherPID, {name: "second", status: "running"}); v.selected = pid;
  f.panel.open = true; const pending = f.hooks.logs(v);
  const button = new Element("button"); button.dataset.processLogs = otherPID; button.matches = () => false;
  f.panel.emit("click", {target: {closest: () => button}}); assert.equal(v.selected, pid); assert.equal(f.calls.length, 1);
  f.respond(); await pending; assert.equal(f.at("log-heading").textContent, "first · output"); assert.equal(v.cursor, 99); assert.equal(f.at("log-more").disabled, true);
  f.api.dispose();
});
