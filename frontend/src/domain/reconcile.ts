import type { Device, DeviceSnapshot } from './lifx';

export const PENDING_STATE_TIMEOUT_MS = 4500;

export interface PendingDeviceState {
  serial: string;
  expected: Pick<Partial<Device>, 'on' | 'brightness' | 'color' | 'kelvin' | 'zones' | 'chain'>;
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
  if (!previous || !sameValue(previous.color, device.color)) expected.color = clone(device.color);
  if (!previous || previous.kelvin !== device.kelvin) expected.kelvin = device.kelvin;
  if (device.kind === 'multizone' && (!previous || !sameValue(previous.zones, device.zones))) expected.zones = clone(device.zones);
  if (device.kind === 'matrix' && (!previous || !sameValue(previous.chain, device.chain))) expected.chain = clone(device.chain);
  if (!hasExpectedState(expected)) return undefined;
  return { serial: device.serial, expected, expiresAt: now + PENDING_STATE_TIMEOUT_MS };
}

export function isPendingConfirmed(device: Device, pending: PendingDeviceState): boolean {
  if (pending.expected.on !== undefined && device.on !== pending.expected.on) return false;
  if (pending.expected.brightness !== undefined && !near(device.brightness, pending.expected.brightness)) return false;
  if (pending.expected.color !== undefined && !sameValue(device.color, pending.expected.color)) return false;
  if (pending.expected.kelvin !== undefined && device.kelvin !== pending.expected.kelvin) return false;
  if (pending.expected.zones !== undefined && !sameValue(device.zones, pending.expected.zones)) return false;
  if (pending.expected.chain !== undefined && !sameValue(device.chain, pending.expected.chain)) return false;
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

function sameValue(a: unknown, b: unknown) {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
}

function clone<T>(value: T): T {
  if (value === undefined) return value;
  return JSON.parse(JSON.stringify(value)) as T;
}

function hasExpectedState(expected: PendingDeviceState['expected']) {
  return Object.values(expected).some((value) => value !== undefined);
}
