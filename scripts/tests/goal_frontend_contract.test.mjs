import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const goals = readFileSync(new URL("../../internal/web/static/goals.js", import.meta.url), "utf8");
const app = readFileSync(new URL("../../internal/web/static/app.js", import.meta.url), "utf8");
const context = vm.createContext({window: {SnowQueue: {blocking: () => false}}, TextEncoder});
vm.runInContext(goals.replace("  window.SnowGoals =", "  globalThis.checkScope = (inspected, snapshot) => { view = {inspected, snapshot, goal: snapshot.goal}; return sameScope(); }; globalThis.checkBudget = text => { view = {draft: {budget: text}}; return budget(); };\n  window.SnowGoals ="), context);
vm.runInContext(app.slice(app.indexOf("  function validGoalRunACK("), app.indexOf("  async function runtimeAction(")) + "\nthis.checkACK = validGoalRunACK;", context);
vm.runInContext("let live;\n" + app.slice(app.indexOf("  const activeStatus ="), app.indexOf("  function attentionState()")) + "\nthis.admission = state => {live = state; return {stop: canStop(), mutate: mutationSafe(), attention: mutationSafe('attention')};};", context);
const goal = {session_id: "s", branch_id: "b", tip_id: "tip", goal_id: "g", goal_run_id: "run-1", objective: "Literal <script> & café 😀", status: "active", blocked_reason: "", deferred: false, running: true, tokens_used: 17, token_budget: 100, budget_remaining: 83, estimated_costs: []};
const current = {project: "p", instance: "i", session: "s"};
const fields = {branch_id: "b", expected_goal_id: "g"};
const snapshot = {project_id: "p", instance_id: "i", session_id: "s", revision: 4, status: "running", cancel_token: "whole-run-stop", goal, goal_run_ack: {session_id: "s", branch_id: "b", goal_id: "g", goal_run_id: "run-1"}};

test("goal DTO validates identity, absence, states, budgets and bounds", () => {
  const validate = value => context.window.SnowGoals.validGoal(value, "s");
  assert.ok(validate(goal));
  assert.ok(validate({...goal, running: false, goal_run_id: "", goal_id: "", objective: "", status: "none"}));
  for (const value of [null, {...goal, session_id: "other"}, {...goal, branch_id: ""}, {...goal, tip_id: "bad\n"}, {...goal, running: "true"}, {...goal, goal_run_id: ""}, {...goal, goal_id: ""}, {...goal, status: "finished"}, {...goal, status: "none"}, {...goal, deferred: 1}, {...goal, tokens_used: -1}, {...goal, tokens_used: Number.MAX_SAFE_INTEGER + 1}, {...goal, token_budget: 0}, {...goal, budget_remaining: -1}, {...goal, objective: "x".repeat(65537)}, {...goal, blocked_reason: "x".repeat(32769)}, {...goal, estimated_costs: Array(33).fill({})}]) assert.ok(!validate(value));
  for (const status of ["paused", "blocked", "usage_limited", "budget_limited", "complete"]) assert.ok(validate({...goal, running: false, goal_run_id: "", status}));
  assert.ok(validate({...goal, token_budget: null, budget_remaining: null}));
});

test("budgets are explicit positive safe integers or absent, never rounded", () => {
  assert.equal(context.checkBudget(""), null);
  for (const text of ["1", "100", "9007199254740991"]) assert.equal(context.checkBudget(text), text);
  for (const text of ["0", "-1", "1.5", "1e3", "+1", " 1", "01", "9007199254740992", "9999999999999999999999"]) assert.equal(context.checkBudget(text), false);
});

test("goal admission ACK retains exact worker, branch, goal and run correlation", () => {
  const validate = value => context.checkACK(value, current, fields, "goal-resume", 3);
  assert.ok(validate(snapshot));
  assert.ok(!validate({...snapshot, goal_run_ack: undefined}), 'a running snapshot is not an admission receipt');
  assert.ok(!validate({...snapshot, revision: 3}), 'admission must advance the reviewed revision');
  const startFields = {...fields, expected_goal_id: 'old', objective: '\u0085 ' + goal.objective + '\n', token_budget: '100'};
  const start = (value, sent = startFields) => context.checkACK(value, current, sent, 'goal-start', 3);
  assert.ok(start(snapshot), 'new goal matches the Go whitespace-normalized objective and explicit budget');
  assert.ok(!start(snapshot, {...startFields, objective: 'different'}));
  assert.ok(!start(snapshot, {...startFields, token_budget: '101'}));
  assert.ok(!start(snapshot, {...startFields, expected_goal_id: 'g'}));
  for (const value of [{...snapshot, project_id: "other"}, {...snapshot, instance_id: "other"}, {...snapshot, session_id: "other"}, {...snapshot, revision: 2}, {...snapshot, revision: 1.5}, {...snapshot, status: "failed"}, {...snapshot, cancel_token: ""}, {...snapshot, goal: {...goal, branch_id: "other"}}, {...snapshot, goal: {...goal, goal_id: "other"}}, {...snapshot, goal_run_ack: {...goal, goal_run_id: "different"}}, {...snapshot, goal_run_ack: {...goal, goal_run_id: "bad\n"}}, {...snapshot, goal_run_ack: {...goal, session_id: "other"}}, {...snapshot, goal_run_ack: {...goal, goal_id: "other"}}]) assert.ok(!validate(value));
  const completed = {...snapshot, status: "idle", cancel_token: "", goal: {...goal, status: "paused", running: false, goal_run_id: ""}};
  assert.ok(!validate({...completed, goal_run_ack: undefined}), "fast completion without an admission receipt is unknown, not guessed");
  assert.ok(validate({...completed, goal_run_ack: goal}), "independent receipt verifies admission without claiming semantic completion");
});

test("whole-goal Stop spans idle gaps and HTTP admission without enabling Prompt", () => {
  const state = {connected: true, status: "idle", turnCancel: true, snapshot};
  assert.ok(context.admission(state).stop);
  assert.ok(!context.admission(state).mutate);
  assert.ok(context.admission(state).attention, "goal permission/input answers remain available");
  for (const actionName of ["goal-start", "goal-resume"]) assert.ok(context.admission({...state, action: true, actionName}).stop);
  assert.ok(context.admission({...state, unknown: true}).stop, "fresh exact active goal permits Stop after unknown admission");
  for (const change of [{connected: false}, {invalidInstance: true}, {metadata: true}, {cancel: {}}, {action: true, actionName: "switch"}, {snapshot: {...snapshot, cancel_token: ""}}, {snapshot: {...snapshot, cancel_requested: true}}]) assert.ok(!context.admission({...state, ...change}).stop);
  assert.ok(!context.admission({...state, snapshot: {...snapshot, goal: {...goal, running: false}}}).stop);
  assert.ok(context.admission({...state, snapshot: {...snapshot, goal: {...goal, running: false}}}).mutate);
});

// Revision is inspection authority, not whichever SSE revision is newest when
// the user confirms. Even unchanged visible facts cannot authorize retargeting.
test("reviewed consent keeps the exact inspection revision", () => {
  const state = {...snapshot, provider: "provider", model: "model", permission_mode: "ask", mode: "default", thinking: "high"};
  const inspected = {goal, revision: 4, facts: {provider: "provider", model: "model", permission: "ask", mode: "default", thinking: "high"}};
  assert.ok(context.checkScope(inspected, state));
  for (const revision of [undefined, null, "4", -1, 0, 3, 5, 4.5, Number.MAX_SAFE_INTEGER + 1]) assert.ok(!context.checkScope({...inspected, revision}, state));
  assert.ok(!context.checkScope(inspected, {...state, revision: 5}));
  assert.ok(!context.checkScope(inspected, {...state, model: "changed"}));
  assert.ok(!context.checkScope(inspected, {...state, permission_mode: "allow"}));
  assert.ok(!context.checkScope(inspected, {...state, mode: "plan"}));
  assert.ok(!context.checkScope(inspected, {...state, goal: {...goal, tip_id: "changed"}}));
});

test("typed goal transport sends only the exact reviewed positive revision", async () => {
  let hooks; const calls = [];
  const scope = vm.createContext({
    current: {...current, revision: 4, panelMutation: "goals"},
    window: {SnowGoals: {init: api => { hooks = api; }}},
    panelIdentity: state => ({project_id: state.project, instance_id: state.instance, session_id: state.session}),
    $: () => null, openDialog() {}, closeDialog() {}, validEditText() {},
    runtimeAction: (action, sent) => { calls.push({action, sent}); return Promise.resolve(true); }
  });
  vm.runInContext("let live = current;\n" + app.slice(app.indexOf("  function setupGoals()"), app.indexOf("  function validGoalRunACK(")) + "\nsetupGoals();", scope);
  assert.equal(await hooks.run("goal-start", {...fields, objective: "Goal", expected_revision: "999"}, 4), true);
  assert.equal(calls[0].sent.expected_revision, "4");
  assert.equal(calls[0].sent.branch_id, "b");
  assert.equal(calls[0].sent.objective, "Goal");
  for (const revision of [undefined, null, 0, -1, "4", 3, 5, 4.5, Number.MAX_SAFE_INTEGER + 1]) assert.equal(await hooks.run("goal-start", fields, revision), false);
  assert.equal(await hooks.run("arbitrary-rpc", fields, 4), false);
  assert.equal(calls.length, 1, "stale or invalid authority never issues a mutation");
  assert.equal(await hooks.run("goal-resume", fields, 4), true);
  assert.equal(calls[1].sent.expected_revision, "4");
  assert.equal(Object.hasOwn(calls[1].sent, "objective"), false);
  scope.current.revision = 5;
  assert.equal(await hooks.run("goal-resume", fields, 4), false);
  assert.equal(calls.length, 2, "new SSE revision cannot silently replace reviewed consent");
});
