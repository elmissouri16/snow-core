import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import readline from 'node:readline';

const mode = process.argv[2] ?? 'echo';
const send = (frame) => process.stdout.write(JSON.stringify(frame) + '\n');
const raw = (value) => process.stdout.write(Buffer.from(value, 'base64'));
const ready = { type: 'rpc_ready', protocol_version: '1', snow_version: 'fake-node', capabilities: ['prompt_completion', 'future_capability'], max_input_bytes: 16777216 };
const alive = () => setInterval(() => {}, 1000);
if (process.env.SDK_PEER_PID_FILE) writeFileSync(process.env.SDK_PEER_PID_FILE, `${process.pid}`);

if (mode === 'silent') alive();
else if (mode === 'raw-handshake') { raw(process.argv[3]); alive(); }
else if (mode === 'stderr-exit') { process.stderr.write('PRIVATE-PEER-DIAGNOSTIC'); process.exit(23); }
else {
  if (mode === 'stderr-flood') process.stderr.write('PRIVATE-PEER-DIAGNOSTIC'.repeat(1_000_000));
  if (mode === 'small-input') ready.max_input_bytes = 90;
  if (mode === 'environment') ready.snow_version = process.env.SDK_ENV ?? 'unset';
  send(ready);
  if (mode === 'block-input' || mode === 'ignore-eof') alive();
  else {
    const lines = readline.createInterface({ input: process.stdin });
    if (mode === 'delayed-read') { process.stdin.pause(); setTimeout(() => process.stdin.resume(), 150); }
    let lastId = 'last';
    const reply = (request, command = request.type) => send({ type: 'response', id: request.id, command, success: true,
      data: { pid: process.pid, args: process.argv.slice(3), cwd: process.cwd(), env: process.env.SDK_ENV ?? null, inherited: process.env.SDK_INHERITED ?? null } });
    lines.on('line', (line) => {
      const request = JSON.parse(line);
      lastId = request.id;
      if (request.type === 'never') return;
      if (request.type === 'delay') { setTimeout(() => reply(request), request.delay ?? 80); return; }
      if (request.type === 'mismatch') { reply(request, 'wrong'); return; }
      if (request.type === 'emit') { for (const frame of request.frames) send(frame); reply(request); return; }
      if (request.type === 'raw') { raw(request.payload); reply(request); return; }
      if (request.type === 'partial-exit') { process.stdout.write('{"type":"future_event"}'); lines.close(); process.stdin.destroy(); return; }
      if (request.type === 'exit-final') {
        send({ type: 'prompt_completed', request_id: request.id, status: 'completed' });
        reply(request);
        lines.close(); process.stdin.destroy(); return;
      }
      if (request.type === 'duplicate') {
        reply(request);
        send({ type: 'response', id: request.id, command: request.type, success: false, error: 'legacy duplicate' });
        return;
      }
      reply(request);
    });
    lines.on('close', () => {
      if (mode === 'descendant') {
        const child = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 700)'], { stdio: ['ignore', process.stdout, 'ignore'] });
        child.unref();
      }
      if (mode === 'descendant') {
        process.stdout.write(JSON.stringify({ type: 'prompt_completed', request_id: lastId, status: 'completed' }) + '\n', () => process.exit(0));
        return;
      }
      if (mode === 'final') send({ type: 'prompt_completed', request_id: lastId, status: 'completed' });
      process.stdout.end();
    });
  }
}
