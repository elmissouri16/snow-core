import assert from 'node:assert/strict';
import {test} from 'node:test';
import {counter, step} from './model.ts';

test('find counter and stepping wrap around and mark truncated totals', () => {
  assert.equal(counter(0, 0, false), 'No matches');
  assert.equal(counter(2, 7, false), '3 of 7');
  assert.equal(counter(0, 500, true), '1 of 500+');
  assert.equal(step(6, 7, 1), 0);
  assert.equal(step(0, 7, -1), 6);
  assert.equal(step(3, 0, 1), 0);
});
