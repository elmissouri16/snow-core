import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { pack } from '../scripts/pack.mjs';

const sdk = fileURLToPath(new URL('../', import.meta.url));
const root = fileURLToPath(new URL('../../../', import.meta.url));
const require = createRequire(import.meta.url);
const compiler = join(dirname(require.resolve('typescript/package.json')), 'bin/tsc');

test('packed SDK installs offline and resolves ESM/CJS runtime and NodeNext declarations', { timeout: 90_000 }, async () => {
  const tarball = await pack();
  assert.ok(tarball.path.startsWith(join(root, 'build/dist/')));
  const names = tarball.files.map((entry) => entry.path);
  assert.ok(names.includes('LICENSE'));
  assert.ok(names.includes('README.md'));
  assert.ok(names.includes('esm/index.js'));
  assert.ok(names.includes('cjs/index.js'));
  assert.ok(names.includes('esm/index.d.ts'));
  assert.ok(names.includes('cjs/index.d.ts'));
  assert.ok(names.includes('cjs/package.json'));
  assert.ok(!names.some((name) => /^(src|tests|scripts|node_modules)\//.test(name)));
  const consumer = join(root, 'build/sdk-node/consumer');
  await rm(consumer, { recursive: true, force: true });
  await mkdir(consumer, { recursive: true });
  await writeFile(join(consumer, 'package.json'), '{"name":"snow-sdk-smoke","version":"0.0.0","private":true,"type":"module"}\n');
  execFileSync('npm', ['install', '--ignore-scripts', '--offline', '--no-audit', '--no-fund', '--package-lock=false', '--save=false', tarball.path], {
    cwd: consumer, encoding: 'utf8', timeout: 30_000,
    env: { ...process.env, npm_config_cache: join(consumer, '.npm-cache') },
  });
  const manifest = JSON.parse(await readFile(join(consumer, 'node_modules/@elmissouri16/snow-sdk/package.json'), 'utf8'));
  assert.equal(manifest.name, '@elmissouri16/snow-sdk');
  assert.equal(manifest.version, '0.1.0-dev');
  assert.equal(manifest.engines.node, '>=22.12.0');
  assert.equal(manifest.type, 'module');
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.devDependencies, undefined);
  assert.equal(manifest.scripts, undefined);
  assert.deepEqual(manifest.exports['.'], {
    import: { types: './esm/index.d.ts', default: './esm/index.js' },
    require: { types: './cjs/index.d.ts', default: './cjs/index.js' },
  });
  const checks = `
    const assert = (await import('node:assert/strict')).default;
    const esm = await import('@elmissouri16/snow-sdk');
    const { createRequire } = await import('node:module');
    const cjs = createRequire(import.meta.url)('@elmissouri16/snow-sdk');
    for (const module of [esm, cjs]) {
      assert.equal(typeof module.SnowClient.start, 'function');
      assert.equal(typeof module.SdkError, 'function');
      assert.equal(typeof module.RemoteRpcError, 'function');
    }
    assert.deepEqual(Object.keys(esm).sort(), Object.keys(cjs).sort());
  `;
  await writeFile(join(consumer, 'runtime.mjs'), checks);
  execFileSync(process.execPath, ['runtime.mjs'], { cwd: consumer, stdio: 'inherit', timeout: 10_000 });
  for (const format of ['mts', 'cts']) {
    await cp(join(sdk, `tests/types/consumer.${format}`), join(consumer, `consumer.${format}`));
  }
  await writeFile(join(consumer, 'tsconfig.json'), JSON.stringify({
    compilerOptions: {
      target: 'ES2022', module: 'NodeNext', moduleResolution: 'NodeNext',
      strict: true, noEmit: true, skipLibCheck: false,
      typeRoots: [join(sdk, 'node_modules/@types')], types: ['node'],
    },
    include: ['consumer.mts', 'consumer.cts'],
  }, null, 2));
  execFileSync(process.execPath, [compiler, '-p', join(consumer, 'tsconfig.json')], {
    cwd: consumer, stdio: 'inherit', timeout: 30_000,
  });
});
