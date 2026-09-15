import test from 'node:test';
import assert from 'node:assert/strict';
import {bootstrap, liveProjection, diffLines, mergeFiles, validChanges, validDiff, validFile, validFiles} from './model.ts';
import type {Entry} from './model.ts';
test('inspection bootstrap requires existing public project fields and bounded CSRF', () => {
  const props = {project: {id: 'p', name: 'Project', path: '/host/project', available: true}, csrf: 'csrf'};
  assert.deepEqual(bootstrap(props), props);
  assert.equal(bootstrap({...props, csrf: 'x'.repeat(513)}), null);
  assert.equal(bootstrap({...props, project: {...props.project, available: 'true'}}), null);
  assert.equal(bootstrap({...props, live: {session_id: 's', provider: 'p'}}), null);
});
test('file DTO validates bounded page offsets, kinds, text and server truncation', () => {
  const listing = {path: '.', entries: [{name: 'file', path: 'file', kind: 'file'}], next_offset: 1, has_more: false, limited: false};
  assert.equal(validFiles(listing), true);
  assert.equal(validFiles({...listing, next_offset: -1}), false);
  assert.equal(validFiles({...listing, entries: [{...listing.entries[0], kind: 'symlink'}]}), false);
  assert.equal(validFiles({...listing, entries: Array(257).fill(listing.entries[0])}), false);
  const preview = {path: 'file', text: '<script>not markup</script>', size: 100, truncated: false};
  assert.equal(validFile(preview), true);
  assert.equal(validFile({...preview, text: 'x'.repeat(65537)}), false);
  assert.equal(validFile({...preview, truncated: 'false'}), false);
});
test('changes and unavailable diffs use actual server projections, not private snapshots', () => {
  const change = {path: 'file', kind: 'staged', status: 'M'};
  assert.equal(validChanges({available: true, reason: '', limited: false, changes: [change]}), true);
  assert.equal(validChanges({available: true, reason: '', limited: false, changes: [{...change, kind: 'commit'}]}), false);
  assert.equal(validDiff({available: false, reason: 'Unavailable', path: 'file', kind: 'staged', text: '', truncated: false}), true);
  assert.equal(validDiff({available: true, path: 'file', kind: 'staged', text: '<html/>', truncated: false}), false);
});
test('file pagination preserves authoritative offsets while sorting folders and numeric paths', () => {
  const entry = (path: string, kind: Entry['kind'] = 'file'): Entry => ({path, name: path, kind});
  const entries = mergeFiles([entry('file10')], [entry('file2'), entry('folder', 'directory'), entry('file10')], true);
  assert.deepEqual(entries.map(e => e.path), ['folder', 'file2', 'file10']);
  assert.equal(mergeFiles(Array.from({length: 4096}, (_, i) => entry(String(i))), [entry('overflow')], true).length, 4096);
  assert.deepEqual(mergeFiles(entries, [entry('fresh')], false).map(e => e.path), ['fresh']);
});
test('diff colouring preserves every character and final newline under bounded line rendering', () => {
  const text = 'diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n context\n';
  const lines = diffLines(text);
  assert.equal(lines.map(l => l.text).join(''), text);
  assert.deepEqual(lines.map(l => l.kind), ['meta', 'meta', 'meta', 'hunk', 'removed', 'added', 'context']);
  const large = '+a\n'.repeat(5000); const bounded = diffLines(large);
  assert.equal(bounded.length, 4097); assert.equal(bounded.at(-1)?.kind, 'context');
  assert.equal(bounded.map(l => l.text).join(''), large);
  assert.equal(diffLines('x'.repeat(70000)).map(l => l.text).join('').length, 65536);
});

test('live presentation rejects stale owner/project identities and copies no private fields', () => {
  const owner = {project_id: 'project', instance_id: 'instance-new', session_id: 'session-new'};
  const projection = {...owner, provider: 'provider', model: 'model', private_state: 'never projected'};
  assert.deepEqual(liveProjection(projection, 'project', owner), {session_id: 'session-new', provider: 'provider', model: 'model'});
  assert.equal(liveProjection(projection, 'another-registration', owner), null);
  for (const identity of ['project_id', 'instance_id', 'session_id'] as const) {
    assert.equal(liveProjection({...projection, [identity]: 'stale'}, 'project', owner), null);
    assert.equal(liveProjection(projection, 'project', {...owner, [identity]: 'replaced'}), null);
    assert.equal(liveProjection({...projection, [identity]: ''}, 'project', {...owner, [identity]: ''}), null);
  }
  for (const [field, limit] of [['project_id', 128], ['instance_id', 256], ['session_id', 512], ['provider', 256], ['model', 512]] as const) {
    const oversized = 'x'.repeat(limit + 1);
    assert.equal(liveProjection({...projection, [field]: oversized}, field === 'project_id' ? oversized : 'project', {...owner, [field]: oversized}), null);
  }
  assert.equal(liveProjection({...projection, provider: undefined}, 'project', owner), null);
  assert.equal(liveProjection({...projection, model: {name: 'model'}}, 'project', owner), null);
});
