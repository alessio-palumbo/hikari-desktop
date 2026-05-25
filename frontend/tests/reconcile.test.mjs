import test from 'node:test';
import assert from 'node:assert/strict';
import { reconcileSnapshot } from '../dist-test/domain/reconcile.js';

const base = {
  locations: [{ id: 'home', name: 'Home' }],
  groups: [{ id: 'living', locationId: 'home', name: 'Living' }],
  devices: [
    {
      groupId: 'living',
      serial: 'd073d501a2c3',
      name: 'Strip',
      model: 'Z',
      kind: 'multizone',
      online: true,
      on: true,
      brightness: 0.5,
      zones: [{ h: 10, s: 0.8, l: 0.5 }],
    },
  ],
};

test('preserves active multizone draft edits during refresh', () => {
  const incoming = {
    ...base,
    devices: [{ ...base.devices[0], brightness: 0.9, zones: [{ h: 200, s: 0.8, l: 0.5 }] }],
  };

  const got = reconcileSnapshot(base, incoming, { draftSerials: new Set(['d073d501a2c3']) });

  assert.equal(got.devices[0].brightness, 0.5);
  assert.equal(got.devices[0].zones[0].h, 10);
  assert.equal(got.devices[0].online, true);
});

test('accepts refresh for devices without active drafts', () => {
  const incoming = {
    ...base,
    devices: [{ ...base.devices[0], brightness: 0.9, zones: [{ h: 200, s: 0.8, l: 0.5 }] }],
  };

  const got = reconcileSnapshot(base, incoming);

  assert.equal(got.devices[0].brightness, 0.9);
  assert.equal(got.devices[0].zones[0].h, 200);
});

test('keeps missing devices as offline instead of dropping them', () => {
  const got = reconcileSnapshot(base, { locations: base.locations, groups: base.groups, devices: [] });

  assert.equal(got.devices.length, 1);
  assert.equal(got.devices[0].serial, 'd073d501a2c3');
  assert.equal(got.devices[0].online, false);
});
