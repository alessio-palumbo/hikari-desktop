import test from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_FLOOR_ID,
  addFloorToLocation,
  addRoomToFloor,
  createFloorPlanFloor,
  createRectangleRoom,
  emptyFloorPlanPreferences,
  ensureLocationFloorPlan,
  loadFloorPlanPreferences,
  normalizeFloorPlanPreferences,
  parseFloorPlanPreferences,
  placeDeviceOnFloor,
  removeFloorFromLocation,
  removeRoomFromFloor,
  saveFloorPlanPreferences,
  setActiveFloor,
  updateFloorLabel,
  updateRoomInFloor,
} from '../dist-test/domain/floorPlan.js';

test('creates a default floor for a location without existing layout', () => {
  const got = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');

  assert.equal(got.locations.home.activeFloorId, DEFAULT_FLOOR_ID);
  assert.equal(got.locations.home.floors.length, 1);
  assert.equal(got.locations.home.floors[0].label, 'Ground');
});

test('preserves existing multi-floor location layout', () => {
  const prefs = {
    version: 1,
    locations: {
      home: {
        activeFloorId: 'upstairs',
        floors: [
          createFloorPlanFloor('ground', 'Ground'),
          createFloorPlanFloor('upstairs', 'Upstairs'),
        ],
      },
    },
  };

  const got = ensureLocationFloorPlan(prefs, 'home');

  assert.equal(got.locations.home.activeFloorId, 'upstairs');
  assert.equal(got.locations.home.floors.length, 2);
});

test('creates rectangle rooms as normalized polygon points', () => {
  const room = createRectangleRoom('living', ' Living Room ', 'living', { x: 0.8, y: -0.2 }, { x: 0.5, y: 0.5 });

  assert.equal(room.label, 'Living Room');
  assert.equal(room.type, 'living');
  assert.deepEqual(room.points, [
    { x: 0.8, y: 0 },
    { x: 1, y: 0 },
    { x: 1, y: 0.3 },
    { x: 0.8, y: 0.3 },
  ]);
});

test('normalizes invalid persisted values defensively', () => {
  const got = normalizeFloorPlanPreferences({
    version: 1,
    locations: {
      home: {
        activeFloorId: 'missing',
        floors: [{
          id: ' ground ',
          label: '',
          rooms: [
            { id: 'bed', label: 'Bedroom', type: 'bedroom', points: [{ x: -1, y: 0 }, { x: 2, y: 0.2 }, { x: 0.5, y: 2 }] },
            { id: 'bad', label: 'Bad', points: [{ x: 0, y: 0 }] },
          ],
          devices: {
            ' serial ': { x: 2, y: -1, roomId: ' bed ' },
            blank: { x: Number.NaN, y: 0.4 },
          },
        }],
      },
    },
  });

  const floor = got.locations.home.floors[0];
  assert.equal(got.locations.home.activeFloorId, 'ground');
  assert.equal(floor.label, 'Floor');
  assert.equal(floor.rooms.length, 1);
  assert.deepEqual(floor.rooms[0].points, [{ x: 0, y: 0 }, { x: 1, y: 0.2 }, { x: 0.5, y: 1 }]);
  assert.deepEqual(floor.devices.serial, { x: 1, y: 0, roomId: 'bed' });
  assert.deepEqual(floor.devices.blank, { x: 0, y: 0.4 });
});

test('places a device immutably on the selected floor', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const got = placeDeviceOnFloor(prefs, 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.25, y: 0.75, roomId: 'living' });

  assert.equal(prefs.locations.home.floors[0].devices.d073d5000001, undefined);
  assert.deepEqual(got.locations.home.floors[0].devices.d073d5000001, { x: 0.25, y: 0.75, roomId: 'living' });
});

test('moving a device to another floor removes the previous placement', () => {
  const prefs = {
    version: 1,
    locations: {
      home: {
        activeFloorId: 'ground',
        floors: [
          { ...createFloorPlanFloor('ground', 'Ground'), devices: { d073d5000001: { x: 0.1, y: 0.1 } } },
          createFloorPlanFloor('upstairs', 'Upstairs'),
        ],
      },
    },
  };

  const got = placeDeviceOnFloor(prefs, 'home', 'upstairs', 'd073d5000001', { x: 0.8, y: 0.6 });

  assert.equal(got.locations.home.floors[0].devices.d073d5000001, undefined);
  assert.deepEqual(got.locations.home.floors[1].devices.d073d5000001, { x: 0.8, y: 0.6 });
});

test('adds a room to a floor without mutating existing layout', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });

  const got = addRoomToFloor(prefs, 'home', DEFAULT_FLOOR_ID, room);

  assert.equal(prefs.locations.home.floors[0].rooms.length, 0);
  assert.deepEqual(got.locations.home.floors[0].rooms, [room]);
});

test('adds, activates, renames, and removes floors', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const withFloor = addFloorToLocation(prefs, 'home', createFloorPlanFloor('upstairs', 'Upstairs'));
  const activated = setActiveFloor(withFloor, 'home', DEFAULT_FLOOR_ID);
  const renamed = updateFloorLabel(activated, 'home', DEFAULT_FLOOR_ID, 'Main Floor');
  const removed = removeFloorFromLocation(renamed, 'home', DEFAULT_FLOOR_ID);

  assert.equal(withFloor.locations.home.activeFloorId, 'upstairs');
  assert.equal(withFloor.locations.home.floors.length, 2);
  assert.equal(activated.locations.home.activeFloorId, DEFAULT_FLOOR_ID);
  assert.equal(renamed.locations.home.floors[0].label, 'Main Floor');
  assert.equal(removed.locations.home.floors.length, 1);
  assert.equal(removed.locations.home.activeFloorId, 'upstairs');
});

test('does not remove the last floor', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const got = removeFloorFromLocation(prefs, 'home', DEFAULT_FLOOR_ID);

  assert.equal(got.locations.home.floors.length, 1);
  assert.equal(got.locations.home.activeFloorId, DEFAULT_FLOOR_ID);
});

test('removing the active floor selects the previous floor', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const withFloor1 = addFloorToLocation(prefs, 'home', createFloorPlanFloor('floor-1', 'Floor 1'));
  const withFloor2 = addFloorToLocation(withFloor1, 'home', createFloorPlanFloor('floor-2', 'Floor 2'));
  const removed = removeFloorFromLocation(withFloor2, 'home', 'floor-2');

  assert.equal(withFloor2.locations.home.activeFloorId, 'floor-2');
  assert.equal(removed.locations.home.activeFloorId, 'floor-1');
});

test('updates and moves rooms', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });
  const withRoom = addRoomToFloor(prefs, 'home', DEFAULT_FLOOR_ID, room);
  const movedPoints = [
    { x: 0.2, y: 0.25 },
    { x: 0.6, y: 0.25 },
    { x: 0.6, y: 0.55 },
    { x: 0.2, y: 0.55 },
  ];
  const updated = updateRoomInFloor(withRoom, 'home', DEFAULT_FLOOR_ID, 'living', { label: 'Lounge', type: 'other', points: movedPoints });

  assert.equal(updated.locations.home.floors[0].rooms[0].label, 'Lounge');
  assert.equal(updated.locations.home.floors[0].rooms[0].type, 'other');
  assert.deepEqual(updated.locations.home.floors[0].rooms[0].points, movedPoints);
});

test('moving rooms carries assigned devices', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });
  const withRoom = addRoomToFloor(prefs, 'home', DEFAULT_FLOOR_ID, room);
  const withDevices = placeDeviceOnFloor(withRoom, 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.2, y: 0.3, roomId: 'living' });
  const moved = updateRoomInFloor(withDevices, 'home', DEFAULT_FLOOR_ID, 'living', {
    points: room.points.map((point) => ({ x: point.x + 0.1, y: point.y + 0.2 })),
  });

  const placement = moved.locations.home.floors[0].devices.d073d5000001;
  assert.equal(placement.roomId, 'living');
  assert.ok(Math.abs(placement.x - 0.3) < 0.000001);
  assert.ok(Math.abs(placement.y - 0.5) < 0.000001);
});

test('moving rooms can set exact assigned device placements', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });
  const withRoom = addRoomToFloor(prefs, 'home', DEFAULT_FLOOR_ID, room);
  const withDevices = placeDeviceOnFloor(withRoom, 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.2, y: 0.3, roomId: 'living' });
  const moved = updateRoomInFloor(withDevices, 'home', DEFAULT_FLOOR_ID, 'living', {
    points: room.points.map((point) => ({ x: point.x + 0.1, y: point.y + 0.2 })),
    devices: {
      d073d5000001: { x: 0.300004, y: 0.500006, roomId: 'living' },
    },
  });

  assert.deepEqual(moved.locations.home.floors[0].devices.d073d5000001, { x: 0.300004, y: 0.500006, roomId: 'living' });
});

test('removing rooms unassigns devices from the floor', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });
  const withRoom = addRoomToFloor(prefs, 'home', DEFAULT_FLOOR_ID, room);
  const withDevice = placeDeviceOnFloor(withRoom, 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.2, y: 0.3, roomId: 'living' });
  const removed = removeRoomFromFloor(withDevice, 'home', DEFAULT_FLOOR_ID, 'living');

  assert.equal(removed.locations.home.floors[0].rooms.length, 0);
  assert.equal(removed.locations.home.floors[0].devices.d073d5000001, undefined);
});

test('loads and saves preferences through a storage boundary', () => {
  const writes = new Map();
  const storage = {
    getItem: (key) => writes.get(key) ?? null,
    setItem: (key, value) => writes.set(key, value),
  };
  const prefs = placeDeviceOnFloor(emptyFloorPlanPreferences(), 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.25, y: 0.75 });

  saveFloorPlanPreferences(storage, prefs, 'floor-test');
  const got = loadFloorPlanPreferences(storage, 'floor-test');

  assert.deepEqual(got, prefs);
});

test('falls back to empty preferences for missing or incompatible data', () => {
  assert.deepEqual(parseFloorPlanPreferences(null), emptyFloorPlanPreferences());
  assert.deepEqual(parseFloorPlanPreferences('{"version":2,"locations":{}}'), emptyFloorPlanPreferences());
});
