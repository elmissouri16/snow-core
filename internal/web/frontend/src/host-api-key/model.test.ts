import test from 'node:test';
import assert from 'node:assert/strict';
import { metadata, providerID, readMetadataResponse, validSecret, writtenReceipt } from './model.ts';

const provider = 'openai-compatible';
const inspect = () => ({
  provider_id: provider, api_key_supported: true, replace_required: false, revision: 'missing',
  status: { provider_id: provider, state: 'unavailable', reason: 'credential_missing', checked_locally: true },
  checked_locally: true, applies_to: 'future_runtime',
});
const receipt = () => ({ ...inspect(), replace_required: true, revision: 'b'.repeat(64),
  status: { provider_id: provider, state: 'configured', reason: 'credential_present', checked_locally: true },
});

test('provider grammar permits existing profile IDs but not path/query or whitespace input', () => {
  for (const value of [provider, 'opencode-go', 'opencode-zen', 'chatgpt', 'profile_a.b-2', 'a'.repeat(64)]) assert.equal(providerID(value), true);
  for (const value of ['', 'A', '_a', 'a'.repeat(65), '../a', 'a/b', 'a?secret=x', 'a\n', ' a', 'a\u2028']) assert.equal(providerID(value), false);
});

test('only allowlisted exact-provider local metadata enters state', () => {
  const data = { ...inspect(), secret: 'FIXTURE_ONLY', private: { ignored: true }, status: { ...inspect().status, token: 'FIXTURE_ONLY' } };
  assert.deepEqual(metadata(data, provider), { provider_id: provider, api_key_supported: true, replace_required: false, revision: 'missing', state: 'unavailable', reason: 'credential_missing' });
  for (const patch of [
    { provider_id: 'opencode-go' }, { api_key_supported: 'true' }, { replace_required: 1 },
    { revision: 'missing\n' }, { revision: 'B'.repeat(64) }, { revision: 'b'.repeat(64) + '\n' },
    { checked_locally: false }, { applies_to: 'current_runtime' }, { status: null },
    { status: { ...inspect().status, provider_id: 'opencode-go' } },
    { status: { ...inspect().status, checked_locally: false } },
    { status: { ...inspect().status, state: 'configured' } },
    { status: { ...inspect().status, reason: 'arbitrary diagnostic' } },
  ]) assert.equal(metadata({ ...inspect(), ...patch }, provider), null);
  for (const invalid of [null, [], 1, 'text']) assert.equal(metadata(invalid, provider), null);
  const chatgpt = { ...inspect(), provider_id: 'chatgpt', status: { ...inspect().status, provider_id: 'chatgpt' } };
  assert.equal(metadata(chatgpt, 'chatgpt'), null);
  assert.equal(metadata({ ...chatgpt, api_key_supported: false }, 'chatgpt')?.api_key_supported, false);
});

test('a successful write requires the complete exact-provider configured receipt, not an inspection', () => {
  assert.equal(writtenReceipt(receipt(), provider), true);
  assert.equal(writtenReceipt(receipt(), 'opencode-go'), false);
  assert.equal(writtenReceipt(inspect(), provider), false);
  for (const patch of [{ api_key_supported: false }, { replace_required: false }, { revision: 'missing' },
    { status: { ...receipt().status, reason: 'anonymous_access' } }]) {
    assert.equal(writtenReceipt({ ...receipt(), ...patch }, provider), false);
  }
});

test('secret validation bounds UTF-8 bytes and rejects padding, controls and malformed Unicode', () => {
  for (const value of ['x', 'x'.repeat(4096), '界'.repeat(1365), '😀'.repeat(1024)]) assert.equal(validSecret(value), true);
  for (const value of ['', 'x'.repeat(4097), '界'.repeat(1366), '😀'.repeat(1025), ' padded', 'padded ', 'a\nb', 'a\u0000b', 'a\u0085b', '\ud800', '\udfff']) assert.equal(validSecret(value), false);
});

test('response reader bounds streamed UTF-8 bytes, cancels overflow, and rejects malformed data', async () => {
  assert.deepEqual(await readMetadataResponse(new Response(JSON.stringify(inspect()))), inspect());
  // Split a Unicode code point across chunks; decoding is streaming and fatal.
  const bytes = new TextEncoder().encode('"界"');
  assert.equal(await readMetadataResponse(new Response(new ReadableStream({ start(controller) {
    controller.enqueue(bytes.slice(0, 2)); controller.enqueue(bytes.slice(2)); controller.close();
  } }))), '界');
  let cancelled = false;
  await assert.rejects(readMetadataResponse(new Response(new ReadableStream({ start(controller) {
    controller.enqueue(new Uint8Array(65537).fill(32));
  }, cancel() { cancelled = true; } }))));
  assert.equal(cancelled, true);
  // Multibyte input under 64K characters still exceeds the byte limit.
  await assert.rejects(readMetadataResponse(new Response(JSON.stringify('界'.repeat(22000)))));
  await assert.rejects(readMetadataResponse(new Response(new Uint8Array([34, 255, 34]))));
  await assert.rejects(readMetadataResponse(new Response('not json')));
  await assert.rejects(readMetadataResponse(new Response(null, { status: 204 })));
});

test('HTTP failures and redirected receipts are never read or treated as status evidence', async () => {
  let cancelled = false;
  await assert.rejects(readMetadataResponse(new Response(new ReadableStream({ cancel() { cancelled = true; } }), { status: 409 })));
  assert.equal(cancelled, true);
  const response = new Response(JSON.stringify(receipt()));
  Object.defineProperty(response, 'redirected', { value: true });
  await assert.rejects(readMetadataResponse(response));
});
