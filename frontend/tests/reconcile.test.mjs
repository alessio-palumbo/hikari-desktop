import test from 'node:test';
import assert from 'node:assert/strict';
import { commandIntent, draftIntent, prepareDeviceCommand } from '../dist-test/domain/commands.js';
import { activateEditedDevice } from '../dist-test/domain/editor.js';
import { previewLightness, previewOpacity } from '../dist-test/domain/lifx.js';
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

test('overlays pending multizone zones over stale snapshot values', () => {
  const previous = { ...base.devices[0], zones: [{ h: 10, s: 0.8, l: 0.5 }] };
  const optimistic = { ...previous, zones: [{ h: 240, s: 0.9, l: 0.7 }] };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...previous }] };

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 2000 });

  assert.equal(got.devices[0].zones[0].h, 240);
  assert.equal(got.devices[0].zones[0].l, 0.7);
});

test('overlays pending matrix pixels over stale snapshot values', () => {
  const previous = {
    ...base.devices[0],
    kind: 'matrix',
    zones: undefined,
    chain: [{ id: 0, x: 0, y: 0, w: 2, h: 1, rows: [{ cols: 2, offset: 0 }], pixels: [{ h: 10, s: 0.8, l: 0.5 }, { h: 20, s: 0.8, l: 0.5 }] }],
  };
  const optimistic = {
    ...previous,
    chain: [{ ...previous.chain[0], pixels: [{ h: 120, s: 0.9, l: 0.6 }, { h: 20, s: 0.8, l: 0.5 }] }],
  };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...previous }] };

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 2000 });

  assert.equal(got.devices[0].chain[0].pixels[0].h, 120);
  assert.equal(got.devices[0].chain[0].pixels[1].h, 20);
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

test('stops overlaying pending zones after snapshot confirms them', () => {
  const previous = { ...base.devices[0], zones: [{ h: 10, s: 0.8, l: 0.5 }] };
  const optimistic = { ...previous, zones: [{ h: 240, s: 0.9, l: 0.7 }] };
  const pending = createPendingState(optimistic, previous, 1000);
  const incoming = { ...base, devices: [{ ...optimistic }] };

  assert.equal(isPendingConfirmed(incoming.devices[0], pending), true);

  const got = reconcileSnapshot({ ...base, devices: [optimistic] }, incoming, { pending: { [optimistic.serial]: pending }, now: 2000 });

  assert.equal(got.devices[0].zones[0].h, 240);
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

test('preview lightness responds to brightness even when pixel lightness is zero', () => {
  const color = { h: 210, s: 0.8, l: 0 };

  assert.ok(previewLightness(color, 0.8, true) > previewLightness(color, 0.1, true));
});

test('preview lightness keeps maximum kelvin whites below washout range', () => {
  const color = { h: 0, s: 0, l: 0, kelvin: 6500 };

  assert.ok(previewLightness(color, 1, true) < 0.8);
});

test('preview opacity only dims off devices', () => {
  assert.equal(previewOpacity(true), 1);
  assert.equal(previewOpacity(false), 0.3);
});

test('activates edited multizone devices before apply', () => {
  const device = {
    ...base.devices[0],
    on: false,
    brightness: 0,
    zones: [
      { h: 10, s: 0.8, l: 0.4 },
      { h: 20, s: 0.8, l: 0.8 },
    ],
  };

  const got = activateEditedDevice(device);

  assert.equal(got.on, true);
  assert.ok(Math.abs(got.brightness - 0.6) < 0.001);
});

test('activates edited matrix devices before apply', () => {
  const device = {
    ...base.devices[0],
    kind: 'matrix',
    on: false,
    brightness: 0,
    zones: undefined,
    chain: [{ id: 0, x: 0, y: 0, w: 2, h: 1, rows: [{ cols: 2, offset: 0 }], pixels: [{ h: 10, s: 0.8, l: 0.3 }, { h: 20, s: 0.8, l: 0.7 }] }],
  };

  const got = activateEditedDevice(device);

  assert.equal(got.on, true);
  assert.equal(got.brightness, 0.5);
});

test('classifies direct power changes as power intent', () => {
  const previous = { ...base.devices[0], on: true, brightness: 0.5 };
  const next = { ...previous, on: false };

  assert.equal(commandIntent(next, previous), 'power');
});

test('classifies direct brightness changes as brightness intent', () => {
  const previous = { ...base.devices[0], brightness: 0.5, zones: [{ h: 10, s: 0.8, l: 0.5 }] };
  const next = { ...previous, brightness: 0.25 };

  assert.equal(commandIntent(next, previous), 'brightness');
});

test('keeps off-device color selection as color intent', () => {
  const previous = { ...base.devices[0], on: false, brightness: 0, color: { h: 10, s: 0.8, l: 0.5 } };
  const next = { ...previous, on: true, brightness: 0.55, color: { h: 240, s: 0.9, l: 0.55 } };

  assert.equal(commandIntent(next, previous), 'color');
});

test('prepares multizone brightness command without changing intent payload source', () => {
  const previous = { ...base.devices[0], brightness: 0.5, zones: [{ h: 10, s: 0.8, l: 0.5 }] };
  const next = { ...previous, brightness: 0.25 };

  const got = prepareDeviceCommand(next, previous);

  assert.equal(got.brightness, 0.25);
  assert.equal(got.zones[0].h, 10);
  assert.equal(got.zones[0].l, 0.25);
});

test('maps draft devices to zone and matrix intents', () => {
  assert.equal(draftIntent(base.devices[0]), 'zones');
  assert.equal(draftIntent({ ...base.devices[0], kind: 'matrix', zones: undefined, chain: [] }), 'matrix');
  assert.equal(draftIntent({ ...base.devices[0], kind: 'single', zones: undefined, color: { h: 10, s: 0.8, l: 0.5 } }), 'color');
});
