import type { Device, DeviceSnapshot } from './lifx';

export const PENDING_STATE_TIMEOUT_MS = 4500;

export interface PendingDeviceState {
  serial: string;
  expected: Pick<Partial<Device>, 'on' | 'brightness'>;
  expiresAt: number;
}

export interface ReconcileOptions {
  draftSerials?: Set<string>;
  pending?: Record<string, PendingDeviceState>;
  now?: number;
}

export function reconcileSnapshot(current: DeviceSnapshot, incoming: DeviceSnapshot, options: ReconcileOptions = {}): DeviceSnapshot {
  const draftSerials = options.draftSerials ?? new Set<string>();
  const pending = options.pending ?? {};
  const now = options.now ?? Date.now();
  const incomingBySerial = new Map(incoming.devices.map((device) => [device.serial, device]));
  const currentBySerial = new Map(current.devices.map((device) => [device.serial, device]));
  const devices = incoming.devices.map((device) => {
    const currentDevice = currentBySerial.get(device.serial);
    if (currentDevice && draftSerials.has(device.serial) && currentDevice.kind !== 'single') {
      return applyPendingState({ ...currentDevice, online: device.online }, pending[device.serial], now);
    }
    return applyPendingState(device, pending[device.serial], now);
  });

  for (const device of current.devices) {
    if (!incomingBySerial.has(device.serial)) {
      devices.push({ ...device, online: false });
    }
  }

  return {
    locations: incoming.locations.length ? incoming.locations : current.locations,
    groups: incoming.groups.length ? incoming.groups : current.groups,
    devices,
  };
}

export function createPendingState(device: Device, previous?: Device, now = Date.now()): PendingDeviceState | undefined {
  const expected: PendingDeviceState['expected'] = {};
  if (!previous || previous.on !== device.on) expected.on = device.on;
  if (!previous || !near(previous.brightness, device.brightness)) expected.brightness = device.brightness;
  if (expected.on === undefined && expected.brightness === undefined) return undefined;
  return { serial: device.serial, expected, expiresAt: now + PENDING_STATE_TIMEOUT_MS };
}

export function isPendingConfirmed(device: Device, pending: PendingDeviceState): boolean {
  if (pending.expected.on !== undefined && device.on !== pending.expected.on) return false;
  if (pending.expected.brightness !== undefined && !near(device.brightness, pending.expected.brightness)) return false;
  return true;
}

export function isPendingExpired(pending: PendingDeviceState, now = Date.now()): boolean {
  return now >= pending.expiresAt;
}

function applyPendingState(device: Device, pending: PendingDeviceState | undefined, now: number): Device {
  if (!pending || isPendingExpired(pending, now) || isPendingConfirmed(device, pending)) return device;
  return { ...device, ...pending.expected };
}

function near(a: number, b: number) {
  return Math.abs(a - b) < 0.01;
}
