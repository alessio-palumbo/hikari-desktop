import assert from 'node:assert/strict';
import test from 'node:test';
import { planRoomDim, planRoomDimRestore } from '../dist-test/domain/presenceLighting.js';

const device = (serial, brightness, overrides = {}) => ({
  serial,
  groupId: 'room',
  name: serial,
  model: 'Light',
  kind: 'single',
  online: true,
  on: true,
  brightness,
  capability: { hasColor: true, kelvinMin: 2500, kelvinMax: 9000 },
  color: { h: 210, s: 0.8, l: brightness },
  ...overrides,
});

test('room dim snapshots each on device and leaves off devices unchanged', () => {
  const devices = [device('a', 0.8), device('b', 0.5), device('c', 0.4, { on: false })];
  const plan = planRoomDim(devices, 0.1);

  assert.deepEqual(plan.devices.map(({ serial, brightness, on }) => ({ serial, brightness, on })), [
    { serial: 'a', brightness: 0.1, on: true },
    { serial: 'b', brightness: 0.1, on: true },
  ]);
  assert.deepEqual(plan.ownership, {
    a: { previousBrightness: 0.8, appliedBrightness: 0.1 },
    b: { previousBrightness: 0.5, appliedBrightness: 0.1 },
  });
  assert.deepEqual(plan.devices[0].color, devices[0].color);
});

test('room dim restore is per-device and only restores state still owned by Hikari', () => {
  const original = [device('a', 0.8), device('b', 0.5), device('c', 0.7)];
  const plan = planRoomDim(original, 0.1);
  const current = [
    device('a', 0.105),
    device('b', 0.35),
    device('c', 0.1, { on: false }),
  ];

  const restored = planRoomDimRestore(current, plan.ownership);

  assert.deepEqual(restored.map(({ serial, brightness }) => ({ serial, brightness })), [
    { serial: 'a', brightness: 0.8 },
  ]);
});
