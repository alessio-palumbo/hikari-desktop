import test from 'node:test';
import assert from 'node:assert/strict';

import { displayedEffectStatus } from '../dist-test/domain/effectState.js';

const device = (firmwareEffect) => ({ serial: 'd073d501a2c3', firmwareEffect });

test('shows externally observed firmware effects with their reported speed', () => {
  assert.deepEqual(displayedEffectStatus(device({ running: true, effect: 'move', speedMs: 4000 })), {
    serial: 'd073d501a2c3',
    running: true,
    effect: 'move',
    speedMs: 4000,
    ownedByHikari: undefined,
  });
});

test('settled firmware stop observations clear optimistic firmware state', () => {
  assert.deepEqual(
    displayedEffectStatus(device({ running: false }), { serial: 'd073d501a2c3', running: true, effect: 'move' }),
    { serial: 'd073d501a2c3', running: false },
  );
});

test('firmware acknowledgement window preserves optimistic start and stop state', () => {
  const pending = device({ running: false, hikariPending: true });
  const starting = { serial: 'd073d501a2c3', running: true, effect: 'move' };
  const stopping = { serial: 'd073d501a2c3', running: false, effect: 'move' };
  assert.equal(displayedEffectStatus(pending, starting), starting);
  assert.equal(displayedEffectStatus(pending, stopping), stopping);
});

test('optimistic stop wins over the last Hikari-owned running observation', () => {
  const observed = device({ running: true, effect: 'move', ownedByHikari: true });
  const stopping = { serial: 'd073d501a2c3', running: false, effect: 'move' };
  assert.equal(displayedEffectStatus(observed, stopping), stopping);
});

test('optimistic start wins over a stale stopped observation until acknowledgement', () => {
  const observed = device({ running: false });
  const starting = { serial: 'd073d501a2c3', running: true, effect: 'move', pendingUntil: 5000 };
  assert.equal(displayedEffectStatus(observed, starting, 1000), starting);
  assert.deepEqual(displayedEffectStatus(observed, starting, 6000), {
    serial: 'd073d501a2c3',
    running: false,
  });
});

test('observed firmware off does not clear a running Hikari-rendered effect', () => {
  const running = { serial: 'd073d501a2c3', running: true, effect: 'flow' };
  assert.equal(displayedEffectStatus(device({ running: false }), running), running);
});
