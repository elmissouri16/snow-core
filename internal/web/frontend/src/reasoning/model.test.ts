import test from "node:test";
import assert from "node:assert/strict";
import {facts, options, sameScope, valid} from "./model.ts";
import type {Inspection, Snapshot} from "./model.ts";
const identity = {project_id: "project", instance_id: "instance", session_id: "session"};
const result = (): Inspection => ({...identity, branch_id: "branch", tip_id: "", revision: 3, provider: "provider", model: "model-without-name-heuristics", mode: "plan", permission_mode: "deny", thinking: "medium", reasoning_summary: "auto", text_verbosity: "low", thinking_levels: ["off", "medium", "high"], reasoning_summaries: [], text_verbosities: ["low", "high"], current_session_available: true, defaults_available: false});
test("public reasoning DTO accepts only bounded facts and verified session scope", () => {
  assert.equal(valid(result(), identity), true);
  for (const extra of [{revision: 0}, {revision: -1}, {revision: 1.5}, {revision: "3"}, {revision: Number.MAX_SAFE_INTEGER + 1}, {permission_mode: "ALLOW"}, {mode: "other"}, {current_session_available: false}, {defaults_available: true}, {branch_id: ""}, {tip_id: null}, {project_id: "other"}, {instance_id: "replacement"}, {session_id: "other"}]) {
    assert.equal(valid({...result(), ...extra}, identity), false, JSON.stringify(extra));
  }
  for (const value of [null, undefined, false, 3, "wrong", []]) assert.equal(valid(value, identity), false);
  for (const key of facts) {
    for (const value of ["\u0000", "private\ntext", "\u007f", "a".repeat(257), 3, false]) assert.equal(valid({...result(), [key]: value}, identity), false, key);
  }
  assert.equal(valid({...result(), tip_id: "tip"}, identity), true);
});
test("model capability lists are optional, bounded and unique without browser guesses", () => {
  for (const key of Object.values(options)) {
    for (const value of [undefined, null, [], ["worker-specific"]]) assert.equal(valid({...result(), [key]: value}, identity), true);
    for (const value of [["high", "high"], [""], ["private\ntext"], [2], "high", {}, Array.from({length: 17}, (_, i) => String(i))]) assert.equal(valid({...result(), [key]: value}, identity), false);
    assert.equal(valid({...result(), [key]: Array.from({length: 16}, (_, i) => String(i))}, identity), true);
  }
});
test("same-scope admission compares authoritative snapshot facts and revision, not unrelated metadata", () => {
  const inspected = result(), snapshot: Snapshot = {...inspected};
  assert.equal(sameScope(inspected, snapshot), true);
  assert.equal(sameScope(null, snapshot), false);
  assert.equal(sameScope(inspected, null), false);
  for (const key of ["project_id", "instance_id", "session_id", "provider", "model", "mode", "permission_mode", "thinking"]) assert.equal(sameScope(inspected, {...snapshot, [key]: "changed"}), false, key);
  for (const revision of [2, 4]) assert.equal(sameScope(inspected, {...snapshot, revision}), false);
  // The runtime snapshot does not advertise every inspection field; full
  // branch/tip/response facts are carried separately in the confirmed payload.
  assert.equal(sameScope(inspected, {...snapshot, messages: [], status: "idle"}), true);
});
