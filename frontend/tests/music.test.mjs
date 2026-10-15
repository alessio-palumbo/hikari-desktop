import assert from 'node:assert/strict';
import test from 'node:test';
import { musicActive, musicAutomationDevices, newerMusicStatus, appendMusicSignal, musicSignalLevel, musicSignalEndAt } from '../dist-test/domain/music.js';
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

test('Music signal history rejects stale snapshots, other sessions and invalid levels', () => {
  const first = {runId:'run',atMs:1000,inputDb:-30,low:.2,mid:.3,high:.4};
  const history = appendMusicSignal([],first,'run');
  assert.deepEqual(history,[first]);
  assert.equal(appendMusicSignal(history,{...first,atMs:900},'run'),history);
  assert.equal(appendMusicSignal(history,first,'run'),history);
  assert.equal(appendMusicSignal(history,{...first,runId:'other',atMs:1200},'run'),history);
  assert.equal(appendMusicSignal(history,{...first,inputDb:NaN,atMs:1200},'run'),history);
  assert.equal(appendMusicSignal(history,first,undefined),history);
});

test('Music signal history is bounded by sample count and elapsed time', () => {
  let history=[];
  for(let i=0;i<200;i++) history=appendMusicSignal(history,{runId:'run',atMs:i*100,inputDb:-30},'run');
  assert.equal(history.length,120);
  assert.equal(history[0].atMs,8000);
  history=appendMusicSignal(history,{runId:'run',atMs:40000,inputDb:-30},'run');
  assert.equal(history.length,1);
});

test('Music traces share a finite normalized scale rather than input dB', () => {
  assert.equal(musicSignalLevel(0),0);
  assert.equal(musicSignalLevel(.5),.5);
  assert.equal(musicSignalLevel(1),1);
  assert.equal(musicSignalLevel(10),1);
  assert.equal(musicSignalLevel(-100),0);
  assert.equal(musicSignalLevel(NaN),0);
});

test('Music history reveals a new segment without snapping the live edge', () => {
  assert.equal(musicSignalEndAt(1000,0,true,false),900);
  assert.equal(musicSignalEndAt(1000,50,true,false),950);
  assert.equal(musicSignalEndAt(1000,100,true,false),musicSignalEndAt(1100,0,true,false));
});

test('Music history stops advancing on stalled input and respects reduced motion', () => {
  assert.equal(musicSignalEndAt(1000,1000,true,false),1000);
  assert.equal(musicSignalEndAt(1000,-20,true,false),900);
  assert.equal(musicSignalEndAt(1000,20,false,false),1000);
  assert.equal(musicSignalEndAt(1000,20,true,true),1000);
});
