import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, mkdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { SnowClient } from '../../../build/sdk-node/test/client.js';
const executable = process.env.SNOW_BIN ? resolve(process.env.SNOW_BIN) : undefined;

async function start(t, options = {}) {
  const root = await mkdtemp(join(tmpdir(), 'snow-node-sdk-'));
  const workers = [];
  t.after(async () => {
    try {
      for (const { client, drain } of workers) {
        await client.close();
        await drain.catch(() => {});
      }
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  const own = client => {
    const events = [];
    const drain = (async () => { for await (const event of client.events()) events.push(event); })();
    void drain.catch(() => {});
    const worker = { client, events, drain };
    workers.push(worker);
    return worker;
  };
  const cwd = join(root, 'project');
  const home = join(root, 'home');
  await mkdir(cwd); await mkdir(home);
  const env = { PATH: process.env.PATH, HOME: home, XDG_CONFIG_HOME: join(home, '.config'), XDG_DATA_HOME: join(home, '.local/share'), XDG_CACHE_HOME: join(home, '.cache') };
  const worker = own(await SnowClient.start({ executable, cwd, env, provider: 'fake', permission: 'deny', ...options }));
  return { ...worker, cwd, env, own };
}

test('real Snow: handshake, normalized streaming lifecycle, sessions and model selection', { skip: !executable }, async t => {
  const { client, events } = await start(t);
  assert.equal(client.ready.protocol_version, '1');
  assert.ok(client.ready.capabilities.includes('prompt_completion'));
  assert.equal((await client.models.list()).current, 'fake-1');
  await client.models.set({ provider: 'fake', model: 'fake-1', thinking: 'off' });
  await client.modes.set('plan');
  const first = await client.sessions.info();
  const run = await client.prompt('Credential-free Node SDK lifecycle check');
  assert.equal((await run.result).status, 'completed');
  const history = await client.sessions.messages({ limit: 25 });
  assert.ok(history.messages.some(message => message.role === 'user'));
  const created = await client.sessions.create();
  assert.notEqual(created.session_id, first.session_id);
  await client.sessions.rename('SDK integration');
  await client.sessions.open(first.session_id);
  assert.equal((await client.sessions.info()).session_id, first.session_id);
  const listed = await client.sessions.list();
  assert.ok(listed.sessions.some(session => session.session_id === created.session_id));
  await client.close();
  assert.ok(events.some(event => event.type === 'turn_done'));
  assert.ok(events.some(event => event.type === 'prompt_completed' && event.request_id === run.requestId));
});

test('real Snow: no-session remains ephemeral; independent resume retains original history', { skip: !executable }, async t => {
  const { client, cwd, env, own } = await start(t, { noSession: true });
  assert.deepEqual((await client.sessions.list()).sessions, []);
  assert.equal((await (await client.prompt('Ephemeral prompt')).result).status, 'completed');
  assert.deepEqual((await client.sessions.list()).sessions, []);
  await client.close();
  const { client: saved, drain: drainSaved } = own(await SnowClient.start({ executable, cwd, env, provider: 'fake' }));
  const info = await saved.sessions.info();
  assert.equal((await (await saved.prompt('Durable marker')).result).status, 'completed');
  await saved.close(); await drainSaved;
  const { client: resumed } = own(await SnowClient.start({ executable, cwd, env, provider: 'fake' }));
  await resumed.sessions.open(info.session_id);
  const history = await resumed.sessions.messages({ limit: 25 });
  assert.ok(history.messages.some(message => message.role === 'user' && message.content.some(block => block.text === 'Durable marker')));
});
