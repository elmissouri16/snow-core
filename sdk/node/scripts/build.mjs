import { execFileSync } from 'node:child_process';
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const sdk = fileURLToPath(new URL('../', import.meta.url));
const root = fileURLToPath(new URL('../../../', import.meta.url));
const output = join(root, 'build/sdk-node');
const stage = join(output, 'package');
const require = createRequire(import.meta.url);
const compiler = join(dirname(require.resolve('typescript/package.json')), 'bin/tsc');

for (const name of ['package', 'types', 'test']) {
  await rm(join(output, name), { recursive: true, force: true });
}
await mkdir(stage, { recursive: true });
// These unbundled ESM modules are internal test fixtures, never package exports.
execFileSync(process.execPath, [compiler, '-p', join(sdk, 'tsconfig.json')], { cwd: sdk, stdio: 'inherit', timeout: 120_000 });
await writeFile(join(output, 'test/package.json'), '{"type":"module"}\n');
execFileSync(process.execPath, [compiler, '-p', join(sdk, 'tsconfig.build.json')], { cwd: sdk, stdio: 'inherit', timeout: 120_000 });
for (const format of ['esm', 'cjs']) {
  await build({
    absWorkingDir: sdk,
    entryPoints: ['src/index.ts'],
    outfile: join(stage, format, 'index.js'),
    bundle: true,
    platform: 'node',
    format,
    target: 'node22.12',
    sourcemap: false,
    legalComments: 'none',
  });
  await cp(join(output, 'types'), join(stage, format), { recursive: true });
}
await writeFile(join(stage, 'cjs/package.json'), '{"type":"commonjs"}\n');
const source = JSON.parse(await readFile(join(sdk, 'package.json'), 'utf8'));
const { scripts: _scripts, devDependencies: _dev, ...manifest } = source;
Object.assign(manifest, {
  main: './cjs/index.js',
  module: './esm/index.js',
  types: './esm/index.d.ts',
  exports: {
    '.': {
      import: { types: './esm/index.d.ts', default: './esm/index.js' },
      require: { types: './cjs/index.d.ts', default: './cjs/index.js' },
    },
    './package.json': './package.json',
  },
  files: ['esm', 'cjs', 'README.md', 'LICENSE'],
});
await writeFile(join(stage, 'package.json'), `${JSON.stringify(manifest, null, 2)}\n`);
await cp(join(sdk, 'README.md'), join(stage, 'README.md'));
await cp(join(root, 'LICENSE'), join(stage, 'LICENSE'));
console.log('Built ESM/CJS package, declarations, and internal test modules under build/sdk-node.');
