import test from 'node:test';
import assert from 'node:assert/strict';
import {budget, facts, sameScope, validGoal} from './model.ts';
import type {Goal, Inspection, Snapshot} from './model.ts';

const goal: Goal = {session_id: 's', branch_id: 'b', tip_id: 'tip', goal_id: 'g', goal_run_id: 'run-1', objective: 'Literal <script> & café 😀', status: 'active', blocked_reason: '', deferred: false, running: true, tokens_used: 17, token_budget: 100, budget_remaining: 83, estimated_costs: []};

test('goal DTO validates scope, absence, states, budgets and bounded public text', () => {
  assert.ok(validGoal(goal, 's'));
  assert.ok(validGoal({...goal, running: false, goal_run_id: '', goal_id: '', objective: '', status: 'none'}, 's'));
  for (const invalid of [null, [], 3, {...goal, session_id: 'other'}, {...goal, branch_id: ''}, {...goal, tip_id: 'bad\n'}, {...goal, running: 'true'}, {...goal, goal_run_id: ''}, {...goal, goal_id: ''}, {...goal, status: 'finished'}, {...goal, status: 'none'}, {...goal, deferred: 1}, {...goal, tokens_used: -1}, {...goal, tokens_used: Number.MAX_SAFE_INTEGER + 1}, {...goal, token_budget: 0}, {...goal, budget_remaining: -1}, {...goal, objective: '😀'.repeat(16385)}, {...goal, blocked_reason: 'x'.repeat(32769)}, {...goal, estimated_costs: Array(33).fill({})}]) assert.equal(validGoal(invalid, 's'), false);
  for (const status of ['paused', 'blocked', 'usage_limited', 'budget_limited', 'complete']) assert.ok(validGoal({...goal, running: false, goal_run_id: '', status}, 's'));
  assert.ok(validGoal({...goal, token_budget: null, budget_remaining: null}, 's'));
});

test('budgets never round, normalize, or accept implicit/exponential values', () => {
  assert.equal(budget(''), null);
  for (const text of ['1', '100', '9007199254740991']) assert.equal(budget(text), text);
  for (const text of ['0', '-1', '1.5', '1e3', '+1', ' 1', '01', '9007199254740992', '9999999999999999999999']) assert.equal(budget(text), false);
});

test('review requires exact positive revision, goal facts and runtime authority', () => {
  const snapshot: Snapshot = {goal, revision: 4, provider: 'p', model: 'm', permission_mode: 'ask', mode: 'default', thinking: 'medium'};
  const inspected: Inspection = {goal, revision: 4, facts: facts(snapshot)};
  assert.ok(sameScope(inspected, snapshot, goal));
  for (const revision of [0, -1, 3, 5, 4.5, '4', null, undefined]) assert.equal(sameScope(inspected, {...snapshot, revision}, goal), false);
  assert.equal(sameScope({...inspected, revision: 0}, {...snapshot, revision: 0}, goal), false);
  for (const field of ['provider', 'model', 'permission_mode', 'mode', 'thinking']) assert.equal(sameScope(inspected, {...snapshot, [field]: 'different'}, goal), false);
  for (const change of [{session_id: 'other'}, {branch_id: 'other'}, {tip_id: 'new'}, {goal_id: ''}, {goal_run_id: 'other'}, {running: false}, {status: 'paused'}, {deferred: true}, {objective: 'changed'}, {tokens_used: 18}, {token_budget: 101}, {budget_remaining: 82}]) assert.equal(sameScope(inspected, snapshot, {...goal, ...change}), false);
  assert.equal(sameScope(null, snapshot, goal), false);
  assert.equal(sameScope(inspected, null, goal), false);
  assert.equal(sameScope(inspected, snapshot, null), false);
});
