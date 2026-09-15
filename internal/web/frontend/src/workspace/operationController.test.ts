import test from 'node:test';
import assert from 'node:assert/strict';
import { OperationController } from './operationController.ts';
const id = '11111111-1111-4111-8111-111111111111';
const operation = {
  id,
  kind: 'create',
  name: 'new',
  state: 'awaiting_registration',
  revision: 2,
  parent: { path: '/host', device: '1', inode: '2' },
  child: { path: '/host/new', device: '1', inode: '3' },
  outcome: 'observed',
  created_at: 100,
  updated_at: 101,
};
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' },
  });
test('explicit grant, consent and exact form bodies precede one mutation; no automatic registration', async () => {
  const old = globalThis.fetch,
    calls: { url: string; body: URLSearchParams }[] = [];
  globalThis.fetch = async (input, options) => {
    const url = String(input);
    calls.push({ url, body: new URLSearchParams(String(options?.body || '')) });
    return json(
      url === '/projects/folders/select'
        ? { operation_id: id, path: '/host', expires_at: Date.now() + 10000 }
        : operation,
    );
  };
  const c = new OperationController('explicit-test', true);
  try {
    assert.equal(calls.length, 0);
    c.edit('parent', '/host');
    c.edit('name', 'new');
    await c.select();
    assert.deepEqual(Object.fromEntries(calls[0].body), {
      csrf: 'explicit-test',
      path: '/host',
    });
    c.reviewCreation();
    await c.confirm();
    assert.equal(calls.length, 1, 'unchecked review cannot mutate');
    c.checkConsent(true);
    await Promise.all([c.confirm(), c.confirm()]);
    assert.equal(calls.length, 2);
    assert.equal(calls[1].url, '/projects/create');
    assert.deepEqual(Object.fromEntries(calls[1].body), {
      csrf: 'explicit-test',
      operation_id: id,
      name: 'new',
    });
    assert.equal(c.getSnapshot().grant, null);
    assert.equal(c.getSnapshot().uncertain, '');
    assert.equal(c.getSnapshot().operations[0].state, 'awaiting_registration');
    assert.equal(
      calls.length,
      2,
      'no automatic refresh, register, clone, activate or send',
    );
  } finally {
    c.dispose();
    globalThis.fetch = old;
  }
});
test('retired mutation receipt cannot publish into replacement owner or remove uncertainty', async () => {
  const old = globalThis.fetch;
  let resolve!: (response: Response) => void;
  let calls = 0;
  globalThis.fetch = async (input) => {
    calls++;
    return String(input) === '/projects/folders/select'
      ? json({
          operation_id: id,
          path: '/host',
          expires_at: Date.now() + 10000,
        })
      : new Promise<Response>((r) => {
          resolve = r;
        });
  };
  const c = new OperationController('detached-test', true);
  let replacement: OperationController | undefined;
  try {
    c.edit('parent', '/host');
    c.edit('name', 'new');
    await c.select();
    c.reviewCreation();
    c.checkConsent(true);
    const pending = c.confirm();
    c.dispose();
    replacement = new OperationController('detached-test', true);
    assert.equal(replacement.getSnapshot().pending, true);
    resolve(json(operation));
    await pending;
    assert.equal(replacement.getSnapshot().pending, false);
    assert.equal(replacement.getSnapshot().uncertain, id);
    assert.equal(replacement.getSnapshot().operations.length, 0);
    await replacement.select();
    assert.equal(calls, 2, 'unknown mutation cannot acquire a new grant');
  } finally {
    c.dispose();
    replacement?.dispose();
    globalThis.fetch = old;
  }
});
test('wrong destination receipt remains unknown and no failed command is replayed', async () => {
  const old = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async (input) => {
    calls++;
    return json(
      String(input) === '/projects/folders/select'
        ? { operation_id: id, path: '/host', expires_at: Date.now() + 10000 }
        : {
            ...operation,
            name: 'other',
            child: { ...operation.child, path: '/host/other' },
          },
    );
  };
  const c = new OperationController('wrong-receipt', true);
  try {
    c.edit('parent', '/host');
    c.edit('name', 'new');
    await c.select();
    c.reviewCreation();
    c.checkConsent(true);
    await c.confirm();
    assert.equal(c.getSnapshot().uncertain, id);
    assert.equal(c.getSnapshot().operations.length, 0);
    await c.confirm();
    assert.equal(calls, 2);
  } finally {
    c.dispose();
    globalThis.fetch = old;
  }
});
test('closing a panel retires grants and remote drafts without issuing cancellation', async () => {
  const old = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    return json({
      operation_id: id,
      path: '/host',
      expires_at: Date.now() + 10000,
    });
  };
  const c = new OperationController('retire-test', true);
  try {
    c.edit('parent', '/host');
    c.edit('name', 'new');
    c.edit('kind', 'clone');
    c.edit('remote', 'https://example.com/team/repo');
    await c.select();
    c.reviewCreation();
    c.retire();
    assert.equal(c.getSnapshot().grant, null);
    assert.equal(c.getSnapshot().review, null);
    assert.equal(c.getSnapshot().draft.remote, '');
    assert.equal(calls, 1);
  } finally {
    c.dispose();
    globalThis.fetch = old;
  }
});

test('folder browser retains existing CSRF POST transport and rejects relative paths', async () => {
  const old = globalThis.fetch;
  const calls: {
    url: string;
    method: string | undefined;
    body: URLSearchParams;
  }[] = [];
  globalThis.fetch = async (input, options) => {
    calls.push({
      url: String(input),
      method: options?.method,
      body: new URLSearchParams(String(options?.body || '')),
    });
    return json({
      path: '/host',
      parent: '/',
      folders: [],
      next_offset: 0,
      has_more: false,
    });
  };
  const c = new OperationController('folder-transport', true);
  try {
    await c.browse('relative');
    assert.equal(calls.length, 0);
    await c.browse('/host', 32);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/projects/folders');
    assert.equal(calls[0].method, 'POST');
    assert.deepEqual(Object.fromEntries(calls[0].body), {
      csrf: 'folder-transport',
      path: '/host',
      offset: '32',
    });
    assert.equal(c.getSnapshot().folder?.path, '/host');
    assert.equal(
      c.getSnapshot().grant,
      null,
      'browsing never selects authority',
    );
  } finally {
    c.dispose();
    globalThis.fetch = old;
  }
});
