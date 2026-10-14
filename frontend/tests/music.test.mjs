import assert from 'node:assert/strict';
import test from 'node:test';
import { musicActive, musicAutomationDevices, newerMusicStatus } from '../dist-test/domain/music.js';
import { partitionDimOwnership, preserveDimOwnership } from '../dist-test/domain/presenceLighting.js';

test('Music reserves targets during startup, playback and restoration only', () => {
  const devices = [{serial:'a'},{serial:'b'}];
  for (const state of ['starting','running','stopping']) {
    assert.equal(musicActive({state,serials:['a']}),true);
    assert.deepEqual(musicAutomationDevices(devices,{state,serials:['a']}),[devices[1]]);
  }
  for (const state of ['idle','stopped','error']) assert.deepEqual(musicAutomationDevices(devices,{state,serials:['a']}),devices);
});
test('old Music events and initial snapshots cannot overwrite a pending start', () => {
  const current={state:'starting',serials:['a'],runId:'new',revision:4};
  assert.equal(newerMusicStatus(current,{state:'stopped',serials:[],revision:3}),current);
  assert.equal(newerMusicStatus(current,{state:'idle',serials:[],revision:4}),current);
  assert.equal(newerMusicStatus(current,{state:'running',serials:['a'],runId:'new',revision:5}).state,'running');
});
test('dim restoration defers Music targets without losing per-device ownership', () => {
  const ownership={a:{previousBrightness:.8,appliedBrightness:.1},b:{previousBrightness:.4,appliedBrightness:.1}};
  const {ready,pending}=partitionDimOwnership(ownership,new Set(['a']));
  assert.deepEqual(ready,{b:ownership.b});
  assert.deepEqual(pending,{a:ownership.a});
  assert.equal(Object.keys(ownership).length,2);
});
test('resuming dim preserves earlier brightness only for still-owned states', () => {
  const previous={a:{previousBrightness:.8,appliedBrightness:.1},b:{previousBrightness:.4,appliedBrightness:.1}};
  const next={a:{previousBrightness:.1,appliedBrightness:.1},b:{previousBrightness:.3,appliedBrightness:.1}};
  const result=preserveDimOwnership(previous,next,[{serial:'a',on:true,brightness:.1},{serial:'b',on:true,brightness:.3}]);
  assert.equal(result.a.previousBrightness,.8);
  assert.equal(result.b.previousBrightness,.3);
});
