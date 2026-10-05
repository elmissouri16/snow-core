#!/usr/bin/env node
import readline from 'node:readline';
const send = frame => process.stdout.write(JSON.stringify(frame) + '\n');
const caps = ['prompt_completion', 'models_list', 'model_discovery', 'session_info', 'session_management', 'messages_page', 'permission_interaction', 'user_input'];
send({ type: 'rpc_ready', protocol_version: '1', snow_version: '0.1.0-dev', max_input_bytes: 16777216, capabilities: process.env.SDK_NO_CAPS ? [] : caps });
send({ type: 'future_event', extra: 'preserved' });
let active;
let aborts = 0;
const reply = (request, data, success = true) => send({ type: 'response', id: request.id, command: request.type, success, ...(data === undefined ? {} : { data }), ...(!success ? { error: 'rejected', error_code: 'invalid' } : {}) });
const complete = (request, status) => {
  active = undefined;
  send({ type: 'prompt_completed', request_id: request.id, status, ...(status === 'failed' ? { error: 'provider failed' } : {}) });
};
const lines = readline.createInterface({ input: process.stdin });
lines.on('line', line => {
  const request = JSON.parse(line);
  switch (request.type) {
    case 'prompt':
      active = request;
      if (request.message === 'lost admission') {
        setTimeout(() => complete(request, 'completed'), 80);
        break;
      }
      if (request.message === 'completion first') {
        complete(request, 'completed');
        reply(request);
        break;
      }
      if (request.message === 'reject') { active = undefined; reply(request, undefined, false); break; }
      reply(request);
      if (request.message === 'worker exits') { setTimeout(() => process.exit(0), 20); break; }
      if (request.message === 'slow') break;
      send({ type: 'text_delta', text: request.message });
      send({ type: 'turn_done' });
      if (request.message === 'fail') reply(request, undefined, false);
      complete(request, request.message === 'fail' ? 'failed' : 'completed');
      break;
    case 'abort':
      aborts++;
      reply(request);
      if (active) complete(active, 'canceled');
      break;
    case 'usage': reply(request, { aborts, args: process.argv.slice(2), active: active?.id }); break;
    case 'models_list': reply(request, { provider: 'fake', current: 'fake-1', models: [] }); break;
    case 'models_discover': reply(request, { models: [], partial: false, truncated: false }); break;
    case 'session_open': reply(request, undefined, false); break;
    default: reply(request, request.params ?? {});
  }
});
lines.on('close', () => process.exit(0));
