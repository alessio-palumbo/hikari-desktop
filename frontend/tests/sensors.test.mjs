import assert from 'node:assert/strict';
import test from 'node:test';
import { presenceReading, sensorSignalReading } from '../dist-test/domain/sensors.js';

const sensor = (overrides = {}) => ({
  id: 'sensor-1',
  name: 'Sensor',
  capabilities: ['presence', 'target_count'],
  online: true,
  presenceKnown: true,
  present: true,
  targetCount: { known: true, value: 1 },
  ...overrides,
});

test('presence reading includes observed target counts', () => {
  assert.equal(presenceReading(sensor({ targetCount: { known: true, value: 1 } })), 'occupied (1)');
  assert.equal(presenceReading(sensor({ targetCount: { known: true, value: 2 } })), 'occupied (2)');
});

test('presence reading marks an advertised tracking limit as saturated', () => {
  assert.equal(presenceReading(sensor({ targetCount: { known: true, value: 3, max: 3 } })), 'occupied (3+)');
  assert.equal(presenceReading(sensor({ targetCount: { known: true, value: 10, max: 10 } })), 'occupied (10+)');
});

test('presence reading omits counts for clear and unsupported observations', () => {
  assert.equal(presenceReading(sensor({ present: false, targetCount: { known: true, value: 0, max: 3 } })), 'clear');
  assert.equal(presenceReading(sensor({ capabilities: ['presence'], targetCount: undefined })), 'occupied');
  assert.equal(presenceReading(sensor({ targetCount: { known: true, value: 2 } })), 'occupied (2)');
});

test('sensor signal is live and unknown while unavailable', () => {
  assert.equal(sensorSignalReading(sensor({ rssiDbm: -58 })), '-58 dBm');
  assert.equal(sensorSignalReading(sensor({ online: false, rssiDbm: -58 })), 'unknown');
  assert.equal(sensorSignalReading(sensor({ rssiDbm: undefined })), 'unknown');
});
