import test from 'node:test';
import assert from 'node:assert/strict';
import { supportedDeviceEffects, deviceEffectSource } from '../dist-test/domain/effects.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';
import { displayedEffectStatus } from '../dist-test/domain/effectState.js';

test('temporal effects support all lights with full-cycle speed defaults', () => {
  for (const kind of [DeviceKind.Single, DeviceKind.Multizone, DeviceKind.Matrix]) {
    const effects = supportedDeviceEffects({ kind });
    assert.equal(effects.find(effect => effect.id === 'breathe').speed.defaultMs, 4000);
    assert.equal(effects.find(effect => effect.id === 'color_cycle').speed.defaultMs, 8000);
  }
  assert.deepEqual(supportedDeviceEffects({ kind: DeviceKind.Switch }), []);
});

test('firmware observations do not clear running temporal app effects', () => {
  for (const effect of ['breathe', 'color_cycle']) {
    assert.equal(deviceEffectSource(effect), 'app');
    const running = { serial: 'd073d501a2c3', running: true, effect };
    assert.equal(displayedEffectStatus({ serial: running.serial, firmwareEffect: { running: false } }, running), running);
  }
});
