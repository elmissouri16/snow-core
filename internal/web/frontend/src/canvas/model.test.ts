import assert from 'node:assert/strict';
import {test} from 'node:test';
import {relativePath, touchedFiles} from './model.ts';

test('touched files come only from completed write/edit result text inside the project', () => {
  const root = '/srv/project';
  const activities = [
    {id: 'a', tool: 'write', status: 'completed', output: 'Wrote 12 bytes to /srv/project/src/main.go'},
    {id: 'b', tool: 'edit', status: 'completed', output: 'Replaced 2 occurrence(s) in README.md'},
    {id: 'c', tool: 'write', status: 'running', output: ''},
    {id: 'd', tool: 'bash', status: 'completed', output: 'Wrote 1 bytes to x'},
    {id: 'e', tool: 'write', status: 'completed', output: 'Wrote 3 bytes to /etc/passwd'},
    {id: 'f', tool: 'write', status: 'completed', output: 'Wrote 3 bytes to ../escape.txt'},
  ];
  assert.deepEqual(touchedFiles(activities, root).map(t => [t.path, t.tool, t.activity]), [['src/main.go', 'write', 'a'], ['README.md', 'edit', 'b']]);
  assert.equal(relativePath('./a//b.txt', root), 'a/b.txt');
  assert.equal(relativePath('/srv/project', root), '');
  assert.equal(relativePath('', root), '');
});
