import test from 'node:test';
import assert from 'node:assert/strict';
import { createFloorPlanFloor, emptyFloorPlanPreferences, ensureLocationFloorPlan } from '../dist-test/domain/floorPlan.js';
import {
  createFloorPlanProfile,
  createDefaultFloorPlanLocation,
  devicesForFloorPlanProfile,
  emptyFloorPlanProfilePreferences,
  floorPlanObservation,
  floorPlanProfileMatchesObservation,
  loadFloorPlanProfilePreferences,
  normalizeFloorPlanProfilePreferences,
  observeFloorPlanProfile,
  parseFloorPlanProfilePreferences,
  resolveFloorPlanProfile,
  saveFloorPlanProfilePreferences,
  selectedFloorPlanProfileId,
  updateFloorPlanProfileLayout,
} from '../dist-test/domain/floorPlanProfiles.js';

function layout() {
  return { activeFloorId: 'ground', floors: [createFloorPlanFloor('ground', 'Ground')] };
}

function preferences(...profiles) {
  return {
    version: 2,
    profiles: Object.fromEntries(profiles.map((profile) => [profile.id, profile])),
  };
}

test('matches a floor plan by device serial after its LIFX location changes', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), {
    deviceSerials: ['luna'],
    locationIds: ['lifx-location:home'],
  });

  const got = resolveFloorPlanProfile(preferences(home), {
    deviceSerials: ['luna'],
    locationIds: ['lifx-location:office'],
  });

  assert.equal(got.kind, 'matched');
  assert.equal(got.profileId, 'home');
  assert.equal(got.candidates[0].serialMatches, 1);
});

test('device continuity outranks a location-only match', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['luna'], locationIds: [] });
  const office = createFloorPlanProfile('office', 'Office', layout(), { deviceSerials: [], locationIds: ['office-location'] });

  const got = resolveFloorPlanProfile(preferences(home, office), {
    deviceSerials: ['luna'],
    locationIds: ['office-location'],
  });

  assert.equal(got.kind, 'matched');
  assert.equal(got.profileId, 'home');
  assert.deepEqual(got.candidates.map((candidate) => [candidate.serialMatches, candidate.locationMatches]), [[1, 0], [0, 1]]);
});

test('matches a profile from partial device discovery', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), {
    deviceSerials: ['one', 'two', 'three'],
    locationIds: ['home-location'],
  });

  const got = resolveFloorPlanProfile(preferences(home), { deviceSerials: ['two'], locationIds: [] });

  assert.equal(got.kind, 'matched');
  assert.equal(got.profileId, 'home');
});

test('returns ambiguous when profiles have equal evidence', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['portable'], locationIds: [] });
  const office = createFloorPlanProfile('office', 'Office', layout(), { deviceSerials: ['portable'], locationIds: [] });

  const got = resolveFloorPlanProfile(preferences(home, office), { deviceSerials: ['portable'], locationIds: [] });

  assert.equal(got.kind, 'ambiguous');
  assert.deepEqual(got.candidates.map((candidate) => candidate.profileId), ['home', 'office']);
});

test('returns new when no profile has matching evidence', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['home-light'], locationIds: ['home-location'] });

  assert.deepEqual(
    resolveFloorPlanProfile(preferences(home), { deviceSerials: ['office-light'], locationIds: ['office-location'] }),
    { kind: 'new', candidates: [] },
  );
});

test('an explicit profile selection overrides an automatic match', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['home-light'], locationIds: [] });
  const office = createFloorPlanProfile('office', 'Office', layout(), { deviceSerials: ['office-light'], locationIds: [] });
  const current = preferences(home, office);
  const resolution = resolveFloorPlanProfile(current, { deviceSerials: ['home-light'], locationIds: [] });

  assert.equal(selectedFloorPlanProfileId(current, resolution), 'home');
  assert.equal(selectedFloorPlanProfileId(current, resolution, 'office'), 'office');
  assert.equal(selectedFloorPlanProfileId(current, resolution, 'missing'), 'home');
});

test('manual profile evidence survives incremental discovery but not another LAN', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), {
    deviceSerials: ['known'],
    locationIds: ['home-location'],
  });

  assert.equal(floorPlanProfileMatchesObservation(home, {
    deviceSerials: ['known', 'new-light'],
    locationIds: ['home-location'],
  }), true);
  assert.equal(floorPlanProfileMatchesObservation(home, {
    deviceSerials: ['office-light'],
    locationIds: ['office-location'],
  }), false);
  assert.equal(floorPlanProfileMatchesObservation(home, { deviceSerials: [], locationIds: [] }), true);
});

test('observing a confirmed profile accumulates evidence but excludes ignored devices', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['known'], locationIds: ['home-location'] });
  home.ignoredSerials = ['coworker'];

  const got = observeFloorPlanProfile(preferences(home), 'home', {
    deviceSerials: ['known', 'new-light', 'coworker'],
    locationIds: ['home-location', 'new-location'],
  });

  assert.equal(got.activeProfileId, 'home');
  assert.deepEqual(got.profiles.home.knownDeviceSerials, ['known', 'new-light']);
  assert.deepEqual(got.profiles.home.locationHints, ['home-location', 'new-location']);
});

test('observing unchanged evidence preserves preference identity', () => {
  const home = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['known'], locationIds: ['home-location'] });
  const current = { ...preferences(home), activeProfileId: 'home' };

  assert.equal(observeFloorPlanProfile(current, 'home', {
    deviceSerials: ['known'],
    locationIds: ['home-location'],
  }), current);
});

test('normalizes persisted profiles defensively', () => {
  const got = normalizeFloorPlanProfilePreferences({
    version: 2,
    activeProfileId: 'missing',
    profiles: {
      home: {
        id: ' home ',
        name: ' Home ',
        knownDeviceSerials: ['b', 'a', 'a', ''],
        locationHints: ['location'],
        ignoredSerials: ['ignored'],
        layout: layout(),
      },
      invalid: { id: 'invalid', layout: { floors: [] } },
    },
  });

  assert.equal(got.activeProfileId, undefined);
  assert.deepEqual(Object.keys(got.profiles), ['home']);
  assert.deepEqual(got.profiles.home.knownDeviceSerials, ['a', 'b']);
});

test('empty profile preferences are versioned', () => {
  assert.deepEqual(emptyFloorPlanProfilePreferences(), { version: 2, profiles: {} });
});

test('migrates a meaningful location layout into a profile', () => {
  const legacy = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'lifx-location:home');
  legacy.locations['lifx-location:home'].floors[0].rooms.push({
    id: 'living',
    label: 'Living',
    type: 'living',
    points: [{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 1, y: 1 }],
  });

  const got = parseFloorPlanProfilePreferences(JSON.stringify(legacy));
  const [profile] = Object.values(got.profiles);

  assert.equal(profile.name, 'Floor plan');
  assert.deepEqual(profile.locationHints, ['lifx-location:home']);
  assert.equal(profile.layout.floors[0].rooms[0].id, 'living');
});

test('does not migrate empty generated location layouts', () => {
  const legacy = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'lifx-location:home');

  assert.deepEqual(parseFloorPlanProfilePreferences(JSON.stringify(legacy)), emptyFloorPlanProfilePreferences());
});

test('migrates multiple meaningful location layouts as separate profiles', () => {
  const legacy = ensureLocationFloorPlan(emptyFloorPlanPreferences(), 'Home');
  legacy.locations.Home.floors[0].rooms.push({
    id: 'living', label: 'Living', type: 'living', points: [{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 1, y: 1 }],
  });
  const withOffice = ensureLocationFloorPlan(legacy, 'Office');
  withOffice.locations.Office.floors[0].rooms.push({
    id: 'desk', label: 'Desk', type: 'office', points: [{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 1, y: 1 }],
  });

  const got = parseFloorPlanProfilePreferences(JSON.stringify(withOffice));

  assert.equal(Object.keys(got.profiles).length, 2);
  assert.equal(got.activeProfileId, undefined);
  assert.deepEqual(Object.values(got.profiles).map((profile) => profile.name).sort(), ['Home', 'Office']);
});

test('loads and saves profiles through a storage boundary', () => {
  let value = null;
  const storage = {
    getItem: () => value,
    setItem: (_key, next) => { value = next; },
  };
  const profile = createFloorPlanProfile('home', 'Home', createDefaultFloorPlanLocation());
  const input = preferences(profile);

  saveFloorPlanProfilePreferences(storage, input);

  assert.deepEqual(loadFloorPlanProfilePreferences(storage), input);
});

test('observes only currently online devices and their locations', () => {
  const got = floorPlanObservation({
    locations: [{ id: 'home', name: 'Home' }, { id: 'office', name: 'Office' }],
    groups: [{ id: 'living', locationId: 'home', name: 'Living' }, { id: 'desk', locationId: 'office', name: 'Desk' }],
    devices: [
      { serial: 'online', groupId: 'living', online: true },
      { serial: 'offline', groupId: 'desk', online: false },
    ],
  });

  assert.deepEqual(got, { deviceSerials: ['online'], locationIds: ['home'] });
});

test('updates a profile layout without changing its identity evidence', () => {
  const profile = createFloorPlanProfile('home', 'Home', layout(), { deviceSerials: ['known'], locationIds: ['home-location'] });

  const got = updateFloorPlanProfileLayout(preferences(profile), 'home', (current) => ({
    ...current,
    floors: [...current.floors, createFloorPlanFloor('upstairs', 'Upstairs')],
  }));

  assert.equal(got.profiles.home.layout.floors.length, 2);
  assert.deepEqual(got.profiles.home.knownDeviceSerials, ['known']);
});

test('scopes floor devices to current LAN and profile evidence', () => {
  const profile = createFloorPlanProfile('office', 'Office', {
    activeFloorId: 'ground',
    floors: [{
      id: 'ground',
      label: 'Ground',
      rooms: [],
      devices: { placed: { x: 0.5, y: 0.5 } },
    }],
  }, { deviceSerials: ['known-offline'], locationIds: [] });
  profile.ignoredSerials = ['ignored-online'];

  const got = devicesForFloorPlanProfile(profile, [
    device('online', true),
    device('known-offline', false),
    device('placed', false),
    device('other-profile', false),
    device('ignored-online', true),
  ]);

  assert.deepEqual(got.map((entry) => entry.serial), ['online', 'known-offline', 'placed']);
});

function device(serial, online) {
  return {
    serial,
    online,
    groupId: 'group',
    name: serial,
    model: 'LIFX A19',
    kind: 'single',
    on: false,
    brightness: 0,
    capability: { hasColor: true, minKelvin: 2500, maxKelvin: 9000 },
  };
}
