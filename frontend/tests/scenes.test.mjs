import test from 'node:test';
import assert from 'node:assert/strict';
import { addSceneMembers, sceneCaptureScopes, sceneMembers, visibleScenes } from '../dist-test/domain/scenes.js';
import { DeviceKind } from '../dist-test/domain/lifx.js';

const light = (serial, name, online = true, kind = DeviceKind.Single) => ({ serial, name, online, kind, groupId: 'desk', on: false });
const snapshot = {
  locations: [{ id: 'home', name: 'Home' }],
  groups: [{ id: 'desk', locationId: 'home', name: 'Desk' }],
  devices: [light('b', 'Strip', true, DeviceKind.Multizone), light('a', 'Lamp'), light('off', 'Offline', false), light('switch', 'Switch', true, DeviceKind.Switch)],
};
const scene = { id: 'scene-one', name: 'Evening', savedAtMs: 1000, targets: [{ serial: 'a', label: 'Old Lamp' }, { serial: 'b', label: 'Strip' }] };

test('adding overlapping groups, rooms and individual lights deduplicates without mutating selection', () => {
  const initial = ['offline', 'a'];
  const group = ['a', 'b'];
  const selected = addSceneMembers(addSceneMembers(initial, group), ['b', 'c']);
  assert.deepEqual(selected, ['offline', 'a', 'b', 'c']);
  assert.deepEqual(addSceneMembers(selected, ['a']), selected);
  assert.deepEqual(initial, ['offline', 'a']);
  assert.deepEqual(group, ['a', 'b']);
  assert.deepEqual(addSceneMembers([], []), []);
});

test('scene capture scopes contain online lights, including powered-off lights, in stable serial order', () => {
  const scopes = sceneCaptureScopes(snapshot);
  assert.deepEqual(scopes[0].serials, ['a', 'b']);
  assert.deepEqual(scopes.find((scope) => scope.id === 'group:desk').serials, ['a', 'b']);
  assert.equal(scopes.find((scope) => scope.id === 'group:desk').name, 'Home / Desk');
  assert.equal(scopes.some((scope) => scope.id === 'device:switch' || scope.id === 'device:off'), false);
  assert.deepEqual(scopes.filter((scope) => scope.kind === 'device').map((scope) => scope.name), ['Lamp', 'Strip']);
});

test('room scope reuses placement membership but excludes unavailable and non-light devices', () => {
  const rooms = [{ id: 'room:bedroom', name: 'Ground / Bedroom', kind: 'room', serials: ['b', 'a', 'a', 'off', 'switch', 'missing'] }];
  const scopes = sceneCaptureScopes(snapshot, rooms);
  assert.deepEqual(scopes.find((scope) => scope.kind === 'room').serials, ['a', 'b']);
  assert.deepEqual(rooms[0].serials, ['b', 'a', 'a', 'off', 'switch', 'missing']);
});

test('saved membership does not expand when lights are added to a group', () => {
  const members = sceneMembers(scene, [...snapshot.devices, light('new', 'New Lamp')]);
  assert.deepEqual(members.map((member) => member.serial), ['a', 'b']);
  assert.equal(members[0].label, 'Lamp');
});

test('missing members stay visible using captured labels and matching is by serial, not label', () => {
  const members = sceneMembers(scene, [light('other', 'Old Lamp'), light('b', 'Strip', false)]);
  assert.equal(members[0].label, 'Old Lamp');
  assert.equal(members[0].available, false);
  assert.equal(members[1].available, false);
});

test('info keeps saved colors and brightness even when live device state differs', () => {
  const saved = { ...scene, targets: [{ serial: 'a', label: 'Lamp', on: false, brightness: 0.4, colors: [{ h: 0, s: 0, l: 1, kelvin: 2000 }] }] };
  const [member] = sceneMembers(saved, [{ ...light('a', 'Renamed'), on: true, brightness: 1, color: { h: 240, s: 1, l: 1 } }]);
  assert.equal(member.label, 'Renamed');
  assert.equal(member.on, false);
  assert.equal(member.brightness, 0.4);
  assert.deepEqual(member.colors, saved.targets[0].colors);
});

test('scene search supports names, current member labels, and serials without changing saved order', () => {
  const scenes = [{ ...scene, id: 'scene-two', name: 'Morning' }, scene];
  assert.deepEqual(visibleScenes(scenes, snapshot.devices, ' evening ').map((entry) => entry.id), ['scene-one']);
  assert.equal(visibleScenes(scenes, snapshot.devices, 'Lamp').length, 2);
  assert.equal(visibleScenes(scenes, snapshot.devices, 'no matches').length, 0);
  assert.deepEqual(visibleScenes(scenes, snapshot.devices, '').map((entry) => entry.name), ['Evening', 'Morning']);
  assert.equal(scenes[0].name, 'Morning');
});
