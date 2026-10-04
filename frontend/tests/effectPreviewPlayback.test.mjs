import assert from 'node:assert/strict';
import test from 'node:test';
import { playEffectPreviewFrames } from '../dist-test/domain/effectPreviewPlayback.js';

function clock() {
  const tasks = [];
  return {
    schedule(callback, delay) {
      const task = { callback, delay, cancelled: false };
      tasks.push(task);
      return () => { task.cancelled = true; };
    },
    tick() {
      const task = tasks.shift();
      if (task && !task.cancelled) task.callback();
      return task;
    },
  };
}

test('preview finishes only after the last frame has been displayed for a full step', () => {
  const timer = clock();
  const frames = [];
  let finished = 0;
  playEffectPreviewFrames(2, 100, (index) => frames.push(index), () => finished++, timer.schedule);
  assert.deepEqual(frames, [0]);
  assert.equal(timer.tick().delay, 100);
  assert.deepEqual(frames, [0, 1]);
  assert.equal(finished, 0);
  timer.tick();
  assert.equal(finished, 1);
  assert.equal(timer.tick(), undefined);
});

test('closing or changing a preview cancels its remaining frames and completion callback', () => {
  const timer = clock();
  const frames = [];
  let finished = false;
  const stop = playEffectPreviewFrames(3, 50, (index) => frames.push(index), () => { finished = true; }, timer.schedule);
  stop();
  timer.tick();
  assert.deepEqual(frames, [0]);
  assert.equal(finished, false);
});

test('replay starts at frame zero without continuing the previous clip', () => {
  const timer = clock();
  const frames = [];
  const first = playEffectPreviewFrames(2, 50, (index) => frames.push(['first', index]), () => {}, timer.schedule);
  first();
  playEffectPreviewFrames(2, 50, (index) => frames.push(['replay', index]), () => {}, timer.schedule);
  timer.tick();
  timer.tick();
  assert.deepEqual(frames, [['first', 0], ['replay', 0], ['replay', 1]]);
});

test('cleanup also cancels pending completion after the final frame', () => {
  const timer = clock();
  let finished = false;
  const stop = playEffectPreviewFrames(1, 50, () => {}, () => { finished = true; }, timer.schedule);
  stop();
  timer.tick();
  assert.equal(finished, false);
});
