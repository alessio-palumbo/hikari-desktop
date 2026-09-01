import test from 'node:test';
import assert from 'node:assert/strict';
import { collectLocations, devicesInLocationCollection, groupsInLocationCollection, locationCollectionByKey, locationCollectionForID } from '../dist-test/domain/locationCollections.js';

test('deduplicates location labels while preserving source UUIDs', () => {
  const collections = collectLocations([
    { id: 'location-b', name: 'Office' },
    { id: 'location-a', name: 'office' },
    { id: 'location-home', name: 'Home' },
  ]);

  assert.equal(collections.length, 2);
  assert.deepEqual(collections[1], {
    key: 'location:office',
    name: 'Office',
    locationIds: ['location-a', 'location-b'],
  });
});

test('normalizes whitespace when collecting location labels', () => {
  const collections = collectLocations([
    { id: 'first', name: 'My Office' },
    { id: 'second', name: '  my   office  ' },
  ]);

  assert.equal(collections.length, 1);
  assert.equal(collections[0].key, 'location:my%20office');
  assert.deepEqual(collections[0].locationIds, ['first', 'second']);
});

test('resolves collections by presentation key and source UUID', () => {
  const collections = collectLocations([
    { id: 'first', name: 'Office' },
    { id: 'second', name: 'Office' },
  ]);

  assert.equal(locationCollectionForID(collections, 'second')?.key, 'location:office');
  assert.deepEqual(locationCollectionByKey(collections, 'location:office')?.locationIds, ['first', 'second']);
});

test('selects groups and devices across every source location UUID', () => {
  const [office] = collectLocations([
    { id: 'office-one', name: 'Office' },
    { id: 'office-two', name: 'Office' },
  ]);
  const groups = [
    { id: 'desk', locationId: 'office-one', name: 'Desk' },
    { id: 'meeting', locationId: 'office-two', name: 'Meeting' },
    { id: 'home', locationId: 'home', name: 'Home' },
  ];
  const devices = [
    { serial: 'desk-light', groupId: 'desk' },
    { serial: 'meeting-light', groupId: 'meeting' },
    { serial: 'home-light', groupId: 'home' },
  ];

  assert.deepEqual(groupsInLocationCollection(office, groups).map((group) => group.id), ['desk', 'meeting']);
  assert.deepEqual(devicesInLocationCollection(office, groups, devices).map((device) => device.serial), ['desk-light', 'meeting-light']);
});
