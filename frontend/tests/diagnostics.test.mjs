import assert from 'node:assert/strict';
import test from 'node:test';
import { deviceUptime } from '../dist-test/domain/diagnostics.js';

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
