import assert from 'node:assert/strict';
import test from 'node:test';
import { validateOrganizationProps, catalogPageURL, archivedPageURL } from './model.ts';

const id = '00112233-4455-6677-8899-aabbccddeeff';
const project = { id, name: 'Workspace', path: '/tmp/workspace', available: true, state: 'available', issue: '', pinned: false };
const session = { id: 'saved_A-1', name: '', updated: 'Today', pinned: false, archived: false };
function props() {
  return { csrf: 'a'.repeat(64), error: '', organization: {
    projects: [{ ...project }], archived: [], project: { ...project }, sessions: [{ ...session }],
    offset: 0, nextURL: `/?offset=25&project=${id}&view=organization`,
    archivedNextURL: '/?archived_offset=25&view=organization', live: false,
  } };
}

test('accepts backend presentation values without imposing live-sidebar limits', () => {
  const value = props();
  value.organization.projects[0].path = '/tmp/with\nnewline\\backslash';
  value.organization.sessions[0].name = '🙂'.repeat(4096);
  value.organization.sessions[0].updated = '292278994-08-17T07:12:55Z';
  assert.equal(validateOrganizationProps(value), value);
  for (const state of [null, { ...value.organization, project: null, sessions: [], nextURL: '' },
    { ...value.organization, live: true, sessions: [], nextURL: '' }]) {
    assert.doesNotThrow(() => validateOrganizationProps({ ...value, organization: state }));
  }
});

test('rejects invalid types, identities, duplicates, offsets and over-cap collections', () => {
  const changes = [
    p => { p.csrf = 'x'; },
    p => { p.error = 'x'.repeat(4097); },
    p => { p.organization.projects[0].id = '../other'; },
    p => { p.organization.projects[0].name = '🙂'.repeat(33); },
    p => { p.organization.projects[0].path = '/x'.repeat(2049); },
    p => { p.organization.projects.push({ ...project }); },
    p => { p.organization.archived.push({ ...project }); },
    p => { p.organization.sessions[0].id = 'saved&project=other'; },
    p => { p.organization.sessions[0].archived = 'true'; },
    p => { p.organization.sessions[0].name = 'x'.repeat(4097); },
    p => { p.organization.sessions.push({ ...session }); },
    p => { p.organization.sessions = Array.from({length: 26}, (_, i) => ({ ...session, id: `saved-${i}` })); },
    p => { p.organization.offset = '0'; },
    p => { p.organization.offset = -0; },
    p => { p.organization.offset = 10_001; },
    p => { p.organization.offset = 0.5; },
    p => { p.organization.live = true; },
    p => { p.organization.nextURL = '/?offset=025&project=' + id + '&view=organization'; },
  ];
  for (const change of changes) {
    const value = props();
    change(value);
    assert.throws(() => validateOrganizationProps(value), /Invalid organization page/);
  }
});

test('pagination only admits canonical local Go organization routes', () => {
  const catalog = `/?offset=10000&project=${id}&view=organization`;
  assert.equal(catalogPageURL(catalog, id, 25), catalog);
  assert.equal(archivedPageURL('/?archived_offset=10000&view=organization'), '/?archived_offset=10000&view=organization');
  for (const bad of ['https://evil.test/', '//evil.test/', 'javascript:alert(1)',
    '/?offset=25&project=' + id + '&view=organization&extra=x',
    '/?offset=25&project=' + id + '&view=organization#fragment',
    '/?offset=10001&project=' + id + '&view=organization',
    '/?offset=0&project=' + id + '&view=organization']) {
    assert.equal(catalogPageURL(bad, id, 0), null);
    const value = props();
    value.organization.nextURL = bad;
    assert.throws(() => validateOrganizationProps(value));
  }
  assert.equal(catalogPageURL(catalog, 'another', 0), null);
  for (const bad of ['/?archived_offset=025&view=organization', '/?archived_offset=10001&view=organization', '/?archived_offset=25&view=projects']) {
    assert.equal(archivedPageURL(bad), null);
  }
});
