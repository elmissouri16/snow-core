import { lstat, mkdtemp, readdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { buildAssets, generatedRoot } from './build.mjs';

async function files(root, prefix = '') {
  let entries;
  try {
    if (!(await lstat(root)).isDirectory()) throw new Error(`Not a regular directory: ${root}`);
    entries = await readdir(root, { withFileTypes: true });
  } catch (error) {
    if (error.code === 'ENOENT' && !prefix) return new Map();
    throw error;
  }
  const result = new Map();
  for (const entry of entries.sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0)) {
    const name = prefix + entry.name;
    if (entry.isDirectory()) {
      for (const [key, value] of await files(join(root, entry.name), `${name}/`)) result.set(key, value);
    } else if (entry.isFile()) {
      result.set(name, await readFile(join(root, entry.name)));
    } else {
      throw new Error(`Generated assets must be regular files, not links or special files: ${name}`);
    }
  }
  return result;
}

// Read-only comparison: never repair stale checked-in assets during a check.
export async function compareTrees(expectedRoot, actualRoot) {
  const expected = await files(expectedRoot);
  const actual = await files(actualRoot);
  const differences = [];
  for (const name of [...new Set([...expected.keys(), ...actual.keys()])].sort()) {
    if (!actual.has(name)) differences.push(`missing: ${name}`);
    else if (!expected.has(name)) differences.push(`extra: ${name}`);
    else if (!expected.get(name).equals(actual.get(name))) differences.push(`stale: ${name}`);
  }
  return differences;
}

export async function checkAssets() {
  const temporary = await mkdtemp(join(tmpdir(), 'snow-frontend-check-'));
  try {
    await buildAssets(temporary);
    const differences = await compareTrees(temporary, generatedRoot);
    if (differences.length) {
      throw new Error(`Generated frontend assets differ:\n${differences.join('\n')}\nRun npm run build in internal/web/frontend and commit the generated tree.`);
    }
    console.log('Generated frontend assets and third-party notices are current.');
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  checkAssets().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
