import {spawn} from 'node:child_process';
import {mkdtemp, chmod, mkdir, rm, realpath} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve, dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';
import {chromeBinary, debuggingURL, connect} from '../live-stream/cdp.mjs';
import {exercise} from './tests.mjs';

const repository = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..');
const artifacts = process.env.SNOW_HOST_CONTROLS_ARTIFACTS ? resolve(process.env.SNOW_HOST_CONTROLS_ARTIFACTS) : null;
const exited = child => new Promise(resolve => {child.once('exit', resolve); child.once('error', resolve);});
async function compile(binary) {
  await new Promise((resolve, reject) => {
    const child = spawn('go', ['test', '-p', '1', '-c', '-o', binary, './cmd/snow'], {cwd: repository, env: {...process.env, GOMAXPROCS: '2'}, stdio: ['ignore', 'pipe', 'pipe'], timeout: 120000});
    let output = '';
    for (const stream of [child.stdout, child.stderr]) stream.on('data', chunk => {output = (output + chunk).slice(-65536);});
    child.once('error', reject); child.once('close', code => code === 0 ? resolve() : reject(Error('Fixture compile failed: ' + output)));
  });
}
async function run(width, theme, binary) {
  const directory = await realpath(await mkdtemp(join(tmpdir(), 'snow-manager-host-controls-'))); await chmod(directory, 0o700);
  let manager, managerExited, chrome, chromeExited, client, timer;
  try {
    const deadline = new Promise((_, reject) => {timer = setTimeout(() => reject(Error('Native host-controls deadline exceeded')), 200000);});
    return await Promise.race([deadline, (async () => {
      manager = spawn(binary, ['-test.run=^TestWebHostControlsBrowserFixture$', '-test.count=1', '-test.timeout=230s'], {
        cwd: repository, env: {PATH: '/usr/bin:/bin', TMPDIR: directory, HOME: join(directory, 'home'), SNOW_HOST_CONTROLS_BROWSER_DIR: directory, GOMAXPROCS: '2'}, stdio: ['pipe', 'pipe', 'pipe']
      });
      managerExited = exited(manager); manager.stdin.on('error', () => {});
      const ready = await new Promise((resolve, reject) => {
        let buffer = '', received = 0;
        manager.stdout.on('data', chunk => {
          received += chunk.length; if (received > 65536) {reject(Error('Private startup output limit')); return;}
          buffer += chunk; let i;
          while ((i = buffer.indexOf('\n')) >= 0) {
            const line = buffer.slice(0, i); buffer = buffer.slice(i + 1);
            // Never echo credential-bearing private IPC or fixture diagnostics.
            if (line.startsWith('SNOW_HOST_CONTROLS_READY ')) {
              try {resolve(JSON.parse(line.slice('SNOW_HOST_CONTROLS_READY '.length)));} catch {reject(Error('Invalid private startup frame'));}
            }
          }
        });
        manager.stderr.resume(); manager.once('error', () => reject(Error('Fixture launch failed'))); manager.once('exit', () => reject(Error('Fixture exited before ready')));
      });
      for (const [name, scheme] of [['origin', 'https:'], ['httpOrigin', 'http:']]) {
        const u = new URL(ready[name]); if (u.protocol !== scheme || u.hostname !== '127.0.0.1' || !u.port || u.port === '7331') throw Error('Invalid isolated fixture listener');
      }
      if (ready.directory !== directory || !/^[A-Za-z0-9+/]{43}=$/.test(ready.spki)) throw Error('Invalid private fixture scope');
      chrome = spawn(chromeBinary(), ['--headless=new', '--no-sandbox', '--disable-gpu', '--disable-background-networking', '--disable-component-update', '--disable-default-apps', '--disable-sync', '--no-first-run', '--no-default-browser-check', `--ignore-certificate-errors-spki-list=${ready.spki}`, '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=0', `--user-data-dir=${join(directory, 'chrome')}`, 'about:blank'], {stdio: ['ignore', 'ignore', 'pipe']});
      chromeExited = exited(chrome); client = await connect(await debuggingURL(chrome));
      const result = await exercise({client, ready, width, theme, artifacts});
      console.log(JSON.stringify({width, height: 740, theme, ...result})); return result;
    })()]);
  } finally {
    clearTimeout(timer);
    if (client) {try {await Promise.race([client.send('Browser.close'), delay(1000)]);} catch {} client.close();}
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) chrome.kill('SIGKILL');
    if (chromeExited) await Promise.race([chromeExited, delay(2000)]);
    manager?.stdin.end('stop\n'); if (managerExited) await Promise.race([managerExited, delay(15000)]);
    if (manager && manager.exitCode === null && manager.signalCode === null) {manager.kill('SIGKILL'); await managerExited;}
    await rm(directory, {recursive: true, force: true, maxRetries: 3, retryDelay: 100});
  }
}
const build = await mkdtemp(join(tmpdir(), 'snow-host-controls-build-'));
let passed = 0, failures = 0, reports = 0;
try {
  if (artifacts) await mkdir(artifacts, {recursive: true, mode: 0o700});
  const binary = process.env.SNOW_HOST_CONTROLS_BINARY ? resolve(process.env.SNOW_HOST_CONTROLS_BINARY) : join(build, 'snow-fixture.test');
  if (!process.env.SNOW_HOST_CONTROLS_BINARY) await compile(binary);
  const widths = process.env.SNOW_HOST_CONTROLS_WIDTH ? [Number(process.env.SNOW_HOST_CONTROLS_WIDTH)] : [320, 1280];
  const themes = process.env.SNOW_HOST_CONTROLS_THEME ? [process.env.SNOW_HOST_CONTROLS_THEME] : ['dark', 'light'];
  if (widths.some(w => ![320, 1280].includes(w)) || themes.some(t => !['dark', 'light'].includes(t))) throw Error('Only the 320/1280 dark/light matrix is supported');
  for (const width of widths) for (const theme of themes) {
    reports++;
    try {const result = await run(width, theme, binary); passed += result.results.length - result.failures.length; failures += result.failures.length;}
    catch (error) {passed += (error.results?.length || 0) - (error.failures?.length || 0); failures += 1 + (error.failures?.length || 0); console.error(JSON.stringify({width, theme, fatal: error.message, passedAssertions: (error.results?.length || 0) - (error.failures?.length || 0), failures: error.failures || [], observation: error.observation}));}
  }
} catch (error) {failures++; console.error('manager-host-controls: ' + error.message);}
finally {await rm(build, {recursive: true, force: true});}
console.log(`${passed} assertions passed; ${failures} failures across ${reports} native host-control reports.`);
if (failures) process.exitCode = 1;
