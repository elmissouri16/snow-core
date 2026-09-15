import assert from "node:assert/strict";
import test from "node:test";
import {description, idleSafe, scope, unchanged, validACK, validCompaction} from "./model.ts";
import type {Compaction, Snapshot} from "./model.ts";
const identity = {project_id: "project", instance_id: "instance", session_id: "session"};
const fields = {session_id: "session", branch_id: "main", expected_tip_id: "", expected_revision: "7"};
const captured: Compaction = {state: "running", session_id: "session", branch_id: "main", expected_tip_id: "", summarized_messages: 20, retained_messages: 8, progress_done: false, used_fallback: false, request_id: "42", compaction_id: "compact-one", turn_id: "compact-one", turn_origin: "compact", root_epoch: 1, turn_sequence: 1};
const snapshot: Snapshot = {revision: 7, status: "idle", goal: {session_id: "session", branch_id: "main", tip_id: ""}};
const ui = {supported: true, readable: true, safe: true};

test("status validates all native correlation fields and bounded counts", () => {
  for (const state of ["pending", "running", "completed", "noop", "fallback", "canceled", "failed", "uncertain"]) assert.equal(validCompaction({...captured, state}, "session"), true);
  for (const changes of [
    {session_id: "foreign"}, {branch_id: ""}, {branch_id: "bad\nbranch"}, {branch_id: "x".repeat(257)}, {expected_tip_id: null},
    {state: "done"}, {request_id: ""}, {compaction_id: "other"}, {turn_id: "other"}, {turn_origin: "prompt"},
    {root_epoch: 0}, {root_epoch: Number.MAX_SAFE_INTEGER + 1}, {turn_sequence: -1}, {turn_sequence: 1.5},
    {summarized_messages: -1}, {retained_messages: 1073741825}, {summarized_messages: "20"},
    {progress_done: 1}, {used_fallback: null},
  ]) assert.equal(validCompaction({...captured, ...changes}, "session"), false, JSON.stringify(changes));
  assert.equal(validCompaction({...captured, summarized_messages: 1073741824, retained_messages: 0}, "session"), true);
  for (const value of [null, false, "running", [], {}]) assert.equal(validCompaction(value, "session"), false);
});

test("only pending, failed and uncertain admit the entirely uncaptured receipt", () => {
  const uncaptured = {...captured, request_id: "", compaction_id: "", turn_id: "", turn_origin: "", root_epoch: 0, turn_sequence: 0};
  for (const state of ["pending", "failed", "uncertain"]) {
    assert.equal(validCompaction({...uncaptured, state}, "session"), true);
    for (const changes of [{request_id: "42"}, {compaction_id: "c"}, {turn_id: "c"}, {turn_origin: "compact"}, {root_epoch: 1}, {turn_sequence: 1}]) assert.equal(validCompaction({...uncaptured, state, ...changes}, "session"), false);
  }
  for (const state of ["running", "completed", "noop", "fallback", "canceled"]) assert.equal(validCompaction({...uncaptured, state}, "session"), false);
});

test("ACK checks owner, reviewed revision, exact branch and native root correlation", () => {
  const ack = {compaction_id: "compact-one", turn_id: "compact-one", turn_origin: "compact", root_epoch: 1, turn_sequence: 1, session_id: "session", branch_id: "main"};
  const result = {...identity, revision: 9, compaction_ack: ack};
  assert.equal(validACK(result, identity, fields), true);
  assert.equal(validACK({...result, revision: 7}, identity, fields), true);
  for (const changes of [{project_id: "foreign"}, {instance_id: "foreign"}, {session_id: "foreign"}, {revision: 6}, {revision: 0}, {revision: "9"}, {revision: 1.5}, {compaction_ack: null}]) assert.equal(validACK({...result, ...changes}, identity, fields), false);
  for (const changes of [{session_id: "foreign"}, {branch_id: "foreign"}, {compaction_id: ""}, {turn_id: "foreign"}, {turn_origin: "prompt"}, {root_epoch: 0}, {turn_sequence: 0}]) assert.equal(validACK({...result, compaction_ack: {...ack, ...changes}}, identity, fields), false);
  assert.equal(validACK(null, identity, fields), false);
});

test("review binds session, branch, empty tip and revision; refresh requires new review", () => {
  assert.deepEqual(scope(snapshot, "session"), fields);
  assert.equal(unchanged(fields, snapshot, "session"), true);
  assert.equal(unchanged(fields, {...snapshot, revision: 8}, "session"), false);
  for (const goal of [{...snapshot.goal, session_id: "other"}, {...snapshot.goal, branch_id: "other"}, {...snapshot.goal, tip_id: "other"}]) assert.equal(unchanged(fields, {...snapshot, goal}, "session"), false);
  assert.equal(scope(snapshot, "other"), null);
  for (const revision of [0, -1, "7", Number.MAX_SAFE_INTEGER + 1]) assert.equal(scope({...snapshot, revision}, "session"), null);
  assert.equal(scope({...snapshot, goal: {...snapshot.goal, tip_id: "bad\ntip"}}, "session"), null);
  assert.equal(unchanged(null, snapshot, "session"), false);
});

test("idle admission rejects pending work, nonterminal goals and uncertain operation", () => {
  assert.equal(idleSafe(snapshot, ui, false), true);
  assert.equal(idleSafe(snapshot, {...ui, safe: false}, false), false);
  assert.equal(idleSafe(snapshot, ui, true), false);
  for (const changes of [{status: "running"}, {cancel_requested: true}, {permission: {}}, {input: {}}, {queue: {items: [{}]}}, {goal: null}, {goal: {...snapshot.goal, running: true}}, {goal: {...snapshot.goal, goal_id: "goal", status: "paused"}}]) assert.equal(idleSafe({...snapshot, ...changes}, ui, false), false);
  for (const state of ["pending", "running", "uncertain"]) assert.equal(idleSafe({...snapshot, compaction: {...captured, state}}, ui, false), false);
  for (const status of ["complete", "budget_limited"]) assert.equal(idleSafe({...snapshot, goal: {...snapshot.goal, goal_id: "goal", status}}, ui, false), true);
  assert.equal(idleSafe({...snapshot, compaction: {...captured, state: "completed"}}, ui, false), true);
});

test("progress is not completion, and terminal descriptions retain public counts and caveats", () => {
  assert.match(description({...captured, progress_done: true}), /waiting for native cleanup.*Stop remains available/);
  assert.match(description({...captured, state: "completed"}), /20 messages summarized · 8 retained/);
  assert.match(description({...captured, state: "fallback"}), /not a full provider summary/);
  assert.match(description({...captured, state: "uncertain"}), /Nothing will be retried/);
});
