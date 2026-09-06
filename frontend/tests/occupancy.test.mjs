import assert from 'node:assert/strict';
import test from 'node:test';
import { initialRoomOccupancyState, reconcileRoomOccupancy } from '../dist-test/domain/occupancy.js';

const config = { sensorIds: ['sensor-1'], lightingEnabled: true, offDelaySeconds: 30 };
const node = (present, online = true, presenceKnown = true, id = 'sensor-1') => ({
  id, name: id, capabilities: ['presence'], online, presenceKnown, present,
});

test('presence enters occupied once and duplicate observations issue no command', () => {
  const first = reconcileRoomOccupancy(initialRoomOccupancyState(), config, [node(true)], 1000);
  assert.equal(first.state.phase, 'occupied');
  assert.equal(first.command, 'on');

  const duplicate = reconcileRoomOccupancy(first.state, config, [node(true)], 2000);
  assert.equal(duplicate.state.phase, 'occupied');
  assert.equal(duplicate.command, undefined);
});

test('absence starts pending off and expiry turns lights off once', () => {
  const occupied = reconcileRoomOccupancy(initialRoomOccupancyState(), config, [node(true)], 0).state;
  const pending = reconcileRoomOccupancy(occupied, config, [node(false)], 1000);
  assert.equal(pending.state.phase, 'pending-off');
  assert.equal(pending.state.pendingUntil, 31000);
  assert.equal(pending.command, undefined);

  const expired = reconcileRoomOccupancy(pending.state, config, [node(false)], 31000);
  assert.equal(expired.state.phase, 'unoccupied');
  assert.equal(expired.command, 'off');

  const duplicate = reconcileRoomOccupancy(expired.state, config, [node(false)], 32000);
  assert.equal(duplicate.command, undefined);
});

test('presence during pending off cancels the delay without another on command', () => {
  const occupied = reconcileRoomOccupancy(initialRoomOccupancyState(), config, [node(true)], 0).state;
  const pending = reconcileRoomOccupancy(occupied, config, [node(false)], 1000).state;
  const returned = reconcileRoomOccupancy(pending, config, [node(true)], 2000);

  assert.deepEqual(returned.state, { phase: 'occupied', lightingEnabled: true });
  assert.equal(returned.command, undefined);
});

test('offline sensor is uncertainty and never expires pending off', () => {
  const occupied = reconcileRoomOccupancy(initialRoomOccupancyState(), config, [node(true)], 0).state;
  const pending = reconcileRoomOccupancy(occupied, config, [node(false)], 1000).state;
  const offline = reconcileRoomOccupancy(pending, config, [node(false, false, false)], 60000);

  assert.equal(offline.state.phase, 'occupied');
  assert.equal(offline.command, undefined);
});

test('multiple sensors use OR semantics and require all sensors for confident absence', () => {
  const multi = { ...config, sensorIds: ['sensor-1', 'sensor-2'] };
  const occupied = reconcileRoomOccupancy(initialRoomOccupancyState(), multi, [node(false), node(true, true, true, 'sensor-2')], 0);
  assert.equal(occupied.state.phase, 'occupied');
  assert.equal(occupied.command, 'on');

  const uncertain = reconcileRoomOccupancy(occupied.state, multi, [node(false), node(false, false, false, 'sensor-2')], 1000);
  assert.equal(uncertain.state.phase, 'occupied');

  const pending = reconcileRoomOccupancy(uncertain.state, multi, [node(false), node(false, true, true, 'sensor-2')], 2000);
  assert.equal(pending.state.phase, 'pending-off');
});
