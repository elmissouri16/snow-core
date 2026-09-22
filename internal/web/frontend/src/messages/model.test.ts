import assert from 'node:assert/strict';
import test from 'node:test';
import { boundedOutput, projectActivities, projectMessages, sameActivityProjection, sameMessageProjection } from './model.ts';
import { imageURL } from './imageTransport.ts';
import { markdownTags, safeLink } from './markdownModel.ts';
import { defaultActions, mergeActions } from './actions.ts';
import type { ActionPresentationPatch } from './actions.ts';

test('message public projection retains explicit markers, deduplicates identities and rejects oversize HTML whole', () => {
  const messages = projectMessages([
    {id: 'private', role: 'tool', text: 'not a user-facing message'},
    {id: 'marker', role: 'tool_activity'}, {id: 'marker', role: 'assistant', text: 'duplicate'},
    {role: 'tool_activity'}, {id: 'x'.repeat(257), role: 'tool_activity'},
    {id: 'u', role: 'user', text: 'text', can_edit: true, provider_private: 'never retain'},
    {id: 'a', role: 'assistant', text: '<script>text</script>', html: 'x'.repeat(131073), can_regenerate: true}
  ]);
  assert.deepEqual(messages.map(message => [message.id, message.role]), [['marker', 'tool_activity'], ['u', 'user'], ['a', 'assistant']]);
  assert.equal(messages[1].editable, true); assert.equal(messages[1].reusable, true);
  assert.equal(messages[2].html, ''); assert.equal(messages[2].text, '<script>text</script>');
  assert.equal(messages[2].regeneratable, true);
  assert.equal(JSON.stringify(messages).includes('provider_private'), false);
  assert.equal(projectMessages(Array.from({length: 110}, (_, index) => ({id: String(index), role: 'user'}))).length, 100);
});

test('image-bearing and truncated/multibyte messages never become resend copies', () => {
  const [image, truncated, multibyte] = projectMessages([
    {id: 'i', role: 'user', text: '', can_edit: true, images: [{index: 0, mime_type: 'image/png', url: ''}]},
    {id: 't', role: 'user', text: 'bounded', truncated: true, can_edit: true},
    {id: 'm', role: 'user', text: '💡'.repeat(17000)}
  ]);
  assert.equal(image.editable, false); assert.equal(image.reusable, false);
  assert.equal(truncated.reusable, false); assert.equal(truncated.editable, true, 'full edit prepare remains authoritative');
  assert.equal(multibyte.reusable, false);
});

test('saved tools preserve unknown outcomes and UTF-8/global budgets without treating output as markup', () => {
  assert.deepEqual(boundedOutput('a💡é', 5), {text: 'a💡', bytes: 5, truncated: true});
  const [message] = projectMessages([{id: 'a', role: 'assistant', tools: [
    {id: 'unknown', tool: 'bash', status: 'running', output_available: true, output: 'must not imply success'},
    {id: 'missing', tool: 'read', status: 'completed', output_available: false},
    {id: 'known', tool: 'read', status: 'completed', output_available: true, output: '<img src=x>'},
    ...Array.from({length: 70}, (_, index) => ({id: `t${index}`, tool: 'read', status: 'completed', output_available: true, output: '💡'.repeat(3000)}))
  ]}]);
  assert.equal(message.tools.length, 64); assert.equal(message.toolsOmitted, true);
  assert.equal(message.tools[0].label, 'Outcome unknown');
  assert.equal(message.tools[0].output, 'No result recorded; execution outcome unknown');
  assert.equal(message.tools[1].output, 'Public output was not recorded');
  assert.equal(message.tools[2].output, '<img src=x>');
  assert.ok(message.tools.reduce((bytes, tool) => bytes + (tool.available && tool.status !== 'unresolved' ? Buffer.byteLength(tool.output) : 0), 0) <= 131072);
  assert.ok(message.tools.every(tool => Buffer.byteLength(tool.output) <= 8192));
});

test('runtime activity binds only an exact marker, bounds output, and handles unknown statuses safely', () => {
  const [known, unknown] = projectActivities([
    {id: 'a', message_id: 'marker', tool: 'bash', status: 'unknown', is_error: true, output: 'a'.repeat(16385)},
    {id: 'a', status: 'completed'}, {id: 'b', status: '__proto__', message_id: 'x'.repeat(257)}
  ]);
  assert.equal(known.messageID, 'marker'); assert.equal(known.data.label, 'Outcome unknown');
  assert.equal(known.data.output.length, 16384); assert.equal(known.data.truncated, true);
  assert.equal(unknown.messageID, ''); assert.equal(unknown.data.label, 'Status unavailable');
});

test('runtime projections detect semantic changes without treating fresh snapshot objects as changes', () => {
  const messages = [
    {id: 'u', role: 'user', text: 'hello', can_edit: true},
    {id: 'a', role: 'assistant', text: 'answer', html: '<p>answer</p>', tools: [{id: 't', tool: 'read', status: 'completed', output_available: true, output: 'done'}]},
  ];
  const activities = [{id: 't', message_id: 'marker', tool: 'read', status: 'completed', output: 'done'}];
  const firstMessages = projectMessages(messages), firstActivities = projectActivities(activities);
  assert.equal(sameMessageProjection(firstMessages, projectMessages(structuredClone(messages))), true);
  assert.equal(sameActivityProjection(firstActivities, projectActivities(structuredClone(activities))), true);
  assert.equal(sameMessageProjection(firstMessages, projectMessages([{...messages[0], text: 'changed'}, messages[1]])), false);
  assert.equal(sameActivityProjection(firstActivities, projectActivities([{...activities[0], output: 'changed'}])), false);
});

test('image URLs require exact authenticated local route, identity, raster type and query', () => {
  const origin = 'https://127.0.0.1:4444', scope = {project: 'p', instance: 'i', session: 's', live: true};
  const route = '/projects/p/runtime/images/m/0?instance_id=i&session_id=s';
  assert.equal(imageURL(route, scope, 'm', 0, 'image/png', origin), route);
  assert.equal(imageURL(origin + route, scope, 'm', 0, 'image/png', origin), route);
  assert.equal(imageURL(route + '&turn_id=t', scope, 'm', 0, 'image/png', origin), route + '&turn_id=t');
  for (const value of [' ' + route, 'https://evil.test' + route, '//' + origin.slice(8) + route, route + '#fragment', route + '&session_id=s', route + '&extra=x', route.replace('instance_id=i', 'instance_id=old'), route.replace('/runtime/images', '/runtime/../runtime/images'), route.replace('/m/0', '/other/0'), route.replace('/m/0', '/m/00')]) {
    assert.equal(imageURL(value, scope, 'm', 0, 'image/png', origin), '', value);
  }
  assert.equal(imageURL(route, scope, 'm', 0, 'image/svg+xml', origin), '');
  const saved = '/projects/p/sessions/s/images/m/0';
  assert.equal(imageURL(saved, {...scope, live: false}, 'm', 0, 'image/png', origin), saved);
  assert.equal(imageURL(saved + '?instance_id=i', {...scope, live: false}, 'm', 0, 'image/png', origin), '');
});

test('Markdown policy admits only passive server presentation and explicit public links', () => {
  for (const tag of ['script', 'style', 'img', 'svg', 'iframe', 'object', 'form', 'input', 'video']) assert.equal(markdownTags.has(tag), false);
  for (const tag of ['p', 'pre', 'code', 'table', 'a']) assert.equal(markdownTags.has(tag), true);
  for (const raw of ['javascript:alert(1)', 'data:text/html,x', '/relative', '//host/path', 'https://user:password@example.test/x', 'file:///etc/passwd']) assert.equal(safeLink(raw), undefined);
  assert.equal(safeLink('https://example.test/a?q=1&x=2'), 'https://example.test/a?q=1&x=2');
});


test('action projections default closed and merge independent parent presentation without admitting rows', () => {
  const initial = defaultActions();
  for (const group of [initial.reuse, initial.edit, initial.regenerate]) { assert.equal(group.hidden, true); assert.equal(group.disabled, true); }
  const editing = mergeActions(initial, {edit: {hidden: false, disabled: false, messageID: 'user'}});
  const regenerating = mergeActions(editing, {regenerate: {hidden: false, disabled: true}, historicalDisabled: true});
  assert.deepEqual(regenerating.edit, editing.edit);
  assert.equal(regenerating.reuse.hidden, true); assert.equal(regenerating.historicalDisabled, true);
  const cleared = mergeActions(regenerating, {edit: {messageID: ''}, historicalDisabled: false});
  assert.equal(cleared.edit.messageID, ''); assert.equal(cleared.edit.hidden, false); assert.equal(cleared.historicalDisabled, false);
  assert.equal(mergeActions(cleared, {}), cleared, 'no-op projection avoids a redundant commit');
  assert.equal(defaultActions().edit.disabled, true, 'new lifecycle never inherits enabled action state');
});

test('action projection bounds presentation text and rejects malformed capability-looking flags', () => {
  const malformed = {reuse: {disabled: 'false', hidden: null, title: 'x'.repeat(900)}, edit: {messageID: 'x'.repeat(257)}, historicalDisabled: 'false'} as unknown as ActionPresentationPatch;
  const current = mergeActions(defaultActions(), {reuse: {disabled: false, hidden: false}, edit: {messageID: 'before'}});
  const next = mergeActions(current, malformed);
  assert.equal(next.reuse.disabled, true); assert.equal(next.reuse.hidden, true); assert.equal(next.reuse.title.length, 512);
  assert.equal(next.edit.messageID, ''); assert.equal(next.historicalDisabled, true);
});
