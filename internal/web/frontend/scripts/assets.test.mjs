import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { runtimeNotices } from './build.mjs';
import { compareTrees } from './check.mjs';

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'snow-assets-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

async function put(root, name, content) {
  const path = join(root, name);
  await mkdir(join(path, '..'), { recursive: true });
  await writeFile(path, content);
}

test('asset verification compares complete names and bytes without repairing files', async (t) => {
  const root = await fixture(t);
  const expected = join(root, 'expected');
  const actual = join(root, 'actual');
  await put(expected, 'app.js', Buffer.from([0, 1, 2]));
  await put(expected, 'THIRD-PARTY-NOTICES.txt', 'full licenses\n');
  assert.deepEqual(await compareTrees(expected, actual), [
    'missing: THIRD-PARTY-NOTICES.txt', 'missing: app.js',
  ]);
  await put(actual, 'app.js', Buffer.from([0, 1, 3]));
  await put(actual, 'old/chunk.js', 'obsolete');
  assert.deepEqual(await compareTrees(expected, actual), [
    'missing: THIRD-PARTY-NOTICES.txt', 'stale: app.js', 'extra: old/chunk.js',
  ]);
  assert.deepEqual(await readFile(join(actual, 'app.js')), Buffer.from([0, 1, 3]));
  assert.equal(await readFile(join(actual, 'old/chunk.js'), 'utf8'), 'obsolete');
  await rm(join(actual, 'old'), { recursive: true });
  await put(actual, 'app.js', Buffer.from([0, 1, 2]));
  await put(actual, 'THIRD-PARTY-NOTICES.txt', 'full licenses\n');
  assert.deepEqual(await compareTrees(expected, actual), []);
  await put(actual, 'THIRD-PARTY-NOTICES.txt', 'stale licenses\n');
  assert.deepEqual(await compareTrees(expected, actual), ['stale: THIRD-PARTY-NOTICES.txt']);
});

test('asset verification rejects symlinks instead of following them', async (t) => {
  const root = await fixture(t);
  await put(root, 'expected/app.js', 'bundle');
  await mkdir(join(root, 'actual'));
  await symlink(join(root, 'expected/app.js'), join(root, 'actual/app.js'));
  await assert.rejects(compareTrees(join(root, 'expected'), join(root, 'actual')), /not links/);
});

test('notices retain full license text and use validated locked installed versions', async (t) => {
  const root = await fixture(t);
  const lock = { packages: {} };
  const packages = ['react', 'react-dom', 'scheduler'];
  for (const [index, name] of packages.entries()) {
    const metadata = { name, version: `1.2.${index}`, license: 'MIT' };
    lock.packages[`node_modules/${name}`] = metadata;
    await put(root, `node_modules/${name}/package.json`, JSON.stringify(metadata));
    await put(root, `node_modules/${name}/LICENSE`, `Copyright ${name}\n\nPermission text ${index}.\nWarranty text.\n`);
  }
  await put(root, 'package-lock.json', JSON.stringify(lock));
  const notices = await runtimeNotices(root);
  assert.equal(await runtimeNotices(root), notices);
  assert.ok(!notices.includes(root));
  for (const [index, name] of packages.entries()) {
    assert.ok(notices.includes(`${name}@1.2.${index} (MIT)\n`));
    assert.ok(notices.includes(await readFile(join(root, `node_modules/${name}/LICENSE`), 'utf8')));
  }
  await put(root, 'node_modules/scheduler/package.json', JSON.stringify({ name: 'scheduler', version: '99.0.0', license: 'MIT' }));
  await assert.rejects(runtimeNotices(root), /scheduler metadata differs/);
});
