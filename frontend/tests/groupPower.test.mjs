import test from 'node:test';
import assert from 'node:assert/strict';
import { prepareLightPowerUpdates, prepareDeviceUpdate } from '../dist-test/domain/commands.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';

const lamp = { serial: 'lamp', kind: DeviceKind.Single, on: true, brightness: 0.6, color: { h: 30, s: 1, l: 0.6 } };
const ceiling = { serial: 'ceiling', kind: DeviceKind.Matrix, on: false, brightness: 0.4, chain: [{ pixels: [{ h: 200, s: 1, l: 0.4 }] }], lightParts: [{ part: 'main' }, { part: 'uplight' }], lightPartStates: { all: { known: true, on: false, devicePoweredOn: false, brightness: 0.4 } } };
const switchDevice = { serial: 'switch', kind: DeviceKind.Switch, on: true };

test('mixed group off sends power-only intent even to an already-off multipart light', () => {
  const devices = [lamp, ceiling, switchDevice];
  const commands = prepareLightPowerUpdates(devices, false);
  assert.deepEqual(commands.map((command) => command.device.serial), ['lamp', 'ceiling']);
  for (const command of commands) {
    const previous = devices.find((device) => device.serial === command.device.serial);
    assert.equal(command.device.on, false);
    assert.equal(prepareDeviceUpdate(command.device, previous, command.intent).intent, 'power');
    assert.equal(command.device.brightness, previous.brightness);
    assert.deepEqual(command.device.chain, previous.chain);
    assert.deepEqual(command.device.color, previous.color);
    assert.deepEqual(command.device.lightPartStates, previous.lightPartStates);
  }
  assert.equal(lamp.on, true);
  assert.equal(ceiling.on, false);
});

test('batch on and repeated off retain explicit power intent and ignore switches', () => {
  for (const on of [true, false]) {
    const commands = prepareLightPowerUpdates([{ ...lamp, on }, { ...ceiling, on }, switchDevice], on);
    assert.equal(commands.length, 2);
    assert.ok(commands.every((command) => command.intent === 'power' && command.device.on === on));
  }
  assert.deepEqual(prepareLightPowerUpdates([switchDevice], false), []);
});
