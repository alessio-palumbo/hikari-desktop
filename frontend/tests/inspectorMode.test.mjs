import assert from 'node:assert/strict';
import test from 'node:test';
import { compatibleInspectorMode } from '../dist-test/domain/inspectorMode.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';

const light = (kind, hasColor = true) => ({ kind, capability: { hasColor } });

test('effects tab stays open between matrix and multizone lights', () => {
  assert.equal(compatibleInspectorMode('effects', light(DeviceKind.Matrix)), 'effects');
  assert.equal(compatibleInspectorMode('effects', light(DeviceKind.Multizone)), 'effects');
});

test('effects tab stays open for colour and white-only single-zone lights', () => {
  assert.equal(compatibleInspectorMode('effects', light(DeviceKind.Single)), 'effects');
  assert.equal(compatibleInspectorMode('effects', light(DeviceKind.Single, false)), 'effects');
});

test('white tab is retained on both colour and white-only lights', () => {
  for (const kind of [DeviceKind.Single, DeviceKind.Multizone, DeviceKind.Matrix]) {
    assert.equal(compatibleInspectorMode('white', light(kind)), 'white');
    assert.equal(compatibleInspectorMode('white', light(kind, false)), 'white');
  }
});

test('colour tab is retained when supported and falls back for white-only lights', () => {
  assert.equal(compatibleInspectorMode('color', light(DeviceKind.Matrix)), 'color');
  assert.equal(compatibleInspectorMode('color', light(DeviceKind.Single, false)), 'white');
  assert.equal(compatibleInspectorMode('color', { kind: DeviceKind.Single }), 'color');
});

test('visiting a switch does not discard the light-tab preference', () => {
  for (const mode of ['color', 'white', 'effects']) {
    assert.equal(compatibleInspectorMode(mode, { kind: DeviceKind.Switch }), mode);
  }
});
