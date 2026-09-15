import test from 'node:test';
import assert from 'node:assert/strict';
import {publicRequest} from './page.mjs';

test('public route evidence retains field names but never credential values or headers', () => {
  const secret = 'FICTIONAL_PRIVATE_CANARY';
  const evidence = publicRequest({method:'POST', url:'https://127.0.0.1:44321/settings/providers/opencode-go/api-key', postData:new URLSearchParams({csrf:'private-csrf', secret, confirm_save:'host'}).toString(), headers:{Cookie:'private-cookie'}}, [secret]);
  assert.deepEqual(evidence.fields, ['confirm_save','csrf','secret']);
  assert.equal(evidence.local, true); assert.equal(evidence.secretInURL, false);
  for (const value of [secret,'private-csrf','private-cookie']) assert.equal(JSON.stringify(evidence).includes(value), false);
});
test('URL canaries and nonloopback requests are flagged without retaining URL values', () => {
  const evidence = publicRequest({method:'GET',url:'https://example.invalid/path?q=canary',postData:''}, ['canary']);
  assert.equal(evidence.local,false);assert.equal(evidence.secretInURL,true);
  assert.equal(JSON.stringify(evidence).includes('canary'),false);
});

test('path or field-name canaries are redacted even when a regression puts a key there', () => {
  const evidence = publicRequest({method:'POST',url:'https://127.0.0.1:44321/canary',postData:'canary=value'}, ['canary']);
  assert.equal(evidence.secretInURL,true);
  assert.equal(JSON.stringify(evidence).includes('canary'),false);
});
