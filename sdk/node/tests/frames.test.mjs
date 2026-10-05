import test from 'node:test';
import assert from 'node:assert/strict';
import { FrameDecoder, decodeFrame, validateReady, validateNotification } from '../../../build/sdk-node/test/frames.js';

const bytes = (s) => Buffer.from(s);
const malformed = (fn) => assert.throws(fn, { code: 'malformed_frame' });
const ready = { type: 'rpc_ready', protocol_version: '1', snow_version: 'test', capabilities: ['future'], max_input_bytes: 1 };

test('strict JSON rejects duplicates, escaped equal keys, invalid UTF8 and nonobjects', () => {
  for (const frame of [' ', '\r', 'null', '[]', '{}', '{"Type":"event"}', '{"type":1}',
    '{"type":"x"} {}', '{"type":"x","type":"y"}', '{"type":"x","\\u0074ype":"y"}',
    '{"type":"x","data":{"a":1,"\\u0061":2}}', '{"type":"x","data":[{"z":0,"z":1}]}',
    '{"type":"x","text":"\\x00"}', '\ufeff{"type":"x"}']) malformed(() => decodeFrame(bytes(frame)));
  malformed(() => decodeFrame(Buffer.from([123, 34, 116, 121, 112, 101, 34, 58, 34, 255, 34, 125])));
  malformed(() => decodeFrame(bytes('{"type":"x","data":' + '['.repeat(130) + '0' + ']'.repeat(130) + '}')));
  assert.equal(decodeFrame(bytes('{"type":"x","data":{"a":1,"A":2}}')).type, 'x');
});

test('LF byte framing survives every split, UTF8 splits, CRLF and Unicode separators', () => {
  const source = bytes('\n{"type":"future","text":"🙂a\u2028b"}\r\n\n');
  for (let split = 0; split <= source.length; split++) {
    const decoder = new FrameDecoder(100);
    const found = [];
    decoder.write(source.subarray(0, split), (frame, size) => found.push([frame, size]));
    decoder.write(source.subarray(split), (frame, size) => found.push([frame, size]));
    decoder.end();
    assert.equal(found.length, 1);
    assert.equal(found[0][0].text, '🙂a\u2028b');
    assert.equal(found[0][1], source.length - 3);
  }
});

test('frame payload bounds are exact and unterminated EOF is invalid', () => {
  const frame = bytes('{"type":"x"}');
  const decoder = new FrameDecoder(frame.length);
  decoder.write(frame, () => assert.fail('no LF yet'));
  decoder.write(bytes('\n'), () => {});
  decoder.end();
  const tooBig = new FrameDecoder(frame.length);
  tooBig.write(frame, () => {});
  assert.throws(() => tooBig.write(bytes(' '), () => {}), { code: 'frame_too_large' });
  const partial = new FrameDecoder(100);
  partial.write(frame, () => {});
  malformed(() => partial.end());
  const resumed = [];
  partial.write(bytes('{"type":"valid"}\n'), frame => resumed.push(frame));
  partial.end();
  assert.deepEqual(resumed, [{ type: 'valid' }]);
  const zero = new FrameDecoder(1);
  zero.write(bytes('\n\n'), () => assert.fail());
  zero.end();
});

test('readiness validates essential fields and copies immutable capabilities', () => {
  const r = validateReady(ready);
  ready.capabilities.push('later');
  assert.deepEqual(r.capabilities, ['future']);
  assert(Object.isFrozen(r));
  assert(Object.isFrozen(r.capabilities));
  assert.throws(() => validateReady({ ...ready, protocol_version: '2' }), { code: 'protocol_version' });
  for (const update of [{ snow_version: null }, { capabilities: ['a', 1] }, { max_input_bytes: 0 }, { max_input_bytes: 1.1 }]) {
    malformed(() => validateReady({ ...ready, ...update }));
  }
});

test('completion essentials checked while future fields and events survive', () => {
  validateNotification({ type: 'future', extra: { type: 1 } });
  validateNotification({ type: 'prompt_completed', request_id: '1', status: 'completed', future: true });
  validateNotification({ type: 'goal_run_completed', request_id: '1', goal_run_id: 'g', goal_id: 'g1', status: 'finished' });
  const compact = { type: 'compaction_completed', request_id: 'r', compaction_id: 't', session_id: 's', branch_id: 'b', turn_id: 't',
    turn_origin: 'compact', root_epoch: 1, turn_sequence: 1, status: 'noop', summarized_messages: 0, retained_messages: 0, used_fallback: false };
  validateNotification({ ...compact, future: 1 });
  const project = { type: 'project_completed', request_id: 'r', operation_id: 'o', child: { path: '/project', device: '0', inode: '123' }, status: 'succeeded' };
  validateNotification(project);
  for (const frame of [{ type: 'rpc_ready' }, { type: 'response', id: '1', command: 'x' },
    { type: 'response', id: '1', command: 'x', success: null }, { type: 'prompt_completed', request_id: '1', status: 'nope' },
    { ...compact, used_fallback: undefined }, { ...compact, summarized_messages: -1 },
    { ...project, child: { path: '/a/../b', device: '01', inode: '1' } }]) malformed(() => validateNotification(frame));
});
