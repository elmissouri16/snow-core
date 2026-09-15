import { spawnSync } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const frontendRoot = fileURLToPath(new URL('..', import.meta.url));
export const generatedRoot = resolve(frontendRoot, '../static/generated');
const runtimePackages = ['react', 'react-dom', 'scheduler'];

// Use installed license texts, but reject metadata that diverges from npm ci's
// lockfile. Build-tool licenses are not runtime bundle licenses.
export async function runtimeNotices(root = frontendRoot) {
  const lock = JSON.parse(await readFile(join(root, 'package-lock.json'), 'utf8'));
  const sections = ['Snow web runtime third-party notices\n'];
  for (const name of runtimePackages) {
    const packageRoot = join(root, 'node_modules', name);
    const metadata = JSON.parse(await readFile(join(packageRoot, 'package.json'), 'utf8'));
    const locked = lock.packages?.[`node_modules/${name}`];
    if (!locked || metadata.name !== name || !metadata.version ||
        metadata.version !== locked.version || metadata.license !== locked.license) {
      throw new Error(`Installed ${name} metadata differs from package-lock.json; run npm ci --ignore-scripts`);
    }
    const license = await readFile(join(packageRoot, 'LICENSE'), 'utf8');
    if (!license.trim()) throw new Error(`Missing license text for ${name}`);
    sections.push(`${name}@${metadata.version} (${metadata.license})\n${license}${license.endsWith('\n') ? '' : '\n'}`);
  }
  return sections.join('\n');
}

export async function buildAssets(outDir = generatedRoot) {
  const typecheck = spawnSync(process.execPath, [
    join(frontendRoot, 'node_modules/typescript/bin/tsc'), '--noEmit',
  ], { cwd: frontendRoot, stdio: 'inherit', timeout: 120_000 });
  if (typecheck.error) throw typecheck.error;
  if (typecheck.status !== 0) throw new Error('Frontend typecheck failed');

  // Validate notices before Vite can empty the requested output directory.
  const notices = await runtimeNotices();
  const { build } = await import('vite');
  await build({
    root: frontendRoot,
    configFile: join(frontendRoot, 'vite.config.ts'),
    mode: 'production',
    envDir: false,
    build: { outDir: resolve(outDir), emptyOutDir: true, watch: null },
  });
  await writeFile(join(outDir, 'THIRD-PARTY-NOTICES.txt'), notices, 'utf8');
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  buildAssets().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
