import assert from "node:assert/strict";
import test from "node:test";
import {ActivityAccessEnded, boundedJSON, fetchActivity, MAX_SUMMARY_BYTES, projectLink, validateSummary} from "./summary.ts";
import type {ActivityProject, ActivitySummary} from "./summary.ts";

function project(id = "one"): ActivityProject {
  return {
    project_id: id, name: "Project " + id, project_url: projectLink(id), session_id: "saved-session",
    session_url: projectLink(id, "saved-session"), folder_state: "available", runtime_state: "permission",
    host_running: true, permissions: 1, questions: 0, failed: false, recovery: true,
    recovery_state: "admission_unknown", queued: 1, review: 1, unavailable: false,
  };
}
function summary(projects = [project()]): ActivitySummary {
  return {updated_at: "2026-06-01T12:00:00Z", projects,
    counts: {registered: projects.length, running: 1, permissions: 1, questions: 0, failed: 0, recovery: 1, queued: 1, review: 1}};
}
function response(body: BodyInit = JSON.stringify(summary()), headers: HeadersInit = {"Content-Type": "application/json; charset=utf-8"}) {
  return new Response(body, {headers});
}

test("summary accepts the bounded navigation-only DTO and exact generated session links", () => {
  assert.equal(validateSummary(summary()).projects[0].session_id, "saved-session");
  assert.equal(validateSummary(summary(Array.from({length: 100}, (_, i) => project("p" + i)))).projects.length, 100);
  const noSession = {...project(), session_id: "", session_url: ""};
  assert.equal(validateSummary(summary([noSession])).projects[0].session_url, "");
  assert.equal(projectLink("one", "saved-session"), "/?view=projects&project=one&session=saved-session");
});

test("summary rejects cardinality, duplicate IDs, invalid identifiers and malformed states", () => {
  assert.throws(() => validateSummary(summary(Array.from({length: 101}, (_, i) => project("p" + i)))));
  assert.throws(() => validateSummary(summary([project(), project()])));
  for (const change of [
    {project_id: "../one"}, {session_id: "a/b"}, {session_id: "x".repeat(129)}, {name: "x".repeat(129)},
    {runtime_state: "toString"}, {recovery_state: "replay"}, {folder_state: "toString"},
    {permissions: 2}, {questions: -1}, {queued: 8, review: 1}, {review: 0.1}, {host_running: 1},
    {unavailable: null}, {project_url: null}, {session_url: null},
  ]) assert.throws(() => validateSummary(summary([{...project(), ...change} as ActivityProject])));
  for (const counts of [{...summary().counts, review: 801}, {...summary().counts, running: 101}, {...summary().counts, failed: NaN}]) {
    assert.throws(() => validateSummary({...summary(), counts}));
  }
  assert.throws(() => validateSummary({...summary(), updated_at: "not a date"}));
});

test("wire navigation URLs must match IDs, contain no extra parameters and stay relative", () => {
  for (const url of [
    "javascript:alert(1)", "https://elsewhere.invalid/?view=projects&project=one", "//elsewhere.invalid/",
    "/?view=projects&project=other", "/?view=projects&project=one&project=one",
    "/?view=projects&project=one&activate=true", "/?view=projects&project=one#fragment",
    "/?view=projects&project=one&session=saved-session", "/?view=projects&project=one\n",
  ]) assert.throws(() => validateSummary(summary([{...project(), project_url: url}])));
  assert.throws(() => validateSummary(summary([{...project(), session_url: projectLink("one", "wrong-session")}])));
  assert.throws(() => projectLink("../unsafe"));
  assert.throws(() => projectLink("safe", "unsafe/session"));
});

test("bounded body requires JSON media type, valid UTF-8 and valid JSON", async () => {
  const signal = new AbortController().signal;
  assert.deepEqual(await boundedJSON(response(), signal), summary());
  await assert.rejects(boundedJSON(response("{}", {"Content-Type": "application/jsonp"}), signal), /type/);
  await assert.rejects(boundedJSON(response(new Uint8Array([0xc3, 0x28])), signal));
  await assert.rejects(boundedJSON(response('{"projects":'), signal));
  await assert.rejects(boundedJSON(new Response(null, {headers: {"Content-Type": "application/json"}}), signal), /body/);
});

test("body and advertised length independently enforce the 256 KiB limit", async () => {
  const signal = new AbortController().signal;
  for (const length of [String(MAX_SUMMARY_BYTES + 1), "invalid", "-1"]) {
    await assert.rejects(boundedJSON(response("{}", {"Content-Type": "application/json", "Content-Length": length}), signal), /size/);
  }
  let canceled = false;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array(MAX_SUMMARY_BYTES));
      controller.enqueue(new Uint8Array(1));
    },
    cancel() { canceled = true; },
  });
  await assert.rejects(boundedJSON(response(stream), signal), /size/);
  assert.equal(canceled, true);
});

test("transport is one same-origin no-store GET and rejects access loss without consuming JSON", async () => {
  let calls = 0;
  const controller = new AbortController();
  const request: typeof fetch = async (input, options) => {
    calls++;
    assert.equal(input, "/activity");
    assert.deepEqual(options, {method: "GET", credentials: "same-origin", cache: "no-store", redirect: "error", headers: {Accept: "application/json"}, signal: controller.signal});
    return response();
  };
  assert.deepEqual(await fetchActivity(controller.signal, request), summary());
  assert.equal(calls, 1);
  for (const status of [401, 403]) {
    await assert.rejects(fetchActivity(controller.signal, async () => new Response("not JSON", {status})), ActivityAccessEnded);
  }
  await assert.rejects(fetchActivity(controller.signal, async () => new Response("unavailable", {status: 503})), /refresh/);
});

test("aborted header and body completions cannot yield a summary or an auth transition", async () => {
  const controller = new AbortController();
  const request: typeof fetch = async () => { controller.abort(); return new Response("", {status: 401}); };
  await assert.rejects(fetchActivity(controller.signal, request), {name: "AbortError"});
  let canceled = false;
  const bodyController = new AbortController();
  const stream = new ReadableStream<Uint8Array>({cancel() { canceled = true; }});
  const reading = boundedJSON(response(stream), bodyController.signal);
  bodyController.abort();
  await assert.rejects(reading, {name: "AbortError"});
  assert.equal(canceled, true);
  await assert.rejects(fetchActivity(controller.signal, async () => { throw Error("aborted request started"); }), {name: "AbortError"});
});
