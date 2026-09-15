import test from 'node:test';
import assert from 'node:assert/strict';
import {inventory, logPage, resultForScope, validRecord} from './model.ts';
const id = `proc_${'a'.repeat(32)}`;
const process = {process_id: id, name: 'server', status: 'running'};
const log = {process_id: id, status: 'running', output: 'ready\n', next_cursor: 6, omitted_bytes: 0, eof: false};
test('inventory accepts bounded public records, never PID handles or duplicate authority', () => {
  assert.deepEqual(inventory({processes: [process], truncated: false}), {processes: [process], truncated: false});
  assert.equal(inventory({processes: [process, process], truncated: false}), null);
  assert.equal(inventory({processes: [], truncated: 'false'}), null);
  assert.equal(validRecord({...process, process_id: '1234'}), false);
  assert.equal(validRecord({...process, name: 'é'.repeat(33)}), false);
  assert.equal(validRecord({...process, status: 'unknown'}), false);
  assert.equal(validRecord({...process, ready: 'true'}), false);
});
test('output pages bind selected process and monotonic safe cursor with byte bound', () => {
  assert.equal(logPage(log, id, 7), null);
  assert.equal(logPage({...log, next_cursor: Number.MAX_SAFE_INTEGER + 1}, id), null);
  assert.equal(logPage({...log, process_id: `proc_${'b'.repeat(32)}`}, id), null);
  assert.equal(logPage({...log, output: 'é'.repeat(16385)}, id), null);
  assert.equal(logPage({...log, omitted_bytes: -1}, id), null);
  assert.equal(logPage({...log, eof: 'false'}, id), null);
  assert.deepEqual(logPage(log, id), {output: 'ready\n', next_cursor: 6, omitted_bytes: 0, eof: false});
});
test('logs are plain bounded text with terminal escapes removed, not HTML interpreted', () => {
  assert.equal(logPage({...log, output: '\x1b[31m<b>red</b>\x1b[0m\n\tplain\x00\x1b]0;title\x07'}, id)?.output, '<b>red</b>\n\tplain');
});
test('response authority requires project, instance and current session together', () => {
  const scope = {project: 'p', instance: 'i', session: 's'}, result = {session_id: 's', processes: [process], truncated: false};
  const envelope = {project_id: 'p', instance_id: 'i', result};
  assert.equal(resultForScope(envelope, scope), result);
  for (const altered of [{...scope, project: 'other'}, {...scope, instance: 'new'}, {...scope, session: 'next'}]) assert.equal(resultForScope(envelope, altered), null);
  assert.equal(resultForScope({...envelope, result: []}, scope), null);
});
