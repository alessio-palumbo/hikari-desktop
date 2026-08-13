import test from 'node:test';
import assert from 'node:assert/strict';
import { DeviceKind } from '../dist-test/domain/lifx.js';
import { applyTextCommandAction, executableTextCommandTargets } from '../dist-test/domain/textCommands.js';

const light = {
  groupId: 'desk',
  serial: 'light',
  name: 'Desk Lamp',
  model: 'A19',
  kind: DeviceKind.Single,
  online: true,
  on: false,
  brightness: 0.35,
  capability: { hasColor: true, kelvinMin: 1500, kelvinMax: 9000 },
  color: { h: 30, s: 0.4, l: 0.35, kelvin: 2700 },
  kelvin: 2700,
};

const switchDevice = {
  groupId: 'desk',
  serial: 'switch',
  name: 'Desk Switch',
  model: 'Switch',
  kind: DeviceKind.Switch,
  online: true,
  on: true,
  brightness: 0,
  capability: { hasColor: false, kelvinMin: 0, kelvinMax: 0 },
};

test('text command power-only updates only power', () => {
  const got = applyTextCommandAction({ ...light, on: true }, { power: false });

  assert.equal(got.on, false);
  assert.equal(got.brightness, light.brightness);
  assert.deepEqual(got.color, light.color);
});

test('text command brightness updates brightness and powers on when non-zero', () => {
  const got = applyTextCommandAction(light, { brightness: 65 });

  assert.equal(got.on, true);
  assert.equal(got.brightness, 0.65);
  assert.equal(got.color.l, 0.65);
  assert.equal(got.color.kelvin, 2700);
});

test('text command brightness clamps to device scale', () => {
  assert.equal(applyTextCommandAction(light, { brightness: 130 }).brightness, 1);
  assert.equal(applyTextCommandAction(light, { brightness: -10 }).brightness, 0);
});

test('text command hue and saturation clears kelvin color and powers on', () => {
  const got = applyTextCommandAction(light, { hue: 220, saturation: 80 });

  assert.equal(got.on, true);
  assert.equal(got.color.h, 220);
  assert.equal(got.color.s, 0.8);
  assert.equal(got.color.kelvin, undefined);
});

test('text command kelvin sets white color and powers on', () => {
  const got = applyTextCommandAction(light, { kelvin: 4000 });

  assert.equal(got.on, true);
  assert.equal(got.kelvin, 4000);
  assert.equal(got.color.s, 0);
  assert.equal(got.color.kelvin, 4000);
});

test('text command brightness plus color powers on and keeps selected color brightness', () => {
  const got = applyTextCommandAction(light, { hue: 120, saturation: 50, brightness: 25 });

  assert.equal(got.on, true);
  assert.equal(got.brightness, 0.25);
  assert.equal(got.color.h, 120);
  assert.equal(got.color.s, 0.5);
  assert.equal(got.color.l, 0.25);
});

test('text command target resolver returns light targets', () => {
  const got = executableTextCommandTargets([{ targets: [{ serial: 'light' }], action: { power: true } }], [light, switchDevice]);

  assert.equal(got.length, 1);
  assert.equal(got[0].serial, 'light');
});

test('text command target resolver rejects missing target before execution', () => {
  assert.throws(
    () => executableTextCommandTargets([{ targets: [{ serial: 'missing' }], action: { power: true } }], [light]),
    /Device missing is no longer available/,
  );
});

test('text command target resolver rejects switch targets before execution', () => {
  assert.throws(
    () => executableTextCommandTargets([{ targets: [{ serial: 'switch' }], action: { power: true } }], [light, switchDevice]),
    /Desk Switch is not a supported light target/,
  );
});
