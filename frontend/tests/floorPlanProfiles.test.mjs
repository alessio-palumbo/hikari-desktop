import test from 'node:test';
import assert from 'node:assert/strict';
import { createFloorPlanFloor } from '../dist-test/domain/floorPlan.js';
import {
  createFloorPlanProfile,
  emptyFloorPlanProfilePreferences,
  normalizeFloorPlanProfilePreferences,
  observeFloorPlanProfile,
  resolveFloorPlanProfile,
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
