import test from 'node:test';
import assert from 'node:assert/strict';
import { reconnectionStatus, RECONNECTION_GRACE_MS } from '../dist-test/domain/reconnection.js';
import { isRecoverableNetworkError, networkErrorMessage } from '../dist-test/domain/networkErrors.js';

test('temporary network loss shows reconnection instead of an immediate interface error', () => {
  assert.deepEqual(reconnectionStatus('No network interfaces found.', 1000, 1100), {
    message: 'reconnecting LAN devices', showActions: false,
  });
});

test('persistent failure exposes its specific error and controls after the grace period', () => {
  const error = 'Selected network interface unavailable. Choose another interface or Automatic.';
  assert.equal(reconnectionStatus(error, 1000, 1000 + RECONNECTION_GRACE_MS - 1).showActions, false);
  assert.deepEqual(reconnectionStatus(error, 1000, 1000 + RECONNECTION_GRACE_MS), { message: error, showActions: true });
});

test('repeated failures use the original outage deadline even when their message changes', () => {
  const startedAt = 1000;
  assert.equal(reconnectionStatus('No network interfaces found.', startedAt, 2000).showActions, false);
  assert.equal(reconnectionStatus('Connection lost.', startedAt, startedAt + RECONNECTION_GRACE_MS + 500).showActions, true);
});

test('recovery clears the presentation immediately and a new outage has a new grace period', () => {
  assert.equal(reconnectionStatus(undefined, undefined, 1500), undefined);
  assert.equal(reconnectionStatus(undefined, 1000, 1500), undefined);
  assert.equal(reconnectionStatus('No network interfaces found.', 5000, 5100).showActions, false);
});

test('errors unrelated to network recovery are not delayed', () => {
  const error = 'Unable to load device state.';
  assert.deepEqual(reconnectionStatus(error, undefined, 1100), { message: error, showActions: true });
});

test('lifxlan-go named-interface errors enter recovery before their message is displayed', () => {
  for (const error of [
    'broadcast interface "en0" not found',
    new Error('refresh discovery after network change: broadcast interface "en0" not found'),
    'broadcast interface index 4 not found',
    'configured broadcast interface not found',
  ]) {
    assert.equal(isRecoverableNetworkError(error), true);
    const startedAt = isRecoverableNetworkError(error) ? 1000 : undefined;
    const message = networkErrorMessage(error);
    assert.notEqual(message, 'No network interfaces found.');
    assert.deepEqual(reconnectionStatus(message, startedAt, 1100), { message: 'reconnecting LAN devices', showActions: false });
    assert.deepEqual(reconnectionStatus(message, startedAt, 1000 + RECONNECTION_GRACE_MS), { message, showActions: true });
  }
});

test('automatic-interface exhaustion and configured-interface loss are recoverable', () => {
  for (const error of [
    'no suitable broadcast interface found',
    'create lifx controller: failed to create client: no suitable broadcast interface found',
    'network interface "en0" is not available',
    'No network interfaces found.',
  ]) assert.equal(isRecoverableNetworkError(error), true);
  assert.equal(networkErrorMessage('no suitable broadcast interface found'), 'No network interfaces found.');
  assert.equal(isRecoverableNetworkError('Unable to load device state.'), false);
});
