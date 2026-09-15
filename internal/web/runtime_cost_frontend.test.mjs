import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";
import test from "node:test";

function fixture() {
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.dataset = {}; this.textContent = ""; }
    append(...children) { this.children.push(...children); }
  }
  const context = vm.createContext({window: {}, document: {createElement: tag => new Element(tag)}});
  vm.runInContext(readFileSync(new URL("./static/costs.js", import.meta.url), "utf8"), context);
  return {api: context.window.SnowCosts, Element};
}

test("unknown differs from verified zero and partial recorded estimates", () => {
  const {api} = fixture();
  for (const telemetry of [undefined, {}, {available: true}, {available: true, cost: {known: false, total: 0}}, {available: false, cost: {known: true, currency: "USD", total: 1}}]) {
    assert.equal(api.presentation(telemetry).known, false);
    assert.match(api.presentation(telemetry).value, /^Unknown/);
  }
  const zero = api.presentation({available: true, cost: {known: true, currency: "USD", total: 0}});
  assert.equal(zero.known, true);
  assert.equal(zero.value, "USD 0.00");
  for (const total of [0.00000001, 0.000001, 1.25, 1e12, Number.MAX_VALUE]) {
    const result = api.presentation({available: true, cost: {known: true, currency: "EUR", total}});
    assert.equal(result.known, true);
    assert.match(result.value, /^EUR /);
    assert.notEqual(result.value, "EUR 0.00");
    assert.ok(result.value.length < 40);
  }
});

test("invalid cost and currency cannot render misleading amounts or markup", () => {
  const {api} = fixture();
  for (const total of [-1, NaN, Infinity, -Infinity, "1", null]) {
    assert.equal(api.presentation({available: true, cost: {known: true, currency: "USD", total}}).known, false);
  }
  for (const currency of ["", "usd", "<img src=x onerror=alert(1)>", "USD".repeat(1000), null]) {
    assert.equal(api.presentation({available: true, cost: {known: true, currency, total: 1}}).known, false);
  }
});

test("popover renderer states accounting limitations and session reset stays unknown", () => {
  const {api, Element} = fixture();
  const dl = new Element("dl");
  api.render(dl, {available: true, cost: {known: true, currency: "USD", total: 1}});
  const [label, amount, note] = dl.children[0].children;
  assert.equal(label.textContent, "Recorded cost estimate");
  assert.equal(amount.dataset.workflowCost, "");
  assert.equal(amount.dataset.known, "true");
  assert.match(note.textContent, /exclude unpriced requests/);
  assert.match(note.textContent, /currency coverage is not verified/);
  assert.match(note.textContent, /not a billing charge/);
  const next = new Element("dl");
  api.render(next, {available: true, cost: null});
  assert.equal(next.children[0].children[1].dataset.known, "false");
  assert.match(next.children[0].children[1].textContent, /^Unknown/);
});
