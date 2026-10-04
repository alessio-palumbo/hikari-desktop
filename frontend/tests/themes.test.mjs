import assert from 'node:assert/strict';
import test from 'node:test';
import { themeRequest, themes, themeColors, themeEditorDraft, moveThemeColor, sortedUserThemes, themeSelectionAction } from '../dist-test/domain/themes.js';
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

test('editor copies native palette stop order without mutating built-ins', () => {
  const original = { ...themes[0], palette: { Base: [themes[0].palette.Base[0]], Accents: [themes[0].palette.Base[1]], Backgrounds: [themes[0].palette.Base[2]] } };
  const draft = themeEditorDraft(original);
  assert.equal(draft.id, undefined);
  assert.equal(draft.theme.name, 'Sunset copy');
  assert.deepEqual(draft.theme.palette.Base, themeColors(original));
  draft.theme.palette.Base[0].Hue = 99;
  assert.equal(original.palette.Base[0].Hue, 18);
});

test('editing preserves identity while duplication creates a separate draft', () => {
  const existing = themeEditorDraft(themes[0], 'user-example');
  assert.equal(existing.id, 'user-example');
  assert.equal(existing.theme.name, 'Sunset');
  assert.equal(themeEditorDraft(existing.theme).id, undefined);
  assert.equal(themeEditorDraft().theme.palette.Base.length, 2);
});

test('color ordering is immutable and invalid moves do nothing', () => {
  const draft = themeEditorDraft(themes[0]);
  const moved = moveThemeColor(draft.theme, 0, 2);
  assert.deepEqual(moved.palette.Base.map((color) => color.Hue), [340, 275, 18]);
  assert.deepEqual(draft.theme.palette.Base.map((color) => color.Hue), [18, 340, 275]);
  assert.equal(moveThemeColor(draft.theme, 0, -1), draft.theme);
  assert.equal(moveThemeColor(draft.theme, 2, 3), draft.theme);
});

test('user themes sort by name then stable identity without mutating the list', () => {
  const entries = [{ id: 'user-z', theme: { ...themes[0], name: 'Ocean' } }, { id: 'user-b', theme: { ...themes[0], name: 'amber' } }, { id: 'user-a', theme: { ...themes[0], name: 'Amber' } }];
  assert.deepEqual(sortedUserThemes(entries).map((entry) => entry.id), ['user-a', 'user-b', 'user-z']);
  assert.equal(entries[0].id, 'user-z');
});

test('theme selection applies directly unless local preview is open', () => {
  assert.equal(themeSelectionAction(false), 'apply');
  assert.equal(themeSelectionAction(true), 'preview');
});
