import type { Device } from './lifx.js';

export interface TextCommandAction {
  power?: boolean;
  hue?: number;
  saturation?: number;
  brightness?: number;
  kelvin?: number;
  durationMs?: number;
}

export interface TextCommandTarget {
  serial: string;
}

export interface TextCommand {
  targets: TextCommandTarget[];
  action: TextCommandAction;
}

export function executableTextCommandTargets(commands: TextCommand[], devices: Device[]): Device[] {
  const bySerial = new Map(devices.map((device) => [device.serial, device]));
  const targets: Device[] = [];
  for (const command of commands) {
    for (const target of command.targets) {
      const device = bySerial.get(target.serial);
      if (!device) throw new Error('Device ' + target.serial + ' is no longer available');
      if (device.kind === 'switch') throw new Error(device.name + ' is not a supported light target');
      targets.push(device);
    }
  }
  return targets;
}

export function applyTextCommandAction(device: Device, action: TextCommandAction): Device {
  let next: Device = { ...device };
  const color = { ...(next.color ?? { h: 38, s: 0, l: next.brightness || 0.55 }) };
  if (typeof action.power === 'boolean') next = { ...next, on: action.power };
  if (typeof action.brightness === 'number') {
    const brightness = clamp(action.brightness / 100, 0, 1);
    next = { ...next, brightness };
    color.l = brightness || color.l;
  }
  if (typeof action.kelvin === 'number') {
    color.h = 38;
    color.s = 0;
    color.kelvin = action.kelvin;
    next = { ...next, kelvin: action.kelvin, color, on: true };
  }
  if (typeof action.hue === 'number' || typeof action.saturation === 'number') {
    color.h = typeof action.hue === 'number' ? action.hue : color.h;
    color.s = typeof action.saturation === 'number' ? action.saturation / 100 : color.s;
    delete color.kelvin;
    next = { ...next, color, on: true };
  }
  if (typeof action.brightness === 'number' && action.brightness > 0) next = { ...next, on: true, color };
  return next;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
