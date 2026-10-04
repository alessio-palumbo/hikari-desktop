import assert from 'node:assert/strict';
import test from 'node:test';
import { deviceEffects, effectSettingsDiffer, formatEffectParameter, quantizeEffectParameter } from '../dist-test/domain/effects.js';

test('appearance settings are limited to supported configurable Hikari effects', () => {
  assert.deepEqual(deviceEffects.filter((effect) => effect.configurable).map((effect) => effect.id), ['snake', 'worm', 'wave', 'ring', 'comet', 'sparkle', 'scanner']);
  assert.ok(deviceEffects.filter((effect) => effect.configurable).every((effect) => effect.source === 'app'));
});

test('effect parameters distinguish relative percentages, cell lengths and counts', () => {
  assert.equal(formatEffectParameter(1.5, '%'), '150%');
  assert.equal(formatEffectParameter(5, 'cells'), '5 cells');
  assert.equal(formatEffectParameter(1.6, 'cells'), '1.6 cells');
  assert.equal(formatEffectParameter(3), '3');
});

test('parameter steps preserve Hikari defaults rather than offsetting from registry minimums', () => {
  const floor = { min: 0.01, max: 1, step: 0.05 };
  assert.equal(quantizeEffectParameter(0.55, floor), 0.55);
  assert.equal(quantizeEffectParameter(0.553, floor), 0.55);
  assert.equal(quantizeEffectParameter(0.01, floor), 0.01);
  assert.equal(quantizeEffectParameter(1.3, floor), 1);
  assert.equal(quantizeEffectParameter(1.55, { min: 1, max: 2, step: 0.05 }), 1.55);
  assert.equal(quantizeEffectParameter(0.18, { min: 0.01, max: 1, step: 0.01 }), 0.18);
});

test('speed-only settings become dirty until the selected speed is applied', () => {
  assert.equal(effectSettingsDiffer(4000, 4000, {}, {}), false);
  assert.equal(effectSettingsDiffer(6000, 4000, {}, {}), true);
  assert.equal(effectSettingsDiffer(6000, 6000, {}, {}), false);
});

test('speed and appearance share Apply and Reset comparisons', () => {
  const defaults = { density: 0.18, background_floor: 0.55 };
  const applied = { ...defaults, density: 0.3 };
  assert.equal(effectSettingsDiffer(2000, 2000, applied, applied), false);
  assert.equal(effectSettingsDiffer(2000, 2000, applied, defaults), true);
  assert.equal(effectSettingsDiffer(2000, 3000, defaults, applied), true);
  assert.equal(effectSettingsDiffer(2000, 2000, defaults, defaults), false);
  assert.equal(effectSettingsDiffer(2000, 2000, { ...defaults, density: 0.18000000001 }, defaults), false);
});

test('reset stays unapplied until defaults replace the successful custom settings', () => {
  const defaults = { density: 0.18, background_floor: 0.55 };
  const custom = { density: 0.4, background_floor: 0.8 };
  assert.equal(effectSettingsDiffer(4000, 4000, custom, custom), false);
  assert.equal(effectSettingsDiffer(2000, 4000, defaults, custom), true);
  assert.equal(effectSettingsDiffer(2000, 2000, defaults, defaults), false);
});

test('failed application must remain dirty against the last successful settings', () => {
  const applied = { density: 0.18 };
  const draft = { density: 0.4 };
  assert.equal(effectSettingsDiffer(3000, 2000, draft, applied), true);
  assert.equal(effectSettingsDiffer(3000, 3000, draft, draft), false);
});
