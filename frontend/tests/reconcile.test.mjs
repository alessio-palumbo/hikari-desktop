import test from 'node:test';
import assert from 'node:assert/strict';
import { createPendingState, isPendingConfirmed, reconcileSnapshot } from '../dist-test/domain/reconcile.js';

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

test('overlays pending power and brightness over stale snapshot values', () => {
  const previous = { ...base.devices[0], on: true, brightness: 0.8 };
  const optimistic = { ...previous, on: false, brightness: 0.2 };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...previous }] };

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 2000 });

  assert.equal(got.devices[0].on, false);
  assert.equal(got.devices[0].brightness, 0.2);
});

test('stops overlaying pending state after snapshot confirms it', () => {
  const previous = { ...base.devices[0], on: true, brightness: 0.8 };
  const optimistic = { ...previous, on: false, brightness: 0.2 };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...optimistic }] };

  assert.equal(isPendingConfirmed(incoming.devices[0], pending), true);

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 2000 });

  assert.equal(got.devices[0].on, false);
  assert.equal(got.devices[0].brightness, 0.2);
});

test('lets expired pending state fall back to snapshot values', () => {
  const previous = { ...base.devices[0], on: true, brightness: 0.8 };
  const optimistic = { ...previous, on: false, brightness: 0.2 };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...previous }] };

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 6000 });

  assert.equal(got.devices[0].on, true);
  assert.equal(got.devices[0].brightness, 0.8);
});
