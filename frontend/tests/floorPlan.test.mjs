import test from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_FLOOR_ID,
  addFloorToLocation,
  addRoomToFloor,
  bringRoomToFront,
  createFloorPlanFloor,
  createRectangleRoom,
  emptyFloorPlanPreferences,
  ensureLocationFloorPlan,
  loadFloorPlanPreferences,
  moveRoomEdge,
  normalizeFloorPlanPreferences,
  parseFloorPlanPreferences,
  placeDeviceOnFloor,
  pointInRoom,
  removeDeviceFromFloorPlan,
  removeFloorFromLocation,
  removeRoomFromFloor,
  roomAtPoint,
  roomInteriorPoint,
  saveFloorPlanPreferences,
  setActiveFloor,
  updateFloorLabel,
  updateRoomInFloor,
  keepRoomDevicesInsideShape,
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

test('moves a room edge perpendicular to the edge', () => {
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });

  const points = moveRoomEdge(room, 0, { x: 0.2, y: -0.1 });

  assert.deepEqual(points, [
    { x: 0.1, y: 0.1 },
    { x: 0.5, y: 0.1 },
    { x: 0.5, y: 0.5 },
    { x: 0.1, y: 0.5 },
  ]);
});

test('clamps a dragged room edge to the floor boundary', () => {
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.2 }, { x: 0.4, y: 0.3 });

  const points = moveRoomEdge(room, 3, { x: -0.5, y: 0.2 });

  assert.equal(points[0].x, 0);
  assert.equal(points[3].x, 0);
  assert.equal(points[0].y, 0.2);
  assert.equal(points[3].y, 0.5);
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
            orphaned: { x: 0.3, y: 0.6, roomId: 'missing-room' },
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
  assert.deepEqual(floor.devices.orphaned, { x: 0.3, y: 0.6 });
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

test('removing a device from the floor plan makes it unassigned', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const placed = placeDeviceOnFloor(prefs, 'home', DEFAULT_FLOOR_ID, 'd073d5000001', { x: 0.25, y: 0.75 });
  const removed = removeDeviceFromFloorPlan(placed, 'home', 'd073d5000001');

  assert.equal(placed.locations.home.floors[0].devices.d073d5000001.x, 0.25);
  assert.equal(removed.locations.home.floors[0].devices.d073d5000001, undefined);
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

test('brings a room to the front of the layer stack', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const lower = createRectangleRoom('lower', 'Lower', 'living', { x: 0.1, y: 0.1 }, { x: 0.3, y: 0.3 });
  const middle = createRectangleRoom('middle', 'Middle', 'office', { x: 0.2, y: 0.2 }, { x: 0.3, y: 0.3 });
  const upper = createRectangleRoom('upper', 'Upper', 'kitchen', { x: 0.3, y: 0.3 }, { x: 0.3, y: 0.3 });
  const withRooms = [lower, middle, upper].reduce((current, room) => addRoomToFloor(current, 'home', DEFAULT_FLOOR_ID, room), prefs);

  const got = bringRoomToFront(withRooms, 'home', DEFAULT_FLOOR_ID, 'middle');
  assert.deepEqual(got.locations.home.floors[0].rooms.map((room) => room.id), ['lower', 'upper', 'middle']);
});

test('keeps room layer unchanged when already frontmost', () => {
  const prefs = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'home');
  const lower = createRectangleRoom('lower', 'Lower', 'living', { x: 0.1, y: 0.1 }, { x: 0.3, y: 0.3 });
  const upper = createRectangleRoom('upper', 'Upper', 'kitchen', { x: 0.3, y: 0.3 }, { x: 0.3, y: 0.3 });
  const withRooms = [lower, upper].reduce((current, room) => addRoomToFloor(current, 'home', DEFAULT_FLOOR_ID, room), prefs);

  const got = bringRoomToFront(withRooms, 'home', DEFAULT_FLOOR_ID, 'upper');

  assert.deepEqual(got.locations.home.floors[0].rooms.map((room) => room.id), ['lower', 'upper']);
});

test('finds rooms using polygon hit testing', () => {
  const living = createRectangleRoom('living', 'Living Room', 'living', { x: 0.1, y: 0.1 }, { x: 0.3, y: 0.3 });
  const kitchen = createRectangleRoom('kitchen', 'Kitchen', 'kitchen', { x: 0.5, y: 0.1 }, { x: 0.3, y: 0.3 });

  assert.equal(roomAtPoint([living, kitchen], { x: 0.2, y: 0.2 })?.id, 'living');
  assert.equal(roomAtPoint([living, kitchen], { x: 0.6, y: 0.2 })?.id, 'kitchen');
  assert.equal(roomAtPoint([living, kitchen], { x: 0.9, y: 0.9 }), undefined);
});

test('selects the topmost room when room polygons overlap', () => {
  const lower = createRectangleRoom('lower', 'Lower', 'living', { x: 0.1, y: 0.1 }, { x: 0.5, y: 0.5 });
  const upper = createRectangleRoom('upper', 'Upper', 'office', { x: 0.3, y: 0.3 }, { x: 0.5, y: 0.5 });

  assert.equal(roomAtPoint([lower, upper], { x: 0.4, y: 0.4 })?.id, 'upper');
});

test('detects points inside shaped rooms', () => {
  const room = {
    id: 'l-room',
    label: 'L Room',
    points: [
      { x: 0.1, y: 0.1 },
      { x: 0.7, y: 0.1 },
      { x: 0.7, y: 0.3 },
      { x: 0.3, y: 0.3 },
      { x: 0.3, y: 0.7 },
      { x: 0.1, y: 0.7 },
    ],
  };

  assert.equal(pointInRoom({ x: 0.2, y: 0.2 }, room), true);
  assert.equal(pointInRoom({ x: 0.2, y: 0.6 }, room), true);
  assert.equal(pointInRoom({ x: 0.55, y: 0.55 }, room), false);
});

test('keeps room devices inside a shaped room without moving valid placements', () => {
  const room = {
    id: 'l-room',
    label: 'L Room',
    points: [
      { x: 0.1, y: 0.1 },
      { x: 0.7, y: 0.1 },
      { x: 0.7, y: 0.3 },
      { x: 0.3, y: 0.3 },
      { x: 0.3, y: 0.7 },
      { x: 0.1, y: 0.7 },
    ],
  };
  const devices = {
    inside: { x: 0.2, y: 0.2, roomId: 'l-room' },
    escaped: { x: 0.55, y: 0.55, roomId: 'l-room' },
  };

  const got = keepRoomDevicesInsideShape(room, devices);

  assert.deepEqual(got.inside, devices.inside);
  assert.equal(got.escaped.roomId, 'l-room');
  assert.equal(pointInRoom(got.escaped, room), true);
});

test('returns an interior room point near the requested point', () => {
  const room = createRectangleRoom('living', 'Living Room', 'living', { x: 0.2, y: 0.2 }, { x: 0.4, y: 0.4 });
  const got = roomInteriorPoint(room, { x: 0.95, y: 0.95 });

  assert.equal(pointInRoom(got, room), true);
  assert.ok(got.x > 0.4);
  assert.ok(got.y > 0.4);
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
