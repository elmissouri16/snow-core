import { execFileSync } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../../', import.meta.url));
export async function pack() {
  const destination = join(root, 'build/dist');
  await mkdir(destination, { recursive: true });
  const result = execFileSync('npm', ['pack', '--ignore-scripts', '--json', '--pack-destination', destination], {
    cwd: join(root, 'build/sdk-node/package'), encoding: 'utf8', timeout: 120_000,
    env: { ...process.env, npm_config_audit: 'false', npm_config_fund: 'false' },
  });
  const entries = JSON.parse(result);
  return { ...entries[0], path: join(destination, entries[0].filename) };
}
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await import('./build.mjs');
  console.log((await pack()).path);
}
