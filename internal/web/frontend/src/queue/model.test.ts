import assert from "node:assert/strict";
import {test} from "node:test";
import {blocking, canChange, canEnqueue, presentation, sameDraft, validAcknowledgement, validQueue} from "./model.ts";
import type {Admission, Draft, Queue} from "./model.ts";
// Same validation supplied by app.js validEditText; UTF-8 is the backend limit.
const validText = (text: unknown): boolean => typeof text === "string" && text.length <= 65536 && !!text.trim() && !/[\u0000\uD800-\uDFFF]/u.test(text) && new TextEncoder().encode(text).length <= 65536;
// Exact RuntimeQueue JSON shape: internal/web/queue_http.go and
// scripts/tests/browser/queue-next/fixture.mjs (no fabricated RPC-only fields).
function queue(): Queue {return {token: "root-queue-token", revision: 1, can_enqueue: true, items: [{id: "queued-id", text: "queued text", state: "pending"}]};}
function current(): Admission {return {queue: queue(), ui: {connected: true, status: "running", busy: false, unknown: false, stopping: false, editing: false}, store: {editors: new Map(), unknown: false}, invalid: false, composing: false};}
test("accepts the backend public DTO and every delivery/review state", () => {
  for (const state of ["pending", "starting", "held", "uncertain"] as const) {
    const q = queue(); q.items[0]!.state = state; assert.ok(validQueue(q, validText));
  }
  assert.ok(validQueue({...queue(), token: "", can_enqueue: false, items: []}, validText));
});
test("rejects invalid queue authority, unsafe revisions and incomplete DTOs", () => {
  for (const q of [null, {}, {...queue(), token: 1}, {...queue(), token: "x".repeat(257)}, {...queue(), revision: -1}, {...queue(), revision: 0.1}, {...queue(), revision: Number.MAX_SAFE_INTEGER + 1}, {...queue(), can_enqueue: "true"}, {...queue(), items: null}]) assert.equal(validQueue(q, validText), false);
});
test("item IDs are unique and bounded; unknown delivery states fail closed", () => {
  for (const fields of [{id: ""}, {id: "x".repeat(257)}, {id: null}, {state: "missing"}, {state: "delivered"}, {text: null}]) assert.equal(validQueue({...queue(), items: [{...queue().items[0], ...fields}]}, validText), false);
  assert.equal(validQueue({...queue(), items: [queue().items[0], queue().items[0]]}, validText), false);
});
test("text hook preserves exact Unicode and rejects malformed or oversized input", () => {
  for (const text of ["", " \n", "a\u0000b", "a\ud800", "\udc00b", "x".repeat(65537), "é".repeat(32769)]) assert.equal(validQueue({...queue(), items: [{id: "i", state: "pending", text}]}, validText), false);
  const text = " **literal** & <source>\nKeep exact café 😀 text. ";
  const q = {...queue(), items: [{id: "i", state: "pending", text}]};
  assert.ok(validQueue(q, validText)); assert.equal(q.items[0]!.text, text);
  assert.equal(validQueue(queue(), () => false), false);
});
test("eight slots and 256 KiB aggregate bound match runtime queue limits", () => {
  const items = Array.from({length: 8}, (_, i) => ({id: String(i), state: "pending", text: "x"}));
  assert.ok(validQueue({...queue(), items}, validText));
  assert.equal(validQueue({...queue(), items: [...items, {id: "extra", state: "pending", text: "x"}]}, validText), false);
  for (const item of items) item.text = "é".repeat(16384);
  assert.ok(validQueue({...queue(), items}, validText));
  items[0]!.text += "x"; assert.equal(validQueue({...queue(), items}, validText), false);
});
test("draft capture checks revision even after typing and undo", () => {
  const d: Draft = {value: "hello", revision: 1, start: 1, end: 4, direction: "backward"};
  assert.ok(sameDraft(d, {...d}));
  for (const fields of [{value: "new"}, {revision: 2}, {start: 0}, {end: 3}, {direction: "forward" as const}]) assert.equal(sameDraft(d, {...d, ...fields}), false);
});
test("enqueue synchronously obeys every independent admission guard", () => {
  assert.ok(canEnqueue(current()));
  for (const field of ["busy", "unknown", "stopping", "editing"] as const) {const c = current(); c.ui![field] = true; assert.equal(canEnqueue(c), false);}
  for (const status of ["idle", "permission", "input", "goal-running", "compacting", "failed", "closing"]) {const c = current(); c.ui!.status = status; assert.equal(canEnqueue(c), false);}
  const c = current();
  c.ui!.connected = false; assert.equal(canEnqueue(c), false); c.ui!.connected = true;
  c.composing = true; assert.equal(canEnqueue(c), false); c.composing = false;
  c.invalid = true; assert.equal(canEnqueue(c), false); c.invalid = false;
  c.store.unknown = true; assert.equal(canEnqueue(c), false); c.store.unknown = false;
  c.store.busy = {action: "queue-enqueue", id: "", revision: 1, token: "root-queue-token", focus: null}; assert.equal(canEnqueue(c), false); c.store.busy = false;
  c.queue!.token = ""; assert.equal(canEnqueue(c), false); c.queue = queue();
  c.queue.can_enqueue = false; assert.equal(canEnqueue(c), false); c.queue.can_enqueue = true;
  c.queue.items = Array.from({length: 8}, (_, i) => ({id: String(i), text: "x", state: "pending"})); assert.equal(canEnqueue(c), false);
  c.queue = null; assert.equal(canEnqueue(c), false);
});
test("held/uncertain work blocks normal sends but retains goal-time review removal", () => {
  const c = current(); assert.equal(blocking(c), false);
  for (const state of ["held", "uncertain"] as const) {
    c.queue!.items[0]!.state = state; c.ui!.status = "goal-running";
    assert.ok(blocking(c)); assert.ok(canChange(c)); assert.equal(canEnqueue(c), false);
    assert.equal(presentation(c).state, "Remove reviewed pending items before starting or switching conversations");
  }
});
test("unknown and invalid state remain blocking when queue projection is missing", () => {
  const c = current(); c.queue = null; c.invalid = true; assert.ok(blocking(c));
  c.invalid = false; c.store.unknown = true; assert.ok(blocking(c));
  c.store.unknown = false; assert.equal(blocking(c), false);
});
test("ACK requires the original nonce and strictly newer CAS, not latest SSE revision", () => {
  const q = queue(), op = {token: q.token, revision: 1};
  assert.equal(validAcknowledgement(q, op, q, validText), false);
  assert.ok(validAcknowledgement({...q, revision: 2}, op, {...q, revision: 4}, validText));
  assert.equal(validAcknowledgement({...q, revision: 2, token: "foreign"}, op, q, validText), false);
  assert.equal(validAcknowledgement({...q, revision: 2}, op, {...q, token: "new-root"}, validText), false);
  assert.equal(validAcknowledgement({...q, revision: 2}, op, null, validText), false);
  assert.equal(validAcknowledgement({...q, revision: Number.MAX_SAFE_INTEGER + 1}, op, q, validText), false);
});
test("presentation describes the foreign composer without writing it", () => {
  const c = current(); assert.equal(presentation(c).label, "Queue next"); assert.equal(presentation(c).hidden, false);
  c.store.busy = {action: "queue-enqueue", id: "", revision: 1, token: "root-queue-token", focus: null};
  assert.equal(presentation(c).label, "Queuing…"); assert.equal(presentation(c).disabled, true);
  c.store.busy = false; c.store.unknown = true; assert.match(presentation(c).state!, /nothing will retry/);
  c.store.unknown = false; c.ui!.status = "idle"; assert.equal(presentation(c).hidden, true); assert.equal(presentation(c).state, null);
});
