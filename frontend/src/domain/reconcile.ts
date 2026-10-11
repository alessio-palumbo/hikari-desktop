import { DeviceKind, type Device, type DeviceSnapshot, type LightPart } from './lifx.js';

export const PENDING_STATE_TIMEOUT_MS = 4500;

export interface PendingDeviceState {
  serial: string;
  endpointRevision?: number;
  partBrightness?: Partial<Record<LightPart, number>>;
  partPower?: Partial<Record<LightPart, boolean>>;
  expected: Pick<Partial<Device>, 'on' | 'brightness' | 'color' | 'kelvin' | 'zones' | 'chain' | 'relays' | 'buttonConfig'>;
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
    if (currentDevice && (currentDevice.endpointRevision ?? 0) !== (device.endpointRevision ?? 0)) return device;
    if (currentDevice && draftSerials.has(device.serial) && currentDevice.kind !== DeviceKind.Single) {
      return applyPendingState({ ...currentDevice, online: device.online, ipAddress: device.ipAddress, lightPartStates: device.lightPartStates }, pending[device.serial], now);
    }
    return applyPendingState(device, pending[device.serial], now);
  });

  for (const device of current.devices) {
    if (!incomingBySerial.has(device.serial)) {
      devices.push({ ...device, online: false });
    }
  }

  const referencedGroupIds = new Set(devices.map((device) => device.groupId));
  const groups = mergeReferenced(
    incoming.groups,
    current.groups,
    referencedGroupIds,
    (group) => group.id,
  );
  const referencedLocationIds = new Set(groups.map((group) => group.locationId));
  const locations = mergeReferenced(
    incoming.locations,
    current.locations,
    referencedLocationIds,
    (location) => location.id,
  );

  return {
    revision: incoming.revision ?? current.revision,
    locations,
    groups,
    devices,
  };
}

export function isSnapshotStale(current: DeviceSnapshot, incoming: DeviceSnapshot): boolean {
  return current.revision !== undefined
    && incoming.revision !== undefined
    && incoming.revision < current.revision;
}

function mergeReferenced<T>(incoming: T[], current: T[], referencedIds: Set<string>, id: (value: T) => string): T[] {
  const merged = incoming.filter((value) => referencedIds.has(id(value)));
  const included = new Set(merged.map(id));
  for (const value of current) {
    const valueId = id(value);
    if (referencedIds.has(valueId) && !included.has(valueId)) {
      merged.push(value);
      included.add(valueId);
    }
  }
  return merged;
}

export function createPendingState(device: Device, previous?: Device, now = Date.now()): PendingDeviceState | undefined {
  const expected: PendingDeviceState['expected'] = {};
  if (!previous || previous.on !== device.on) expected.on = device.on;
  if (!previous || !near(previous.brightness, device.brightness)) expected.brightness = device.brightness;
  if (!previous || !sameValue(previous.color, device.color)) expected.color = clone(device.color);
  if (!previous || previous.kelvin !== device.kelvin) expected.kelvin = device.kelvin;
  if (device.kind === DeviceKind.Multizone && (!previous || !sameValue(previous.zones, device.zones))) expected.zones = clone(device.zones);
  if (device.kind === DeviceKind.Matrix && (!previous || !sameValue(previous.chain, device.chain))) expected.chain = clone(device.chain);
  if (device.kind === DeviceKind.Switch && (!previous || !sameValue(previous.relays, device.relays))) expected.relays = clone(device.relays);
  if (device.kind === DeviceKind.Switch && (!previous || !sameValue(previous.buttonConfig, device.buttonConfig))) expected.buttonConfig = clone(device.buttonConfig);
  if (!hasExpectedState(expected)) return undefined;
  return { serial: device.serial, endpointRevision: device.endpointRevision, expected, expiresAt: now + PENDING_STATE_TIMEOUT_MS };
}

export function isPendingConfirmed(device: Device, pending: PendingDeviceState): boolean {
  if ((device.endpointRevision ?? 0) !== (pending.endpointRevision ?? 0)) return true;
  for (const [part, brightness] of Object.entries(pending.partBrightness ?? {})) {
    const observed = device.lightPartStates?.[part as LightPart];
    if (!observed?.known || !near(observed.brightness, brightness)) return false;
  }
  for (const [part, on] of Object.entries(pending.partPower ?? {})) {
    const observed = device.lightPartStates?.[part as LightPart];
    if (!observed?.known || observed.on !== on) return false;
  }
  if (pending.expected.on !== undefined && device.on !== pending.expected.on) return false;
  if (pending.expected.brightness !== undefined && !near(device.brightness, pending.expected.brightness)) return false;
  if (pending.expected.color !== undefined && !sameValue(device.color, pending.expected.color)) return false;
  if (pending.expected.kelvin !== undefined && device.kelvin !== pending.expected.kelvin) return false;
  if (pending.expected.zones !== undefined && !sameValue(device.zones, pending.expected.zones)) return false;
  if (pending.expected.chain !== undefined && !sameValue(device.chain, pending.expected.chain)) return false;
  if (pending.expected.relays !== undefined && !sameValue(device.relays, pending.expected.relays)) return false;
  if (pending.expected.buttonConfig !== undefined && !sameValue(device.buttonConfig, pending.expected.buttonConfig)) return false;
  return true;
}

export function isPendingExpired(pending: PendingDeviceState, now = Date.now()): boolean {
  return now >= pending.expiresAt;
}

export function applyPendingState(device: Device, pending: PendingDeviceState | undefined, now = Date.now()): Device {
  if (!pending || isPendingExpired(pending, now) || isPendingConfirmed(device, pending)) return device;
  const next = { ...device, ...pending.expected };
  if ((pending.partBrightness || pending.partPower) && device.lightPartStates) {
    next.lightPartStates = { ...device.lightPartStates };
    for (const [part, brightness] of Object.entries(pending.partBrightness ?? {})) {
      const key = part as LightPart;
      const observed = device.lightPartStates[key];
      if (observed?.known) next.lightPartStates[key] = { ...observed, brightness };
    }
    for (const [part, on] of Object.entries(pending.partPower ?? {})) {
      const key = part as LightPart;
      const current = next.lightPartStates[key];
      if (current?.known) next.lightPartStates[key] = { ...current, on };
    }
  }
  return next;
}

// Show requested slider/dot values immediately, without predicting shared
// device power or the library's inherited brightness after activation.
export function createPendingPartBrightness(device: Device, part: LightPart, brightness: number, previous?: PendingDeviceState, now = Date.now()): PendingDeviceState {
  const reusable = previous && !isPendingExpired(previous, now) && (previous.endpointRevision ?? 0) === (device.endpointRevision ?? 0);
  const values = { ...(reusable ? previous.partBrightness : {}) };
  if (part === 'all') {
    values.all = brightness;
    for (const info of device.lightParts ?? []) values[info.part] = brightness;
  } else {
    values[part] = brightness;
    // Activation can change dormant state in the other part. Let observations
    // provide aggregate brightness rather than predicting library sequencing.
    delete values.all;
  }
  const pending = createPendingPartPower(device, part, brightness > 0, previous, now);
  const expected = { ...pending.expected };
  if (values.all === undefined) delete expected.brightness;
  else expected.brightness = values.all;
  return { ...pending, partBrightness: values, expected };
}

export function createPendingPartPower(device: Device, part: LightPart, on: boolean, previous?: PendingDeviceState, now = Date.now()): PendingDeviceState {
  const reusable = previous && !isPendingExpired(previous, now) && (previous.endpointRevision ?? 0) === (device.endpointRevision ?? 0);
  const values = { ...(reusable ? previous.partPower : {}) };
  if (part === 'all') {
    values.all = on;
    for (const info of device.lightParts ?? []) values[info.part] = on;
  } else {
    values[part] = on;
    const other = part === 'main' ? 'uplight' : 'main';
    const otherState = device.lightPartStates?.[other];
    if (on) values.all = true;
    else if (otherState?.known) values.all = values[other] ?? otherState.on;
    else delete values.all;
  }
  const expected = { ...(reusable ? previous.expected : {}) };
  // Explicit power can inherit or clear stored brightness. Do not carry an
  // earlier slider expectation across that operation.
  delete expected.brightness;
  if (values.all === undefined) delete expected.on;
  else expected.on = values.all;
  return {
    serial: device.serial,
    endpointRevision: device.endpointRevision,
    partPower: values,
    expected,
    expiresAt: now + PENDING_STATE_TIMEOUT_MS,
  };
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
