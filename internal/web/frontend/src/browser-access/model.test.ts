import test from "node:test";
import assert from "node:assert/strict";
import {boundedJSON, MAX_INVENTORY_BYTES, validateInventory, validateReceipt} from "./model.ts";
const id = "browser_" + "a".repeat(32);
const browser = {id, label: "Paired browser", current: true, created: "2026-01-01T00:00:00Z", last_used: "2026-01-02T00:00:00Z", expires: "2026-01-31T00:00:00Z"};
test("inventory validates bounded public identifiers, labels, dates and uniqueness", () => {
  assert.deepEqual(validateInventory({browsers: [browser], limit: 8}), [browser]);
  for (const change of [{id: "a".repeat(64)}, {id: id.toUpperCase()}, {label: ""}, {label: " trailing "}, {label: "hidden\u202e"},
    {label: "é".repeat(41)}, {current: "true"}, {created: null}, {created: "2026-02-30T00:00:00Z"}, {last_used: "invalid"}, {expires: "2025-01-01T00:00:00Z"}]) {
    assert.throws(() => validateInventory({browsers: [{...browser, ...change}], limit: 8}));
  }
  assert.throws(() => validateInventory({browsers: [browser, browser], limit: 8}));
  assert.throws(() => validateInventory({browsers: Array(9).fill(browser), limit: 8}));
  assert.throws(() => validateInventory({browsers: [browser], limit: 9}));
  const text = {...browser, label: "<script>fictional</script>"};
  assert.equal(validateInventory({browsers: [text], limit: 8})[0]?.label, text.label);
});
test("receipt must identify precisely the confirmed browser and carry a boolean", () => {
  assert.deepEqual(validateReceipt({revoked_id: id, signed_out: false}, id), {revoked_id: id, signed_out: false});
  assert.throws(() => validateReceipt({revoked_id: "browser_" + "b".repeat(32), signed_out: false}, id));
  assert.throws(() => validateReceipt({revoked_id: id, signed_out: "false"}, id));
});
test("JSON consumes only bounded valid UTF-8 and respects aborted body reads", async () => {
  const signal = new AbortController().signal;
  const response = (body: BodyInit, headers = {}) => new Response(body, {headers: {"Content-Type": "application/json", ...headers}});
  assert.deepEqual(await boundedJSON(response('{"browsers":[]}'), signal), {browsers: []});
  await assert.rejects(boundedJSON(response(" ".repeat(MAX_INVENTORY_BYTES + 1)), signal));
  await assert.rejects(boundedJSON(response("{}", {"Content-Length": String(MAX_INVENTORY_BYTES + 1)}), signal));
  await assert.rejects(boundedJSON(response(new Uint8Array([0xff])), signal));
  await assert.rejects(boundedJSON(response("{}", {"Content-Type": "text/html"}), signal));
  const controller = new AbortController();
  const stream = new ReadableStream<Uint8Array>({start(stream) {stream.enqueue(new TextEncoder().encode("{"));}});
  const reading = boundedJSON(response(stream), controller.signal);
  controller.abort();
  await assert.rejects(reading);
});
