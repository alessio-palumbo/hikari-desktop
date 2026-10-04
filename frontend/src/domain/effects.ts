import { DeviceKind, type Device } from './lifx.js';

export type DeviceEffect = 'move' | 'flame' | 'morph' | 'clouds' | 'snake' | 'worm' | 'concentric_frames' | 'waterfall' | 'rockets' | 'wave' | 'ring' | 'flow' | 'comet' | 'sparkle' | 'scanner';

export type DeviceEffectSource = 'firmware' | 'app';
export type EffectParameters = Record<string, number | string>;

export interface EffectPreference {
  speedMs: number;
  params?: EffectParameters;
}

export type DeviceEffectPreferences = Partial<Record<DeviceEffect, EffectPreference>>;

export function normalizeEffectPreference(preference: EffectPreference, effect: DeviceEffectDefinition): EffectPreference {
  return {
    speedMs: Number.isFinite(preference.speedMs) ? clamp(preference.speedMs, effect.speed.minMs, effect.speed.maxMs) : effect.speed.defaultMs,
    params: effect.configurable ? preference.params : undefined,
  };
}

export interface EffectPanels {
  preview?: DeviceEffect;
  settings?: DeviceEffect;
}

export function toggleEffectPanel(current: EffectPanels, panel: keyof EffectPanels, effect: DeviceEffect): EffectPanels {
  const other = panel === 'preview' ? 'settings' : 'preview';
  return {
    [panel]: current[panel] === effect ? undefined : effect,
    [other]: current[other] === effect ? effect : undefined,
  };
}

export interface DeviceEffectDefinition {
  id: DeviceEffect;
  label: string;
  description: string;
  source: DeviceEffectSource;
  configurable?: boolean;
  deviceKinds: Device['kind'][];
  minFirmware?: string;
  speed: EffectSpeedDefinition;
  speedByKind?: Partial<Record<Device['kind'], EffectSpeedDefinition>>;
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
    configurable: true,
    label: 'Snake',
    description: 'Hikari-rendered matrix trail',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 12000 },
  },
  {
    id: 'worm',
    configurable: true,
    label: 'Worm',
    description: 'Segmented matrix trail',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 12000 },
  },
  {
    id: 'concentric_frames',
    configurable: true,
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
    configurable: true,
    label: 'Wave',
    description: 'Rolling matrix wave',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 1000 },
  },
  {
    id: 'ring',
    configurable: true,
    label: 'Ring',
    description: 'Expanding matrix ring',
    source: 'app',
    deviceKinds: [DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 2000 },
  },
  {
    id: 'flow',
    configurable: true,
    label: 'Flow',
    description: 'Scrolling color flow',
    source: 'app',
    deviceKinds: [DeviceKind.Multizone, DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 4000 },
  },
  {
    id: 'comet',
    configurable: true,
    label: 'Comet',
    description: 'Bright trailing sweep',
    source: 'app',
    deviceKinds: [DeviceKind.Multizone],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 4000 },
  },
  {
    id: 'sparkle',
    configurable: true,
    label: 'Sparkle',
    description: 'Scattered fading highlights',
    source: 'app',
    deviceKinds: [DeviceKind.Multizone, DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 2000 },
  },
  {
    id: 'scanner',
    configurable: true,
    label: 'Scanner',
    description: 'Soft side-to-side band',
    source: 'app',
    deviceKinds: [DeviceKind.Multizone, DeviceKind.Matrix],
    speed: { minMs: 1000, maxMs: 30000, defaultMs: 2000 },
    speedByKind: {
      [DeviceKind.Multizone]: { minMs: 1000, maxMs: 30000, defaultMs: 4000 },
    },
  },
];

export function supportedDeviceEffects(device: Device): DeviceEffectDefinition[] {
  return deviceEffects
    .filter((effect) => effect.deviceKinds.includes(device.kind) && firmwareSupported(device.firmware, effect.minFirmware))
    .map((effect) => ({ ...effect, speed: effect.speedByKind?.[device.kind] ?? effect.speed }));
}

export function supportedFirmwareEffects(device: Device): DeviceEffectDefinition[] {
  return supportedDeviceEffects(device);
}

export function deviceEffectSource(effect: string): DeviceEffectSource | undefined {
  return deviceEffects.find((definition) => definition.id === effect)?.source;
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

export function effectSettingsDiffer(speedMs: number, referenceSpeedMs: number, values: EffectParameters, reference: EffectParameters): boolean {
  return speedMs !== referenceSpeedMs || [...new Set([...Object.keys(values), ...Object.keys(reference)])].some((key) => {
    const value = values[key];
    const baseline = reference[key];
    return typeof value === 'number' && typeof baseline === 'number' ? Math.abs(value - baseline) > 0.000001 : value !== baseline;
  });
}

export function quantizeEffectParameter(value: number, range: { min: number; max: number; step: number }): number {
  return clamp(Number((Math.round(value / range.step) * range.step).toFixed(6)), range.min, range.max);
}

export function formatEffectParameter(value: number | string, unit?: string): string {
  if (typeof value === 'string') return value;
  if (unit === '%') return `${Math.round(value * 100)}%`;
  const number = Number(value.toFixed(2));
  return unit ? `${number} ${unit}` : String(number);
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
