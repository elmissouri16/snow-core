import test from 'node:test';
import assert from 'node:assert/strict';
import { bound, cursor, historyID, sameMetadata, sameSelection, validHistoryInventory, validName, validPage, validPreparation, validPreview, verifiedSelection } from './model.ts';
import type { Identity, Page, Selection, Version } from './model.ts';

const identity: Identity = {project_id: 'project', instance_id: 'instance', session_id: 'session'};
const target: Version = {branch_id: 'saved', tip_id: 'tip', name: 'Saved branch', current: false};
const page: Page = {...identity, revision: 4, current_branch_id: 'current', current_tip_id: 'current-tip', versions: [target], has_more: false, next_cursor: ''};
const selected: Selection = {...identity, revision: 4, current_branch_id: page.current_branch_id, current_tip_id: page.current_tip_id, branch_id: target.branch_id, tip_id: target.tip_id, name: target.name};
const preview = {...identity, revision: 4, branch_id: target.branch_id, tip_id: target.tip_id, messages: [{role: 'assistant', text: 'Saved answer'}], has_more: false, next_cursor: ''};

test('inventory validates exact parent identity, current branch/tip, bounded unique rows and cursor truth', () => {
  assert.equal(validPage(page, identity), true);
  for (const key of ['project_id', 'session_id', 'instance_id'] as const) {
    assert.equal(bound({...page, [key]: 'different'}, identity), false);
    assert.equal(validPage({...page, [key]: 'different'}, identity), false);
  }
  for (const revision of [-1, 0.5, Number.MAX_SAFE_INTEGER + 1, '4', undefined]) assert.equal(validPage({...page, revision}, identity), false);
  assert.equal(validPage({...page, revision: 0}, identity), true);
  assert.equal(validPage({...page, versions: [target, target]}, identity), false);
  assert.equal(validPage({...page, versions: [{...target, current: true}]}, identity), false);
  assert.equal(validPage({...page, versions: [{...target, branch_id: 'current', tip_id: '', current: true}]}, identity), false);
  assert.equal(validPage({...page, current_tip_id: '', versions: [{...target, branch_id: 'current', tip_id: '', current: true}]}, identity), true);
  assert.equal(validPage({...page, has_more: true}, identity), false);
  assert.equal(validPage({...page, next_cursor: 'next'}, identity), false);
  assert.equal(validPage({...page, has_more: true, next_cursor: 'next'}, identity), true);
  assert.equal(validPage({...page, versions: Array(101).fill(target)}, identity), false);
  assert.equal(cursor('x'.repeat(4096)), true);
  assert.equal(cursor('x'.repeat(4097)), false);
  assert.equal(cursor('cursor\n'), false);
});

test('read-only preview binds exact typed branch/tip and bounds UTF-8 messages/tools', () => {
  assert.equal(validPreview(preview, target, identity), true);
  assert.equal(validPreview({...preview, tip_id: 'other'}, target, identity), false);
  assert.equal(validPreview({...preview, branch_id: 'other'}, target, identity), false);
  assert.equal(validPreview({...preview, instance_id: 'retired'}, target, identity), false);
  assert.equal(validPreview({...preview, has_more: true, next_cursor: 'next'}, target, identity), true);
  assert.equal(validPreview({...preview, messages: [{role: 'private', text: ''}]}, target, identity), false);
  assert.equal(validPreview({...preview, messages: Array(257).fill(preview.messages[0])}, target, identity), false);
  assert.equal(validPreview({...preview, messages: [{role: 'assistant', text: '😀'.repeat(262144)}]}, target, identity), true);
  assert.equal(validPreview({...preview, messages: [{role: 'assistant', text: '😀'.repeat(262145)}]}, target, identity), false);
  assert.equal(validPreview({...preview, messages: [{role: 'tool_activity', text: '', tools: [{tool: 'bash', status: 'saved'}]}]}, target, identity), true);
  assert.equal(validPreview({...preview, messages: [{role: 'tool_activity', text: '', tools: Array(129).fill({tool: 'bash'})}]}, target, identity), false);
});

test('restore preparation rejects altered origins, scope, missing tokens and expired confirmations', () => {
  const now = Date.parse('2026-01-01T00:00:00Z');
  const prepared = {...identity, branch_id: target.branch_id, tip_id: target.tip_id, current_branch_id: page.current_branch_id, current_tip_id: page.current_tip_id, restore_token: 'one-use-token', expires_at: '2026-01-01T00:01:00Z'};
  assert.equal(validPreparation(prepared, target, page, identity, now), true);
  for (const key of ['project_id', 'session_id', 'instance_id', 'branch_id', 'tip_id', 'current_branch_id', 'current_tip_id']) assert.equal(validPreparation({...prepared, [key]: 'other'}, target, page, identity, now), false);
  for (const restore_token of ['', null, 'bad\n', 'x'.repeat(257)]) assert.equal(validPreparation({...prepared, restore_token}, target, page, identity, now), false);
  for (const expires_at of ['', 'invalid', null, '2026-01-01T00:00:00Z', '2025-12-31T23:59:59Z']) assert.equal(validPreparation({...prepared, expires_at}, target, page, identity, now), false);
});

test('history confirmation compares every exact selection field and validates metadata without coercion', () => {
  assert.equal(sameSelection(selected, {...selected}), true);
  assert.equal(sameSelection(null, selected), false);
  for (const key of Object.keys(selected)) assert.equal(sameSelection(selected, {...selected, [key]: 'different'}), false);
  const result = {...identity, branch_id: target.branch_id, tip_id: target.tip_id, name: 'New name', revision: 5};
  assert.equal(sameMetadata(result, selected, 'New name'), true);
  assert.equal(sameMetadata({...result, revision: 3}, selected, 'New name'), false);
  assert.equal(sameMetadata({...result, revision: '5'}, selected, 'New name'), false);
  assert.equal(sameMetadata({...result, name: 'Other'}, selected, 'New name'), false);
  assert.equal(sameMetadata({...result, session_id: 'child'}, selected, 'New name'), false);
  assert.equal(historyID('detached-child'), true);
  assert.equal(historyID('child\u0085'), false);
});

test('history names use action-specific Unicode character and UTF-8 byte limits', () => {
  for (const action of ['history-branch-fork', 'history-branch-rename', 'history-session-fork']) {
    for (const name of ['', ' padded', 'padded ', 'a\nb', 'a\u0085b', 'a\u007fb']) assert.equal(validName(name, action), false);
    assert.equal(validName('😀'.repeat(64), action), true);
    assert.equal(validName('😀'.repeat(65), action), false);
  }
  assert.equal(validName('x'.repeat(64), 'history-branch-fork'), true);
  assert.equal(validName('x'.repeat(65), 'history-branch-fork'), false);
  assert.equal(validName('x'.repeat(72), 'history-session-fork'), true);
  assert.equal(validName('x'.repeat(73), 'history-session-fork'), false);
});


test('selection authority is immutable and revoked by refresh, stale scope, phase or mismatched revision/tip', () => {
  const state = {dialog: {open: true}, selected: target, preview, page, snapshot: {revision: 4}};
  const selection = verifiedSelection(state);
  assert.deepEqual(selection, selected);
  assert.equal(Object.isFrozen(selection), true);
  assert.notEqual(selection, selected);
  assert.equal(verifiedSelection(null), null);
  for (const change of [
    {dialog: {open: false}}, {operation: {}}, {phase: 'preparing'}, {phase: 'ready'},
    {phase: 'committing'}, {stale: true}, {page: null}, {preview: null}, {selected: null},
    {snapshot: {revision: 5}}, {page: {...page, revision: 5}},
    {preview: {...preview, branch_id: 'other'}}, {preview: {...preview, tip_id: 'other'}},
  ]) assert.equal(verifiedSelection({...state, ...change}), null);
  // A display-only retained preview cannot authorize another history action.
  const refreshing = {...state, page: null, selected: null, preview: null, retained: state};
  assert.equal(verifiedSelection(refreshing), null);
  assert.equal(verifiedSelection({...refreshing, page}), null);
});


test('inventory presentation permits only bounded exact same-project conversation links', () => {
  const project = '12345678-1234-1234-1234-123456789abc';
  const url = `/?view=projects&project=${project}&session=${encodeURIComponent('saved child/😀')}`;
  const value = {text: 'Saved conversations refreshed.', rows: [{name: 'Detached child', url}]};
  assert.equal(validHistoryInventory(value, project), true);
  assert.equal(validHistoryInventory({text: 'Refreshing saved conversations…', rows: []}, project), true);
  assert.equal(validHistoryInventory(value, 'different-project'), false);
  for (const invalid of [
    'javascript:alert(1)', `https://example.com${url}`, `//example.com${url}`,
    url + '&extra=true', url + '#fragment', url.replace('session=', 'other='),
    `/?view=projects&project=${project}&session=`, `/?view=projects&project=${project}&session=%XX`,
  ]) assert.equal(validHistoryInventory({...value, rows: [{name: 'Child', url: invalid}]}, project), false);
  assert.equal(validHistoryInventory({...value, rows: Array(1001).fill(value.rows[0])}, project), false);
  assert.equal(validHistoryInventory({...value, text: 'x'.repeat(4097)}, project), false);
  assert.equal(validHistoryInventory({...value, rows: [{name: 'x'.repeat(513), url}]}, project), false);
});
