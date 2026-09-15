import test from 'node:test';
import assert from 'node:assert/strict';
import {
  validateColdProps,
  validateHomeProps,
  validateLoginProps,
} from './model.ts';
import {
  absolutePath,
  anonymousRemote,
  destination,
  leafName,
  validateGrant,
  validateInventory,
  validateOperation,
  validateReceipt,
} from './operationModel.ts';
const id = '11111111-1111-4111-8111-111111111111';
const project = {
  id,
  name: 'Project',
  path: '/host/project',
  available: true,
  trusted: false,
  skillsEnabled: false,
};
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
test('public bootstrap selects fields and rejects malformed project ownership', () => {
  assert.deepEqual(
    validateHomeProps({
      projects: [project],
      error: '',
      privateRuntime: { secret: 'not projected' },
    }),
    { projects: [project], error: '' },
  );
  assert.throws(() =>
    validateHomeProps({ projects: [project, project], error: '' }),
  );
  assert.throws(() =>
    validateHomeProps({
      projects: [{ ...project, available: 'true' }],
      error: '',
    }),
  );
  const cold = {
    project,
    csrf: 'csrf',
    error: '',
    sessionID: '',
    sessionTitle: '',
    runtimeEnabled: true,
    hasHistory: true,
    nextURL: `/?view=projects&project=${id}&offset=2`,
    recoveryMessage: '',
    recoveryURL: '',
  };
  assert.equal(validateColdProps(cold).nextURL, cold.nextURL);
  const sortedNext = `/?offset=30&project=${id}&session=1789427125316-f22sxorm&view=projects`;
  const sortedRecovery = `/?project=${id}&session=saved_session&view=projects`;
  assert.equal(validateColdProps({...cold, nextURL: sortedNext}).nextURL, sortedNext);
  assert.equal(validateColdProps({...cold, recoveryURL: sortedRecovery}).recoveryURL, sortedRecovery);
  for (const sessionID of ['1789427125316-f22sxorm', 'fixture-saved-markdown', 'saved_session', id])
    assert.equal(validateColdProps({...cold, sessionID}).sessionID, sessionID);
  for (const sessionID of ['../saved', 'saved/session', 'saved session', 'saved\n', 'x'.repeat(129)])
    assert.throws(() => validateColdProps({...cold, sessionID}));
  for (const nextURL of [
    'https://other.invalid/',
    '//other.invalid/',
    `/?view=projects&project=wrong`,
    `/?view=projects&project=${id}&project=${id}`,
    `/?view=projects&project=${id}&session=../other`,
    `/?view=projects&project=${id}&offset=10001`,
    `/?view=projects&project=${id}&next=https://other.invalid`,
    `/?view=projects&project=${id}#unexpected`,
    `/other?view=projects&project=${id}`,
    `/?view=projects&project=${id}\\evil`,
  ])
    assert.throws(() => validateColdProps({ ...cold, nextURL }));
  assert.deepEqual(
    validateLoginProps({ csrf: 'csrf', error: '', code: 'never project this' }),
    { csrf: 'csrf', error: '' },
  );
});
test('destination leaves and remotes retain original safety boundaries', () => {
  for (const value of [
    '..',
    '.',
    'a/b',
    'a\\b',
    ' leading',
    'trailing ',
    'a\n',
    'é'.repeat(65),
  ])
    assert.equal(leafName(value), false, value);
  assert.equal(leafName('valid-name'), true);
  assert.equal(absolutePath('/host/project'), true);
  assert.equal(absolutePath('relative/path'), false);
  assert.equal(absolutePath('/bad\u0000path'), false);
  assert.equal(destination('/', 'new'), '/new');
  assert.equal(
    anonymousRemote('https://example.com/team/repository.git'),
    true,
  );
  for (const value of [
    'https://user:pass@example.com/team/repo',
    'https://example.com/team/repo?token=x',
    'https://example.com/team/repo#fragment',
    'ssh://example.com/team/repo',
    'https://example.com/team/../repo',
  ])
    assert.equal(anonymousRemote(value), false);
});
test('operation receipts require exact owner, monotonic revision and child identity', () => {
  assert.equal(validateReceipt(operation, id, 1).revision, 2);
  assert.throws(() =>
    validateReceipt(operation, '22222222-2222-4222-8222-222222222222'),
  );
  assert.throws(() => validateReceipt(operation, id, 2));
  assert.throws(() =>
    validateOperation({
      ...operation,
      child: { ...operation.child, path: '/host/other' },
    }),
  );
  assert.throws(() => validateOperation({ ...operation, outcome: 'unknown' }));
  assert.throws(() => validateOperation({ ...operation, state: 'succeeded' }));
  assert.throws(() =>
    validateInventory(
      { operations: [operation, operation], next_offset: 2, has_more: false },
      0,
    ),
  );
  assert.throws(() =>
    validateInventory(
      { operations: [operation], next_offset: 2, has_more: false },
      0,
    ),
  );
});
test('one-use grants have bounded expiry and public path', () => {
  assert.equal(
    validateGrant({ operation_id: id, path: '/host', expires_at: 1100 }, 1000)
      .operation_id,
    id,
  );
  assert.throws(() =>
    validateGrant({ operation_id: id, path: '/host', expires_at: 1000 }, 1000),
  );
  assert.throws(() =>
    validateGrant(
      { operation_id: id, path: '/host', expires_at: 302001 },
      1000,
    ),
  );
  assert.throws(() =>
    validateGrant(
      { operation_id: id, path: 'relative', expires_at: 1100 },
      1000,
    ),
  );
});

test('opening reservation clamps non-finite and oversized heights to the viewport', async () => {
  const { openingHeight } = await import('./model.ts');
  assert.equal(openingHeight(320.9, 768), 320);
  assert.equal(openingHeight(12000, 768), 768);
  assert.equal(openingHeight(-1, 768), 0);
  assert.equal(openingHeight(320, -1), 0);
  assert.equal(openingHeight(Number.NaN, 768), 0);
  assert.equal(openingHeight(Number.POSITIVE_INFINITY, 768), 0);
  assert.equal(openingHeight(320, Number.POSITIVE_INFINITY), 0);
});
