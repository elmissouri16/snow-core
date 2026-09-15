import test from 'node:test';
import assert from 'node:assert/strict';
import {canDelete, sessionRows, validateInventory, validateShellBootstrap, boundedText} from './model.ts';
const project = '12345678-1234-1234-1234-123456789012';
const bootstrap = () => ({csrf: 'csrf', version: 'test', view: 'projects', project, session: 's1',
  hostSettingsEnabled: true, apiKeyEnabled: false, tls: false, pairingCode: '',
  projects: [{id: project, name: 'Example', path: '/workspace', available: true, trustRemembered: true, skillsEnabled: false, pinned: true}],
  sessions: [{session_id: 's1', name: 'One'}], live: null});
const inventory = () => ({project_id: project, instance_id: '', sessions: [{session_id: 's1', name: 'One'}], available: true, delete_supported: true, active_session_id: '', next_offset: 1, has_more: true});
test('shell bootstrap projects public metadata only; validates bounds and identities', () => {
  const result = validateShellBootstrap({...bootstrap(), privateState: 'not projected'});
  assert.equal(result.projects[0].trustRemembered, true);
  assert.equal('privateState' in result, false);
  assert.throws(() => validateShellBootstrap({...bootstrap(), csrf: 'x'.repeat(513)}));
  assert.throws(() => validateShellBootstrap({...bootstrap(), projects: [...bootstrap().projects, ...bootstrap().projects]}));
  assert.throws(() => validateShellBootstrap({...bootstrap(), projects: [{...bootstrap().projects[0], name: 'é'.repeat(100)}]}));
  assert.throws(() => validateShellBootstrap({...bootstrap(), project: 'not registered'}));
  assert.throws(() => validateShellBootstrap({...bootstrap(), live: {project, session: 's1', instance: '', title: 'One', renameAvailable: true, renameDisabled: false, newDisabled: false}}));
});
test('latest inventory withdraws deletion rather than merging the prior capability', () => {
  const first = validateInventory(inventory(), project, 0, '', null);
  const revoked = validateInventory({...inventory(), delete_supported: false}, project, 0, '', null);
  assert.equal(first.deleteSupported, true); assert.equal(revoked.deleteSupported, false);
  assert.equal(validateInventory({...inventory(), active_session_id: undefined}, project, 0, '', null).deleteSupported, false);
  assert.equal(canDelete({supported: false, available: true, active: false, instance: ''}), false);
  assert.equal(canDelete({supported: true, available: true, active: true, instance: 'owner'}), false);
  assert.equal(canDelete({supported: true, available: false, active: false, instance: ''}), false);
  assert.equal(canDelete({supported: true, available: true, active: false, instance: ''}), true);
});
test('inventory rejects stale page/live ownership and malformed row identities', () => {
  assert.throws(() => validateInventory(inventory(), project, 1, 'owner', null));
  assert.throws(() => validateInventory(inventory(), project, 0, '', {project, session: 'active', instance: 'owner', title: '', renameAvailable: false, renameDisabled: true, newDisabled: true}));
  assert.throws(() => validateInventory({...inventory(), project_id: 'other'}, project, 0, '', null));
  assert.throws(() => sessionRows([{session_id: 's', name: ''}, {session_id: 's', name: ''}]));
  assert.throws(() => sessionRows(Array.from({length: 101}, (_, i) => ({session_id: String(i), name: ''}))));
});
test('inventory body read remains bounded', async () => {
  assert.equal(await boundedText(new Response('hello'), 5), 'hello');
  await assert.rejects(boundedText(new Response('hello!'), 5));
});
