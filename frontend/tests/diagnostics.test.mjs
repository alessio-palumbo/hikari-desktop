import assert from 'node:assert/strict';
import test from 'node:test';
import { deviceSignal, deviceUptime, healthDevices, lastResponseAge, signalQuality } from '../dist-test/domain/diagnostics.js';

test('uptime uses a stable baseline and formats elapsed time compactly', () => {
  const baseline = 1700000000000;
  const device = { online: true, estimatedBootedAtMs: baseline };
  assert.equal(deviceUptime(device, baseline + 30000), '<1m');
  assert.equal(deviceUptime(device, baseline + 30 * 60000), '30m');
  assert.equal(deviceUptime(device, baseline + 65 * 60000), '1h 5m');
  assert.equal(deviceUptime(device, baseline + 49 * 3600000), '2d 1h');
  assert.equal(deviceUptime(device, baseline - 1000), '<1m');
});

test('uptime remains unknown for offline devices and missing or invalid baselines', () => {
  assert.equal(deviceUptime({ online: false, estimatedBootedAtMs: 1700000000000 }), 'unknown');
  assert.equal(deviceUptime({ online: true }), 'unknown');
  assert.equal(deviceUptime({ online: true, estimatedBootedAtMs: NaN }), 'unknown');
});

test('health retains offline devices and searches across LAN membership', () => {
  const snapshot = {
    locations: [{ id: 'home', name: 'Home' }],
    groups: [{ id: 'desk', locationId: 'home', name: 'Desk' }],
    devices: [
      { serial: 'b', name: 'Bulb', model: 'LIFX', groupId: 'desk', online: true },
      { serial: 'a', name: 'Strip', model: 'Beam', groupId: 'desk', online: false },
    ],
  };
  assert.deepEqual(healthDevices(snapshot, '', 'name', false).map((d) => d.serial), ['b', 'a']);
  assert.deepEqual(healthDevices(snapshot, 'home', 'name', false).map((d) => d.serial), ['b', 'a']);
  assert.deepEqual(healthDevices(snapshot, 'beam', 'name', false).map((d) => d.serial), ['a']);
  assert.equal(healthDevices(snapshot, 'absent', 'name', false).length, 0);
});

test('signal sorting compares RSSI and SNR by quality rather than incompatible raw scales', () => {
  const snapshot = { locations: [], groups: [], devices: [
    { serial: 'a', name: 'RSSI', online: true, rssi: -50, rssiText: 'Excellent' },
    { serial: 'b', name: 'SNR', online: true, rssi: 8, rssiText: 'Poor' },
  ] };
  assert.deepEqual(healthDevices(snapshot, '', 'signal', false).map((d) => d.serial), ['b', 'a']);
  assert.equal(deviceSignal(snapshot.devices[0]), '-50 dBm');
  assert.equal(deviceSignal(snapshot.devices[1]), '8 SNR');
  assert.equal(deviceSignal({ online: false, rssi: -50 }), 'unknown');
  assert.equal(deviceSignal({ online: true, rssi: 0 }), 'unknown');
  assert.equal(signalQuality(snapshot.devices[0]), 'good');
  assert.equal(signalQuality(snapshot.devices[1]), 'poor');
  assert.equal(signalQuality({ online: true, rssi: -65, rssiText: 'Fair' }), 'fair');
  assert.equal(signalQuality({ online: false, rssi: -50, rssiText: 'Excellent' }), 'unknown');
});

test('last response age uses receipt time independently of state changes', () => {
  const now = 1700000000000;
  assert.equal(lastResponseAge({ lastSeenAtMs: now - 5000, lastStateChangeAtMs: now - 86400000 }, now), '5s');
  assert.equal(lastResponseAge({ lastSeenAtMs: now + 1000 }, now), '<1s');
  assert.equal(lastResponseAge({ lastSeenAtMs: now - 120000 }, now), '2m');
  assert.equal(lastResponseAge({}, now), 'unknown');
});
