import { DeviceKind, type Device } from './lifx.js';

export type DeviceEffect = 'move' | 'flame' | 'morph' | 'clouds';

export interface FirmwareEffectDefinition {
  id: DeviceEffect;
  label: string;
  description: string;
  deviceKinds: Device['kind'][];
  minFirmware?: string;
}

export const firmwareEffects: FirmwareEffectDefinition[] = [
  {
    id: 'move',
    label: 'Move',
    description: 'Animated zone sweep',
    deviceKinds: [DeviceKind.Multizone],
  },
  {
    id: 'flame',
    label: 'Flame',
    description: 'Warm flickering motion',
    deviceKinds: [DeviceKind.Matrix],
  },
  {
    id: 'morph',
    label: 'Morph',
    description: 'Smooth palette drift',
    deviceKinds: [DeviceKind.Matrix],
  },
  {
    id: 'clouds',
    label: 'Clouds',
    description: 'Soft sky movement',
    deviceKinds: [DeviceKind.Matrix],
    minFirmware: '4.8',
  },
];

export function supportedFirmwareEffects(device: Device): FirmwareEffectDefinition[] {
  return firmwareEffects.filter((effect) => effect.deviceKinds.includes(device.kind) && firmwareSupported(device.firmware, effect.minFirmware));
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
