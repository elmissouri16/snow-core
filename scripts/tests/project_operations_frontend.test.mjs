import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../../internal/web/static/project-operations.js", import.meta.url), "utf8");
const template = readFileSync(new URL("../../internal/web/templates/project_operations.html", import.meta.url), "utf8");
const id = "11111111-1111-4111-8111-111111111111", otherID = "22222222-2222-4222-8222-222222222222";
const identity = { path: "/host/parent", device: "1", inode: "2" };
const operation = (overrides = {}) => ({ id, name: "created", kind: "create", state: "admitted", revision: 1, created_at: Date.now(), updated_at: Date.now(), parent: identity, child: ["running", "succeeded", "awaiting_registration"].includes(overrides.state) ? { path: "/host/parent/created", device: "1", inode: "3" } : { path: "", device: "", inode: "" }, outcome: "not_observed", ...overrides });
class Element {
  constructor(tag = "div") { this.tag = tag; this.nodes = new Map(); this.children = []; this.dataset = {}; this.listeners = new Map(); this.value = ""; this.disabled = false; this.hidden = false; this.checked = false; this.isConnected = true; this.open = false; this.textContent = ""; }
  set innerHTML(_) { throw new Error("HTML insertion is forbidden"); }
  querySelector(selector) { return this.nodes.get(selector) || null; }
  querySelectorAll(selector) {
    const result = new Set();
    const walk = node => { if (selector === "[data-op-row-action]" && node.dataset.opRowAction) result.add(node); node.children.forEach(walk); };
    this.nodes.forEach(walk); this.children.forEach(walk); return [...result];
  }
  append(...nodes) { this.children.push(...nodes); for (const node of nodes) node.parent = this; }
  replaceChildren(...nodes) { this.children = []; this.append(...nodes); }
  setAttribute(name, value) { this[name] = value; }
  focus() { this.focused = true; }
  contains(node) { return node === this || [...this.nodes.values()].includes(node) || this.children.some(child => child.contains(node)); }
  addEventListener(name, fn) { if (!this.listeners.has(name)) this.listeners.set(name, []); this.listeners.get(name).push(fn); }
  async emit(name, event = {}) { for (const fn of this.listeners.get(name) || []) await fn({ target: this, preventDefault() {}, ...event }); }
  async click() { if (!this.disabled) return this.emit("click"); }
}
function fixture(options = {}) {
  const document = new Element(), window = new Element(), requests = [];
  const allRoots = [];
  function makeRoot(auth = "fictional-csrf") {
    const root = new Element("details"); root.dataset.enabled = options.enabled === false ? "false" : "true";
    for (const [, name] of template.matchAll(/data-op-([a-z-]+)/g)) root.nodes.set(`[data-op-${name}]`, new Element());
    root.querySelector("[data-op-csrf]").value = auth;
    allRoots.push(root); return root;
  }
  const root = makeRoot();
  document.readyState = "complete";
  document.querySelectorAll = selector => selector === "[data-project-operations]" ? allRoots.filter(root => root.isConnected) : [];
  document.createElement = tag => new Element(tag);
  const context = vm.createContext({ document, window, AbortController, AbortSignal, TextEncoder, TextDecoder, URLSearchParams,
    fetch: (url, options) => new Promise((resolve, reject) => requests.push({ url, options, resolve, reject })) });
  vm.runInContext(source, context);
  const at = (name, owner = root) => owner.querySelector(`[data-op-${name}]`);
  const settle = async () => { await new Promise(resolve => setImmediate(resolve)); };
  const respond = async (index, status, value) => { requests[index].resolve({ status, ok: status >= 200 && status < 300, text: async () => JSON.stringify(value) }); await settle(); };
  async function input(name, value, owner = root) { at(name, owner).value = value; await at(name, owner).emit("input"); }
  async function select() {
    await input("parent", "/host/parent"); const index = requests.length, pending = at("select").click(); await settle();
    assert.equal(requests[index].url, "/projects/folders/select");
    await respond(index, 200, { operation_id: id, path: "/host/parent", expires_at: Date.now() + 300000, authority: "host-user OS authority" }); await pending;
  }
  async function review(kind = "create") { await input("name", "created"); await input("kind", kind); if (kind === "clone") await input("remote", "https://example.com/team/repo.git"); await at("review").click(); }
  async function confirm() { at("confirm-check").checked = true; await at("confirm-check").emit("change"); const pending = at("confirm").click(); await settle(); return { pending }; }
  async function load(ops = [], offset = 0, more = false) {
    const index = requests.length, pending = at("refresh").click(); await settle();
    await respond(index, 200, { operations: ops, next_offset: offset + ops.length, has_more: more }); await pending;
  }
  function action(verb, opID = id) { const row = at("list").children.find(row => row.dataset.operationId === opID); return row.children.at(-1).children.find(button => button.dataset.opRowAction === verb); }
  return { document, window, root, requests, makeRoot, at, settle, respond, input, select, review, confirm, load, action };
}

test("entry is idle; existing folder picker hook fills draft without authority or mutation", async () => {
  const f = fixture(); assert.equal(f.requests.length, 0);
  assert.equal(f.window.SnowProjectOperations.setParentPath(f.root, "/host/chosen"), true);
  assert.equal(f.at("parent").value, "/host/chosen"); assert.equal(f.requests.length, 0); assert.equal(f.at("review").disabled, true);
  const pending = f.at("browse").click(); await f.settle();
  assert.equal(f.requests[0].url, "/projects/folders"); assert.equal(f.requests[0].options.method, "POST");
  await f.respond(0, 200, { path: "/host/chosen", parent: "/host", folders: [{ name: "<svg onload=alert(1)>", path: "/host/chosen/nested" }], next_offset: 1, has_more: false }); await pending;
  assert.equal(f.at("folder-list").children[0].children[0].textContent, "<svg onload=alert(1)>");
  await f.at("folder-use").click(); assert.equal(f.requests.length, 1); assert.equal(f.at("review").disabled, true);
});

test("create requires explicit parent selection, immutable review and checked confirmation", async () => {
  const f = fixture(); await f.select(); await f.review();
  assert.equal(f.at("confirmation").hidden, false); assert.match(f.at("confirm-detail").textContent, /\/host\/parent\/created/);
  assert.equal(f.at("confirm").disabled, true); assert.equal(f.requests.length, 1);
  const { pending } = await f.confirm(); const req = f.requests[1];
  assert.equal(req.url, "/projects/create"); assert.equal(req.options.body.get("operation_id"), id);
  assert.equal(req.options.body.get("name"), "created"); assert.equal(req.options.body.get("csrf"), "fictional-csrf");
  assert.equal(req.options.redirect, "error"); assert.equal(req.options.credentials, "same-origin");
  assert.equal(f.at("review").disabled, true); assert.equal(f.at("confirmation").hidden, true);
  await f.respond(1, 202, operation()); await pending;
  assert.equal(f.requests.length, 2); assert.match(f.at("status").textContent, /Admitted/);
  assert.match(f.at("status").textContent, /No navigation or activation/);
  await f.at("confirm").click(); assert.equal(f.requests.length, 2);
});

test("clone clears locator on submission and never treats accepted ACK as success", async () => {
  const f = fixture(); await f.select(); await f.review("clone");
  assert.match(f.at("confirm-effects").textContent, /anonymous HTTPS/); assert.match(f.at("confirm-effects").textContent, /disk quota/);
  const { pending } = await f.confirm(); assert.equal(f.requests[1].url, "/projects/clone");
  assert.equal(f.requests[1].options.body.get("remote"), "https://example.com/team/repo.git"); assert.equal(f.at("remote").value, "");
  await f.respond(1, 202, operation({ kind: "clone", remote: "https://example.com/team/repo.git" })); await pending;
  assert.match(f.at("list").children[0].children[0].textContent, /Admitted/); assert.doesNotMatch(f.at("status").textContent, /Created and registered/);
});

test("lost response retains request ID, consumes grant and permits only explicit read recovery", async () => {
  const f = fixture(); await f.select(); await f.review("clone"); const { pending } = await f.confirm();
  f.requests[1].reject(new Error("connection lost")); await pending;
  assert.equal(f.at("uncertain").hidden, false); assert.match(f.at("uncertain-text").textContent, new RegExp(id)); assert.equal(f.at("draft").disabled, true);
  await f.at("confirm").click(); await f.at("select").click(); assert.equal(f.requests.length, 2);
  const check = f.at("check").click(); await f.settle(); assert.equal(f.requests[2].url, `/operations/${id}`); assert.equal(f.requests[2].options.method, undefined);
  await f.respond(2, 200, operation({ state: "needs_review", outcome: "unknown", revision: 5 })); await check;
  assert.equal(f.requests.length, 3); assert.equal(f.at("uncertain").hidden, true); assert.equal(f.at("review").disabled, true);
  assert.match(f.at("status").textContent, /Needs review/);
});

test("close aborts reads, never aborts write, and stale completion cannot bind to reopened view", async () => {
  const f = fixture(); const reading = f.at("refresh").click(); await f.settle();
  f.window.SnowProjectOperations.dispose(f.root); assert.equal(f.requests[0].options.signal.aborted, true);
  await f.respond(0, 200, { operations: [operation()], next_offset: 1, has_more: false }); await reading;
  assert.equal(f.at("list").children.length, 0);
  await f.select(); await f.review(); const { pending } = await f.confirm(); const write = f.requests[2];
  f.window.SnowProjectOperations.dispose(f.root); assert.equal(write.options.signal.aborted, false);
  f.root.open = true; await f.root.emit("toggle"); assert.equal(f.at("refresh").disabled, true);
  await f.respond(2, 202, operation()); await pending;
  assert.equal(f.at("list").children.length, 0); assert.equal(f.at("check").disabled, false); assert.equal(f.at("uncertain").hidden, false);
  assert.equal(f.requests.length, 3);
});

test("replacement root keeps request uncertainty but a different browser authorization cannot inherit drafts", async () => {
  const f = fixture(); await f.select(); await f.review(); const { pending } = await f.confirm();
  f.window.SnowProjectOperations.dispose(f.root); f.root.isConnected = false;
  const replacement = f.makeRoot(); f.window.SnowProjectOperations.init(replacement);
  assert.equal(f.at("check", replacement).disabled, true); assert.match(f.at("uncertain-text", replacement).textContent, new RegExp(id));
  const other = f.makeRoot("other-browser-csrf"); f.window.SnowProjectOperations.init(other);
  assert.equal(f.at("parent", other).value, ""); assert.equal(f.at("uncertain", other).hidden, true);
  await f.respond(1, 202, operation()); await pending;
  assert.equal(f.at("check", replacement).disabled, false); assert.equal(f.at("list", replacement).children.length, 0);
});

for (const bad of ["https://user:secret@example.com/a/b", "https://example.com/a/b?token=secret", "https://example.com/a/b#secret", "git@example.com:a/b", "https://example.com/a/%62", "https://example.com/a/../b"]) test(`reject secret/unsupported locator without request: ${bad.split(":")[0]}`, async () => {
  const f = fixture(); await f.select(); await f.input("name", "created"); await f.input("kind", "clone"); await f.input("remote", bad); await f.at("review").click();
  assert.equal(f.requests.length, 1); assert.equal(f.at("confirmation").hidden, true); assert.equal(f.at("remote").value, ""); assert.doesNotMatch(f.at("status").textContent, /secret/);
  const replacement = f.makeRoot(); f.window.SnowProjectOperations.init(replacement); assert.equal(f.at("remote", replacement).value, "");
});

test("name validation and IME never submit implicitly", async () => {
  const f = fixture(); await f.select();
  for (const name of ["", ".", "..", " trim", "trim ", "a/b", "a\\b", "x\n", "é".repeat(65)]) {
    await f.input("name", name); await f.at("review").click(); assert.equal(f.at("confirmation").hidden, true);
  }
  await f.input("name", "created"); await f.at("name").emit("compositionstart"); await f.at("review").click(); assert.equal(f.at("confirmation").hidden, true);
  let prevented = false; await f.at("name").emit("keydown", { key: "Enter", isComposing: true, preventDefault() { prevented = true; } }); assert.equal(prevented, false); assert.equal(f.requests.length, 1);
  await f.at("name").emit("compositionend"); await f.at("review").click(); assert.equal(f.at("confirmation").hidden, false); assert.equal(f.requests.length, 1);
});

test("ordinary registration remains explicit review and preserves unknown operation result", async () => {
  const f = fixture(); await f.load([operation({ state: "needs_review", outcome: "unknown", revision: 8 })]);
  await f.action("register").click(); assert.match(f.at("confirm-effects").textContent, /ordinary registration/); assert.match(f.at("confirm-effects").textContent, /will not convert/);
  const { pending } = await f.confirm(); const req = f.requests[1]; assert.equal(req.url, `/operations/${id}/register`);
  assert.equal(req.options.body.get("revision"), "8"); assert.equal(req.options.body.get("review"), "true");
  await f.respond(1, 200, operation({ state: "needs_review", outcome: "unknown", revision: 9, project_id: otherID })); await pending;
  assert.match(f.at("status").textContent, /Needs review/); assert.doesNotMatch(f.at("status").textContent, /Created and registered/);
  assert.equal(f.requests.length, 2);
});

for (const [action, state, outcome] of [["cancel", "running", "observed"], ["reconcile", "interrupted", "unknown"], ["dismiss", "failed", "observed"]]) test(`${action} uses reviewed exact-ID revision CAS and no automatic follow-up`, async () => {
  const f = fixture(); await f.load([operation({ state, outcome, revision: 4 })]); await f.action(action).click();
  if (action === "dismiss") assert.match(f.at("confirm-effects").textContent, /No files/);
  if (action === "reconcile") assert.match(f.at("confirm-effects").textContent, /does not retry/);
  if (action === "cancel") assert.match(f.at("confirm-effects").textContent, /cleanup is confirmed/);
  const { pending } = await f.confirm(); assert.equal(f.requests[1].url, `/operations/${id}/${action}`); assert.equal(f.requests[1].options.body.get("revision"), "4");
  await f.respond(1, action === "dismiss" ? 204 : 200, operation({ state: action === "cancel" ? "cancel_requested" : "needs_review", outcome, revision: 5 })); await pending;
  assert.equal(f.requests.length, 2);
  if (action === "dismiss") { assert.equal(f.at("list").children.length, 0); assert.match(f.at("status").textContent, /Files and project registration are unchanged/); }
  else if (action === "cancel") assert.match(f.at("status").textContent, /cleanup not yet confirmed/);
});

test("CAS conflict and wrong response target keep uncertainty guard instead of replay", async () => {
  for (const response of [409, 200]) {
    const f = fixture(); await f.load([operation({ state: "interrupted", outcome: "unknown", revision: 4 })]); await f.action("reconcile").click(); const { pending } = await f.confirm();
    await f.respond(1, response, operation({ id: otherID, state: "needs_review", outcome: "unknown", revision: 5 })); await pending;
    assert.equal(f.at("uncertain").hidden, false); assert.equal(f.at("list").children[0].dataset.operationId, id); assert.equal(f.requests.length, 2);
  }
});

test("bounded inventory, explicit pagination, no credentials in GET", async () => {
  const f = fixture(); await f.load([operation({ name: "<b>name</b>".replaceAll("/", "") })], 0, true);
  assert.equal(f.at("list").children[0].children[0].textContent, "<b>name<b> · Admitted"); assert.equal(f.requests[0].options.body, undefined);
  const next = f.at("next").click(); await f.settle(); assert.equal(f.requests[1].url, "/operations?offset=1");
  await f.respond(1, 200, { operations: [operation({ id: otherID })], next_offset: 2, has_more: false }); await next;
  assert.equal(f.at("list").children.length, 1); assert.equal(f.at("list").children[0].dataset.operationId, otherID); assert.equal(f.at("first").hidden, false);
  const oversized = fixture(); const pending = oversized.at("refresh").click(); await oversized.settle();
  await oversized.respond(0, 200, { operations: Array.from({ length: 33 }, () => operation()), next_offset: 33, has_more: false }); await pending;
  assert.equal(oversized.at("list").children.length, 0); assert.match(oversized.at("status").textContent, /Could not read/);
});

test("disabled capabilities do not issue fallback requests; source has no storage/navigation/HTML injection", async () => {
  const f = fixture({ enabled: false }); await f.at("refresh").click(); await f.at("select").click(); assert.equal(f.requests.length, 0);
  assert.doesNotMatch(source, /\b(?:localStorage|sessionStorage|innerHTML|insertAdjacentHTML)\b/);
  assert.doesNotMatch(source, /location\.(?:assign|replace)|\/activate|\/prompt|\/bash/);
  assert.match(template, /not confined to Snow/); assert.match(template, /No disk quota/); assert.match(template, /SSH/);
});


test("inconsistent succeeded record never claims creation or registration", async () => {
  const f = fixture(); await f.load([operation({ state: "succeeded", outcome: "unknown", project_id: otherID })]);
  assert.equal(f.at("list").children.length, 0); assert.match(f.at("status").textContent, /Could not read/);
  assert.doesNotMatch(f.at("status").textContent, /Created and registered/);
});
