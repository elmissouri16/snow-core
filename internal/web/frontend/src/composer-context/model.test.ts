import test from 'node:test';
import assert from 'node:assert/strict';
import {base64, bytes, composerCommands, imageType, matchingCommands, previewDimensionsSafe, previewURL, releasePreview, validText} from './model.ts';
import type {Item} from './types.ts';

const png = Uint8Array.from(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64'));
function header(width: number, height: number) {
  const result = Uint8Array.from(png), view = new DataView(result.buffer);
  view.setUint32(16, width); view.setUint32(20, height); return result;
}
const image = (data = png): Item => ({id: 1, version: 1, label: 'https://not-a-source.test/💠.png', size: data.length,
  state: 'ready', kind: 'image', mime: 'image/png', data: base64(data)});

test('web command completion is bounded to native controls and ranks exact, prefix, then fuzzy matches', () => {
  assert.deepEqual(matchingCommands('model').map(command => command.name), ['/model']);
  assert.deepEqual(matchingCommands('mod').map(command => command.name), ['/model']);
  assert.deepEqual(matchingCommands('cmp').map(command => command.name), ['/compact']);
  assert.equal(matchingCommands('login').length, 0);
  assert.equal(matchingCommands('').length, composerCommands.length);
  assert.equal(new Set(composerCommands.map(command => command.name)).size, composerCommands.length);
  assert.ok(composerCommands.every(command => command.name.startsWith('/') && command.description));
});

test('valid text rejects NUL and either unpaired surrogate without discarding supplementary Unicode', () => {
  for (const invalid of [null, undefined, 2, '\0', '\ud800', '\udfff', 'x\ud800y', '\udc00\ud800']) assert.equal(validText(invalid), false);
  for (const valid of ['', 'A\nB', 'a💠b', '\ud800\udc00']) assert.equal(validText(valid), true);
  assert.equal(bytes('💠'), 4);
});
test('image detection uses bounded signatures, not file extension or MIME claims', () => {
  assert.equal(imageType(png), 'image/png');
  assert.equal(imageType(Uint8Array.from([255, 216, 255])), 'image/jpeg');
  assert.equal(imageType(new TextEncoder().encode('GIF89a')), 'image/gif');
  assert.equal(imageType(new TextEncoder().encode('RIFF0000WEBP')), 'image/webp');
  assert.equal(imageType(new TextEncoder().encode('<svg/>')), '');
  assert.equal(imageType(new TextEncoder().encode('%PDF-')), '');
  assert.equal(imageType(new Uint8Array()), '');
});
test('preview header limits reject pixel bombs before decoding at exact axis/pixel boundaries', () => {
  for (const [width, height] of [[1, 1], [16384, 1], [1, 16384], [8000, 5000]]) assert.equal(previewDimensionsSafe(header(width, height), 'image/png'), true);
  for (const [width, height] of [[0, 1], [1, 0], [16385, 1], [1, 16385], [8000, 5001]]) assert.equal(previewDimensionsSafe(header(width, height), 'image/png'), false);
  for (let size = 0; size < 33; size++) assert.equal(previewDimensionsSafe(png.subarray(0, size), 'image/png'), false);
});
test('private image URLs are created once from retained bytes and revoked once without altering capture data', t => {
  const blobs: Blob[] = [], revoked: string[] = [];
  t.mock.method(URL, 'createObjectURL', (blob: Blob) => { blobs.push(blob); return 'blob:private-image'; });
  t.mock.method(URL, 'revokeObjectURL', (url: string) => revoked.push(url));
  const item = image(), data = item.data;
  assert.equal(previewURL(item), 'blob:private-image');
  assert.equal(previewURL(item), 'blob:private-image');
  assert.equal(blobs.length, 1); assert.equal(blobs[0].type, 'image/png'); assert.equal(blobs[0].size, png.length);
  releasePreview(item); releasePreview(item);
  assert.deepEqual(revoked, ['blob:private-image']); assert.equal(item.data, data); assert.equal(item.state, 'ready');
});
test('unavailable or unsafe previews do not change image eligibility or accidentally create source URLs', t => {
  let created = 0;
  t.mock.method(URL, 'createObjectURL', () => { created++; throw new Error('not supported'); });
  for (const item of [image(header(10000, 10000)), {...image(), size: png.length + 1}, {...image(), mime: 'image/svg+xml'}, image()]) {
    const data = item.data;
    assert.equal(previewURL(item), ''); assert.equal(previewURL(item), '');
    assert.equal(item.state, 'ready'); assert.equal(item.data, data);
  }
  assert.equal(created, 1);
});
