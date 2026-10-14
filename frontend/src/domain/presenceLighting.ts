import type { Device } from './lifx.js';

export interface DimmedDeviceState {
  previousBrightness: number;
  appliedBrightness: number;
}

export type RoomDimOwnership = Record<string, DimmedDeviceState>;

export function partitionDimOwnership(ownership: RoomDimOwnership, paused: Set<string>): { ready: RoomDimOwnership; pending: RoomDimOwnership } {
  const ready: RoomDimOwnership = {}, pending: RoomDimOwnership = {};
  for (const [serial, state] of Object.entries(ownership)) (paused.has(serial) ? pending : ready)[serial] = state;
  return { ready, pending };
}

export function preserveDimOwnership(previous: RoomDimOwnership, next: RoomDimOwnership, devices: Device[]): RoomDimOwnership {
  const result = { ...previous, ...next };
  for (const device of devices) {
    const owned = previous[device.serial];
    if (owned && next[device.serial] && device.on && near(device.brightness, owned.appliedBrightness)) result[device.serial] = { ...next[device.serial], previousBrightness: owned.previousBrightness };
  }
  return result;
}

export interface RoomDimPlan {
  devices: Device[];
  ownership: RoomDimOwnership;
}

const BRIGHTNESS_OWNERSHIP_TOLERANCE = 0.01;

export function planRoomDim(devices: Device[], brightness: number): RoomDimPlan {
  const appliedBrightness = clampBrightness(brightness);
  const ownership: RoomDimOwnership = {};
  const updates: Device[] = [];
  for (const device of devices) {
    if (!device.online || !device.on) continue;
    ownership[device.serial] = { previousBrightness: device.brightness, appliedBrightness };
    updates.push({ ...device, brightness: appliedBrightness });
  }
  return { devices: updates, ownership };
}

export function planRoomDimRestore(devices: Device[], ownership: RoomDimOwnership): Device[] {
  const bySerial = new Map(devices.map((device) => [device.serial, device]));
  const updates: Device[] = [];
  for (const [serial, owned] of Object.entries(ownership)) {
    const device = bySerial.get(serial);
    if (!device?.online || !device.on || !near(device.brightness, owned.appliedBrightness)) continue;
    updates.push({ ...device, brightness: owned.previousBrightness });
  }
  return updates;
}

function clampBrightness(brightness: number): number {
  return Math.max(0.01, Math.min(1, brightness));
}

function near(left: number, right: number): boolean {
  return Math.abs(left - right) < BRIGHTNESS_OWNERSHIP_TOLERANCE;
}
