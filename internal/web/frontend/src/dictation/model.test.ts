import test from 'node:test';
import assert from 'node:assert/strict';
import {quietEnough, rms, resample} from './whistle.ts';

test('silence ends dictation sooner once something was said', () => {
  const silence = {level: 0.01, afterSpeechSeconds: 4, withoutSpeechSeconds: 12};
  const state = {heardSpeech: false, lastLoudAt: 0, startedAt: 0};
  assert.equal(quietEnough(state, 0, 11_000, silence), false, 'quiet start waits the long limit');
  assert.equal(quietEnough(state, 0, 12_000, silence), true, 'a quiet start ends after the long limit');
  const spoken = {heardSpeech: false, lastLoudAt: 0, startedAt: 0};
  assert.equal(quietEnough(spoken, 0.2, 1_000, silence), false, 'speech keeps going');
  assert.equal(spoken.heardSpeech && spoken.lastLoudAt === 1_000, true);
  assert.equal(quietEnough(spoken, 0, 4_900, silence), false, 'a short pause keeps going');
  assert.equal(quietEnough(spoken, 0, 5_000, silence), true, 'four seconds of quiet after speech ends it');
});
test('level and resampling helpers', () => {
  assert.equal(rms(new Float32Array([0, 0, 0])), 0);
  assert.ok(Math.abs(rms(new Float32Array([0.5, -0.5])) - 0.5) < 1e-6);
  assert.equal(resample(new Float32Array([1, 1, 1, 1]), 32000).length, 2);
  assert.equal(resample(new Float32Array(16000), 16000).length, 16000);
});
