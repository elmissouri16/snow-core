import {test} from 'node:test';
import assert from 'node:assert/strict';
import {activeTurn, choices, contextMeter, isPolicy, presentation, sessionChoices, statusLabel, telemetryView, validName} from './model.ts';
import type {Snapshot, Telemetry} from './model.ts';
const snapshot: Snapshot = {project_id: 'p', instance_id: 'i', session_id: 's', status: 'idle'};

test('rename trims names and applies the 256 UTF-8 byte limit, not character count', () => {
  assert.equal(validName(' \n '), false);
  assert.equal(validName(' Snow '), true);
  assert.equal(validName('é'.repeat(128)), true);
  assert.equal(validName('é'.repeat(129)), false);
  assert.equal(validName('🧊'.repeat(64)), true);
  assert.equal(validName('🧊'.repeat(65)), false);
});

test('choices reject foreign runtime/project or malformed inventory envelopes', () => {
  const value = {instance_id: 'i', project_id: 'p', models: [], sessions: []};
  assert.ok(choices(value, 'i', 'p'));
  for (const bad of [null, [], {...value, instance_id: 'old'}, {...value, project_id: 'other'}, {...value, models: null}, {...value, sessions: {}}]) assert.equal(choices(bad, 'i', 'p'), null);
});

test('choices keep valid IDs, normalize untrusted labels, and preserve bounded-discovery flags', () => {
  const parsed = choices({instance_id: 'i', models: [null, {provider: '', id: 'no'}, {provider: 'p', id: 'model', name: 3}, {provider: 'p', id: 'named', name: 'Nice'}], sessions: [null, {name: 'missing'}, {session_id: 's', name: false}], models_partial: true, models_truncated: true, sessions_truncated: true, sessions_available: false}, 'i', 'p');
  assert.deepEqual(parsed?.models.map(({provider, id, name}) => ({provider, id, name})), [{provider: 'p', id: 'model', name: ''}, {provider: 'p', id: 'named', name: 'Nice'}]);
  assert.deepEqual(parsed?.sessions, [{session_id: 's', name: ''}]);
  assert.equal(parsed?.models_partial, true); assert.equal(parsed?.models_truncated, true);
  assert.equal(parsed?.sessions_available, false); assert.equal(parsed?.sessions_truncated, true);
  assert.deepEqual(sessionChoices([{session_id: 'target', name: 'Saved'}, undefined]), [{session_id: 'target', name: 'Saved'}]);
  assert.equal(sessionChoices({}), null);
});

test('recorded cost requires available usage, explicit knowledge, valid currency and finite nonnegative total', () => {
  const known: Telemetry = {available: true, cost: {known: true, currency: 'USD', total: 1.25}};
  assert.deepEqual(presentation(known), {known: true, value: `USD ${(1.25).toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 6})}`});
  for (const value of [undefined, {...known, available: false}, {...known, cost: {...known.cost, known: false}}, {...known, cost: {...known.cost, currency: 'usd'}}, ...[-1, NaN, Infinity].map(total => ({...known, cost: {...known.cost, total}}))]) assert.deepEqual(presentation(value), {known: false, value: 'Unknown · cost not available'});
});

test('tiny positive and extreme recorded subtotals never masquerade as verified zero', () => {
  const cost = (total: number) => presentation({available: true, cost: {known: true, currency: 'USD', total}}).value;
  assert.equal(cost(0), 'USD 0.00');
  assert.equal(cost(1e-9), 'USD 1.000e-9');
  assert.equal(cost(1e12), 'USD 1.000e+12');
});

test('context ring has no percentage without both finite measured context and a positive window', () => {
  for (const value of [undefined, {context_tokens: 10, context_window: 100}, {context_available: true, context_tokens: -1, context_window: 100}, {context_available: true, context_tokens: 1, context_window: 0}, {context_available: true, context_tokens: Infinity, context_window: 100}]) assert.deepEqual(contextMeter(value), {known: false, percent: 0});
  assert.deepEqual(contextMeter({context_available: true, context_tokens: 25, context_window: 100}), {known: true, percent: 25});
  assert.deepEqual(contextMeter({context_available: true, context_tokens: 200, context_window: 100}), {known: true, percent: 100});
  assert.deepEqual(contextMeter({context_available: true, context_tokens: 0, context_window: 100}), {known: true, percent: 0});
});

test('usage differentiates unavailable values, measured input and estimates', () => {
  assert.deepEqual(telemetryView(snapshot), {contextLabel: 'Estimated context', context: 'Unknown', usage: 'Unknown', cost: 'Unknown', knownCost: false});
  const view = telemetryView({...snapshot, telemetry: {context_available: true, estimated: false, context_tokens: 50, context_window: 0, available: true, input_tokens: 0, output_tokens: 0, total_tokens: 0}});
  assert.equal(view.contextLabel, 'Last reported input'); assert.equal(view.context, '50 tokens / unknown window'); assert.equal(view.usage, '0 in · 0 out · 0 total');
});

test('status and policy labels never promote unknown authority to an active capability', () => {
  assert.equal(isPolicy('allow'), true); assert.equal(isPolicy('toString'), false); assert.equal(isPolicy(undefined), false);
  assert.equal(activeTurn('permission'), true); assert.equal(activeTurn('input'), true); assert.equal(activeTurn('idle'), false);
  assert.equal(statusLabel({...snapshot, cancel_requested: true}), 'Stopping');
  assert.equal(statusLabel({...snapshot, goal: {running: true}}), 'Goal running');
  assert.equal(statusLabel({...snapshot, status: 'new-worker-state'}), 'Unavailable');
});
