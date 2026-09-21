import test from "node:test";
import assert from "node:assert/strict";
import {draftRows, parseDefaults, parseProviders, request, saveBody} from "./model.ts";
import type {Target} from "./model.ts";

const globalTarget: Target = {scope: "global", project: ""};
function defaults(target: Target = globalTarget) {
  const group = {
    provider_model: {explicit: null, effective: {provider: "opencode-go", model: ""}, source: "builtin"},
    thinking: {explicit: null, effective: "off", source: "builtin"},
    ...(target.scope === "global" ? {
      reasoning_summary: {explicit: null, effective: "auto", source: "builtin"},
      text_verbosity: {explicit: null, effective: "low", source: "builtin"},
    } : {}),
  };
  return {scope: target.scope, project_id: target.project, revision: "a".repeat(64), applies_to: "future_runtime",
    availability: "not_network_verified", [target.scope]: group};
}
test("scope projections validate ownership, revision, availability, effective and explicit values", () => {
  for (const data of [null, [], {...defaults(), scope: "project"}, {...defaults(), project_id: false},
    {...defaults(), revision: ""}, {...defaults(), revision: "x".repeat(129)},
    {...defaults(), applies_to: "current_runtime"}, {...defaults(), availability: "network_verified"},
    {...defaults(), global: {}}]) assert.throws(() => parseDefaults(data, globalTarget));
  const loaded = parseDefaults(defaults(), globalTarget);
  assert.equal(loaded.group.provider_model.effective.model, "");
  assert.equal(draftRows(loaded).length, 4);
  const project: Target = {scope: "project", project: "project_fixture"};
  assert.equal(draftRows(parseDefaults(defaults(project), project)).length, 2);
  assert.throws(() => parseDefaults(defaults(project), {...project, project: "other_project"}));
  for (const invalidField of [null, [], {explicit: null, effective: "unknown", source: "builtin"},
    {explicit: "unknown", effective: "off", source: "builtin"}, {effective: "off", source: "builtin"},
    {explicit: null, effective: "off", source: "project"}]) {
    assert.throws(() => parseDefaults({...defaults(), global: {...loaded.group, thinking: invalidField}}, globalTarget));
  }
});
test("partial provider/model projection inherits provider but keeps the saved model", () => {
  const loaded = parseDefaults(defaults(), globalTarget);
  const data = {...defaults(), global: {...loaded.group, provider_model: {
    explicit: {provider: "", model: "model-v1"}, effective: {provider: "custom-profile", model: "model-v1"}, source: "global",
  }}};
  const row = draftRows(parseDefaults(data, globalTarget))[0];
  assert.deepEqual(row?.value, {provider: "custom-profile", model: "model-v1"});
  for (const effective of [{provider: "", model: ""}, {provider: "bad/provider", model: "m"}, {provider: "valid", model: "m".repeat(257)}]) {
    assert.throws(() => parseDefaults({...data, global: {...data.global, provider_model: {...data.global.provider_model, effective}}}, globalTarget));
  }
});
test("conditional body omits unchanged/reset values, preserves paired writes and exact scope fields", () => {
  const loaded = parseDefaults(defaults(), globalTarget), rows = draftRows(loaded);
  assert.equal(saveBody("csrf-token", loaded, rows), null);
  const changed = rows.map(row => row.name === "provider_model" ? {...row, op: "set" as const, value: {provider: "custom-profile", model: "model-v1"}} :
    row.name === "thinking" ? {...row, op: "reset" as const} : row);
  assert.deepEqual(Object.fromEntries(saveBody("csrf-token", loaded, changed)!), {
    csrf: "csrf-token", scope: "global", expected_revision: "a".repeat(64),
    provider_model_op: "set", provider: "custom-profile", model: "model-v1", thinking_op: "reset",
  });
  const project: Target = {scope: "project", project: "project_fixture"};
  const projectLoaded = parseDefaults(defaults(project), project);
  const projectRows = draftRows(projectLoaded).map(row => row.name === "thinking" ? {...row, op: "set" as const, value: "high"} : row);
  assert.deepEqual(Object.fromEntries(saveBody("csrf-token", projectLoaded, projectRows)!), {
    csrf: "csrf-token", scope: "project", project: "project_fixture", expected_revision: "a".repeat(64), thinking_op: "set", thinking: "high",
  });
});
test("provider statuses stay local and reject malformed, duplicate, oversized or arbitrary diagnostics", () => {
  const provider = {provider_id: "custom-profile", state: "configured", reason: "anonymous_access", checked_locally: true};
  assert.deepEqual(parseProviders({checked_locally: true, providers: [provider]}), [provider]);
  for (const data of [null, {checked_locally: "yes", providers: []}, {checked_locally: true, providers: [null]},
    {checked_locally: true, providers: [provider, provider]}, {checked_locally: true, providers: Array(129).fill(provider)},
    ...[{checked_locally: false}, {provider_id: "private/path"}, {state: "verified"}, {reason: "private diagnostic"}, {state: "expired"}]
      .map(change => ({checked_locally: true, providers: [{...provider, ...change}]}))]) assert.throws(() => parseProviders(data));
});
test("explicit transport retains same-origin, no-store, redirect denial and bounded replies without retries", async t => {
  const calls: {url: unknown; options: RequestInit | undefined}[] = [];
  let status = 200, text = JSON.stringify(defaults());
  t.mock.method(globalThis, "fetch", async (url: unknown, options?: RequestInit) => {
    calls.push({url, options}); return new Response(text, {status, headers: {"Content-Type": "application/json"}});
  });
  const signal = new AbortController().signal;
  assert.equal(calls.length, 0);
  await request("/settings/host?scope=global", signal, {headers: {Accept: "application/json"}});
  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.options?.credentials, "same-origin");
  assert.equal(calls[0]?.options?.cache, "no-store");
  assert.equal(calls[0]?.options?.redirect, "error");
  assert.ok(calls[0]?.options?.signal instanceof AbortSignal);
  text = "x".repeat(65537);
  await assert.rejects(request("/settings/host", signal), /size/);
  status = 409; text = "conflict";
  await assert.rejects(request("/settings/host", signal), /conflict/);
  assert.equal(calls.length, 3);
});


test("stream byte cap is enforced before decoding, independent of Content-Length", async t => {
  let canceled = 0, calls = 0;
  const bytes = new TextEncoder().encode(JSON.stringify("é".repeat(32768)));
  assert.ok(bytes.byteLength > 65536);
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    return new Response(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(bytes.subarray(0, 32768));
        controller.enqueue(bytes.subarray(32768));
      },
      cancel() { canceled++; },
    }), {headers: {"Content-Type": "application/json", "Content-Length": "12"}});
  });
  await assert.rejects(request("/settings/host", new AbortController().signal), /size/);
  assert.equal(canceled, 1);
  assert.equal(calls, 1);
});

test("bounded JSON accepts exactly 64 KiB and split UTF-8 but rejects malformed/truncated UTF-8", async t => {
  let chunks: Uint8Array[] = [new TextEncoder().encode(JSON.stringify("x".repeat(65534)))];
  t.mock.method(globalThis, "fetch", async () => new Response(new ReadableStream<Uint8Array>({
    start(controller) { for (const chunk of chunks) controller.enqueue(chunk); controller.close(); },
  }), {headers: {"Content-Type": "application/json; charset=utf-8"}}));
  const signal = new AbortController().signal;
  assert.equal(await request("/settings/host", signal), "x".repeat(65534));
  chunks = [new Uint8Array([34, 0xc3]), new Uint8Array([0xa9, 34])];
  assert.equal(await request("/settings/host", signal), "é");
  for (const bytes of [[34, 0xc3, 0x28, 34], [34, 0xc3]]) {
    chunks = [new Uint8Array(bytes)];
    await assert.rejects(request("/settings/host", signal), TypeError);
  }
});

test("incorrect JSON media type or advertised byte count cancels before consuming body", async t => {
  let headers: Record<string, string> = {}, canceled = 0;
  t.mock.method(globalThis, "fetch", async () => new Response(new ReadableStream<Uint8Array>({
    cancel() { canceled++; },
  }), {headers}));
  const invalidHeaders: Record<string, string>[] = [{}, {"Content-Type": "text/html"}, {"Content-Type": "application/json", "Content-Length": "65537"},
    {"Content-Type": "application/json", "Content-Length": "invalid"}];
  for (const value of invalidHeaders) {
    headers = value;
    await assert.rejects(request("/settings/host", new AbortController().signal), /type|size/);
  }
  assert.equal(canceled, 4);
});

test("abort settles a stalled body without awaiting an uncooperative cancellation or retrying", {timeout: 1000}, async t => {
  let canceled = 0, calls = 0;
  let started: () => void = () => {};
  const reading = new Promise<void>(resolve => { started = resolve; });
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    return new Response(new ReadableStream<Uint8Array>({
      pull() { started(); return new Promise<void>(() => {}); },
      cancel() { canceled++; return new Promise<void>(() => {}); },
    }), {headers: {"Content-Type": "application/json"}});
  });
  const controller = new AbortController();
  const pending = request("/settings/host", controller.signal);
  const rejected = assert.rejects(pending, {name: "AbortError"});
  await reading;
  controller.abort();
  await rejected;
  assert.equal(canceled, 1);
  assert.equal(calls, 1);
});

test("late headers and a body resolved in the abort turn cannot produce a receipt", async t => {
  let canceled = 0;
  let respond: (response: Response) => void = () => {};
  t.mock.method(globalThis, "fetch", () => new Promise<Response>(resolve => { respond = resolve; }));
  const controller = new AbortController();
  const pending = request("/settings/host", controller.signal);
  controller.abort();
  respond(new Response(new ReadableStream<Uint8Array>({cancel() { canceled++; }}), {headers: {"Content-Type": "application/json"}}));
  await assert.rejects(pending, {name: "AbortError"});
  assert.equal(canceled, 1);

  let complete: () => void = () => {}, ready: () => void = () => {};
  const reading = new Promise<void>(resolve => { ready = resolve; });
  t.mock.method(globalThis, "fetch", async () => new Response(new ReadableStream<Uint8Array>({
    start(stream) { complete = () => { stream.enqueue(new TextEncoder().encode("{}")); stream.close(); }; },
    pull() { ready(); },
  }), {headers: {"Content-Type": "application/json"}}));
  const next = new AbortController(), body = request("/settings/host", next.signal);
  const rejected = assert.rejects(body, {name: "AbortError"});
  await reading;
  complete();
  next.abort();
  await rejected;
});
