import assert from 'node:assert/strict';
import test from 'node:test';
import { themeRequest, themes } from '../dist-test/domain/themes.js';
import { compatibleInspectorMode } from '../dist-test/domain/inspectorMode.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';

test('theme targets are unique online lights in stable serial order', () => {
  const device = (serial, kind = DeviceKind.Single, online = true) => ({ serial, kind, online });
  const request = themeRequest(themes[0], [device('b'), device('a', DeviceKind.Matrix), device('c', DeviceKind.Switch), device('d', DeviceKind.Multizone, false), device('a')]);
  assert.deepEqual(request.serials, ['a', 'b']);
  assert.equal(request.brightness, 'preserve');
  assert.equal(request.theme, themes[0]);
});

test('empty inventory produces no theme targets', () => {
  assert.deepEqual(themeRequest(themes[0], []).serials, []);
});

test('theme palette wire fields match native library colors and stay within ranges', () => {
  for (const theme of themes) {
    const wire = JSON.parse(JSON.stringify(theme));
    assert.ok(wire.palette.Base.length >= 2);
    for (const color of wire.palette.Base) {
      assert.deepEqual(Object.keys(color).sort(), ['Brightness', 'Hue', 'Kelvin', 'Saturation']);
      assert.ok(color.Hue >= 0 && color.Hue < 360);
      assert.ok(color.Saturation >= 0 && color.Saturation <= 100);
      assert.equal(color.Brightness, 100);
      assert.ok(color.Kelvin >= 1500 && color.Kelvin <= 9000);
    }
  }
});

test('warm white theme contains only Kelvin colors', () => {
  const theme = themes.find((entry) => entry.name === 'Warm whites');
  assert.ok(theme.palette.Base.every((color) => color.Saturation === 0));
  assert.equal(new Set(theme.palette.Base.map((color) => color.Kelvin)).size, 3);
});

test('themes tab survives switching between every light kind and white-only lights', () => {
  for (const kind of Object.values(DeviceKind)) {
    assert.equal(compatibleInspectorMode('themes', { kind, capability: { hasColor: false } }), 'themes');
  }
});
