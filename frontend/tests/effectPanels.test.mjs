import assert from 'node:assert/strict';
import test from 'node:test';
import { toggleEffectPanel } from '../dist-test/domain/effects.js';

test('opening settings on another effect closes the previous preview', () => {
  assert.deepEqual(toggleEffectPanel({ preview: 'snake' }, 'settings', 'worm'), { preview: undefined, settings: 'worm' });
});

test('opening preview on another effect closes the previous settings', () => {
  assert.deepEqual(toggleEffectPanel({ settings: 'snake' }, 'preview', 'worm'), { preview: 'worm', settings: undefined });
});

test('preview and settings can stay open together on the same effect', () => {
  const both = toggleEffectPanel({ preview: 'scanner' }, 'settings', 'scanner');
  assert.deepEqual(both, { preview: 'scanner', settings: 'scanner' });
  assert.deepEqual(toggleEffectPanel(both, 'settings', 'scanner'), { preview: 'scanner', settings: undefined });
  assert.deepEqual(toggleEffectPanel(both, 'preview', 'scanner'), { preview: undefined, settings: 'scanner' });
});

test('switching to another row closes both previous panels', () => {
  const both = { preview: 'scanner', settings: 'scanner' };
  assert.deepEqual(toggleEffectPanel(both, 'preview', 'comet'), { preview: 'comet', settings: undefined });
  assert.deepEqual(toggleEffectPanel(both, 'settings', 'comet'), { preview: undefined, settings: 'comet' });
});
