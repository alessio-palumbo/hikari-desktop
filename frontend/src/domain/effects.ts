import { DeviceKind, type Device } from './lifx.js';

export type DeviceEffect = 'move' | 'flame' | 'morph' | 'clouds' | 'snake' | 'worm' | 'concentric_frames' | 'waterfall' | 'rockets' | 'wave' | 'ring' | 'flow';

export type DeviceEffectSource = 'firmware' | 'app';

export interface DeviceEffectDefinition {
  id: DeviceEffect;
  label: string;
  description: string;
  source: DeviceEffectSource;
  deviceKinds: Device['kind'][];
  minFirmware?: string;
  speed: EffectSpeedDefinition;
}

export interface EffectSpeedDefinition {
  minMs: number;
  maxMs: number;
  defaultMs: number;
}

export const deviceEffects: DeviceEffectDefinition[] = [
  {
    id: 'move',
    label: 'Move',
    description: 'Animated zone sweep',
    source: 'firmware',
    deviceKinds: [DeviceKind.Multizone],
    speed: { minMs: 1000, maxMs: 60000, defaultMs: 20000 },
  },
  {
    id: 'flame',
    label: 'Flame',
    description: 'Warm flickering motion',
    source: 'firmware',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 25000, defaultMs: 3000 },
  },
  {
    id: 'morph',
    label: 'Morph',
    description: 'Smooth palette drift',
    source: 'firmware',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 25000, defaultMs: 3000 },
  },
  {
    id: 'clouds',
    label: 'Clouds',
    description: 'Soft sky movement',
    source: 'firmware',
    deviceKinds: [DeviceKind.Matrix],
    minFirmware: '4.8',
    speed: { minMs: 1000, maxMs: 100000, defaultMs: 100000 },
  },
  {
    id: 'snake',
    label: 'Snake',
    description: 'Hikari-rendered matrix trail',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 12000 },
  },
  {
    id: 'worm',
    label: 'Worm',
    description: 'Segmented matrix trail',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 12000 },
  },
  {
    id: 'concentric_frames',
    label: 'Frames',
    description: 'Concentric matrix borders',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 1000 },
  },
  {
    id: 'waterfall',
    label: 'Waterfall',
    description: 'Layered matrix cascade',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 1000 },
  },
  {
    id: 'rockets',
    label: 'Rockets',
    description: 'Fast pixel launch path',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 1000 },
  },
  {
    id: 'wave',
    label: 'Wave',
    description: 'Rolling matrix wave',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 1000 },
  },
  {
    id: 'ring',
    label: 'Ring',
    description: 'Expanding matrix ring',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 2000 },
  },
  {
    id: 'flow',
    label: 'Flow',
    description: 'Scrolling color flow',
    source: 'app',
    deviceKinds: [DeviceKind.Multizone, DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 4000 },
  },
];

export function supportedDeviceEffects(device: Device): DeviceEffectDefinition[] {
  return deviceEffects.filter((effect) => effect.deviceKinds.includes(device.kind) && firmwareSupported(device.firmware, effect.minFirmware));
}

export function supportedFirmwareEffects(device: Device): DeviceEffectDefinition[] {
  return supportedDeviceEffects(device);
}

export function defaultEffectSpeedMs(effects: DeviceEffectDefinition[]): number {
  return effects[0]?.speed.defaultMs ?? 5000;
}

export function speedToUnit(speedMs: number, speed: EffectSpeedDefinition): number {
  return clamp((speedMs - speed.minMs) / Math.max(1, speed.maxMs - speed.minMs), 0, 1);
}

export function unitToSpeedMs(value: number, speed: EffectSpeedDefinition): number {
  const stepMs = 250;
  const raw = speed.minMs + clamp(value, 0, 1) * (speed.maxMs - speed.minMs);
  return clamp(Math.round(raw / stepMs) * stepMs, speed.minMs, speed.maxMs);
}

export function formatEffectSpeed(speedMs: number): string {
  return `${(speedMs / 1000).toFixed(speedMs % 1000 === 0 ? 0 : 2)}s`;
}

function firmwareSupported(actual: string | undefined, minimum: string | undefined): boolean {
  if (!minimum) return true;
  if (!actual) return false;
  const got = parseVersion(actual);
  const want = parseVersion(minimum);
  if (!got || !want) return false;
  if (got.major !== want.major) return got.major > want.major;
  return got.minor >= want.minor;
}

function parseVersion(version: string): { major: number; minor: number } | undefined {
  const parts = version.match(/\d+/g);
  if (!parts || parts.length < 2) return undefined;
  return { major: Number(parts[0]), minor: Number(parts[1]) };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
