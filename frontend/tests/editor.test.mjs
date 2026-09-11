import test from 'node:test';
import assert from 'node:assert/strict';
import { createDraft, mergeDraftMetadata, updateDraft } from '../dist-test/domain/editor.js';
import { DeviceKind, sortDevicesByHierarchy } from '../dist-test/domain/lifx.js';

const device = {
  groupId: 'living',
  serial: 'd073d501a2c3',
  name: 'Strip',
  model: 'LIFX Beam',
  kind: DeviceKind.Multizone,
  online: true,
  on: true,
  brightness: 0.5,
  capability: { hasColor: true, kelvinMin: 2500, kelvinMax: 9000 },
  zones: [{ h: 20, s: 0.8, l: 0.5 }],
};

test('metadata updates preserve an active device draft and its undo state', () => {
  const edited = updateDraft(createDraft(device), {
    ...device,
    zones: [{ h: 210, s: 0.9, l: 0.5 }],
  });

  const got = mergeDraftMetadata(edited, {
    ...device,
    name: 'Desk Beam',
    groupId: 'desk',
  });

  assert.equal(got.dirty, true);
  assert.equal(got.draft.zones[0].h, 210);
  assert.equal(got.draft.name, 'Desk Beam');
  assert.equal(got.base.groupId, 'desk');
  assert.equal(got.history[0].name, 'Desk Beam');
  assert.equal(got.history[0].groupId, 'desk');
});

test('metadata for another device leaves a draft unchanged', () => {
  const draft = createDraft(device);
  const got = mergeDraftMetadata(draft, { ...device, serial: 'another', name: 'Other' });

  assert.equal(got, draft);
});

test('metadata moves are sorted immediately by location, group, and label', () => {
  const locations = [
    { id: 'home', name: 'Home' },
    { id: 'office', name: 'Office' },
  ];
  const groups = [
    { id: 'living', locationId: 'home', name: 'Living' },
    { id: 'desk', locationId: 'office', name: 'Desk' },
  ];
  const moved = { ...device, name: 'Alpha', groupId: 'desk' };
  const existing = { ...device, serial: 'd073d501a2c4', name: 'Beta', groupId: 'desk' };

  const got = sortDevicesByHierarchy([existing, moved], groups, locations);

  assert.deepEqual(got.map((entry) => entry.name), ['Alpha', 'Beta']);
});
