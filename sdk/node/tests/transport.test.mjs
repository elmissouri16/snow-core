import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, realpath, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { RpcTransport } from '../../../build/sdk-node/test/transport.js';
import { SdkError, RemoteRpcError } from '../../../build/sdk-node/test/errors.js';

const fixture = fileURLToPath(new URL('./fixtures/rpc-peer.mjs', import.meta.url));
const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms));
const opts = (mode = 'echo', extra = {}) => ({ executable: process.execPath, args: [fixture, mode], handshakeTimeoutMs: 2000, shutdownTimeoutMs: 60, ...extra });
async function peer(t, mode = 'echo', extra = {}) {
  const transport = await RpcTransport.start(opts(mode, extra));
  t.after(() => transport.close());
  return transport;
}
async function terminal(transport) {
  return new Promise(resolve => transport.onTerminal(resolve));
}
const raw = (value) => Buffer.from(value).toString('base64');
const requestBytes = (request, id = '1') => Buffer.byteLength(JSON.stringify({ ...request, id }) + '\n');

test('spawn literal argv, cwd, replaced/empty environments and readiness copy', async t => {
  const cwd = await mkdtemp(join(tmpdir(), 'snow-node-'));
  t.after(() => rm(cwd, { recursive: true, force: true }));
  const literal = 'x y; $(touch nope) \' "';
  const transport = await peer(t, 'echo', { args: [fixture, 'echo', literal], cwd, env: { SDK_ENV: 'explicit' } });
  assert.equal(transport.ready.protocol_version, '1');
  assert.equal(transport.ready.snow_version, 'fake-node');
  const readinessCopy = transport.ready;
  assert.throws(() => readinessCopy.capabilities.push('caller-only'), TypeError);
  assert.throws(() => { readinessCopy.max_input_bytes = 1; }, TypeError);
  assert(!transport.ready.capabilities.includes('caller-only'));
  assert.equal(transport.ready.max_input_bytes, 16777216);
  const response = await transport.call({ type: 'inspect', id: 'caller-id' });
  assert.notEqual(response.id, 'caller-id');
  assert.deepEqual(response.data.args, [literal]);
  assert.equal(response.data.cwd, await realpath(cwd));
  assert.equal(response.data.env, 'explicit');
  const empty = await peer(t, 'echo', { env: {} });
  assert.equal((await empty.call({ type: 'inspect' })).data.env, null);
});

test('validation fails before spawn, without peer/OS/env diagnostics', async () => {
  for (const extra of [{ executable: '' }, { args: new Array(1) }, { args: ['\0'] }, { env: { BAD: '\0' } }, { maxFrameBytes: 0 },
    { maxFrameBytes: 64 * 1024 * 1024 + 1 }, { eventBuffer: 4097 }, { maxEventBytes: 256 * 1024 * 1024 + 1 },
    { maxPending: -1 }, { maxPendingBytes: 0 }, { maxPendingBytes: 256 * 1024 * 1024 + 1 }, { writeTimeoutMs: Infinity }, { shutdownTimeoutMs: -1 }]) {
    await assert.rejects(RpcTransport.start(opts('echo', extra)), { code: 'invalid_options', outcomeUnknown: false });
  }
  await assert.rejects(RpcTransport.start(opts('echo', { executable: '/does-not-exist-PRIVATE' })), error => {
    assert.equal(error.code, 'spawn_failed');
    assert(!error.message.includes('PRIVATE'));
    return true;
  });
  await assert.rejects(RpcTransport.start(opts('stderr-exit')), error => !error.message.includes('PRIVATE-PEER'));
});

test('startup timeout/abort tears down and reaps the owned child', async t => {
  const dir = await mkdtemp(join(tmpdir(), 'snow-pid-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const path = join(dir, 'pid');
  await assert.rejects(RpcTransport.start(opts('silent', { env: { SDK_PEER_PID_FILE: path }, handshakeTimeoutMs: 250 })), { code: 'handshake_timeout' });
  const pid = Number(await readFile(path, 'utf8'));
  assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' });
  const abort = new AbortController();
  const start = RpcTransport.start(opts('silent', { signal: abort.signal }));
  setTimeout(() => abort.abort('PRIVATE-reason'), 50);
  await assert.rejects(start, { code: 'aborted' });
});

test('bad readiness and oversized unterminated handshake fail closed', async () => {
  for (const [text, code] of [['{"type":"future"}\n', 'malformed_frame'],
    ['{"type":"rpc_ready","protocol_version":"2"}\n', 'protocol_version'],
    ['{"type":"rpc_ready","protocol_version":"1","snow_version":"x","capabilities":[],"max_input_bytes":0}\n', 'malformed_frame'],
    ['x'.repeat(1025), 'frame_too_large']]) {
    await assert.rejects(RpcTransport.start(opts('raw-handshake', { args: [fixture, 'raw-handshake', raw(text)], maxFrameBytes: 1024 })), { code });
  }
});

test('concurrent calls serialized, first response wins and beforeSend is synchronous', async t => {
  const transport = await peer(t);
  const sent = [];
  const promises = Array.from({ length: 20 }, (_, index) => transport.call({ type: index === 0 ? 'duplicate' : 'inspect' }, {}, id => sent.push(id)));
  assert.equal(sent.length, 1);
  const responses = await Promise.all(promises);
  assert.equal(sent.length, 20);
  assert.equal(new Set(responses.map(frame => frame.id)).size, 20);
  assert(responses.every(frame => frame.success));
  await assert.rejects(transport.call({ type: 'inspect' }, { guard: () => false }), { code: 'run_finished', outcomeUnknown: false });
  assert(!transport.closed);
});

test('notification handlers run before queue, unknown events forwarded and one consumer', async t => {
  const transport = await peer(t);
  const events = transport.events()[Symbol.asyncIterator]();
  assert.throws(() => transport.events(), { code: 'event_consumer' });
  const observed = [];
  const off = transport.onNotification(frame => observed.push(frame.type));
  await transport.call({ type: 'emit', frames: [{ type: 'future', extra: 17 }, { type: 'prompt_completed', request_id: 'public-id', status: 'completed' }] });
  assert.deepEqual(observed, ['future', 'prompt_completed']);
  assert.equal((await events.next()).value.extra, 17);
  assert.equal((await events.next()).value.type, 'prompt_completed');
  off();
  await transport.close();
  assert.equal((await events.next()).done, true);
});

test('response mismatch/invalid frames terminate, queued events precede iteration error', async t => {
  const transport = await peer(t);
  const events = transport.events()[Symbol.asyncIterator]();
  await transport.call({ type: 'emit', frames: [{ type: 'future' }] });
  const error = terminal(transport);
  await assert.rejects(transport.call({ type: 'mismatch' }), { code: 'malformed_frame', outcomeUnknown: true });
  assert.equal((await error).code, 'malformed_frame');
  assert.equal((await events.next()).value.type, 'future');
  await assert.rejects(events.next(), { code: 'malformed_frame' });
  let immediate;
  transport.onTerminal(err => { immediate = err; });
  assert.equal(immediate, transport.terminalError);
});

test('wire/event count and aggregate byte caps fail closed, pending cap rejects locally', async t => {
  for (const extra of [{ eventBuffer: 1 }, { maxEventBytes: 35 }]) {
    const transport = await peer(t, 'echo', extra);
    await assert.rejects(transport.call({ type: 'emit', frames: [{ type: 'future', text: 'one' }, { type: 'future', text: 'two' }] }), { code: 'event_overflow' });
    assert(transport.closed);
  }
  const pending = await peer(t, 'echo', { maxPending: 1 });
  const slow = pending.call({ type: 'delay', delay: 100 });
  await assert.rejects(pending.call({ type: 'inspect' }), { code: 'too_many_pending', outcomeUnknown: false });
  await slow;
  assert(!pending.closed);
  const small = await peer(t, 'small-input');
  await assert.rejects(small.call({ type: 'inspect', text: 'x'.repeat(100) }), { code: 'frame_too_large' });
  assert(!small.closed);
});

test('pending byte budget includes LF and rejects excess locally before send', async t => {
  const request = { type: 'inspect' };
  const transport = await peer(t, 'echo', { maxPendingBytes: requestBytes(request) - 1 });
  let sent = false;
  await assert.rejects(transport.call(request, {}, () => { sent = true; }), { code: 'pending_bytes_exceeded', outcomeUnknown: false });
  assert.equal(sent, false);
  assert.equal(transport.closed, false);
  assert.equal((await transport.call({ type: 'x' })).success, true);
  const exact = await peer(t, 'echo', { maxPendingBytes: requestBytes(request) });
  assert.equal((await exact.call(request)).success, true);
});

test('written buffers leave the byte budget before responses settle', async t => {
  const request = { type: 'delay', delay: 250, text: 'x'.repeat(1024) };
  const transport = await peer(t, 'echo', { maxPendingBytes: requestBytes(request) });
  let settled = false;
  const first = transport.call(request).then(frame => { settled = true; return frame; });
  // The first write still owns its buffer until its callback/drain completes.
  await assert.rejects(transport.call(request), { code: 'pending_bytes_exceeded', outcomeUnknown: false });
  await delay(20);
  assert.equal((await transport.call({ type: 'inspect' })).success, true);
  assert.equal(settled, false);
  await first;
  // Settling the already-written call must not release its reservation twice.
  await assert.rejects(transport.call({ ...request, text: 'x'.repeat(2048) }), { code: 'pending_bytes_exceeded', outcomeUnknown: false });
  assert.equal((await transport.call(request)).success, true);
  assert.equal(transport.closed, false);
});

test('queued abort releases its byte reservation without affecting the active write', async t => {
  const large = { type: 'inspect', text: 'x'.repeat(4 * 1024 * 1024) };
  const small = { type: 'inspect', text: 'queued' };
  const transport = await peer(t, 'delayed-read', { maxPendingBytes: requestBytes(large) + requestBytes(small) });
  const first = transport.call(large);
  const controller = new AbortController();
  let canceledSent = false;
  const canceled = transport.call(small, { signal: controller.signal }, () => { canceledSent = true; });
  await assert.rejects(transport.call(small), { code: 'pending_bytes_exceeded', outcomeUnknown: false });
  controller.abort();
  await assert.rejects(canceled, { code: 'aborted', outcomeUnknown: false });
  assert.equal(canceledSent, false);
  const accepted = transport.call(small);
  assert.equal((await first).success, true);
  assert.equal((await accepted).success, true);
  assert.equal(transport.closed, false);
  assert.equal((await transport.call(small)).success, true);
});

test('per-call wait abort/timeout does not send remote abort, unknown outcome tracked', async t => {
  const transport = await peer(t);
  const abort = new AbortController();
  const waiting = transport.call({ type: 'never' }, { signal: abort.signal });
  await delay(20);
  abort.abort();
  await assert.rejects(waiting, { code: 'aborted', outcomeUnknown: true });
  await assert.rejects(transport.call({ type: 'never' }, { timeoutMs: 25 }), { code: 'response_timeout', outcomeUnknown: true });
  const pre = new AbortController(); pre.abort();
  await assert.rejects(transport.call({ type: 'inspect' }, { signal: pre.signal }), { code: 'aborted', outcomeUnknown: false });
  assert.equal((await transport.call({ type: 'inspect' })).success, true);
});

test('stalled partial write timeout poisons transport and queued calls remain unsent', async t => {
  const transport = await peer(t, 'block-input', { writeTimeoutMs: 50 });
  const first = transport.call({ type: 'inspect', text: 'x'.repeat(4 * 1024 * 1024) });
  const second = transport.call({ type: 'inspect' });
  await assert.rejects(first, { code: 'write_timeout', outcomeUnknown: true });
  await assert.rejects(second, { code: 'write_timeout', outcomeUnknown: false });
  await transport.close();
  assert(transport.closed);
});

test('queued abort remains unsent and does not interrupt another request write', async t => {
  const transport = await peer(t, 'block-input', { writeTimeoutMs: 120 });
  const first = transport.call({ type: 'inspect', text: 'x'.repeat(4 * 1024 * 1024) });
  const firstRejected = assert.rejects(first, { code: 'write_timeout' });
  const abort = new AbortController();
  const second = transport.call({ type: 'inspect' }, { signal: abort.signal });
  abort.abort();
  await assert.rejects(second, { code: 'aborted', outcomeUnknown: false });
  await firstRejected;
});

test('run cancellation guard is evaluated after the write gate, not while queued', async t => {
  const transport = await peer(t, 'delayed-read');
  const first = transport.call({ type: 'inspect', text: 'x'.repeat(4 * 1024 * 1024) });
  let active = true;
  let guarded = false;
  let sent = false;
  const next = transport.call({ type: 'inspect' }, { guard: () => { guarded = true; return active; } }, () => { sent = true; });
  assert.equal(guarded, false);
  active = false;
  const rejected = assert.rejects(next, { code: 'run_finished', outcomeUnknown: false });
  await first;
  await rejected;
  assert.equal(guarded, true);
  assert.equal(sent, false);
  assert.equal((await transport.call({ type: 'inspect' })).success, true);
});

test('explicit close is idempotent, drains final completion and ends events normally', async t => {
  const transport = await peer(t, 'final');
  const response = await transport.call({ type: 'inspect' });
  const events = transport.events()[Symbol.asyncIterator]();
  const close1 = transport.close();
  assert.equal(close1, transport.close());
  await close1;
  assert.equal((await events.next()).value.request_id, response.id);
  assert.equal((await events.next()).done, true);
  await assert.rejects(transport.call({ type: 'inspect' }), { code: 'closed' });
});

test('close drains before rejecting unfinished calls; descendant handles cannot hang', async t => {
  const transport = await peer(t, 'descendant');
  const waiting = transport.call({ type: 'never' });
  const rejected = assert.rejects(waiting, { code: 'closed', outcomeUnknown: true });
  const events = transport.events()[Symbol.asyncIterator]();
  let notified = false;
  transport.onNotification(frame => { if (frame.type === 'prompt_completed') notified = true; });
  const started = Date.now();
  await transport.close();
  assert(Date.now() - started < 500);
  assert(notified);
  assert.equal((await events.next()).value.type, 'prompt_completed');
  assert.equal((await events.next()).done, true);
  await rejected;
});

test('child exit drains final frames before terminal and partial EOF rejected', async t => {
  const transport = await peer(t);
  const events = transport.events()[Symbol.asyncIterator]();
  const end = terminal(transport);
  assert.equal((await transport.call({ type: 'exit-final' })).success, true);
  assert.equal((await events.next()).value.type, 'prompt_completed');
  assert.equal((await end).code, 'process_exit');
  await assert.rejects(events.next(), { code: 'process_exit' });
  const partial = await peer(t);
  await assert.rejects(partial.call({ type: 'partial-exit' }), { code: 'malformed_frame' });
});

test('lifetime abort kills/reaps; ignored EOF forced close is bounded; stderr floods discarded', async t => {
  const controller = new AbortController();
  const transport = await peer(t, 'echo', { signal: controller.signal });
  const pid = (await transport.call({ type: 'inspect' })).data.pid;
  controller.abort();
  await transport.close();
  assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' });
  assert.equal(transport.terminalError.code, 'aborted');
  const ignored = await peer(t, 'ignore-eof');
  const start = Date.now(); await ignored.close();
  assert(Date.now() - start < 500);
  const flood = await peer(t, 'stderr-flood');
  assert.equal((await flood.call({ type: 'inspect' })).success, true);
});

test('SDK and remote errors expose stable codes without implicit raw diagnostics', () => {
  const error = new SdkError('closed', 'Safe', { outcomeUnknown: true });
  assert.equal(error.code, 'closed'); assert.equal(error.outcomeUnknown, true);
  const response = { type: 'response', id: '1', command: 'prompt', success: false, error: 'PRIVATE', error_code: 'conflict', data: { a: 1 } };
  const remote = new RemoteRpcError(response);
  assert.equal(remote.error, 'PRIVATE'); assert.equal(remote.errorCode, 'conflict');
  assert.equal(remote.id, '1'); assert.equal(remote.command, 'prompt'); assert.equal(remote.success, false);
  assert(!remote.message.includes('PRIVATE'));
  assert(Object.isFrozen(remote.response));
});
