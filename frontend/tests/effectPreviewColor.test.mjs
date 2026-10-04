import assert from 'node:assert/strict';
import test from 'node:test';
import { effectPreviewColor } from '../dist-test/domain/lifx.js';

const channels = (color) => effectPreviewColor(color).match(/\d+/g).map(Number);

test('effect backgrounds remain distinguishable below the thumbnail brightness floor', () => {
  const dim = channels({ h: 0, s: 1, l: 0.02 });
  const brighter = channels({ h: 0, s: 1, l: 0.08 });
  assert.ok(brighter[0] > dim[0]);
  assert.deepEqual(dim.slice(1), [0, 0]);
});

test('effect peak brightness changes luminance without whitening saturated colours', () => {
  const base = channels({ h: 240, s: 1, l: 0.4 });
  const peak = channels({ h: 240, s: 1, l: 0.6 });
  assert.ok(peak[2] > base[2]);
  assert.deepEqual(peak.slice(0, 2), [0, 0]);
  assert.equal(effectPreviewColor({ h: 240, s: 1, l: 1.5 }), effectPreviewColor({ h: 240, s: 1, l: 1 }));
});

test('white effect frames preserve Kelvin tint and respond to brightness', () => {
  const dim = channels({ h: 120, s: 0, l: 0.1, kelvin: 2000 });
  const bright = channels({ h: 120, s: 0, l: 0.8, kelvin: 2000 });
  assert.ok(bright.every((channel, index) => channel > dim[index]));
  assert.ok(bright[0] > bright[2]);
  assert.equal(effectPreviewColor({ h: 120, s: 0, l: 0, kelvin: 2000 }), 'rgb(0 0 0)');
});
