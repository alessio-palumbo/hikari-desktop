import test from 'node:test';
import assert from 'node:assert/strict';
import { canSubmitSetup, setupMetadataDefaults, setupPhaseLabel } from '../dist-test/domain/onboarding.js';

const request = { attemptId: 'one', bulbSsid: 'LIFX A19 aabbcc', ssid: 'Home', password: 'private-password', openNetwork: false };
test('setup requires an explicit light and valid Wi-Fi credentials', () => {
  assert.equal(canSubmitSetup(request), true);
  for (const change of [{ bulbSsid: '' }, { ssid: '' }, { ssid: request.bulbSsid }, { ssid: 'x'.repeat(33) }, { password: 'short' }, { password: 'x'.repeat(65) }, { openNetwork: true }]) assert.equal(canSubmitSetup({ ...request, ...change }), false);
  assert.equal(canSubmitSetup({ ...request, ssid: 'é'.repeat(17) }), false);
  assert.equal(canSubmitSetup({ ...request, openNetwork: true, password: '' }), true);
});
test('setup progress distinguishes Wi-Fi handoff, recovery and LAN verification', () => {
  assert.equal(setupPhaseLabel('join'), 'Joining the device');
  assert.equal(setupPhaseLabel('rejoin'), 'Reconnecting Wi-Fi');
  assert.equal(setupPhaseLabel('verify'), 'Waiting for LAN discovery');
  assert.equal(setupPhaseLabel('future'), 'Setting up device');
});

test('setup defaults prefer named locations and groups over Unknown and use the setup SSID for a serial label', () => {
  const device = { serial: 'd073d58274eb', name: 'd073d58274eb', groupId: 'unknown' };
  const locations = [{ id: 'unknown-location', name: 'Unknown' }, { id: 'home', name: 'Home' }, { id: 'office', name: 'Office' }];
  const groups = [{ id: 'unknown', name: 'Unknown', locationId: 'unknown-location' }, { id: 'home-unknown', name: 'Unknown', locationId: 'home' }, { id: 'desk', name: 'Desk', locationId: 'home' }];
  assert.deepEqual(setupMetadataDefaults(device, 'LIFX Candle Color 8274eb', locations, groups), { label: 'LIFX Candle Color 8274eb', locationId: 'home', groupId: 'desk' });
  assert.equal(device.name, device.serial);
});

test('setup preserves existing meaningful metadata and handles an empty inventory', () => {
  const device = { serial: 'serial', name: 'My Lamp', groupId: 'desk' };
  assert.deepEqual(setupMetadataDefaults(device, 'LIFX Candle 123456', [{ id: 'office', name: 'Office' }, { id: 'home', name: 'Home' }], [{ id: 'desk', name: 'Desk', locationId: 'home' }]), { label: 'My Lamp', locationId: 'home', groupId: 'desk' });
  assert.deepEqual(setupMetadataDefaults({ ...device, name: 'serial' }, 'LIFX Candle 123456', [], []), { label: 'LIFX Candle 123456', locationId: '', groupId: '' });
});
