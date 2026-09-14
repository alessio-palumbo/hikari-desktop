import test from 'node:test';
import assert from 'node:assert/strict';

import { roomSensorState, sensorAssignmentHints } from '../dist-test/domain/sensorAssignments.js';

const room = (id, label, sensorIds = []) => ({
  id,
  label,
  points: [{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 1, y: 1 }],
  presence: { sensorIds, lightingEnabled: false, offDelaySeconds: 30, clearAction: 'off', dimBrightness: 0.1 },
});
const profile = (id, name, rooms) => ({
  id,
  name,
  locationHints: [],
  deviceSerials: [],
  layout: { activeFloorId: 'ground', floors: [{ id: 'ground', label: 'Ground', rooms, devices: {} }] },
});

test('marks a sensor assigned to another room in the current profile as movable', () => {
  const preferences = {
    version: 2,
    profiles: { office: profile('office', 'Office', [room('meeting', 'Meeting room', ['radar'])]) },
  };
  assert.deepEqual(sensorAssignmentHints(preferences, 'office', 'ground', 'desk'), {
    radar: { kind: 'move', label: 'Meeting room' },
  });
});

test('keeps assignments in another profile as contextual information', () => {
  const preferences = {
    version: 2,
    profiles: {
      home: profile('home', 'Home', [room('bedroom', 'Bedroom', ['radar'])]),
      office: profile('office', 'Office', [room('desk', 'Desk')]),
    },
  };
  assert.deepEqual(sensorAssignmentHints(preferences, 'office', 'ground', 'desk'), {
    radar: { kind: 'other-profile', label: 'Home / Bedroom' },
  });
});

test('does not label a sensor already assigned to the selected room', () => {
  const preferences = {
    version: 2,
    profiles: { office: profile('office', 'Office', [room('desk', 'Desk', ['radar'])]) },
  };
  assert.deepEqual(sensorAssignmentHints(preferences, 'office', 'ground', 'desk'), {});
});

const sensor = (overrides = {}) => ({
  id: 'radar',
  name: 'Radar',
  capabilities: ['presence'],
  online: true,
  presenceKnown: true,
  present: false,
  ...overrides,
});

test('reports the room sensor marker state from assigned sensor availability and presence', () => {
  assert.equal(roomSensorState([], [sensor()]), 'none');
  assert.equal(roomSensorState(['radar'], []), 'offline');
  assert.equal(roomSensorState(['radar'], [sensor({ online: false })]), 'offline');
  assert.equal(roomSensorState(['radar'], [sensor({ presenceKnown: false })]), 'online');
  assert.equal(roomSensorState(['radar'], [sensor()]), 'online');
  assert.equal(roomSensorState(['radar'], [sensor({ present: true })]), 'occupied');
});
