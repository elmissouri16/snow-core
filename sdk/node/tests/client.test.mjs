import assert from 'node:assert/strict';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { SnowClient } from '../../../build/sdk-node/test/client.js';
import { RemoteRpcError } from '../../../build/sdk-node/test/errors.js';
const executable = fileURLToPath(new URL('./client-fixture.mjs', import.meta.url));
async function fixture(t, options = {}) {
  const client = await SnowClient.start({ executable, ...options });
  const events = [];
  const drain = (async () => { for await (const event of client.events()) events.push(event); })();
  void drain.catch(() => {});
  t.after(async () => { await client.close(); await drain.catch(() => {}); });
  return { client, events, drain };
}

test('deny is explicit, options use argument vectors, and readiness copies are isolated', async t => {
  const { client } = await fixture(t, { provider: 'fake', noSession: true, tools: ['read', 'grep'] });
  const response = await client.rpc({ type: 'usage' });
  assert.deepEqual(response.data.args, ['--mode', 'rpc', '--permission', 'deny', '--provider=fake', '--no-session', '--tools=read,grep']);
  const copy = client.ready;
  assert.throws(() => { copy.capabilities.length = 0; }, TypeError);
  assert.ok(client.ready.capabilities.includes('prompt_completion'));
  assert.equal((await client.models.list()).current, 'fake-1');
});

test('invalid startup options reject before process launch', async () => {
  for (const options of [{ permission: 'bad' }, { session: 'x', noSession: true }, { tools: [] }, { tools: ['bash,write'] }, { noSession: 'yes' }]) {
    await assert.rejects(SnowClient.start({ executable: '/missing/snow', ...options }), { code: 'invalid_options' });
  }
});

test('streaming prompt resolves definitive completion and preserves future events', async t => {
  const { client, events } = await fixture(t);
  const run = await client.prompt('hello', { mode: 'plan' });
  assert.equal((await run.result).status, 'completed');
  await client.close();
  assert.ok(events.some(event => event.type === 'future_event' && event.extra === 'preserved'));
  assert.ok(events.some(event => event.type === 'text_delta' && event.text === 'hello'));
  assert.ok(events.some(event => event.type === 'prompt_completed' && event.request_id === run.requestId));
});

test('completion before admission and legacy duplicate response settle exactly once', async t => {
  const { client } = await fixture(t);
  assert.equal((await (await client.prompt('completion first')).result).status, 'completed');
  const failure = await client.prompt('fail');
  assert.equal((await failure.result).status, 'failed');
  assert.equal((await (await client.prompt('after failure')).result).status, 'completed');
});

test('active root is not queued and explicit cancellation waits for terminal evidence', async t => {
  const { client } = await fixture(t);
  const run = await client.prompt('slow');
  await assert.rejects(client.prompt('second'), { code: 'prompt_active' });
  await run.cancel();
  assert.equal((await run.result).status, 'canceled');
  const next = await client.prompt('slow');
  await run.cancel();
  assert.equal((await client.rpc({ type: 'usage' })).data.aborts, 1);
  await next.cancel();
  assert.equal((await next.result).status, 'canceled');
});

test('failed admission frees the slot; remote error does not terminate transport', async t => {
  const { client } = await fixture(t);
  await assert.rejects(client.prompt('reject'), RemoteRpcError);
  await assert.rejects(client.sessions.open('missing'), error => error instanceof RemoteRpcError && error.errorCode === 'invalid');
  const rejected = await client.rpc({ type: 'session_open', params: { session_id: 'missing' } });
  assert.equal(rejected.success, false);
  assert.equal((await (await client.prompt('works')).result).status, 'completed');
});

test('lost admission is not retried and holds the root slot until completion', async t => {
  const { client } = await fixture(t, { responseTimeoutMs: 15 });
  await assert.rejects(client.prompt('lost admission'), error => error.outcomeUnknown === true);
  await assert.rejects(client.prompt('unsafe retry'), { code: 'prompt_active' });
  await new Promise(resolve => setTimeout(resolve, 100));
  assert.equal((await (await client.prompt('after completion')).result).status, 'completed');
});

test('worker loss after admission rejects completion with unknown remote outcome', async t => {
  const { client, drain } = await fixture(t);
  const run = await client.prompt('worker exits');
  await assert.rejects(run.result, error => error.code === 'process_exit' && error.outcomeUnknown === true);
  await assert.rejects(drain, { code: 'process_exit' });
  await client.close();
});

test('missing capabilities fail before sending convenience commands', async t => {
  const { client } = await fixture(t, { env: { ...process.env, SDK_NO_CAPS: '1' } });
  await assert.rejects(client.prompt('hello'), { code: 'unsupported_capability' });
  await assert.rejects(client.models.list(), { code: 'unsupported_capability' });
});

test('permission and question replies are explicit and preserve their distinct wire types', async t => {
  const { client } = await fixture(t, { permission: 'ask' });
  assert.deepEqual(await client.permissions.reply('perm-1', 'deny'), { request_id: 'perm-1', decision: 'deny' });
  assert.deepEqual(await client.questions.reply('question-1', [{ id: 'format', answer: 'JSON' }]), { request_id: 'question-1', answers: [{ id: 'format', answer: 'JSON' }] });
  assert.deepEqual(await client.questions.reject('question-2'), { request_id: 'question-2' });
});
