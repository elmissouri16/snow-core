import assert from 'node:assert/strict';
import test from 'node:test';
import {initialControls, sameControlField} from './model.ts';

test('control projection equality compares nested public fields instead of object identity', () => {
  const controls = initialControls();
  assert.equal(sameControlField('queue', controls.queue, {...controls.queue}), true);
  assert.equal(sameControlField('reuse', controls.reuse, {...controls.reuse}), true);
  assert.equal(sameControlField('edit', controls.edit, {...controls.edit}), true);
  assert.equal(sameControlField('regenerate', controls.regenerate, {...controls.regenerate}), true);
  assert.equal(sameControlField('dialog', controls.dialog, {...controls.dialog}), true);
  assert.equal(sameControlField('queue', controls.queue, {...controls.queue, disabled: !controls.queue.disabled}), false);
  assert.equal(sameControlField('reuse', controls.reuse, {...controls.reuse, text: 'changed'}), false);
  assert.equal(sameControlField('sendDisabled', false, false), true);
  assert.equal(sameControlField('sendDisabled', false, true), false);
});
