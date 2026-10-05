import { spawnSync } from 'node:child_process';
import { readdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const integration = process.argv.includes('--integration');
if (integration && !process.env.SNOW_BIN) {
  throw new Error('test:integration requires SNOW_BIN pointing to a built Snow executable');
}
const tests = fileURLToPath(new URL('../tests/', import.meta.url));
const files = (await readdir(tests)).filter((name) => integration ? name === 'integration.mjs' :
  name.endsWith('.test.mjs') && !/integration/.test(name)).sort().map((name) => fileURLToPath(new URL(name, new URL('../tests/', import.meta.url))));
if (!files.length) throw new Error(`No ${integration ? 'integration' : 'unit/package'} tests found`);
const result = spawnSync(process.execPath, ['--test', ...files], {
  cwd: fileURLToPath(new URL('../', import.meta.url)),
  stdio: 'inherit',
  timeout: integration ? 180_000 : 120_000,
});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;
